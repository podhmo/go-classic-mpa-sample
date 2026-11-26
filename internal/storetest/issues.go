// Package storetest contains the shared behavioral contract exercised by
// the real SQLite store and its web/storybook fakes. It deliberately keeps
// implementation-specific seed and clock hooks outside the suite.
package storetest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// IssueStore is the issue/comment slice of the store contract covered by
// this suite. web.Store and *store.Store both satisfy it structurally.
type IssueStore interface {
	CreateIssue(ctx context.Context, wsID int64, in store.IssueInput, authorID int64) (int64, error)
	GetIssue(ctx context.Context, id int64) (*store.Issue, error)
	ListIssues(ctx context.Context, wsID int64, f store.ListFilter) ([]*store.Issue, int, error)
	UpdateIssue(ctx context.Context, id int64, in store.IssueInput) error
	SetIssueStatus(ctx context.Context, id int64, status string) error
	CreateComment(ctx context.Context, issueID int64, body string, authorID int64) (int64, error)
	CreateLabel(ctx context.Context, wsID int64, kind string, in store.LabelInput) (int64, error)
}

// IssueFixture adapts one implementation to the suite. SeedIssue must
// create or select an open issue in WorkspaceID. BackdateIssue moves both
// timestamps into the past using the implementation's own clock.
type IssueFixture struct {
	Store         IssueStore
	WorkspaceID   int64
	SeedIssue     func(tb testing.TB, in store.IssueInput, authorID int64) int64
	BackdateIssue func(tb testing.TB, id int64, age time.Duration)
	SeedUser      func(tb testing.TB, name string) int64

	// Declared simplifications. These are part of the fake's public test
	// contract rather than silently tolerated drift.
	FiltersIgnored         bool // list ignores filters/pagination (storyStore)
	UpdatesPersist         bool // UpdateIssue writes the new values
	MissingCommentNotFound bool // CreateComment on a missing issue is ErrNotFound
}

