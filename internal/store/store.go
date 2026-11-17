// Package store provides SQLite-backed persistence for the issue tracker.
//
// This file defines the API contract shared by the data layer and the web
// layer.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/events"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("store: not found")

// Store wraps the SQLite database handle.
type Store struct {
	db    *sql.DB
	emit  func(events.Change)
	emitM sync.Mutex
}

// SetListener registers fn as the single change listener: every mutating
// method calls it once after its commit succeeds, so a caller wiring the
// web layer's SSE hub sees writes from every path (HTML forms, /api/v1,
// the CLI) in one place. Nil disables emission. Replace-not-append by
// design: one broadcaster owns the fan-out.
func (s *Store) SetListener(fn func(events.Change)) {
	s.emitM.Lock()
	s.emit = fn
	s.emitM.Unlock()
}

// notify fires the registered listener, if any. It runs after the
// mutator's tx committed — never inside it — so a slow listener cannot
// hold the write open.
func (s *Store) notify(c events.Change) {
	s.emitM.Lock()
	fn := s.emit
	s.emitM.Unlock()
	if fn != nil {
		fn(c)
	}
}

// Timestamps are stored as UTC unix seconds.
const schemaDDL = `
CREATE TABLE IF NOT EXISTS issues (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open', 'closed')),
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS comments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
	body TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS labels (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	color TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS issue_labels (
	issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
	label_id INTEGER NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
	PRIMARY KEY (issue_id, label_id)
);
CREATE INDEX IF NOT EXISTS idx_issues_status_created ON issues(status, created_at);
CREATE INDEX IF NOT EXISTS idx_comments_issue_created ON comments(issue_id, created_at);
CREATE INDEX IF NOT EXISTS idx_issue_labels_label ON issue_labels(label_id);
`

// migrations is the ordered list of schema upgrades: migrations[i] moves a
// database from PRAGMA user_version i to i+1. A brand-new database walks
// the whole list, so initialization and upgrading share a single path; an
// existing database runs only the versions it is missing. New schema
// changes always append here — never edit entries already released.
var migrations = []func(ctx context.Context, tx *sql.Tx) error{
	migrateV1, // base schema (issues, comments, labels, issue_labels)
	migrateV2, // accounts: users/sessions + author_id on issues/comments
	migrateV3, // discussions: categories, discussions, discussion_comments
	migrateV4, // api_tokens: per-user Bearer tokens for /api/v1
	migrateV5, // discussion_threads split out of discussion_comments
	migrateV6, // workspaces: workspaces table + workspace_id on issues/discussions
}

// schemaVersion is the user_version a fully migrated database reports.
var schemaVersion = len(migrations)

func migrateV1(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, schemaDDL)
	return err
}

// migrateV2 adds the accounts schema: users/sessions tables and the
// nullable author_id column on issues and comments. A NULL author_id means
// an anonymous post; anonymous users never get a users row
// (docs/sketch/ja/accounts.md). The ALTERs cannot be re-run, which is
// exactly what the user_version gate guarantees.
func migrateV2(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE COLLATE NOCASE,
	password_hash TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
ALTER TABLE issues ADD COLUMN author_id INTEGER REFERENCES users(id);
ALTER TABLE comments ADD COLUMN author_id INTEGER REFERENCES users(id);
`)
	return err
}

// migrateV3 adds the discussions schema (docs/sketch/ja/discussions.md,
// 案B: 並列テーブル). Discussions have no status column — they never close;
// a qa-category discussion derives "answered" from a comment's is_answer.
// The partial unique index on discussion_comments keeps at most one answer
// per discussion at the database level. parent_id is a self-FK limited to
// top-level parents by store validation (existence is all a FK can check).
func migrateV3(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS discussion_categories (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	slug TEXT NOT NULL UNIQUE,
	description TEXT NOT NULL DEFAULT '',
	format TEXT NOT NULL DEFAULT 'open' CHECK(format IN ('open', 'qa'))
);
CREATE TABLE IF NOT EXISTS discussions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	category_id INTEGER NOT NULL REFERENCES discussion_categories(id),
	author_id INTEGER REFERENCES users(id),
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS discussion_comments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	discussion_id INTEGER NOT NULL REFERENCES discussions(id) ON DELETE CASCADE,
	parent_id INTEGER REFERENCES discussion_comments(id) ON DELETE CASCADE,
	author_id INTEGER REFERENCES users(id),
	is_answer INTEGER NOT NULL DEFAULT 0,
	body TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_dcomments_answer
	ON discussion_comments(discussion_id) WHERE is_answer = 1;
CREATE INDEX IF NOT EXISTS idx_discussions_category_created
	ON discussions(category_id, created_at);
CREATE INDEX IF NOT EXISTS idx_dcomments_discussion_created
	ON discussion_comments(discussion_id, created_at);
`)
	return err
}

