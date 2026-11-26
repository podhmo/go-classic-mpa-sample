package web

import (
	"context"
	"testing"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
	"github.com/podhmo/go-classic-mpa-sample/internal/storetest"
)

func TestIssueContractFakeStore(t *testing.T) {
	storetest.RunIssueContract(t, func(tb testing.TB) storetest.IssueFixture {
		f := newFakeStore()
		return storetest.IssueFixture{
			Store:       f,
			WorkspaceID: 1,
			SeedIssue: func(tb testing.TB, in store.IssueInput) int64 {
				tb.Helper()
				id, err := f.CreateIssue(context.Background(), 1, in, 0)
				if err != nil {
					tb.Fatalf("seed CreateIssue: %v", err)
				}
				return id
			},
			BackdateIssue: func(tb testing.TB, id int64, age time.Duration) {
				tb.Helper()
				f.mu.Lock()
				defer f.mu.Unlock()
				ts := time.Now().Add(-age)
				f.issues[id].CreatedAt = ts
				f.issues[id].UpdatedAt = ts
			},
			UpdatesPersist:         true,
			MissingCommentNotFound: true,
		}
	})
}

func TestDiscussionContractFakeStore(t *testing.T) {
	storetest.RunDiscussionContract(t, func(tb testing.TB) storetest.DiscussionFixture {
		f := newFakeStore()
		return storetest.DiscussionFixture{
			Store:       f,
			WorkspaceID: 1,
			SeedDiscussion: func(tb testing.TB, qa bool) int64 {
				tb.Helper()
				categoryID := int64(1)
				if qa {
					categoryID = 2
				}
				id, err := f.CreateDiscussion(context.Background(), 1, store.DiscussionInput{Title: "contract", CategoryID: categoryID}, 0)
				if err != nil {
					tb.Fatalf("seed CreateDiscussion: %v", err)
				}
				return id
			},
			SeedThread: func(tb testing.TB, discussionID int64, body string) int64 {
				tb.Helper()
				id, err := f.CreateDiscussionThread(context.Background(), discussionID, body, 0)
				if err != nil {
					tb.Fatalf("seed CreateDiscussionThread: %v", err)
				}
				return id
			},
			BackdateDiscussion: func(tb testing.TB, id int64, age time.Duration) {
				tb.Helper()
				f.mu.Lock()
				defer f.mu.Unlock()
				ts := time.Now().Add(-age)
				f.discussions[id].CreatedAt = ts
				f.discussions[id].UpdatedAt = ts
			},
			ThreadsSharePointers:    true,
			OpenCategoryAnswerCheck: true,
		}
	})
}
