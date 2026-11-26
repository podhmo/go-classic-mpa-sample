package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
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
	s, err := Open(filepath.Join(t.TempDir(), "archive-mode=memory.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()

	// Hold both connections while checking them so the pool cannot reuse the
	// first connection for the second assertion.
	conns := make([]*sql.Conn, 0, 2)
	for range 2 {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn: %v", err)
		}
		defer conn.Close()
		conns = append(conns, conn)
	}
	for i, conn := range conns {
		var on int
		if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&on); err != nil {
			t.Fatalf("PRAGMA foreign_keys: %v", err)
		}
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
	var journalMode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
	var synchronous int
	if err := s.db.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatalf("PRAGMA synchronous: %v", err)
	}
	if synchronous != 1 { // NORMAL
		t.Fatalf("synchronous = %d, want NORMAL (1)", synchronous)
	}
}

func TestTransactionsBeginImmediate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "immediate.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}
	t.Cleanup(func() { s1.Close() })
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("Open second: %v", err)
	}
	t.Cleanup(func() { s2.Close() })

	ctx := context.Background()
	tx1, err := s1.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("first BeginTx: %v", err)
	}
	began := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		tx2, err := s2.db.BeginTx(ctx, nil)
		if err == nil {
			close(began)
			err = tx2.Rollback()
		}
		done <- err
	}()

	select {
	case <-began:
		tx1.Rollback()
		<-done
		t.Fatal("second transaction began before the first commit; txlock is not immediate")
	case <-time.After(50 * time.Millisecond):
	}
	if err := tx1.Commit(); err != nil {
		t.Fatalf("first Commit: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("second BeginTx after first Commit: %v", err)
	}
}

// Concurrent writers exercise the immediate txlock: transactions that read
// before writing must serialize at BEGIN instead of failing a deferred
// read-to-write upgrade with SQLITE_BUSY.
func TestConcurrentWritersDoNotBusy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.db")
	const handles = 4
	stores := make([]*Store, 0, handles)
	for range handles {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { s.Close() })
		stores = append(stores, s)
	}
	cat := mustCategory(t, stores[0], "General", "general", CategoryFormatOpen)

	const workers = 8
	const writesPerWorker = 3
	start := make(chan struct{})
	errs := make(chan error, workers*writesPerWorker)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s := stores[i%len(stores)]
			for range writesPerWorker {
				_, err := s.CreateDiscussion(context.Background(), 1, DiscussionInput{
					Title: "concurrent", CategoryID: cat,
				}, 0)
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write: %v", err)
		}
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
	wantTables := []string{"api_tokens", "changes", "comments", "discussion_categories", "discussion_comments", "discussion_labels", "discussion_threads", "discussions", "issue_labels", "issues", "labels", "pr_comments", "pull_requests", "sessions", "sqlite_sequence", "users", "workspaces"}
	if !slices.Equal(tables, wantTables) {
		t.Errorf("tables = %v, want %v", tables, wantTables)
	}

	// Only the named indexes from the schema; the UNIQUE/PK backing
	// indexes (sqlite_autoindex%) must be filtered out.
	wantIndexes := []string{"idx_api_tokens_user", "idx_comments_issue_created", "idx_dcomments_thread_created", "idx_discussion_labels_label", "idx_discussions_workspace_category_created", "idx_discussions_workspace_created", "idx_dthreads_answer", "idx_dthreads_discussion_created", "idx_issue_labels_label", "idx_issues_workspace_status_created", "idx_labels_kind_name_ws", "idx_pr_comments_pr", "idx_pull_requests_open_pair", "idx_pull_requests_status_created", "idx_sessions_user"}
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
	wantTables := []string{"api_tokens", "changes", "comments", "discussion_categories", "discussion_comments", "discussion_labels", "discussion_threads", "discussions", "issue_labels", "issues", "labels", "pr_comments", "pull_requests", "sessions", "sqlite_sequence", "users", "workspaces"}
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
		"SELECT workspace_id, author_id FROM issues LIMIT 0",
		"SELECT author_id FROM comments LIMIT 0",
		"SELECT id FROM users LIMIT 0",
		"SELECT id, expires_at FROM sessions LIMIT 0",
		"SELECT id, user_id, created_at FROM api_tokens LIMIT 0",
		"SELECT id, slug FROM workspaces LIMIT 0",
		"SELECT workspace_id FROM discussions LIMIT 0",
		"SELECT id, is_answer FROM discussion_threads LIMIT 0",
		"SELECT edited_at, hidden_at, hidden_reason FROM comments LIMIT 0",
		"SELECT edited_at, hidden_at, hidden_reason FROM discussion_threads LIMIT 0",
		"SELECT edited_at, hidden_at, hidden_reason FROM discussion_comments LIMIT 0",
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