// migrateV4 adds the api_tokens table (docs/sketch/ja/cli-and-web-api.md
// 「認証」節): per-user Bearer tokens for the JSON API. Like sessions, the
// primary key is the token's SHA-256 hex — plaintext is never stored —
// and revocation is a row delete. Unlike sessions there is no expires_at:
// API tokens are long-lived deploy credentials, managed by issuance
// (`issues auth token`) and deletion rather than idle expiry.
func migrateV4(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS api_tokens (
	id TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);
`)
	return err
}

// migrateV5 restructures discussion comments into the two-table model:
// discussion_threads holds each top-level post (its own body, author and
// the qa answer mark) while discussion_comments keeps only replies, hung
// off thread_id. Top-level rows move into threads keeping their ids and
// replies keep theirs, so existing #comment-N anchors still resolve.
// Replies whose parent was somehow missing have no thread to join and
// are dropped — the store never produces that state (parent validation
// predates it). The comment table is rebuilt rather than ALTERed: its
// parent_id self-FK and per-discussion index cannot be repurposed.
func migrateV5(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE discussion_threads (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	discussion_id INTEGER NOT NULL REFERENCES discussions(id) ON DELETE CASCADE,
	author_id INTEGER REFERENCES users(id),
	is_answer INTEGER NOT NULL DEFAULT 0,
	body TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_dthreads_answer
	ON discussion_threads(discussion_id) WHERE is_answer = 1;
CREATE INDEX idx_dthreads_discussion_created
	ON discussion_threads(discussion_id, created_at);
INSERT INTO discussion_threads(id, discussion_id, author_id, is_answer, body, created_at)
	SELECT id, discussion_id, author_id, is_answer, body, created_at
	FROM discussion_comments WHERE parent_id IS NULL;
CREATE TABLE discussion_comments_new (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	thread_id INTEGER NOT NULL REFERENCES discussion_threads(id) ON DELETE CASCADE,
	author_id INTEGER REFERENCES users(id),
	body TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
INSERT INTO discussion_comments_new(id, thread_id, author_id, body, created_at)
	SELECT id, parent_id, author_id, body, created_at
	FROM discussion_comments
	WHERE parent_id IN (SELECT id FROM discussion_threads);
DROP TABLE discussion_comments;
ALTER TABLE discussion_comments_new RENAME TO discussion_comments;
CREATE INDEX idx_dcomments_thread_created
	ON discussion_comments(thread_id, created_at);
`)
	return err
}

