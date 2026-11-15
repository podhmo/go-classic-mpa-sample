package store

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// mustCategory inserts a discussion category and returns its ID.
func mustCategory(t *testing.T, s *Store, name, slug, format string) int64 {
	t.Helper()
	res, err := s.db.Exec(
		"INSERT INTO discussion_categories(name, slug, description, format) VALUES(?, ?, '', ?)",
		name, slug, format)
	if err != nil {
		t.Fatalf("insert category: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("category id: %v", err)
	}
	return id
}

// mustDiscussion creates a discussion and returns its ID.
func mustDiscussion(t *testing.T, s *Store, categoryID int64, title string) int64 {
	t.Helper()
	id, err := s.CreateDiscussion(context.Background(),
		DiscussionInput{Title: title, Body: "body", CategoryID: categoryID}, 0)
	if err != nil {
		t.Fatalf("CreateDiscussion: %v", err)
	}
	return id
}

func TestDiscussionCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cat := mustCategory(t, s, "General", "general", CategoryFormatOpen)
	cat2 := mustCategory(t, s, "Q&A", "qa", CategoryFormatQA)

	id := mustDiscussion(t, s, cat, "first")

	d, err := s.GetDiscussion(ctx, id)
	if err != nil {
		t.Fatalf("GetDiscussion: %v", err)
	}
	if d.Title != "first" || d.Category.Slug != "general" || d.Author != nil || d.Answered {
		t.Errorf("unexpected discussion: %+v", d)
	}

	time.Sleep(1100 * time.Millisecond) // unix-second granularity
	if err := s.UpdateDiscussion(ctx, id, DiscussionInput{Title: "renamed", Body: "b2", CategoryID: cat2}); err != nil {
		t.Fatalf("UpdateDiscussion: %v", err)
	}
	d, err = s.GetDiscussion(ctx, id)
	if err != nil {
		t.Fatalf("GetDiscussion: %v", err)
	}
	if d.Title != "renamed" || d.Category.Slug != "qa" || !d.UpdatedAt.After(d.CreatedAt) {
		t.Errorf("unexpected after update: %+v", d)
	}

	if err := s.DeleteDiscussion(ctx, id); err != nil {
		t.Fatalf("DeleteDiscussion: %v", err)
	}
	if _, err := s.GetDiscussion(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetDiscussion after delete: %v", err)
	}
	if err := s.DeleteDiscussion(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestDiscussionCategoryValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cat := mustCategory(t, s, "General", "general", CategoryFormatOpen)

	if _, err := s.CreateDiscussion(ctx, DiscussionInput{Title: "t", CategoryID: 999}, 0); !errors.Is(err, ErrInvalidCategory) {
		t.Fatalf("create with unknown category: %v", err)
	}
	id := mustDiscussion(t, s, cat, "ok")
	if err := s.UpdateDiscussion(ctx, id, DiscussionInput{Title: "t", CategoryID: 999}); !errors.Is(err, ErrInvalidCategory) {
		t.Fatalf("update with unknown category: %v", err)
	}
}

func TestListDiscussions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	general := mustCategory(t, s, "General", "general", CategoryFormatOpen)
	qa := mustCategory(t, s, "Q&A", "qa", CategoryFormatQA)

	d1 := mustDiscussion(t, s, general, "label workflow")
	d2 := mustDiscussion(t, s, qa, "why empty export")
	mustDiscussion(t, s, qa, "keyboard shortcuts")

	// category filter
	list, total, err := s.ListDiscussions(ctx, DiscussionListFilter{CategorySlug: "qa"})
	if err != nil || total != 2 {
		t.Fatalf("category=qa: total=%d err=%v", total, err)
	}
	_ = list

	// query matches title/body
	_, total, err = s.ListDiscussions(ctx, DiscussionListFilter{Query: "keyboard"})
	if err != nil || total != 1 {
		t.Fatalf("q=keyboard: total=%d err=%v", total, err)
	}

	// answered filter
	if _, err := s.CreateDiscussionComment(ctx, d1, 0, "no", 0); err != nil {
		t.Fatalf("comment: %v", err)
	}
	cid, err := s.CreateDiscussionComment(ctx, d2, 0, "the answer", 0)
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if err := s.SetCommentAnswer(ctx, d2, cid, true); err != nil {
		t.Fatalf("SetCommentAnswer: %v", err)
	}
	_, total, err = s.ListDiscussions(ctx, DiscussionListFilter{Answered: "yes"})
	if err != nil || total != 1 {
		t.Fatalf("answered=yes: total=%d err=%v", total, err)
	}
	_, total, err = s.ListDiscussions(ctx, DiscussionListFilter{Answered: "no"})
	if err != nil || total != 2 {
		t.Fatalf("answered=no: total=%d err=%v", total, err)
	}

	// sort: newest first by default, oldest reverses
	list, _, err = s.ListDiscussions(ctx, DiscussionListFilter{Sort: "oldest"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !slices.IsSortedFunc(list, func(a, b *Discussion) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	}) {
		t.Fatalf("oldest sort violated: %+v", list)
	}
}

func TestDiscussionCommentThreading(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cat := mustCategory(t, s, "General", "general", CategoryFormatOpen)
	d1 := mustDiscussion(t, s, cat, "d1")
	d2 := mustDiscussion(t, s, cat, "d2")

	c1, err := s.CreateDiscussionComment(ctx, d1, 0, "top1", 0)
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if _, err := s.CreateDiscussionComment(ctx, d1, 0, "top2", 0); err != nil {
		t.Fatalf("comment: %v", err)
	}
	r1, err := s.CreateDiscussionComment(ctx, d1, c1, "reply to top1", 0)
	if err != nil {
		t.Fatalf("reply: %v", err)
	}

	// A reply cannot be a parent (one level only).
	if _, err := s.CreateDiscussionComment(ctx, d1, r1, "nested reply", 0); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("reply-to-reply: %v", err)
	}
	// A comment of another discussion cannot be a parent.
	cOther, err := s.CreateDiscussionComment(ctx, d2, 0, "other", 0)
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if _, err := s.CreateDiscussionComment(ctx, d1, cOther, "x", 0); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("cross-discussion parent: %v", err)
	}
	// Missing discussion.
	if _, err := s.CreateDiscussionComment(ctx, 999, 0, "x", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("comment on missing discussion: %v", err)
	}

	comments, err := s.ListDiscussionComments(ctx, d1)
	if err != nil {
		t.Fatalf("ListDiscussionComments: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("top-level count = %d, want 2", len(comments))
	}
	var found bool
	for _, c := range comments {
		if c.ID == c1 {
			found = true
			if len(c.Replies) != 1 || c.Replies[0].ID != r1 || c.Replies[0].ParentID != c1 {
				t.Fatalf("thread shape: %+v", c)
			}
		}
		if c.ParentID != 0 {
			t.Fatalf("reply leaked to top level: %+v", c)
		}
	}
	if !found {
		t.Fatalf("parent comment missing: %+v", comments)
	}
}

