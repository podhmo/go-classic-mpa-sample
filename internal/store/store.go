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
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("store: not found")

// Issue status values.
const (
	StatusOpen   = "open"
	StatusClosed = "closed"
)

// Issue is a single tracker entry. Labels is populated by read methods.
type Issue struct {
	ID        int64
	Title     string
	Body      string
	Status    string // StatusOpen or StatusClosed
	CreatedAt time.Time
	UpdatedAt time.Time
	Labels    []Label
}

// Comment is a plaintext comment on an issue, shown in chronological order.
type Comment struct {
	ID        int64
	IssueID   int64
	Body      string
	CreatedAt time.Time
}

// Label is a named, colored tag that can be attached to issues.
type Label struct {
	ID    int64
	Name  string
	Color string // CSS hex color, e.g. "#d73a4a"
}

// IssueInput carries validated form values for create/update.
type IssueInput struct {
	Title    string
	Body     string
	LabelIDs []int64
}

// ListFilter controls ListIssues. Zero values apply the defaults documented
// per field.
type ListFilter struct {
	Status  string // "" means StatusOpen; "all" disables status filtering
	Query   string // case-insensitive substring match on title OR body
	LabelID int64  // 0 means no label filter
	Sort    string // "newest" (default, created_at DESC) or "oldest" (ASC)
	Page    int    // 1-based; values < 1 are clamped to 1
	PerPage int    // <= 0 defaults to 20
}

