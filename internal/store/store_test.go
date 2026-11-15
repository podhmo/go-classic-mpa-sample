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
	wantTables := []string{"comments", "discussion_categories", "discussion_comments", "discussions", "issue_labels", "issues", "labels", "sessions", "sqlite_sequence", "users"}
	if !slices.Equal(tables, wantTables) {
		t.Errorf("tables = %v, want %v", tables, wantTables)
	}

	// Only the named indexes from the schema; the UNIQUE/PK backing
	// indexes (sqlite_autoindex%) must be filtered out.
	wantIndexes := []string{"idx_comments_issue_created", "idx_dcomments_answer", "idx_dcomments_discussion_created", "idx_discussions_category_created", "idx_issue_labels_label", "idx_issues_status_created", "idx_sessions_user"}
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
	wantTables := []string{"comments", "discussion_categories", "discussion_comments", "discussions", "issue_labels", "issues", "labels", "sessions", "sqlite_sequence", "users"}
	if !slices.Equal(tables, wantTables) {
		t.Errorf("tables after ANALYZE = %v, want %v", tables, wantTables)
	}
}

func TestMigrationFreshAndLegacyDB(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "mig.db")

	// A fresh database walks every migration and lands on schemaVersion.
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
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Errorf("v2 column check %q: %v", q, err)
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

	// Open must replay every version and keep existing rows.
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
