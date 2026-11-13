// Package store provides SQLite-backed persistence for the issue tracker.
//
// This file defines the API contract shared by the data layer and the web
// layer. Method bodies are implemented in other files of this package.
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
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

// Open opens (or creates) the SQLite database at path, enables foreign keys,
// and initializes the schema. It does NOT insert demo data; callers invoke
// SeedDemoData for that. Use ":memory:" (or a shared-cache memory DSN) for
// tests.
func Open(path string) (*Store, error) {
	return nil, errors.New("store: not implemented")
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return errors.New("store: not implemented")
}

// SeedDemoData inserts the default labels plus demo issues/comments, but only
// when the issues table is empty. It is a no-op otherwise.
func (s *Store) SeedDemoData(ctx context.Context) error {
	return errors.New("store: not implemented")
}

// ListIssues returns the page of issues matching f (each with Labels
// populated) and the total number of matching issues before pagination.
func (s *Store) ListIssues(ctx context.Context, f ListFilter) ([]*Issue, int, error) {
	return nil, 0, errors.New("store: not implemented")
}

// GetIssue returns one issue with Labels, or ErrNotFound.
func (s *Store) GetIssue(ctx context.Context, id int64) (*Issue, error) {
	return nil, errors.New("store: not implemented")
}

// CreateIssue inserts an open issue and returns its new ID (also the issue
// number; IDs are never reused).
func (s *Store) CreateIssue(ctx context.Context, in IssueInput) (int64, error) {
	return 0, errors.New("store: not implemented")
}

// UpdateIssue replaces title/body/labels and bumps UpdatedAt. Returns
// ErrNotFound if the issue does not exist.
func (s *Store) UpdateIssue(ctx context.Context, id int64, in IssueInput) error {
	return errors.New("store: not implemented")
}

// SetIssueStatus sets status to StatusOpen or StatusClosed. Returns
// ErrNotFound if the issue does not exist.
func (s *Store) SetIssueStatus(ctx context.Context, id int64, status string) error {
	return errors.New("store: not implemented")
}

// DeleteIssue removes the issue along with its comments and label links.
// Returns ErrNotFound if the issue does not exist.
func (s *Store) DeleteIssue(ctx context.Context, id int64) error {
	return errors.New("store: not implemented")
}

// ListComments returns comments for an issue in chronological order.
func (s *Store) ListComments(ctx context.Context, issueID int64) ([]*Comment, error) {
	return nil, errors.New("store: not implemented")
}

// CreateComment appends a comment. Returns ErrNotFound if the issue is
// missing.
func (s *Store) CreateComment(ctx context.Context, issueID int64, body string) (int64, error) {
	return 0, errors.New("store: not implemented")
}

// ListLabels returns all labels ordered by name.
func (s *Store) ListLabels(ctx context.Context) ([]*Label, error) {
	return nil, errors.New("store: not implemented")
}
