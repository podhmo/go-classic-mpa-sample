//go:build story

package web

import (
	"testing"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
	"github.com/podhmo/go-classic-mpa-sample/internal/storetest"
)

func TestIssueContractStoryStore(t *testing.T) {
	storetest.RunIssueContract(t, func(tb testing.TB) storetest.IssueFixture {
		s := newStoryStore()
		return storetest.IssueFixture{
			Store:       s,
			WorkspaceID: 1,
			SeedIssue: func(tb testing.TB, _ store.IssueInput) int64 {
				tb.Helper()
				return 2 // fixture issue used for mutation-only stories
			},
			BackdateIssue: func(tb testing.TB, id int64, age time.Duration) {
				tb.Helper()
				ts := storyNow().Add(-age)
				is := s.issues[id]
				is.CreatedAt = ts
				is.UpdatedAt = ts
			},
			FiltersIgnored:         true,
			UpdatesPersist:         false,
			MissingCommentNotFound: false,
		}
	})
}

func TestDiscussionContractStoryStore(t *testing.T) {
	storetest.RunDiscussionContract(t, func(tb testing.TB) storetest.DiscussionFixture {
		s := newStoryStore()
		threadCalls := 0
		return storetest.DiscussionFixture{
			Store:       s,
			WorkspaceID: 1,
			SeedDiscussion: func(tb testing.TB, qa bool) int64 {
				tb.Helper()
				if qa {
					return 2
				}
				return 1
			},
			SeedThread: func(tb testing.TB, discussionID int64, _ string) int64 {
				tb.Helper()
				if discussionID != 2 {
					tb.Fatalf("story fixture has no threads for discussion %d", discussionID)
				}
				threadCalls++
				if threadCalls == 1 {
					return 1
				}
				return 2
			},
			BackdateDiscussion: func(tb testing.TB, id int64, age time.Duration) {
				tb.Helper()
				ts := storyNow().Add(-age)
				d := s.discussions[id]
				d.CreatedAt = ts
				d.UpdatedAt = ts
			},
			ThreadsSharePointers:    true,
			OpenCategoryAnswerCheck: false,
		}
	})
}
