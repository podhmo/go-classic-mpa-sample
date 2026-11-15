// Discussions: GitHub Discussions-style open conversations (schema version
// 3, see migrations in store.go). Unlike issues, discussions never close —
// a Q&A-category discussion reports "answered" through a marked comment
// instead. Design: docs/sketch/ja/discussions.md (案B: 並列テーブル).
package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ErrInvalidCategory reports a discussion input referencing a category
// that does not exist (e.g. a hand-crafted POST).
var ErrInvalidCategory = errors.New("store: unknown discussion category")

// ErrInvalidParent reports a comment reply whose parent is missing, belongs
// to another discussion, or is itself a reply — only top-level comments may
// be replied to (one level of nesting, like GitHub).
var ErrInvalidParent = errors.New("store: reply target is not a top-level comment of this discussion")

// ErrNotAnswerable reports an answer mark on a comment that cannot be the
// answer: the discussion's category is not qa-format, or the comment is a
// reply rather than a top-level comment.
var ErrNotAnswerable = errors.New("store: comment cannot be marked as answer")

// Category formats.
const (
	CategoryFormatOpen = "open"
	CategoryFormatQA   = "qa"
)

// DiscussionCategory is a seed-defined classification; every discussion
// belongs to exactly one. Format "qa" makes its discussions answerable.
type DiscussionCategory struct {
	ID          int64
	Name        string
	Slug        string // used in ?category= URLs
	Description string
	Format      string // CategoryFormatOpen or CategoryFormatQA
}

// Discussion is one conversation thread. Answered reports whether any of
// its comments is marked as the answer (only meaningful for qa categories).
type Discussion struct {
	ID        int64
	Title     string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Category  DiscussionCategory
	Author    *User // nil for an anonymous post
	Answered  bool
}

// DiscussionComment is a comment on a discussion. A zero ParentID is a
// top-level comment; replies carry their parent's ID (one level only).
// Replies is populated by ListDiscussionComments on top-level comments.
type DiscussionComment struct {
	ID           int64
	DiscussionID int64
	ParentID     int64 // 0 = top-level
	IsAnswer     bool
	Body         string
	CreatedAt    time.Time
	Author       *User // nil for an anonymous post
	Replies      []*DiscussionComment
}

// DiscussionInput carries validated form values for create/update.
type DiscussionInput struct {
	Title      string
	Body       string
	CategoryID int64
}

// DiscussionListFilter controls ListDiscussions. Zero values apply the
// defaults documented per field.
type DiscussionListFilter struct {
	CategorySlug string // "" means all categories
	Answered     string // "", "yes" (has an answer comment), or "no"
	Query        string // case-insensitive substring match on title OR body
	Sort         string // "newest" (default, created_at DESC) or "oldest" (ASC)
	Page         int    // 1-based; values < 1 are clamped to 1
	PerPage      int    // <= 0 defaults to 20
}

