//go:build story

// Page stories for the storybook preview (cmd/storybook). Each one calls a
// Renderer method with one data state and prints the response body, so a
// browser shows how the page's markup and CSS hold up without a database,
// a request, or real data. This file is excluded from ordinary builds by
// the story tag.

package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

var (
	storyTime  = time.Date(2026, 10, 30, 14, 30, 0, 0, time.UTC)
	storyTime2 = time.Date(2026, 11, 2, 9, 15, 0, 0, time.UTC)

	storyLabels = []*store.Label{
		{ID: 1, Name: "bug", Color: "#d73a4a"},
		{ID: 2, Name: "enhancement", Color: "#a2eeef"},
		{ID: 3, Name: "documentation", Color: "#0075ca"},
		{ID: 4, Name: "very-long-label-name-for-layout", Color: "#7057ff"},
	}

	storyIssues = []*store.Issue{
		{
			ID: 3, Title: "Long unbreakable title Anticonstitutionnellement-supercalifragilisticexpialidocious overflowing the row",
			Body:      "A very long single-line title to check clamping.",
			Status:    store.StatusOpen,
			CreatedAt: storyTime2, UpdatedAt: storyTime2,
			Labels: []store.Label{*storyLabels[0], *storyLabels[3]},
		},
		{
			ID: 2, Title: "Improve responsive layout",
			Body:      "The filter bar wraps poorly on narrow screens.",
			Status:    store.StatusOpen,
			CreatedAt: storyTime, UpdatedAt: storyTime2,
			Labels: []store.Label{*storyLabels[1]},
		},
		{
			ID: 1, Title: "Fix escaping in titles",
			Body:      "Done.",
			Status:    store.StatusClosed,
			CreatedAt: storyTime, UpdatedAt: storyTime,
			Labels: []store.Label{*storyLabels[0], *storyLabels[2]},
		},
	}

	storyIssue = &store.Issue{
		ID: 2, Title: "Improve responsive layout",
		Body:      "The **filter bar** wraps poorly on narrow screens.\n\n- [ ] check 360px\n- [ ] check `label` select\n\n> [!NOTE]\n> Single newlines should render as line breaks.",
		Status:    store.StatusOpen,
		CreatedAt: storyTime, UpdatedAt: storyTime2,
		Labels: []store.Label{*storyLabels[0], *storyLabels[1]},
	}

	storyComments = []*store.Comment{
		{ID: 1, IssueID: 2, Body: "Reproduced on Firefox at 360px.", CreatedAt: storyTime},
		{ID: 2, IssueID: 2, Body: "The select keeps its intrinsic width; see `min-width` on `.filter-field`.", CreatedAt: storyTime2},
	}
)

func printRec(rec *httptest.ResponseRecorder) {
	// Renderer.execute answers a template failure with http.Error (5xx);
	// surface that instead of printing the error body as a "successful" story.
	if rec.Code >= http.StatusInternalServerError {
		panic(fmt.Sprintf("render failed: %d %s", rec.Code, rec.Body.String()))
	}
	fmt.Print(rec.Body.String())
}

// The renderXxx helpers each wrap one Renderer method: a story body ends
// on one of these unqualified calls, so the storybook index can group a
// page's data-state variants under the render function's name.

func renderIssueList(status int, data issueListPage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderIssueList(rec, status, data)
	printRec(rec)
}

func renderIssueDetail(status int, data issueDetailPage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderIssueDetail(rec, status, data)
	printRec(rec)
}

func renderIssueForm(status int, data issueFormPage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderIssueForm(rec, status, data)
	printRec(rec)
}

func renderIssueDelete(status int, data issueDeletePage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderIssueDelete(rec, status, data)
	printRec(rec)
}

func renderError(status int, heading, message string) {
	rec := httptest.NewRecorder()
	NewRenderer().renderError(rec, status, heading, message)
	printRec(rec)
}

// StoryIssueList renders the issue list with several issues.
func StoryIssueList() {
	renderIssueList(http.StatusOK, issueListPage{
		Title:      "Issues",
		Issues:     storyIssues,
		Labels:     storyLabels,
		Filter:     store.ListFilter{Status: store.StatusOpen, Page: 1, PerPage: perPage},
		Total:      len(storyIssues),
		Page:       1,
		TotalPages: 1,
		RangeStart: 1,
		RangeEnd:   len(storyIssues),
	})
}

// StoryIssueListEmpty renders the issue list with no results.
func StoryIssueListEmpty() {
	renderIssueList(http.StatusOK, issueListPage{
		Title:           "Issues",
		Labels:          storyLabels,
		Filter:          store.ListFilter{Status: store.StatusOpen, Query: "zzz", Page: 1, PerPage: perPage},
		Page:            1,
		TotalPages:      0,
		HasActiveFilter: true,
	})
}

// StoryIssueListPaged renders a middle page of a long filtered list.
func StoryIssueListPaged() {
	renderIssueList(http.StatusOK, issueListPage{
		Title:           "Issues",
		Issues:          storyIssues,
		Labels:          storyLabels,
		Filter:          store.ListFilter{Status: "all", Sort: "oldest", LabelID: 1, Page: 3, PerPage: perPage},
		Total:           95,
		Page:            3,
		TotalPages:      5,
		RangeStart:      41,
		RangeEnd:        60,
		HasActiveFilter: true,
	})
}