// Store wraps the SQLite database handle.
type Store struct {
	db *sql.DB
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

// ListIssues returns the page of issues matching f (each with Labels
// populated) and the total number of matching issues before pagination.
func (s *Store) ListIssues(ctx context.Context, f ListFilter) ([]*Issue, int, error) {
	var conds []string
	var args []any

	status := f.Status
	if status == "" {
		status = StatusOpen
	}
	if status != "all" {
		conds = append(conds, "status = ?")
		args = append(args, status)
	}
	if f.Query != "" {
		conds = append(conds, "(instr(lower(title), lower(?)) > 0 OR instr(lower(body), lower(?)) > 0)")
		args = append(args, f.Query, f.Query)
	}
	if f.LabelID > 0 {
		conds = append(conds, "EXISTS (SELECT 1 FROM issue_labels il WHERE il.issue_id = issues.id AND il.label_id = ?)")
		args = append(args, f.LabelID)
	}

	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM issues"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	dir := "DESC"
	if f.Sort == "oldest" {
		dir = "ASC"
	}
	page := max(f.Page, 1)
	perPage := f.PerPage
	if perPage <= 0 {
		perPage = 20
	}

	q := "SELECT id, title, body, status, created_at, updated_at FROM issues" + where +
		" ORDER BY created_at " + dir + ", id " + dir + " LIMIT ? OFFSET ?"
	rows, err := s.db.QueryContext(ctx, q, append(args, perPage, (page-1)*perPage)...)
	if err != nil {
		return nil, 0, err
	}
	var issues []*Issue
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		issues = append(issues, i)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()

	if err := s.attachLabels(ctx, issues); err != nil {
		return nil, 0, err
	}
	return issues, total, nil
}

// GetIssue returns one issue with Labels, or ErrNotFound.
func (s *Store) GetIssue(ctx context.Context, id int64) (*Issue, error) {
	i, err := scanIssue(s.db.QueryRowContext(ctx,
		"SELECT id, title, body, status, created_at, updated_at FROM issues WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.attachLabels(ctx, []*Issue{i}); err != nil {
		return nil, err
	}
	return i, nil
}

// CreateIssue inserts an open issue and returns its new ID (also the issue
// number; IDs are never reused).
func (s *Store) CreateIssue(ctx context.Context, in IssueInput) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Unix()
	res, err := tx.ExecContext(ctx,
		"INSERT INTO issues(title, body, status, created_at, updated_at) VALUES(?, ?, ?, ?, ?)",
		in.Title, in.Body, StatusOpen, now, now)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := setIssueLabels(ctx, tx, id, in.LabelIDs); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// UpdateIssue replaces title/body/labels and bumps UpdatedAt. Returns
// ErrNotFound if the issue does not exist.
func (s *Store) UpdateIssue(ctx context.Context, id int64, in IssueInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		"UPDATE issues SET title = ?, body = ?, updated_at = ? WHERE id = ?",
		in.Title, in.Body, time.Now().UTC().Unix(), id)
	if err != nil {
		return err
	}
	if err := checkAffected(res); err != nil {
		return err
	}
	if err := setIssueLabels(ctx, tx, id, in.LabelIDs); err != nil {
		return err
	}
	return tx.Commit()
}

// SetIssueStatus sets status to StatusOpen or StatusClosed. Returns
// ErrNotFound if the issue does not exist.
func (s *Store) SetIssueStatus(ctx context.Context, id int64, status string) error {
	if status != StatusOpen && status != StatusClosed {
		return fmt.Errorf("store: invalid status %q", status)
	}
	// updated_at is bumped only when the status actually changes; a no-op
	// post (e.g. closing an already-closed issue) leaves it alone. The
	// updated_at clause precedes the status assignment so the CASE compares
	// against the pre-update status. An existing row always matches the
	// UPDATE, so RowsAffected still reports 0 only when the issue is missing.
	res, err := s.db.ExecContext(ctx,
		"UPDATE issues SET updated_at = CASE WHEN status != ? THEN ? ELSE updated_at END, status = ? WHERE id = ?",
		status, time.Now().UTC().Unix(), status, id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

// DeleteIssue removes the issue along with its comments and label links.
// Returns ErrNotFound if the issue does not exist.
func (s *Store) DeleteIssue(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM issues WHERE id = ?", id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

// ListComments returns comments for an issue in chronological order.
func (s *Store) ListComments(ctx context.Context, issueID int64) ([]*Comment, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, issue_id, body, created_at FROM comments WHERE issue_id = ? ORDER BY created_at ASC, id ASC",
		issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Comment
	for rows.Next() {
		var c Comment
		var ts int64
		if err := rows.Scan(&c.ID, &c.IssueID, &c.Body, &ts); err != nil {
			return nil, err
		}
		c.CreatedAt = time.Unix(ts, 0).UTC()
		out = append(out, &c)
	}
	return out, rows.Err()
}

// CreateComment appends a comment. Returns ErrNotFound if the issue is
// missing.
func (s *Store) CreateComment(ctx context.Context, issueID int64, body string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM issues WHERE id = ?", issueID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().Unix()
	res, err := tx.ExecContext(ctx,
		"INSERT INTO comments(issue_id, body, created_at) VALUES(?, ?, ?)",
		issueID, body, now)
	if err != nil {
		return 0, err
	}
	// A new comment is activity on the issue, so bump its updated_at with
	// the comment's timestamp.
	if _, err := tx.ExecContext(ctx,
		"UPDATE issues SET updated_at = ? WHERE id = ?", now, issueID); err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// ListLabels returns all labels ordered by name.
func (s *Store) ListLabels(ctx context.Context) ([]*Label, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, color FROM labels ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Label
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.ID, &l.Name, &l.Color); err != nil {
			return nil, err
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

// attachLabels populates Labels on each issue with a single grouped query.
func (s *Store) attachLabels(ctx context.Context, issues []*Issue) error {
	if len(issues) == 0 {
		return nil
	}
	byID := make(map[int64]*Issue, len(issues))
	ph := make([]string, len(issues))
	args := make([]any, len(issues))
	for i, is := range issues {
		byID[is.ID] = is
		ph[i] = "?"
		args[i] = is.ID
	}
	q := `SELECT il.issue_id, l.id, l.name, l.color
		FROM issue_labels il JOIN labels l ON l.id = il.label_id
		WHERE il.issue_id IN (` + strings.Join(ph, ",") + `) ORDER BY l.name`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var issueID int64
		var l Label
		if err := rows.Scan(&issueID, &l.ID, &l.Name, &l.Color); err != nil {
			return err
		}
		if is := byID[issueID]; is != nil {
			is.Labels = append(is.Labels, l)
		}
	}
	return rows.Err()
}

// setIssueLabels replaces the label set of an issue inside tx.
func setIssueLabels(ctx context.Context, tx *sql.Tx, issueID int64, labelIDs []int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM issue_labels WHERE issue_id = ?", issueID); err != nil {
		return err
	}
	for _, lid := range labelIDs {
		// Label ids that do not exist in labels make the SELECT yield no
		// row (nothing inserted); duplicates hit the PRIMARY KEY conflict
		// that OR IGNORE skips. Both are silently dropped rather than
		// failing with a constraint error.
		if _, err := tx.ExecContext(ctx,
			"INSERT OR IGNORE INTO issue_labels(issue_id, label_id) SELECT ?, id FROM labels WHERE id = ?",
			issueID, lid); err != nil {
			return err
		}
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanIssue(s scanner) (*Issue, error) {
	var i Issue
	var c, u int64
	if err := s.Scan(&i.ID, &i.Title, &i.Body, &i.Status, &c, &u); err != nil {
		return nil, err
	}
	i.CreatedAt = time.Unix(c, 0).UTC()
	i.UpdatedAt = time.Unix(u, 0).UTC()
	return &i, nil
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
