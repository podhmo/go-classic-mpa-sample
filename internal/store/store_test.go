package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestFileDBPragmas verifies pragmas reach every pooled connection on a real
// file database (foreign_keys is per-connection in SQLite).
func TestFileDBPragmas(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()

	for i := range 10 {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn: %v", err)
		}
		var on int
		if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&on); err != nil {
			conn.Close()
			t.Fatalf("PRAGMA foreign_keys: %v", err)
		}
		conn.Close()
		if on != 1 {
			t.Fatalf("conn %d: foreign_keys = %d, want 1", i, on)
		}
	}
	var bt int
	if err := s.db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&bt); err != nil {
		t.Fatalf("PRAGMA busy_timeout: %v", err)
	}
	if bt != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", bt)
	}
}

func TestSchemaObjects(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tables, indexes, err := s.SchemaObjects(ctx)
	if err != nil {
		t.Fatalf("SchemaObjects: %v", err)
	}

	// All application tables plus sqlite_sequence (AUTOINCREMENT
	// bookkeeping), sorted by name.
	wantTables := []string{"api_tokens", "comments", "discussion_categories", "discussion_comments", "discussion_threads", "discussions", "issue_labels", "issues", "labels", "sessions", "sqlite_sequence", "users", "workspaces"}
	if !slices.Equal(tables, wantTables) {
		t.Errorf("tables = %v, want %v", tables, wantTables)
	}

	// Only the named indexes from the schema; the UNIQUE/PK backing
	// indexes (sqlite_autoindex%) must be filtered out.
	wantIndexes := []string{"idx_api_tokens_user", "idx_comments_issue_created", "idx_dcomments_thread_created", "idx_discussions_workspace_category_created", "idx_discussions_workspace_created", "idx_dthreads_answer", "idx_dthreads_discussion_created", "idx_issue_labels_label", "idx_issues_workspace_status_created", "idx_sessions_user"}
	if !slices.Equal(indexes, wantIndexes) {
		t.Errorf("indexes = %v, want %v", indexes, wantIndexes)
	}
	for _, name := range indexes {
		if strings.HasPrefix(name, "sqlite_autoindex") {
			t.Errorf("internal index %q leaked into SchemaObjects", name)
		}
	}
}

func TestAnalyze(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.Analyze(ctx); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := s.Analyze(ctx); err != nil {
		t.Fatalf("second Analyze: %v", err)
	}

	// ANALYZE may create sqlite_stat% bookkeeping tables; SchemaObjects
	// filters them, so the reported schema stays stable across runs.
	tables, _, err := s.SchemaObjects(ctx)
	if err != nil {
		t.Fatalf("SchemaObjects after ANALYZE: %v", err)
	}
	wantTables := []string{"api_tokens", "comments", "discussion_categories", "discussion_comments", "discussion_threads", "discussions", "issue_labels", "issues", "labels", "sessions", "sqlite_sequence", "users", "workspaces"}
	if !slices.Equal(tables, wantTables) {
		t.Errorf("tables after ANALYZE = %v, want %v", tables, wantTables)
	}
}

func TestMigrationFreshAndLegacyDB(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "mig.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open fresh: %v", err)
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("fresh DB user_version = %d, want %d", version, schemaVersion)
	}
	for _, q := range []string{
		"SELECT author_id FROM issues LIMIT 0",
		"SELECT author_id FROM comments LIMIT 0",
		"SELECT id FROM users LIMIT 0",
		"SELECT id, expires_at FROM sessions LIMIT 0",
		"SELECT id, user_id, created_at FROM api_tokens LIMIT 0",
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Errorf("schema check %q: %v", q, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopening is a no-op, not a re-run of the ALTERs (which would fail).
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen migrated DB: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestMigrationFromVersionZero(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	// Simulate a database created before migrations existed: the v1 schema
	// applied, user_version still 0. Raw driver access, not Open.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.ExecContext(ctx, schemaDDL); err != nil {
		t.Fatalf("apply legacy schema: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO issues(title, body, status, created_at, updated_at) VALUES('keep me', '', 'open', 1, 1)"); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy DB: %v", err)
	}
	defer s.Close()
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("migrated DB user_version = %d, want %d", version, schemaVersion)
	}
	is, err := s.GetIssue(ctx, 1)
	if err != nil {
		t.Fatalf("GetIssue after migration: %v", err)
	}
	if is.Title != "keep me" {
		t.Errorf("existing row lost: title = %q", is.Title)
	}
	var authorID any
	if err := s.db.QueryRowContext(ctx, "SELECT author_id FROM issues WHERE id = 1").Scan(&authorID); err != nil {
		t.Fatalf("author_id column: %v", err)
	}
	if authorID != nil {
		t.Errorf("pre-migration row should keep author_id NULL, got %v", authorID)
	}
}

// TestMigrationV5 carries pre-v5 discussion rows through the threads
// split: top-level comments become threads keeping their ids, replies
// keep theirs, and the answer mark survives on the thread.
func TestMigrationV5(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v4.db")

	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for i := 0; i < 4; i++ { // v1..v4
		if err := migrations[i](ctx, tx); err != nil {
			t.Fatalf("apply migration %d: %v", i+1, err)
		}
	}
	if _, err := tx.ExecContext(ctx, "PRAGMA user_version = 4"); err != nil {
		t.Fatalf("stamp version: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	exec := func(q string, args ...any) {
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	exec("INSERT INTO discussion_categories(name, slug, format) VALUES('Q&A', 'qa', 'qa')")
	exec("INSERT INTO discussions(category_id, title, body, created_at, updated_at) VALUES(1, 'q', '', 1, 1)")
	exec("INSERT INTO discussion_comments(discussion_id, body, created_at) VALUES(1, 'top1', 1)")
	exec("INSERT INTO discussion_comments(discussion_id, body, created_at, is_answer) VALUES(1, 'top2', 2, 1)")
	exec("INSERT INTO discussion_comments(discussion_id, parent_id, body, created_at) VALUES(1, 2, 'reply to top2', 3)")
	exec("INSERT INTO discussion_comments(discussion_id, parent_id, body, created_at) VALUES(1, 1, 'reply to top1', 4)")
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open v4 DB: %v", err)
	}
	defer s.Close()
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("migrated DB user_version = %d, want %d", version, schemaVersion)
	}

	d, err := s.GetDiscussion(ctx, 1)
	if err != nil {
		t.Fatalf("GetDiscussion: %v", err)
	}
	if !d.Answered {
		t.Error("answer mark lost in migration")
	}
	threads, err := s.ListDiscussionThreads(ctx, 1)
	if err != nil {
		t.Fatalf("ListDiscussionThreads: %v", err)
	}
	if len(threads) != 2 || threads[0].ID != 1 || threads[1].ID != 2 {
		t.Fatalf("thread ids not preserved: %+v", threads)
	}
	if !threads[1].IsAnswer {
		t.Error("is_answer not carried onto the thread")
	}
	if len(threads[0].Comments) != 1 || threads[0].Comments[0].ID != 4 {
		t.Fatalf("reply on thread 1: %+v", threads[0].Comments)
	}
	if len(threads[1].Comments) != 1 || threads[1].Comments[0].ID != 3 {
		t.Fatalf("reply on thread 2: %+v", threads[1].Comments)
	}
	if threads[0].Comments[0].ThreadID != 1 || threads[1].Comments[0].ThreadID != 2 {
		t.Error("thread_id not remapped to the preserved parent id")
	}
}
