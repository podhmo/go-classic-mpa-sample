package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// DiscussionStore is the discussion answer slice of the store contract.
type DiscussionStore interface {
	CreateDiscussion(ctx context.Context, wsID int64, in store.DiscussionInput, authorID int64) (int64, error)
	GetDiscussion(ctx context.Context, id int64) (*store.Discussion, error)
	CreateDiscussionThread(ctx context.Context, discussionID int64, body string, authorID int64) (int64, error)
	ListDiscussionThreads(ctx context.Context, discussionID int64) ([]*store.DiscussionThread, error)
	SetThreadAnswer(ctx context.Context, discussionID, threadID int64, answer bool) error
}

// DiscussionFixture adapts one implementation to the answer contract.
// ThreadsSharePointers is an explicit fake/stub specification: web tests
// intentionally reuse mutable thread objects so answer-swap rendering sees
// the post-mutation state, while the SQLite store returns independent rows.
type DiscussionFixture struct {
	Store       DiscussionStore
	WorkspaceID int64

	SeedDiscussion     func(tb testing.TB, qa bool) int64
	SeedThread         func(tb testing.TB, discussionID int64, body string) int64
	BackdateDiscussion func(tb testing.TB, id int64, age time.Duration)

	ThreadsSharePointers    bool
	OpenCategoryAnswerCheck bool // false for stubs with no open-category thread fixture
}

// RunDiscussionContract runs the shared answer-mark behavior suite.
func RunDiscussionContract(t *testing.T, factory func(tb testing.TB) DiscussionFixture) {
	t.Helper()
	t.Run("answer movement errors and bump", func(t *testing.T) {
		fx := factory(t)
		ctx := context.Background()
		did := fx.SeedDiscussion(t, true)
		first := fx.SeedThread(t, did, "first answer")
		second := fx.SeedThread(t, did, "second answer")
		// Normalize fixture state without assuming a create implementation.
		if err := fx.Store.SetThreadAnswer(ctx, did, first, false); err != nil {
			t.Fatalf("normalize first unmarked: %v", err)
		}
		fx.BackdateDiscussion(t, did, time.Hour)
		beforeThreads, err := fx.Store.ListDiscussionThreads(ctx, did)
		if err != nil {
			t.Fatalf("ListDiscussionThreads before: %v", err)
		}
		sharedFirst := findThread(t, beforeThreads, first)
		if sharedFirst.IsAnswer {
			t.Fatalf("fixture normalization failed: thread %d still marked", first)
		}
		beforeDiscussion := mustGetDiscussion(t, fx.Store, did)
		beforeUpdated := beforeDiscussion.UpdatedAt

		if err := fx.Store.SetThreadAnswer(ctx, did, first, true); err != nil {
			t.Fatalf("mark first: %v", err)
		}
		if fx.ThreadsSharePointers {
			if !sharedFirst.IsAnswer {
				t.Fatal("declared shared thread pointer did not reflect answer mutation")
			}
		} else if sharedFirst.IsAnswer {
			t.Fatal("previously returned thread row unexpectedly mutated")
		}
		afterFirst, err := fx.Store.ListDiscussionThreads(ctx, did)
		if err != nil {
			t.Fatalf("ListDiscussionThreads after first mark: %v", err)
		}
		if !findThread(t, afterFirst, first).IsAnswer || findThread(t, afterFirst, second).IsAnswer {
			t.Fatalf("first mark did not select only thread %d", first)
		}

		if err := fx.Store.SetThreadAnswer(ctx, did, second, true); err != nil {
			t.Fatalf("mark second: %v", err)
		}
		threads, err := fx.Store.ListDiscussionThreads(ctx, did)
		if err != nil {
			t.Fatalf("ListDiscussionThreads after: %v", err)
		}
		var marked int64
		var count int
		for _, th := range threads {
			if th.IsAnswer {
				count++
				marked = th.ID
			}
		}
		if count != 1 || marked != second {
			t.Fatalf("answer mark = count %d/thread %d, want only thread %d", count, marked, second)
		}
		d := mustGetDiscussion(t, fx.Store, did)
		if !d.Answered || !d.UpdatedAt.After(beforeUpdated) {
			t.Fatalf("answered/updated_at = %v/%v, want true after %v", d.Answered, d.UpdatedAt, beforeUpdated)
		}

		if err := fx.Store.SetThreadAnswer(ctx, did, second, false); err != nil {
			t.Fatalf("unmark second: %v", err)
		}
		if d := mustGetDiscussion(t, fx.Store, did); d.Answered {
			t.Fatal("discussion still answered after unmark")
		}
		if err := fx.Store.SetThreadAnswer(ctx, did, 424242, true); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("missing thread error = %v, want ErrNotFound", err)
		}
		if err := fx.Store.SetThreadAnswer(ctx, 424242, first, true); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("missing discussion error = %v, want ErrNotFound", err)
		}

		if fx.OpenCategoryAnswerCheck {
			openID := fx.SeedDiscussion(t, false)
			openThread := fx.SeedThread(t, openID, "not answerable")
			if err := fx.Store.SetThreadAnswer(ctx, openID, openThread, true); !errors.Is(err, store.ErrNotAnswerable) {
				t.Fatalf("open-category answer error = %v, want ErrNotAnswerable", err)
			}
		}
	})
}

func findThread(tb testing.TB, threads []*store.DiscussionThread, id int64) *store.DiscussionThread {
	tb.Helper()
	for _, th := range threads {
		if th.ID == id {
			return th
		}
	}
	tb.Fatalf("thread %d missing from %+v", id, threads)
	return nil
}

func mustGetDiscussion(tb testing.TB, s DiscussionStore, id int64) *store.Discussion {
	tb.Helper()
	d, err := s.GetDiscussion(context.Background(), id)
	if err != nil {
		tb.Fatalf("GetDiscussion(%d): %v", id, err)
	}
	return d
}