// StoryIssueListSearch renders the list with a search term that matches
// issue titles — the search-highlight.js enhancement should mark them.
func StoryIssueListSearch() {
	renderIssueList(http.StatusOK, issueListPage{
		Title:           "Issues",
		Issues:          storyIssues,
		Labels:          storyLabels,
		Filter:          store.ListFilter{Status: "all", Query: "layout", Page: 1, PerPage: perPage},
		Total:           len(storyIssues),
		Page:            1,
		TotalPages:      1,
		RangeStart:      1,
		RangeEnd:        len(storyIssues),
		HasActiveFilter: true,
	})
}

// StoryIssueDetail renders the issue detail page with comments.
func StoryIssueDetail() {
	renderIssueDetail(http.StatusOK, issueDetailPage{
		Title:    "#2 Improve responsive layout",
		Issue:    storyIssue,
		Comments: storyComments,
	})
}

// StoryIssueDetailLong renders the detail page with a long comment thread
// so the back-to-top affordance can be exercised by scrolling.
func StoryIssueDetailLong() {
	var comments []*store.Comment
	for i := range 24 {
		comments = append(comments, &store.Comment{
			ID: int64(i + 1), IssueID: 2,
			Body:      fmt.Sprintf("Comment %d — a filler line to make the thread tall enough to scroll.", i+1),
			CreatedAt: storyTime.Add(time.Duration(i) * time.Hour),
		})
	}
	renderIssueDetail(http.StatusOK, issueDetailPage{
		Title:    "#2 Improve responsive layout",
		Issue:    storyIssue,
		Comments: comments,
	})
}

// StoryIssueDetailCommentError re-renders the detail page after a failed
// comment submission.
func StoryIssueDetailCommentError() {
	renderIssueDetail(http.StatusUnprocessableEntity, issueDetailPage{
		Title:        "#2 Improve responsive layout",
		Issue:        storyIssue,
		Comments:     storyComments,
		CommentDraft: "   ",
		CommentError: "Comment is required.",
	})
}

// StoryIssueFormNew renders the empty create form.
func StoryIssueFormNew() {
	renderIssueForm(http.StatusOK, issueFormPage{
		Title:       "New issue",
		Heading:     "New issue",
		Action:      "/issues",
		SubmitLabel: "Create issue",
		Labels:      storyLabels,
	})
}

// StoryIssueFormErrors re-renders the create form with validation errors.
func StoryIssueFormErrors() {
	renderIssueForm(http.StatusUnprocessableEntity, issueFormPage{
		Title:       "New issue",
		Heading:     "New issue",
		Action:      "/issues",
		SubmitLabel: "Create issue",
		Input:       store.IssueInput{Body: "title was empty"},
		Labels:      storyLabels,
		Errors:      issueFormErrors{Title: "Title is required."},
	})
}

// StoryIssueFormEdit renders the populated edit form.
func StoryIssueFormEdit() {
	renderIssueForm(http.StatusOK, issueFormPage{
		Title:       "Edit issue #2",
		Heading:     "Edit issue #2",
		Action:      "/issues/2/edit",
		SubmitLabel: "Save changes",
		Input:       store.IssueInput{Title: storyIssue.Title, Body: storyIssue.Body, LabelIDs: []int64{1, 2}},
		Labels:      storyLabels,
	})
}

// StoryIssueDelete renders the delete confirmation page.
func StoryIssueDelete() {
	renderIssueDelete(http.StatusOK, issueDeletePage{
		Title: "Delete issue #2",
		Issue: storyIssue,
	})
}

// StoryError404 renders the styled 404 page.
func StoryError404() {
	renderError(http.StatusNotFound, "Not found",
		"The page or issue you are looking for does not exist or may have been deleted.")
}

// storyStore implements the Store contract with story fixtures so
// handler-shaped stories drive the real mux — routing, security
// headers, status codes — without a database.
type storyStore struct{}

func (storyStore) ListIssues(ctx context.Context, f store.ListFilter) ([]*store.Issue, int, error) {
	return storyIssues, len(storyIssues), nil
}
func (storyStore) GetIssue(ctx context.Context, id int64) (*store.Issue, error) {
	for _, is := range storyIssues {
		if is.ID == id {
			return is, nil
		}
	}
	return nil, store.ErrNotFound
}
func (storyStore) CreateIssue(ctx context.Context, in store.IssueInput) (int64, error) {
	return 99, nil
}
func (storyStore) UpdateIssue(ctx context.Context, id int64, in store.IssueInput) error {
	return nil
}
func (storyStore) SetIssueStatus(ctx context.Context, id int64, status string) error {
	return nil
}
func (storyStore) DeleteIssue(ctx context.Context, id int64) error {
	return nil
}
func (storyStore) ListComments(ctx context.Context, issueID int64) ([]*store.Comment, error) {
	return storyComments, nil
}
func (storyStore) CreateComment(ctx context.Context, issueID int64, body string) (int64, error) {
	return 1, nil
}
func (storyStore) ListLabels(ctx context.Context) ([]*store.Label, error) {
	return storyLabels, nil
}

// StoryHttpIssueList drives the real mux for GET /issues, so the
// preview replays the handler's status and security headers too.
func StoryHttpIssueList(w http.ResponseWriter, r *http.Request) {
	New(storyStore{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/issues", nil))
}

// StoryHttpNotFound drives the real mux for an unknown path; the
// recorded 404 status is replayed by the preview, unlike print
// stories which always answer 200.
func StoryHttpNotFound(w http.ResponseWriter, r *http.Request) {
	New(storyStore{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/no-such-page", nil))
}