// ListDiscussionCategories returns all categories ordered by name.
func (s *Store) ListDiscussionCategories(ctx context.Context) ([]*DiscussionCategory, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, slug, description, format FROM discussion_categories ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*DiscussionCategory
	for rows.Next() {
		var c DiscussionCategory
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.Description, &c.Format); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// ListDiscussions returns the page of discussions matching f (each with
// Category and Answered populated) and the total before pagination.
func (s *Store) ListDiscussions(ctx context.Context, f DiscussionListFilter) ([]*Discussion, int, error) {
	var conds []string
	var args []any

	if f.CategorySlug != "" {
		conds = append(conds, "c.slug = ?")
		args = append(args, f.CategorySlug)
	}
	if f.Query != "" {
		conds = append(conds, "(instr(lower(d.title), lower(?)) > 0 OR instr(lower(d.body), lower(?)) > 0)")
		args = append(args, f.Query, f.Query)
	}
	switch f.Answered {
	case "yes":
		conds = append(conds, discussionAnsweredCond)
	case "no":
		conds = append(conds, "NOT "+discussionAnsweredCond)
	}

	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM discussions d JOIN discussion_categories c ON c.id = d.category_id"+where,
		args...).Scan(&total); err != nil {
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

	q := `SELECT d.id, d.title, d.body, d.created_at, d.updated_at,
		c.id, c.name, c.slug, c.description, c.format,
		u.id, u.name, u.created_at,
		` + discussionAnsweredCond + `
		FROM discussions d
		JOIN discussion_categories c ON c.id = d.category_id
		LEFT JOIN users u ON u.id = d.author_id` + where +
		" ORDER BY d.created_at " + dir + ", d.id " + dir + " LIMIT ? OFFSET ?"
	rows, err := s.db.QueryContext(ctx, q, append(args, perPage, (page-1)*perPage)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*Discussion
	for rows.Next() {
		d, err := scanDiscussion(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

// discussionAnsweredCond is the EXISTS predicate behind Discussion.Answered
// and the ?answered= filter: a discussion is answered when one of its
// comments carries is_answer = 1. The schema's partial unique index keeps
// that at most one row per discussion.
const discussionAnsweredCond = `EXISTS (
	SELECT 1 FROM discussion_comments dc
	WHERE dc.discussion_id = d.id AND dc.is_answer = 1
)`

// GetDiscussion returns one discussion with Category and Answered, or
// ErrNotFound.
func (s *Store) GetDiscussion(ctx context.Context, id int64) (*Discussion, error) {
	d, err := scanDiscussion(s.db.QueryRowContext(ctx,
		`SELECT d.id, d.title, d.body, d.created_at, d.updated_at,
			c.id, c.name, c.slug, c.description, c.format,
			u.id, u.name, u.created_at,
			`+discussionAnsweredCond+`
			FROM discussions d
			JOIN discussion_categories c ON c.id = d.category_id
			LEFT JOIN users u ON u.id = d.author_id
			WHERE d.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// CreateDiscussion inserts a discussion and returns its new ID (also the
// discussion number; IDs are never reused, and are independent of issue
// numbers). authorID is the posting user's ID, or 0 for an anonymous post.
// Returns ErrInvalidCategory when the category does not exist.
func (s *Store) CreateDiscussion(ctx context.Context, in DiscussionInput, authorID int64) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := requireCategory(ctx, tx, in.CategoryID); err != nil {
		return 0, err
	}
	now := time.Now().UTC().Unix()
	res, err := tx.ExecContext(ctx,
		"INSERT INTO discussions(category_id, title, body, created_at, updated_at, author_id) VALUES(?, ?, ?, ?, ?, ?)",
		in.CategoryID, in.Title, in.Body, now, now, nullID(authorID))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// UpdateDiscussion replaces title/body/category and bumps UpdatedAt.
// Returns ErrNotFound if the discussion does not exist, ErrInvalidCategory
// for an unknown category. Moving a discussion to a non-qa category clears
// any answer marks, since they are a qa-only concept and would otherwise
// stay stranded (list shows Answered, detail can no longer unmark).
func (s *Store) UpdateDiscussion(ctx context.Context, id int64, in DiscussionInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	format, err := requireCategory(ctx, tx, in.CategoryID)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		"UPDATE discussions SET title = ?, body = ?, category_id = ?, updated_at = ? WHERE id = ?",
		in.Title, in.Body, in.CategoryID, time.Now().UTC().Unix(), id)
	if err != nil {
		return err
	}
	if err := checkAffected(res); err != nil {
		return err
	}
	if format != CategoryFormatQA {
		if _, err := tx.ExecContext(ctx,
			"UPDATE discussion_comments SET is_answer = 0 WHERE discussion_id = ? AND is_answer = 1",
			id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// requireCategory verifies the category exists inside tx and returns its
// format (open|qa).
func requireCategory(ctx context.Context, tx *sql.Tx, categoryID int64) (string, error) {
	var format string
	err := tx.QueryRowContext(ctx,
		"SELECT format FROM discussion_categories WHERE id = ?", categoryID).Scan(&format)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidCategory
	}
	return format, err
}

// DeleteDiscussion removes the discussion; its comments and replies go via
// ON DELETE CASCADE. Returns ErrNotFound if the discussion does not exist.
func (s *Store) DeleteDiscussion(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM discussions WHERE id = ?", id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

// ListDiscussionComments returns the comments of a discussion grouped into
// threads: top-level comments in chronological order, each with its replies
// (chronological) attached in Replies. Reply rows never appear at top level.
func (s *Store) ListDiscussionComments(ctx context.Context, discussionID int64) ([]*DiscussionComment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.discussion_id, c.parent_id, c.is_answer, c.body, c.created_at,
			u.id, u.name, u.created_at
			FROM discussion_comments c LEFT JOIN users u ON u.id = c.author_id
			WHERE c.discussion_id = ? ORDER BY c.created_at ASC, c.id ASC`,
		discussionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flat []*DiscussionComment
	for rows.Next() {
		c, err := scanDiscussionComment(rows)
		if err != nil {
			return nil, err
		}
		flat = append(flat, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fold the flat chronological list into one-level threads. A reply
	// whose parent is somehow absent (should not happen under the store's
	// validation) is surfaced as top-level rather than dropped.
	byID := make(map[int64]*DiscussionComment, len(flat))
	for _, c := range flat {
		byID[c.ID] = c
	}
	var out []*DiscussionComment
	for _, c := range flat {
		if c.ParentID != 0 {
			if parent := byID[c.ParentID]; parent != nil {
				parent.Replies = append(parent.Replies, c)
				continue
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// CreateDiscussionComment appends a comment — or, when parentID is nonzero,
// a reply to that top-level comment. Returns ErrNotFound if the discussion
// is missing and ErrInvalidParent when the parent is not a top-level
// comment of this discussion. authorID is the posting user's ID, or 0 for
// an anonymous post.
func (s *Store) CreateDiscussionComment(ctx context.Context, discussionID, parentID int64, body string, authorID int64) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM discussions WHERE id = ?", discussionID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if parentID != 0 {
		var pid int64
		err = tx.QueryRowContext(ctx,
			"SELECT id FROM discussion_comments WHERE id = ? AND discussion_id = ? AND parent_id IS NULL",
			parentID, discussionID).Scan(&pid)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInvalidParent
		}
		if err != nil {
			return 0, err
		}
	}
	now := time.Now().UTC().Unix()
	res, err := tx.ExecContext(ctx,
		"INSERT INTO discussion_comments(discussion_id, parent_id, body, created_at, author_id) VALUES(?, ?, ?, ?, ?)",
		discussionID, nullID(parentID), body, now, nullID(authorID))
	if err != nil {
		return 0, err
	}
	// A new comment or reply is activity on the discussion, so bump its
	// updated_at with the comment's timestamp — the same rule issues use.
	if _, err := tx.ExecContext(ctx,
		"UPDATE discussions SET updated_at = ? WHERE id = ?", now, discussionID); err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// SetCommentAnswer marks (answer=true) or unmarks a top-level comment as
// the discussion's answer, then bumps the discussion's updated_at — the
// mark is activity, the same rule comment posting follows. Only qa-format
// categories support answers. Returns ErrNotFound when the discussion or
// comment is missing, and ErrNotAnswerable when the category is not qa or
// the comment is a reply. Marking a second comment moves the mark; unmarking
// a comment that is not marked is a no-op.
func (s *Store) SetCommentAnswer(ctx context.Context, discussionID, commentID int64, answer bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var format string
	err = tx.QueryRowContext(ctx,
		`SELECT c.format FROM discussions d
		 JOIN discussion_categories c ON c.id = d.category_id
		 WHERE d.id = ?`, discussionID).Scan(&format)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var isTopLevel int
	err = tx.QueryRowContext(ctx,
		"SELECT 1 FROM discussion_comments WHERE id = ? AND discussion_id = ? AND parent_id IS NULL",
		commentID, discussionID).Scan(&isTopLevel)
	if errors.Is(err, sql.ErrNoRows) {
		// Distinguish "comment is a reply" from "comment does not exist":
		// a reply is a real comment of this discussion, so 422-style
		// ErrNotAnswerable fits better than a 404.
		var exists int
		err2 := tx.QueryRowContext(ctx,
			"SELECT 1 FROM discussion_comments WHERE id = ? AND discussion_id = ?",
			commentID, discussionID).Scan(&exists)
		if err2 == nil {
			return ErrNotAnswerable
		}
		if errors.Is(err2, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err2
	}
	if err != nil {
		return err
	}
	if answer && format != CategoryFormatQA {
		return ErrNotAnswerable
	}

	if answer {
		// Move the mark: clear the previous answer (at most one row
		// thanks to the partial unique index), then set the new one.
		if _, err := tx.ExecContext(ctx,
			"UPDATE discussion_comments SET is_answer = 0 WHERE discussion_id = ? AND is_answer = 1",
			discussionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE discussion_comments SET is_answer = 1 WHERE id = ?",
			commentID); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx,
			"UPDATE discussion_comments SET is_answer = 0 WHERE id = ?",
			commentID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE discussions SET updated_at = ? WHERE id = ?",
		time.Now().UTC().Unix(), discussionID); err != nil {
		return err
	}
	return tx.Commit()
}

func scanDiscussion(s scanner) (*Discussion, error) {
	var d Discussion
	var c, u int64
	var aid sql.NullInt64
	var aname sql.NullString
	var acreated sql.NullInt64
	var answered int64
	if err := s.Scan(&d.ID, &d.Title, &d.Body, &c, &u,
		&d.Category.ID, &d.Category.Name, &d.Category.Slug, &d.Category.Description, &d.Category.Format,
		&aid, &aname, &acreated, &answered); err != nil {
		return nil, err
	}
	d.CreatedAt = time.Unix(c, 0).UTC()
	d.UpdatedAt = time.Unix(u, 0).UTC()
	d.Author = scanUser(aid, aname, acreated)
	d.Answered = answered != 0
	return &d, nil
}

func scanDiscussionComment(s scanner) (*DiscussionComment, error) {
	var c DiscussionComment
	var ts int64
	var pid sql.NullInt64
	var aid sql.NullInt64
	var aname sql.NullString
	var acreated sql.NullInt64
	if err := s.Scan(&c.ID, &c.DiscussionID, &pid, &c.IsAnswer, &c.Body, &ts,
		&aid, &aname, &acreated); err != nil {
		return nil, err
	}
	if pid.Valid {
		c.ParentID = pid.Int64
	}
	c.CreatedAt = time.Unix(ts, 0).UTC()
	c.Author = scanUser(aid, aname, acreated)
	return &c, nil
}
