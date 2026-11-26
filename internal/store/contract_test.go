package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
	"github.com/podhmo/go-classic-mpa-sample/internal/storetest"
)

func TestIssueContract(t *testing.T) {
	storetest.RunIssueContract(t, func(tb testing.TB) storetest.IssueFixture {
		path := filepath.Join(tb.TempDir(), "contract.db")
		s, err := store.Open(path)
		if err != nil {
			tb.Fatalf("store.Open: %v", err)
		}
		tb.Cleanup(func() { s.Close() })
		db, err := sql.Open("sqlite", "file:"+path)
		if err != nil {
			tb.Fatalf("sql.Open: %v", err)
		}
		tb.Cleanup(func() { db.Close() })
		return storetest.IssueFixture{
			Store:       s,
			WorkspaceID: 1,
			SeedIssue: func(tb testing.TB, in store.IssueInput) int64 {
				tb.Helper()
				id, err := s.CreateIssue(context.Background(), 1, in, 0)
				if err != nil {
					tb.Fatalf("seed CreateIssue: %v", err)
				}
				return id
			},
			BackdateIssue: func(tb testing.TB, id int64, age time.Duration) {
				tb.Helper()
				ts := time.Now().UTC().Add(-age).Unix()
				if _, err := db.Exec("UPDATE issues SET created_at = ?, updated_at = ? WHERE id = ?", ts, ts, id); err != nil {
					tb.Fatalf("backdate: %v", err)
				}
			},
			UpdatesPersist:         true,
			MissingCommentNotFound: true,
		}
	})
}

func TestDiscussionContract(t *testing.T) {
	storetest.RunDiscussionContract(t, func(tb testing.TB) storetest.DiscussionFixture {
		path := filepath.Join(tb.TempDir(), "discussion-contract.db")
		s, err := store.Open(path)
		if err != nil {
			tb.Fatalf("store.Open: %v", err)
		}
		tb.Cleanup(func() { s.Close() })
		db, err := sql.Open("sqlite", "file:"+path)
		if err != nil {
			tb.Fatalf("sql.Open: %v", err)
		}
		tb.Cleanup(func() { db.Close() })
		category := func(name, slug, format string) int64 {
			tb.Helper()
			res, err := db.Exec("INSERT INTO discussion_categories(name, slug, description, format) VALUES(?, ?, '', ?)", name, slug, format)
			if err != nil {
				tb.Fatalf("insert category: %v", err)
			}
			id, err := res.LastInsertId()
			if err != nil {
				tb.Fatalf("category id: %v", err)
			}
			return id
		}
		openCategory := category("General", "general", store.CategoryFormatOpen)
		qaCategory := category("Q&A", "qa", store.CategoryFormatQA)
		return storetest.DiscussionFixture{
			Store:       s,
			WorkspaceID: 1,
			SeedDiscussion: func(tb testing.TB, qa bool) int64 {
				tb.Helper()
				categoryID := openCategory
				if qa {
					categoryID = qaCategory
				}
				id, err := s.CreateDiscussion(context.Background(), 1, store.DiscussionInput{Title: "contract", CategoryID: categoryID}, 0)
				if err != nil {
					tb.Fatalf("seed CreateDiscussion: %v", err)
				}
				return id
			},
			SeedThread: func(tb testing.TB, discussionID int64, body string) int64 {
				tb.Helper()
				id, err := s.CreateDiscussionThread(context.Background(), discussionID, body, 0)
				if err != nil {
					tb.Fatalf("seed CreateDiscussionThread: %v", err)
				}
				return id
			},
			BackdateDiscussion: func(tb testing.TB, id int64, age time.Duration) {
				tb.Helper()
				ts := time.Now().UTC().Add(-age).Unix()
				if _, err := db.Exec("UPDATE discussions SET created_at = ?, updated_at = ? WHERE id = ?", ts, ts, id); err != nil {
					tb.Fatalf("backdate discussion: %v", err)
				}
			},
			OpenCategoryAnswerCheck: true,
		}
	})
}