func TestSetCommentAnswer(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	open := mustCategory(t, s, "General", "general", CategoryFormatOpen)
	qa := mustCategory(t, s, "Q&A", "qa", CategoryFormatQA)
	dOpen := mustDiscussion(t, s, open, "d-open")
	dQA := mustDiscussion(t, s, qa, "d-qa")

	c1, _ := s.CreateDiscussionComment(ctx, dQA, 0, "c1", 0)
	c2, _ := s.CreateDiscussionComment(ctx, dQA, 0, "c2", 0)
	reply, _ := s.CreateDiscussionComment(ctx, dQA, c1, "r1", 0)
	cOpen, _ := s.CreateDiscussionComment(ctx, dOpen, 0, "c-open", 0)

	// non-qa category cannot mark answers
	if err := s.SetCommentAnswer(ctx, dOpen, cOpen, true); !errors.Is(err, ErrNotAnswerable) {
		t.Fatalf("answer on open category: %v", err)
	}
	// a reply cannot be the answer
	if err := s.SetCommentAnswer(ctx, dQA, reply, true); !errors.Is(err, ErrNotAnswerable) {
		t.Fatalf("answer on reply: %v", err)
	}
	// missing comment/discussion
	if err := s.SetCommentAnswer(ctx, dQA, 999, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("answer on missing comment: %v", err)
	}
	if err := s.SetCommentAnswer(ctx, 999, c1, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("answer on missing discussion: %v", err)
	}

	// mark, then move the mark — at most one answer survives
	if err := s.SetCommentAnswer(ctx, dQA, c1, true); err != nil {
		t.Fatalf("mark c1: %v", err)
	}
	if err := s.SetCommentAnswer(ctx, dQA, c2, true); err != nil {
		t.Fatalf("mark c2: %v", err)
	}
	d, err := s.GetDiscussion(ctx, dQA)
	if err != nil || !d.Answered {
		t.Fatalf("answered flag: %v %+v", err, d)
	}
	var marked int
	for _, c := range mustListDComments(t, s, dQA) {
		if c.IsAnswer {
			marked++
			if c.ID != c2 {
				t.Fatalf("wrong comment marked: %d", c.ID)
			}
		}
	}
	if marked != 1 {
		t.Fatalf("marked = %d, want 1", marked)
	}

	// unmark
	if err := s.SetCommentAnswer(ctx, dQA, c2, false); err != nil {
		t.Fatalf("unmark: %v", err)
	}
	d, _ = s.GetDiscussion(ctx, dQA)
	if d.Answered {
		t.Fatal("still answered after unmark")
	}
}

func mustListDComments(t *testing.T, s *Store, discussionID int64) []*DiscussionComment {
	t.Helper()
	cs, err := s.ListDiscussionComments(context.Background(), discussionID)
	if err != nil {
		t.Fatalf("ListDiscussionComments: %v", err)
	}
	return cs
}

func TestDeleteDiscussionCascadesComments(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cat := mustCategory(t, s, "Q&A", "qa", CategoryFormatQA)
	d := mustDiscussion(t, s, cat, "d")
	c1, _ := s.CreateDiscussionComment(ctx, d, 0, "c1", 0)
	if _, err := s.CreateDiscussionComment(ctx, d, c1, "r1", 0); err != nil {
		t.Fatalf("reply: %v", err)
	}
	if err := s.DeleteDiscussion(ctx, d); err != nil {
		t.Fatalf("DeleteDiscussion: %v", err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM discussion_comments WHERE discussion_id = ?", d).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("comments left after delete: %d", n)
	}
}

func TestDiscussionCommentBumpsUpdatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cat := mustCategory(t, s, "General", "general", CategoryFormatOpen)
	d := mustDiscussion(t, s, cat, "d")

	before, _ := s.GetDiscussion(ctx, d)
	time.Sleep(1100 * time.Millisecond) // unix-second granularity
	if _, err := s.CreateDiscussionComment(ctx, d, 0, "activity", 0); err != nil {
		t.Fatalf("comment: %v", err)
	}
	after, _ := s.GetDiscussion(ctx, d)
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("updated_at not bumped: %v -> %v", before.UpdatedAt, after.UpdatedAt)
	}
}