// migrateV6 adds the workspaces schema (docs/sketch/ja/workspaces.md): a
// workspaces table plus a NOT NULL workspace_id FK on issues and
// discussions. SQLite forbids adding a REFERENCES column with a non-NULL
// default while foreign keys are enabled, so both tables are rebuilt —
// the same pattern migrateV5 used on discussion_comments — and every
// existing row is placed in the freshly created 'default' workspace.
// ON DELETE CASCADE makes deleting a workspace carry its contents away,
// like a GitHub repository deletion.
//
// rebuildNeedsFKOff marks this migration for migrate(): DROP TABLE on a
// parent fires its children's ON DELETE CASCADE even inside a
// transaction (SQLite's implicit DELETE), which would wipe comments and
// threads. The connection's foreign_keys pragma must be OFF around the
// rebuild, and PRAGMA foreign_keys is a no-op inside a transaction — so
// migrate() toggles it on the dedicated connection before BeginTx.
var rebuildNeedsFKOff = map[int]bool{5: true} // migrations[5] = migrateV6
func migrateV6(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE workspaces (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	slug TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
INSERT INTO workspaces(slug, name, description, created_at)
	VALUES('default', 'default', 'Default workspace.', CAST(strftime('%s','now') AS INTEGER));
CREATE TABLE issues_new (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open', 'closed')),
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	author_id INTEGER REFERENCES users(id)
);
INSERT INTO issues_new(id, workspace_id, title, body, status, created_at, updated_at, author_id)
	SELECT i.id, w.id, i.title, i.body, i.status, i.created_at, i.updated_at, i.author_id
	FROM issues i, workspaces w WHERE w.slug = 'default';
DROP TABLE issues;
ALTER TABLE issues_new RENAME TO issues;
CREATE INDEX idx_issues_workspace_status_created ON issues(workspace_id, status, created_at);
CREATE TABLE discussions_new (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	category_id INTEGER NOT NULL REFERENCES discussion_categories(id),
	author_id INTEGER REFERENCES users(id),
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
INSERT INTO discussions_new(id, workspace_id, category_id, author_id, title, body, created_at, updated_at)
	SELECT d.id, w.id, d.category_id, d.author_id, d.title, d.body, d.created_at, d.updated_at
	FROM discussions d, workspaces w WHERE w.slug = 'default';
DROP TABLE discussions;
ALTER TABLE discussions_new RENAME TO discussions;
CREATE INDEX idx_discussions_workspace_created ON discussions(workspace_id, created_at);
CREATE INDEX idx_discussions_workspace_category_created ON discussions(workspace_id, category_id, created_at);
`)
	return err
}

// migrate applies every schema version the database is missing, stamping
// PRAGMA user_version after each. Each version runs in its own
// transaction, so a failed migration leaves the database at the last good
// version and can be retried on the next Open. A database newer than
// schemaVersion is rejected rather than silently downgraded.
func migrate(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("store: database schema version %d is newer than supported %d", version, schemaVersion)
	}
	for ; version < schemaVersion; version++ {
		if rebuildNeedsFKOff[version] {
			if err := migrateWithFKOff(ctx, db, version); err != nil {
				return err
			}
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := migrations[version](ctx, tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: migrate schema to version %d: %w", version+1, err)
		}
		// PRAGMA does not accept bound parameters.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version+1)); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: stamp schema version %d: %w", version+1, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// migrateWithFKOff runs migrations[version] on a dedicated connection
// with foreign_keys disabled, then verifies with PRAGMA foreign_key_check
// before re-enabling. Needed when a migration rebuilds a parent table:
// the implicit DELETE in DROP TABLE would cascade into children.
func migrateWithFKOff(ctx context.Context, db *sql.DB, version int) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// foreign_keys can only change outside a transaction.
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("store: disable foreign keys for migration %d: %w", version+1, err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := migrations[version](ctx, tx); err != nil {
		tx.Rollback()
		return fmt.Errorf("store: migrate schema to version %d: %w", version+1, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version+1)); err != nil {
		tx.Rollback()
		return fmt.Errorf("store: stamp schema version %d: %w", version+1, err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// FKs stayed off through the tx; check the rebuilt data before
	// re-enabling, then restore the pragma this connection's DSN set.
	violations := 0
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&violations); err != nil {
		return fmt.Errorf("store: foreign key check after migration %d: %w", version+1, err)
	}
	if violations > 0 {
		return fmt.Errorf("store: migration to version %d left %d foreign key violations", version+1, violations)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("store: re-enable foreign keys: %w", err)
	}
	return nil
}

// Open opens (or creates) the SQLite database at path, enables foreign keys,
// and migrates the schema to the latest version. It does NOT insert demo
// data; callers invoke SeedDemoData for that. Use ":memory:" (or a
// shared-cache memory DSN) for tests.
func Open(path string) (*Store, error) {
	dsn := path
	switch {
	case path == ":memory:":
		dsn = "file::memory:"
	case !strings.HasPrefix(path, "file:"):
		dsn = "file:" + path
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	dsn += sep + "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(dsn, "file::memory:") || strings.Contains(dsn, "mode=memory") {
		// database/sql opens a fresh in-memory DB per pooled connection, so
		// pin a single connection to keep one shared database alive.
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}
	if err := migrate(context.Background(), db); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.DeleteExpiredSessions(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: sweep expired sessions: %w", err)
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Analyze runs the SQLite ANALYZE command, refreshing the query planner's
// statistics (the internal sqlite_stat% tables). It is safe to re-run — the
// "init" subcommand uses it so a freshly set up database does not start
// with stale or missing planner statistics.
func (s *Store) Analyze(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "ANALYZE"); err != nil {
		return fmt.Errorf("store: analyze: %w", err)
	}
	return nil
}

// SchemaObjects lists the database's tables and named indexes as recorded
// in sqlite_master, each sorted by name. Internal bookkeeping objects are
// filtered so the result reflects only the schema declared by Open:
// sqlite_stat% tables (ANALYZE output) are excluded from tables and
// sqlite_autoindex% (UNIQUE/PRIMARY KEY backing indexes) from indexes.
// sqlite_sequence (AUTOINCREMENT bookkeeping) is a real part of the
// declared schema and stays in the table list.
func (s *Store) SchemaObjects(ctx context.Context) (tables, indexes []string, err error) {
	list := func(where string) ([]string, error) {
		rows, err := s.db.QueryContext(ctx,
			"SELECT name FROM sqlite_master WHERE "+where+" ORDER BY name")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			out = append(out, name)
		}
		return out, rows.Err()
	}

	tables, err = list("type='table' AND name NOT LIKE 'sqlite_stat%'")
	if err != nil {
		return nil, nil, fmt.Errorf("store: list tables: %w", err)
	}
	indexes, err = list("type='index' AND name NOT LIKE 'sqlite_autoindex%'")
	if err != nil {
		return nil, nil, fmt.Errorf("store: list indexes: %w", err)
	}
	return tables, indexes, nil
}

type scanner interface{ Scan(...any) error }

// scanUser builds the author from a LEFT JOIN's nullable user columns; a
// NULL id means an anonymous post.
func scanUser(id sql.NullInt64, name sql.NullString, created sql.NullInt64) *User {
	if !id.Valid {
		return nil
	}
	return &User{ID: id.Int64, Name: name.String, CreatedAt: time.Unix(created.Int64, 0).UTC()}
}

// nullID maps a 0 author ID (anonymous) to SQL NULL.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func checkAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