// RunIssueContract runs the shared issue-store behavior suite. The same
// entry point covers full implementations and declared stubs: deviations
// are explicit fixture capabilities, not per-call ad-hoc skips.
func RunIssueContract(t *testing.T, factory func(tb testing.TB) IssueFixture) {
	t.Helper()
	t.Run("status bump and error ordering", func(t *testing.T) {
		fx := factory(t)
		ctx := context.Background()
		id := fx.SeedIssue(t, store.IssueInput{Title: "contract status"}, 0)
		fx.BackdateIssue(t, id, time.Hour)
		before := mustGetIssue(t, fx.Store, id)
		beforeUpdated := before.UpdatedAt // implementations may return shared mutable pointers

		if err := fx.Store.SetIssueStatus(ctx, id, store.StatusOpen); err != nil {
			t.Fatalf("same status: %v", err)
		}
		if got := mustGetIssue(t, fx.Store, id); !got.UpdatedAt.Equal(beforeUpdated) {
			t.Fatalf("no-op status bumped updated_at: %v -> %v", beforeUpdated, got.UpdatedAt)
		}
		if err := fx.Store.SetIssueStatus(ctx, id, store.StatusClosed); err != nil {
			t.Fatalf("close: %v", err)
		}
		if got := mustGetIssue(t, fx.Store, id); got.Status != store.StatusClosed || !got.UpdatedAt.After(beforeUpdated) {
			t.Fatalf("close status/updated_at = %q/%v, want closed after %v", got.Status, got.UpdatedAt, beforeUpdated)
		}
		if err := fx.Store.SetIssueStatus(ctx, 424242, "bogus"); err == nil || errors.Is(err, store.ErrNotFound) {
			t.Fatalf("invalid status on missing issue = %v, want validation error before ErrNotFound", err)
		}
		if err := fx.Store.SetIssueStatus(ctx, 424242, store.StatusClosed); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("missing issue = %v, want ErrNotFound", err)
		}
	})

	t.Run("update bump semantics", func(t *testing.T) {
		fx := factory(t)
		id := fx.SeedIssue(t, store.IssueInput{Title: "before", Body: "old body"}, 0)
		fx.BackdateIssue(t, id, time.Hour)
		before := mustGetIssue(t, fx.Store, id)
		beforeTitle := before.Title
		beforeUpdated := before.UpdatedAt
		err := fx.Store.UpdateIssue(context.Background(), id, store.IssueInput{Title: "after", Body: "new body"})
		if err != nil {
			t.Fatalf("UpdateIssue: %v", err)
		}
		got := mustGetIssue(t, fx.Store, id)
		if fx.UpdatesPersist {
			if got.Title != "after" || got.Body != "new body" {
				t.Fatalf("update did not persist: %+v", got)
			}
			if !got.UpdatedAt.After(beforeUpdated) {
				t.Fatalf("update did not bump updated_at: %v -> %v", beforeUpdated, got.UpdatedAt)
			}
		} else {
			// storyStore's documented no-op stub: success without persistence.
			if got.Title != beforeTitle || !got.UpdatedAt.Equal(beforeUpdated) {
				t.Fatalf("declared no-op update mutated fixture: before title/time %q/%v after %+v", beforeTitle, beforeUpdated, got)
			}
		}
	})

	t.Run("comment creation bumps parent", func(t *testing.T) {
		fx := factory(t)
		ctx := context.Background()
		id := fx.SeedIssue(t, store.IssueInput{Title: "comment parent"}, 0)
		fx.BackdateIssue(t, id, time.Hour)
		before := mustGetIssue(t, fx.Store, id)
		beforeUpdated := before.UpdatedAt

		cid, err := fx.Store.CreateComment(ctx, id, "hello", 0)
		if err != nil {
			t.Fatalf("CreateComment: %v", err)
		}
		if cid <= 0 {
			t.Fatalf("comment id = %d, want positive", cid)
		}
		if got := mustGetIssue(t, fx.Store, id); !got.UpdatedAt.After(beforeUpdated) {
			t.Fatalf("comment did not bump updated_at: %v -> %v", beforeUpdated, got.UpdatedAt)
		}

		_, err = fx.Store.CreateComment(ctx, 424242, "missing", 0)
		if fx.MissingCommentNotFound {
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("missing parent error = %v, want ErrNotFound", err)
			}
		} else if err != nil {
			t.Fatalf("declared create no-op should report success, got %v", err)
		}
	})

	t.Run("list filter semantics", func(t *testing.T) {
		fx := factory(t)
		ctx := context.Background()
		if fx.FiltersIgnored {
			all, allTotal, err := fx.Store.ListIssues(ctx, fx.WorkspaceID, store.ListFilter{Status: "all", PerPage: 100})
			if err != nil {
				t.Fatalf("unfiltered ListIssues: %v", err)
			}
			filtered, filteredTotal, err := fx.Store.ListIssues(ctx, fx.WorkspaceID, store.ListFilter{Status: "closed", Query: "no-such-term", LabelID: 999, Page: 99, PerPage: 1})
			if err != nil {
				t.Fatalf("filtered ListIssues: %v", err)
			}
			if filteredTotal != allTotal || !slices.Equal(issueIDs(filtered), issueIDs(all)) {
				t.Fatalf("declared filter-ignore stub changed result: total %d/%d ids %v/%v", filteredTotal, allTotal, issueIDs(filtered), issueIDs(all))
			}
			return
		}

		labelID, err := fx.Store.CreateLabel(ctx, fx.WorkspaceID, store.KindLabelIssue, store.LabelInput{Name: "contract-bug", Color: "#ff0000"})
		if err != nil {
			t.Fatalf("CreateLabel: %v", err)
		}
		openOld := fx.SeedIssue(t, store.IssueInput{Title: "Crash report", Body: "alpha", LabelIDs: []int64{labelID}}, 0)
		authorID := fx.SeedUser(t, "contract-author")
		openNew := fx.SeedIssue(t, store.IssueInput{Title: "Other export", Body: "csv alpha"}, authorID)
		closed := fx.SeedIssue(t, store.IssueInput{Title: "Crash closed", Body: "alpha", LabelIDs: []int64{labelID}}, 0)
		if err := fx.Store.SetIssueStatus(ctx, closed, store.StatusClosed); err != nil {
			t.Fatalf("close fixture: %v", err)
		}
		fx.BackdateIssue(t, openOld, 3*time.Hour)
		fx.BackdateIssue(t, openNew, 2*time.Hour)
		fx.BackdateIssue(t, closed, time.Hour)

		check := func(name string, filter store.ListFilter, wantTotal int, wantIDs ...int64) {
			t.Helper()
			issues, total, err := fx.Store.ListIssues(ctx, fx.WorkspaceID, filter)
			if err != nil {
				t.Fatalf("%s: ListIssues: %v", name, err)
			}
			if total != wantTotal || !slices.Equal(issueIDs(issues), wantIDs) {
				t.Fatalf("%s: total/ids = %d/%v, want %d/%v", name, total, issueIDs(issues), wantTotal, wantIDs)
			}
		}
		check("default open newest", store.ListFilter{}, 2, openNew, openOld)
		check("text term", store.ListFilter{Query: "crash"}, 1, openOld)
		check("terms are ANDed", store.ListFilter{Query: "alpha other"}, 1, openNew)
		check("is closed qualifier", store.ListFilter{Query: "crash is:closed"}, 1, closed)
		check("conflicting is qualifiers", store.ListFilter{Query: "is:open is:closed"}, 0)
		check("label qualifier", store.ListFilter{Query: "label:contract-bug"}, 1, openOld)
		check("author qualifier", store.ListFilter{Query: "author:contract-author"}, 1, openNew)
		check("label id", store.ListFilter{LabelID: labelID}, 1, openOld)
		check("oldest all", store.ListFilter{Status: "all", Sort: "oldest"}, 3, openOld, openNew, closed)
		check("total before pagination", store.ListFilter{Status: "all", Sort: "oldest", PerPage: 2, Page: 2}, 3, closed)

		other, otherTotal, err := fx.Store.ListIssues(ctx, fx.WorkspaceID+999, store.ListFilter{Status: "all", PerPage: 100})
		if err != nil {
			t.Fatalf("other workspace: ListIssues: %v", err)
		}
		if otherTotal != 0 || len(other) != 0 {
			t.Fatalf("other workspace total/ids = %d/%v, want empty", otherTotal, issueIDs(other))
		}
	})
}

func mustGetIssue(tb testing.TB, s IssueStore, id int64) *store.Issue {
	tb.Helper()
	i, err := s.GetIssue(context.Background(), id)
	if err != nil {
		tb.Fatalf("GetIssue(%d): %v", id, err)
	}
	return i
}

func issueIDs(issues []*store.Issue) []int64 {
	out := make([]int64, len(issues))
	for i, issue := range issues {
		out[i] = issue.ID
	}
	return out
}
