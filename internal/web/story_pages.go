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
	"net/url"
	"strings"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/gitrepo"
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

var (
	storyTime  = time.Date(2026, 10, 30, 14, 30, 0, 0, time.UTC)
	storyTime2 = time.Date(2026, 11, 2, 9, 15, 0, 0, time.UTC)
	storyTime3 = time.Date(2026, 11, 4, 18, 45, 0, 0, time.UTC)

	storyLabels = []*store.Label{
		{ID: 1, Name: "bug", Color: "#d73a4a", Kind: store.KindLabelIssue},
		{ID: 2, Name: "enhancement", Color: "#a2eeef", Kind: store.KindLabelIssue},
		{ID: 3, Name: "documentation", Color: "#0075ca", Kind: store.KindLabelIssue},
		{ID: 4, Name: "very-long-label-name-for-layout", Color: "#7057ff", Kind: store.KindLabelIssue, WorkspaceID: 1},
	}

	// Discussion labels are a separate kind — issue and discussion pickers
	// draw from different sets. One archived row exercises the management
	// page's archived section; it stays attached to the discussion that
	// carries it.
	storyDLabels = []*store.Label{
		{ID: 11, Name: "question", Color: "#d876e3", Kind: store.KindLabelDiscussion},
		{ID: 12, Name: "show-and-tell", Color: "#bfd4f2", Kind: store.KindLabelDiscussion, WorkspaceID: 1},
	}
	storyArchivedLabels = []*store.Label{
		{ID: 13, Name: "wontfix", Color: "#ffffff", Kind: store.KindLabelIssue, Archived: true},
		{ID: 14, Name: "old-announcement", Color: "#f9d0c4", Kind: store.KindLabelDiscussion, Archived: true},
	}

	storyIssues = []*store.Issue{
		{
			ID: 3, Title: "Long unbreakable title Anticonstitutionnellement-supercalifragilisticexpialidocious overflowing the row",
			WorkspaceID: 1,
			Body:        "A very long single-line title to check clamping.",
			Status:      store.StatusOpen,
			CreatedAt:   storyTime2, UpdatedAt: storyTime2,
			Labels: []store.Label{*storyLabels[0], *storyLabels[3]},
		},
		{
			ID: 2, Title: "Improve responsive layout",
			WorkspaceID: 1,
			Body:        "The filter bar wraps poorly on narrow screens.",
			Status:      store.StatusOpen,
			CreatedAt:   storyTime, UpdatedAt: storyTime2,
			Labels: []store.Label{*storyLabels[1]},
		},
		{
			ID: 1, Title: "Fix escaping in titles",
			WorkspaceID: 1,
			Body:        "Done.",
			Status:      store.StatusClosed,
			CreatedAt:   storyTime, UpdatedAt: storyTime,
			Labels: []store.Label{*storyLabels[0], *storyLabels[2]},
		},
	}

	storyIssue = &store.Issue{
		ID: 2, Title: "Improve responsive layout",
		WorkspaceID: 1,
		Body:        "The **filter bar** wraps poorly on narrow screens.\n\n- [ ] check 360px\n- [ ] check `label` select\n\n> [!NOTE]\n> Single newlines should render as line breaks.",
		Status:      store.StatusOpen,
		CreatedAt:   storyTime, UpdatedAt: storyTime2,
		Labels: []store.Label{*storyLabels[0], *storyLabels[1]},
	}

	storyComments = []*store.Comment{
		{ID: 1, IssueID: 2, Body: "Reproduced on Firefox at 360px.", CreatedAt: storyTime},
		{ID: 2, IssueID: 2, Body: "The select keeps its intrinsic width; see `min-width` on `.filter-field`.", CreatedAt: storyTime2},
	}

	storyCategories = []*store.DiscussionCategory{
		{ID: 1, Name: "Announcements", Slug: "announcements", Format: store.CategoryFormatOpen, Description: "Updates from the maintainers."},
		{ID: 2, Name: "General", Slug: "general", Format: store.CategoryFormatOpen, Description: "Anything related to the project."},
		{ID: 3, Name: "Ideas", Slug: "ideas", Format: store.CategoryFormatOpen, Description: "Share ideas for new features."},
		{ID: 4, Name: "Q&A", Slug: "qa", Format: store.CategoryFormatQA, Description: "Ask the community for help."},
	}

	storyDiscussions = []*store.Discussion{
		{
			ID: 3, Title: "Release v0.3 is out",
			WorkspaceID: 1,
			Body:        "Small announcement.",
			Category:    *storyCategories[0],
			CreatedAt:   storyTime2, UpdatedAt: storyTime2,
		},
		{
			ID: 2, Title: "How do custom labels get their color?",
			WorkspaceID: 1,
			Body:        "I cannot find where the palette is defined.",
			Category:    *storyCategories[3],
			CreatedAt:   storyTime, UpdatedAt: storyTime2,
			Labels:   []store.Label{*storyDLabels[0]},
			Answered: true,
		},
		{
			ID: 1, Title: "Idea: keyboard navigation for the filter bar",
			WorkspaceID: 1,
			Body:        "It would be nice to move between filters with arrow keys.",
			Category:    *storyCategories[2],
			CreatedAt:   storyTime, UpdatedAt: storyTime,
		},
	}

	storyDiscussion = &store.Discussion{
		ID: 2, Title: "How do custom labels get their color?",
		WorkspaceID: 1,
		Body:        "I cannot find where the **palette** is defined.",
		Category:    *storyCategories[3],
		CreatedAt:   storyTime, UpdatedAt: storyTime2,
		Labels:   []store.Label{*storyDLabels[0], *storyArchivedLabels[1]},
		Answered: true,
	}

	storyDThreads = []*store.DiscussionThread{
		{
			ID: 1, DiscussionID: 2, Body: "Look at `label_color` in the seed data.", CreatedAt: storyTime,
			IsAnswer: true,
			Comments: []*store.DiscussionComment{
				{ID: 3, ThreadID: 1, Body: "That was it — thank you!", CreatedAt: storyTime2},
			},
		},
		{ID: 2, DiscussionID: 2, Body: "Same question on Firefox.", CreatedAt: storyTime2,
			Comments: []*store.DiscussionComment{
				{ID: 4, ThreadID: 2, Body: "On Firefox the dialog never opens.", CreatedAt: storyTime2},
				{ID: 5, ThreadID: 2, Body: "That's a known `showModal()` quirk — see #12.", CreatedAt: storyTime3},
				{ID: 6, ThreadID: 2, Body: "Workaround: fall back to `hidden` toggling.", CreatedAt: storyTime3},
			},
		},
	}

	// Moderated variants exercise the edited marker and the hidden
	// state (reason note + collapsed body) — the plain fixtures stay
	// for the default detail stories.
	storyCommentsModerated = []*store.Comment{
		{ID: 1, IssueID: 2, Body: "Reproduced on Firefox at 360px.", CreatedAt: storyTime,
			Moderation: store.Moderation{EditedAt: storyTime2}},
		{ID: 2, IssueID: 2, Body: "The select keeps its intrinsic width; see `min-width` on `.filter-field`.", CreatedAt: storyTime2},
		{ID: 3, IssueID: 2, Body: "BUY MY COINS NOW!!!", CreatedAt: storyTime3,
			Moderation: store.Moderation{Hidden: true, HiddenReason: store.HideReasonSpam}},
	}

	storyDThreadsModerated = []*store.DiscussionThread{
		{
			ID: 1, DiscussionID: 2, Body: "Look at `label_color` in the seed data.", CreatedAt: storyTime,
			IsAnswer:   true,
			Moderation: store.Moderation{EditedAt: storyTime2},
			Comments: []*store.DiscussionComment{
				{ID: 3, ThreadID: 1, Body: "That was it — thank you!", CreatedAt: storyTime2},
			},
		},
		{
			ID: 2, DiscussionID: 2, Body: "Same question on Firefox.", CreatedAt: storyTime2,
			Moderation: store.Moderation{Hidden: true, HiddenReason: store.HideReasonDuplicate},
			Comments: []*store.DiscussionComment{
				{ID: 4, ThreadID: 2, Body: "On Firefox the dialog never opens.", CreatedAt: storyTime2},
				{ID: 5, ThreadID: 2, Body: "That's a known `showModal()` quirk — see #12.", CreatedAt: storyTime3,
					Moderation: store.Moderation{Hidden: true, HiddenReason: store.HideReasonOffTopic}},
			},
		},
	}
)

// storyNow is "now" for fixtures that want timestamps near the present
// (pull requests, edited markers). Pinned — not time.Now() — so storybook
// screenshots stay byte-comparable across runs for visual-diff. A function
// so a different clock can be swapped in later without touching call sites.
func storyNow() time.Time {
	return time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
}

func printRec(rec *httptest.ResponseRecorder) {
	// Renderer execution answers a template failure with http.Error (5xx);
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
	// Stories render in the default locale; the ja set is covered by the
	// locale tests and the real server.
	NewRenderer().renderIssueList(rec, status, data, supportedLangs[0])
	printRec(rec)
}

func renderIssueDetail(status int, data issueDetailPage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderIssueDetail(rec, status, data, supportedLangs[0])
	printRec(rec)
}

func renderIssueForm(status int, data issueFormPage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderIssueForm(rec, status, data, supportedLangs[0])
	printRec(rec)
}

func renderIssueDelete(status int, data issueDeletePage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderIssueDelete(rec, status, data, supportedLangs[0])
	printRec(rec)
}

func renderError(status int, heading, message string) {
	rec := httptest.NewRecorder()
	NewRenderer().renderError(rec, status, heading, message)
	printRec(rec)
}

func renderDiscussionList(status int, data discussionListPage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderDiscussionList(rec, status, data, supportedLangs[0])
	printRec(rec)
}

func renderDiscussionDetail(status int, data discussionDetailPage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderDiscussionDetail(rec, status, data, supportedLangs[0])
	printRec(rec)
}

func renderDiscussionForm(status int, data discussionFormPage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderDiscussionForm(rec, status, data, supportedLangs[0])
	printRec(rec)
}

func renderDiscussionDelete(status int, data discussionDeletePage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderDiscussionDelete(rec, status, data, supportedLangs[0])
	printRec(rec)
}

func renderLabels(status int, data labelsPage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderLabels(rec, status, data, supportedLangs[0])
	printRec(rec)
}

func renderLabelForm(status int, data labelFormPage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderLabelForm(rec, status, data, supportedLangs[0])
	printRec(rec)
}

func renderCommentEdit(status int, data commentEditPage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderCommentEdit(rec, status, data, supportedLangs[0])
	printRec(rec)
}

func renderCommentDelete(status int, data commentDeletePage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderCommentDelete(rec, status, data, supportedLangs[0])
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
		Workspace:  storyWorkspace,
		Section:    "issues",
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
		Workspace:       storyWorkspace,
		Section:         "issues",
	})
}

// StoryIssueListPaged renders a middle page of a long filtered list.
func StoryIssueListPaged() {
	renderIssueList(http.StatusOK, issueListPage{
		Title:      "Issues",
		Issues:     storyIssues,
		Labels:     storyLabels,
		Filter:     store.ListFilter{Status: "all", Sort: "oldest", LabelID: 1, Page: 3, PerPage: perPage},
		Total:      95,
		Page:       3,
		TotalPages: 5,
		Pager: pagerData{
			Page:       3,
			TotalPages: 5,
			PrevURL:    "/w/default/issues?label=1&page=2&sort=oldest&status=all",
			NextURL:    "/w/default/issues?label=1&page=4&sort=oldest&status=all",
		},
		RangeStart:      41,
		RangeEnd:        60,
		HasActiveFilter: true,
		Workspace:       storyWorkspace,
		Section:         "issues",
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
		Workspace:       storyWorkspace,
		Section:         "issues",
	})
}

// StoryIssueDetail renders the issue detail page with comments.
func StoryIssueDetail() {
	renderIssueDetail(http.StatusOK, issueDetailPage{
		Title:     "#2 Improve responsive layout",
		Issue:     storyIssue,
		Comments:  storyComments,
		Workspace: storyWorkspace,
		Section:   "issues",
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
		Title:     "#2 Improve responsive layout",
		Issue:     storyIssue,
		Comments:  comments,
		Workspace: storyWorkspace,
		Section:   "issues",
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
		CommentError: "Body is required.",
		Workspace:    storyWorkspace,
		Section:      "issues",
	})
}

// StoryIssueDetailEmpty renders the detail page with no comments: the
// empty list state plus the bottom comment form's initial state.
func StoryIssueDetailEmpty() {
	renderIssueDetail(http.StatusOK, issueDetailPage{
		Title:     "#2 Improve responsive layout",
		Issue:     storyIssue,
		Workspace: storyWorkspace,
		Section:   "issues",
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
		Workspace:   storyWorkspace,
		Section:     "issues",
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
		Workspace:   storyWorkspace,
		Section:     "issues",
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
		Workspace:   storyWorkspace,
		Section:     "issues",
	})
}

// StoryIssueDelete renders the delete confirmation page.
func StoryIssueDelete() {
	renderIssueDelete(http.StatusOK, issueDeletePage{
		Title:     "Delete issue #2",
		Issue:     storyIssue,
		Workspace: storyWorkspace,
		Section:   "issues",
	})
}

// StoryIssueDetailModerated renders the detail page with an edited
// comment and a hidden one — the "edited" marker, the reason note and
// the collapsed body, plus the Unhide row in place of the hide picker.
func StoryIssueDetailModerated() {
	renderIssueDetail(http.StatusOK, issueDetailPage{
		Title:     "#2 Improve responsive layout",
		Issue:     storyIssue,
		Comments:  storyCommentsModerated,
		Workspace: storyWorkspace,
		Section:   "issues",
	})
}

// StoryCommentEdit renders the shared comment edit page (the issue
// comment flavor — threads and replies use the same template).
func StoryCommentEdit() {
	renderCommentEdit(http.StatusOK, commentEditPage{
		Title:     "Edit comment #2",
		Heading:   "Edit comment #2",
		Action:    "/w/default/issues/2/comments/2/edit",
		CancelURL: "/w/default/issues/2#comment-2",
		Body:      storyComments[1].Body,
		Workspace: storyWorkspace,
		Section:   "issues",
	})
}

// StoryCommentDelete renders the shared delete-confirm page — a thread
// fixture so the "and its N replies" warning is exercised too.
func StoryCommentDelete() {
	renderCommentDelete(http.StatusOK, commentDeletePage{
		Title:     "Delete thread #2",
		Heading:   "Delete thread #2",
		Noun:      "thread",
		Preview:   excerpt(storyDThreads[1].Body),
		Replies:   len(storyDThreads[1].Comments),
		Action:    "/w/default/discussions/2/threads/2/delete",
		CancelURL: "/w/default/discussions/2#thread-2",
		Workspace: storyWorkspace,
		Section:   "discussions",
	})
}

// StoryError404 renders the styled 404 page.
func StoryError404() {
	renderError(http.StatusNotFound, "Not found",
		"The page or issue you are looking for does not exist or may have been deleted.")
}

// StoryDiscussionList renders the discussion list with several entries,
// one of them answered.
func StoryDiscussionList() {
	renderDiscussionList(http.StatusOK, discussionListPage{
		Title:       "Discussions",
		Discussions: storyDiscussions,
		Categories:  storyCategories,
		Filter:      store.DiscussionListFilter{Page: 1, PerPage: perPage},
		Total:       len(storyDiscussions),
		Page:        1,
		TotalPages:  1,
		RangeStart:  1,
		RangeEnd:    len(storyDiscussions),
		Workspace:   storyWorkspace,
		Section:     "discussions",
	})
}

// StoryDiscussionListEmpty renders the filtered discussion list with no
// results.
func StoryDiscussionListEmpty() {
	renderDiscussionList(http.StatusOK, discussionListPage{
		Title:           "Discussions",
		Categories:      storyCategories,
		Filter:          store.DiscussionListFilter{CategorySlug: "qa", Answered: "yes", Query: "zzz", Page: 1, PerPage: perPage},
		Page:            1,
		HasActiveFilter: true,
		Workspace:       storyWorkspace,
		Section:         "discussions",
	})
}

// StoryDiscussionDetail renders the Q&A detail page: an answer-marked
// thread with a reply, plus a second thread with a reply chain.
func StoryDiscussionDetail() {
	renderDiscussionDetail(http.StatusOK, discussionDetailPage{
		Title:      "#2 How do custom labels get their color?",
		Discussion: storyDiscussion,
		Threads:    storyDThreads,
		Workspace:  storyWorkspace,
		Section:    "discussions",
	})
}

// StoryDiscussionDetailReplyOpen renders the reply form expanded: the
// server opens a thread's <details> only when re-rendering a rejected
// reply draft, which is also how the Cancel/Comment action row and the
// reply field error get their visual check.
func StoryDiscussionDetailReplyOpen() {
	renderDiscussionDetail(http.StatusUnprocessableEntity, discussionDetailPage{
		Title:         "#2 How do custom labels get their color?",
		Discussion:    storyDiscussion,
		Threads:       storyDThreads,
		ThreadDraftID: 1,
		CommentDraft:  "   ",
		CommentError:  "Body is required.",
		Workspace:     storyWorkspace,
		Section:       "discussions",
	})
}

// StoryDiscussionDetailLong renders enough threads for the page to
// scroll, so the fixed back-to-top affordance (and its narrow-viewport
// placement) can be exercised.
func StoryDiscussionDetailLong() {
	var threads []*store.DiscussionThread
	for i := range 24 {
		threads = append(threads, &store.DiscussionThread{
			ID: int64(i + 1), DiscussionID: 2,
			Body:      fmt.Sprintf("Thread %d — a filler line to make the page tall enough to scroll.", i+1),
			CreatedAt: storyTime.Add(time.Duration(i) * time.Hour),
		})
	}
	renderDiscussionDetail(http.StatusOK, discussionDetailPage{
		Title:      "#2 How do custom labels get their color?",
		Discussion: storyDiscussion,
		Threads:    threads,
		Workspace:  storyWorkspace,
		Section:    "discussions",
	})
}

// StoryDiscussionFormNew renders the empty create form with categories.
func StoryDiscussionFormNew() {
	renderDiscussionForm(http.StatusOK, discussionFormPage{
		Title:       "New discussion",
		Heading:     "New discussion",
		Action:      "/discussions",
		SubmitLabel: "Create discussion",
		Categories:  storyCategories,
		Labels:      storyDLabels,
		Workspace:   storyWorkspace,
		Section:     "discussions",
	})
}

// StoryDiscussionFormErrors re-renders the create form with a missing
// title and no category selected.
func StoryDiscussionFormErrors() {
	renderDiscussionForm(http.StatusUnprocessableEntity, discussionFormPage{
		Title:       "New discussion",
		Heading:     "New discussion",
		Action:      "/discussions",
		SubmitLabel: "Create discussion",
		Input:       store.DiscussionInput{Body: "title was empty"},
		Categories:  storyCategories,
		Labels:      storyDLabels,
		Errors:      discussionFormErrors{Title: "Title is required.", Category: "Please pick a category."},
		Workspace:   storyWorkspace,
		Section:     "discussions",
	})
}

// StoryDiscussionDelete renders the delete confirmation page.
func StoryDiscussionDelete() {
	renderDiscussionDelete(http.StatusOK, discussionDeletePage{
		Title:      "Delete discussion #2",
		Discussion: storyDiscussion,
		Workspace:  storyWorkspace,
		Section:    "discussions",
	})
}

// StoryLabels renders the label management page: both kinds, the
// shared/workspace scope tags and the archived section.
func StoryLabels() {
	renderLabels(http.StatusOK, labelsPage{
		Title:            "Labels",
		IssueLabels:      storyLabels,
		DiscussionLabels: storyDLabels,
		ArchivedLabels:   storyArchivedLabels,
		Workspace:        storyWorkspace,
		Section:          "labels",
	})
}

// StoryLabelsCreateError re-renders the management page after a failed
// create — the draft values stay in the form with the field errors.
func StoryLabelsCreateError() {
	renderLabels(http.StatusUnprocessableEntity, labelsPage{
		Title:            "Labels",
		IssueLabels:      storyLabels,
		DiscussionLabels: storyDLabels,
		ArchivedLabels:   storyArchivedLabels,
		Input:            store.LabelInput{Name: "bug", Color: "#ff0000"},
		Kind:             "issue",
		Scope:            "workspace",
		Errors:           labelFormErrors{Other: "A label with this name already exists in that scope."},
		Workspace:        storyWorkspace,
		Section:          "labels",
	})
}

// StoryLabelFormEdit renders the label edit form.
func StoryLabelFormEdit() {
	renderLabelForm(http.StatusOK, labelFormPage{
		Title:     "Edit label bug",
		Heading:   "Edit label bug",
		Action:    "/w/default/labels/1/edit",
		Label:     storyLabels[0],
		Input:     store.LabelInput{Name: storyLabels[0].Name, Color: storyLabels[0].Color},
		Workspace: storyWorkspace,
		Section:   "labels",
	})
}

// storyStore implements the Store contract with story fixtures so
// handler-shaped stories drive the real mux — routing, security
// headers, status codes — without a database. newStoryStore gives each
// replayed request its own deep copy of the comment fixtures so
// mutations (e.g. answer marks) are visible inside that request but
// never leak into the shared fixtures.
//
// Declared simplifications vs the real store: list methods return the
// fixtures ignoring filters and pagination; ListDiscussionThreads
// falls back to the fixture threads for an unknown discussion id;
// create, update and delete are no-ops that report success (posts get
// id 99); authentication always succeeds as the demo user and API
// tokens are never valid.
type storyStore struct {
	Store
	issues      map[int64]*store.Issue
	comments    map[int64][]*store.Comment
	discussions map[int64]*store.Discussion
	dthreads    map[int64][]*store.DiscussionThread
}

func newStoryStore() storyStore {
	issues := make(map[int64]*store.Issue, len(storyIssues))
	for _, i := range storyIssues {
		ii := *i
		issues[i.ID] = &ii
	}
	discussions := make(map[int64]*store.Discussion, len(storyDiscussions))
	for _, d := range storyDiscussions {
		dd := *d
		discussions[d.ID] = &dd
	}
	comments := make(map[int64][]*store.Comment, len(storyComments))
	for _, c := range storyComments {
		cc := *c
		comments[c.IssueID] = append(comments[c.IssueID], &cc)
	}
	return storyStore{
		issues:      issues,
		comments:    comments,
		discussions: discussions,
		dthreads: map[int64][]*store.DiscussionThread{
			storyDiscussion.ID: cloneDThreads(storyDThreads),
		},
	}
}

func cloneDThreads(src []*store.DiscussionThread) []*store.DiscussionThread {
	out := make([]*store.DiscussionThread, len(src))
	for i, t := range src {
		tt := *t
		tt.Comments = cloneDComments(t.Comments)
		out[i] = &tt
	}
	return out
}

func cloneDComments(src []*store.DiscussionComment) []*store.DiscussionComment {
	out := make([]*store.DiscussionComment, len(src))
	for i, c := range src {
		cc := *c
		out[i] = &cc
	}
	return out
}

func (storyStore) ListIssues(ctx context.Context, wsID int64, f store.ListFilter) ([]*store.Issue, int, error) {
	return storyIssues, len(storyIssues), nil
}
func (s storyStore) GetIssue(ctx context.Context, id int64) (*store.Issue, error) {
	if is, ok := s.issues[id]; ok {
		return is, nil
	}
	return nil, store.ErrNotFound
}
func (s storyStore) CreateIssue(ctx context.Context, wsID int64, in store.IssueInput, authorID int64) (int64, error) {
	return 99, nil
}
func (storyStore) UpdateIssue(ctx context.Context, id int64, in store.IssueInput) error {
	return nil
}
func (s storyStore) SetIssueStatus(ctx context.Context, id int64, status string) error {
	// Same contract as the real store: the value is validated before the
	// existence check.
	if status != store.StatusOpen && status != store.StatusClosed {
		return fmt.Errorf("store: invalid status %q", status)
	}
	is, ok := s.issues[id]
	if !ok {
		return store.ErrNotFound
	}
	// Same contract as the real store: updated_at bumps only on change.
	if is.Status != status {
		is.Status = status
		is.UpdatedAt = storyNow()
	}
	return nil
}
func (storyStore) DeleteIssue(ctx context.Context, id int64) error {
	return nil
}
func (s storyStore) ListComments(ctx context.Context, issueID int64) ([]*store.Comment, error) {
	// The request-scoped copy — moderation mutations stay visible to the
	// same replayed request (the hide-partial story relies on it).
	if cs, ok := s.comments[issueID]; ok {
		return cs, nil
	}
	return storyComments, nil
}
func (s storyStore) CreateComment(ctx context.Context, issueID int64, body string, authorID int64) (int64, error) {
	// An id outside the fixture range so a fragment inserted by the app
	// mount is distinguishable from fixture comments.
	// Same contract as the real store: a new comment bumps updated_at,
	// which the append fragment's meta node reflects.
	if is, ok := s.issues[issueID]; ok {
		is.UpdatedAt = storyNow()
	}
	return 99, nil
}

// Comment moderation on the request-scoped fixture copy, same
// ErrNotFound contract as the real store.
func (s storyStore) UpdateComment(ctx context.Context, issueID, commentID int64, body string) error {
	c := findComment(s.comments[issueID], commentID)
	if c == nil {
		return store.ErrNotFound
	}
	if c.Body != body {
		c.Body = body
		c.EditedAt = storyNow()
	}
	return nil
}
func (s storyStore) DeleteComment(ctx context.Context, issueID, commentID int64) error {
	if findComment(s.comments[issueID], commentID) == nil {
		return store.ErrNotFound
	}
	return nil
}
func (s storyStore) SetCommentHidden(ctx context.Context, issueID, commentID int64, reason string) error {
	c := findComment(s.comments[issueID], commentID)
	if c == nil {
		return store.ErrNotFound
	}
	if reason != "" && !store.ValidHideReason(reason) {
		return store.ErrInvalidHideReason
	}
	c.Hidden = reason != ""
	c.HiddenReason = reason
	return nil
}
func (s storyStore) UpdateDiscussionThread(ctx context.Context, discussionID, threadID int64, body string) error {
	t := findThread(s.dthreads[discussionID], threadID)
	if t == nil {
		return store.ErrNotFound
	}
	if t.Body != body {
		t.Body = body
		t.EditedAt = storyNow()
	}
	return nil
}
func (s storyStore) DeleteDiscussionThread(ctx context.Context, discussionID, threadID int64) error {
	if findThread(s.dthreads[discussionID], threadID) == nil {
		return store.ErrNotFound
	}
	return nil
}
func (s storyStore) SetDiscussionThreadHidden(ctx context.Context, discussionID, threadID int64, reason string) error {
	t := findThread(s.dthreads[discussionID], threadID)
	if t == nil {
		return store.ErrNotFound
	}
	if reason != "" && !store.ValidHideReason(reason) {
		return store.ErrInvalidHideReason
	}
	t.Hidden = reason != ""
	t.HiddenReason = reason
	return nil
}
func (s storyStore) UpdateDiscussionComment(ctx context.Context, discussionID, threadID, commentID int64, body string) error {
	t := findThread(s.dthreads[discussionID], threadID)
	if t == nil {
		return store.ErrNotFound
	}
	c := findReply(t, commentID)
	if c == nil {
		return store.ErrNotFound
	}
	if c.Body != body {
		c.Body = body
		c.EditedAt = storyNow()
	}
	return nil
}
func (s storyStore) DeleteDiscussionComment(ctx context.Context, discussionID, threadID, commentID int64) error {
	t := findThread(s.dthreads[discussionID], threadID)
	if t == nil || findReply(t, commentID) == nil {
		return store.ErrNotFound
	}
	return nil
}
func (s storyStore) SetDiscussionCommentHidden(ctx context.Context, discussionID, threadID, commentID int64, reason string) error {
	t := findThread(s.dthreads[discussionID], threadID)
	if t == nil {
		return store.ErrNotFound
	}
	c := findReply(t, commentID)
	if c == nil {
		return store.ErrNotFound
	}
	if reason != "" && !store.ValidHideReason(reason) {
		return store.ErrInvalidHideReason
	}
	c.Hidden = reason != ""
	c.HiddenReason = reason
	return nil
}
func (storyStore) ListLabels(ctx context.Context, wsID int64, kind string) ([]*store.Label, error) {
	var out []*store.Label
	for _, l := range append(append([]*store.Label{}, storyLabels...), storyDLabels...) {
		if l.Kind == kind && !l.Archived && (l.WorkspaceID == 0 || l.WorkspaceID == wsID) {
			out = append(out, l)
		}
	}
	return out, nil
}

func (storyStore) ListArchivedLabels(ctx context.Context, wsID int64, kind string) ([]*store.Label, error) {
	var out []*store.Label
	for _, l := range storyArchivedLabels {
		if l.Kind == kind && (l.WorkspaceID == 0 || l.WorkspaceID == wsID) {
			out = append(out, l)
		}
	}
	return out, nil
}

func (storyStore) GetLabel(ctx context.Context, id int64) (*store.Label, error) {
	for _, l := range append(append(append([]*store.Label{}, storyLabels...), storyDLabels...), storyArchivedLabels...) {
		if l.ID == id {
			return l, nil
		}
	}
	return nil, store.ErrNotFound
}

func (storyStore) CreateLabel(ctx context.Context, wsID int64, kind string, in store.LabelInput) (int64, error) {
	panic("storybook: CreateLabel not implemented")
}

func (storyStore) UpdateLabel(ctx context.Context, id int64, in store.LabelInput) error {
	panic("storybook: UpdateLabel not implemented")
}

func (storyStore) ArchiveLabel(ctx context.Context, id int64, archived bool) error {
	panic("storybook: ArchiveLabel not implemented")
}

func (s storyStore) ListDiscussions(ctx context.Context, wsID int64, f store.DiscussionListFilter) ([]*store.Discussion, int, error) {
	out := make([]*store.Discussion, len(storyDiscussions))
	for i, d := range storyDiscussions {
		dd := *d
		dd.Answered = s.answered(d.ID)
		out[i] = &dd
	}
	return out, len(out), nil
}
func (s storyStore) GetDiscussion(ctx context.Context, id int64) (*store.Discussion, error) {
	if d, ok := s.discussions[id]; ok {
		dd := *d
		dd.Answered = s.answered(id)
		return &dd, nil
	}
	return nil, store.ErrNotFound
}

// answered recomputes a discussion's Answered flag from the thread
// fixtures — the same derivation the real store's EXISTS clause performs.
func (s storyStore) answered(discussionID int64) bool {
	for _, t := range s.dthreads[discussionID] {
		if t.IsAnswer {
			return true
		}
	}
	return false
}
func (s storyStore) CreateDiscussion(ctx context.Context, wsID int64, in store.DiscussionInput, authorID int64) (int64, error) {
	return 99, nil
}
func (storyStore) UpdateDiscussion(ctx context.Context, id int64, in store.DiscussionInput) error {
	return nil
}
func (storyStore) DeleteDiscussion(ctx context.Context, id int64) error {
	return nil
}
func (s storyStore) ListDiscussionThreads(ctx context.Context, discussionID int64) ([]*store.DiscussionThread, error) {
	if ts, ok := s.dthreads[discussionID]; ok {
		return ts, nil
	}
	return storyDThreads, nil
}
func (s storyStore) CreateDiscussionThread(ctx context.Context, discussionID int64, body string, authorID int64) (int64, error) {
	// Same contract as the real store: a new thread bumps updated_at.
	if d, ok := s.discussions[discussionID]; ok {
		d.UpdatedAt = storyNow()
	}
	return 99, nil
}
func (s storyStore) CreateDiscussionComment(ctx context.Context, discussionID, threadID int64, body string, authorID int64) (int64, error) {
	// Same contract as the real store: a new comment bumps updated_at.
	if d, ok := s.discussions[discussionID]; ok {
		d.UpdatedAt = storyNow()
	}
	return 99, nil
}
func (s storyStore) SetThreadAnswer(ctx context.Context, discussionID, threadID int64, answer bool) error {
	d, dok := s.discussions[discussionID]
	ts := s.dthreads[discussionID]
	if !dok || ts == nil {
		return store.ErrNotFound
	}
	var target *store.DiscussionThread
	for _, t := range ts {
		if t.ID == threadID {
			target = t
		}
	}
	if target == nil {
		return store.ErrNotFound
	}
	if answer && d.Category.Format != store.CategoryFormatQA {
		return store.ErrNotAnswerable
	}
	if answer {
		for _, t := range ts {
			t.IsAnswer = false
		}
	}
	target.IsAnswer = answer
	// Same contract as the real store: an answer mark bumps the
	// discussion's updated_at, which the swap fragment reflects.
	d.UpdatedAt = storyNow()
	return nil
}
func (storyStore) ListDiscussionCategories(ctx context.Context) ([]*store.DiscussionCategory, error) {
	return storyCategories, nil
}
func (storyStore) CreateUser(ctx context.Context, name, passwordHash string) (int64, error) {
	return 1, nil
}
func (storyStore) AuthenticateUser(ctx context.Context, name, password string) (*store.User, error) {
	return &store.User{ID: 1, Name: name}, nil
}
func (storyStore) CreateSession(ctx context.Context, userID int64, ttl time.Duration) (string, error) {
	return "story-token", nil
}
func (storyStore) SessionUser(ctx context.Context, token string, ttl time.Duration) (*store.User, error) {
	return &store.User{ID: 1, Name: "demo"}, nil
}
func (storyStore) DeleteSession(ctx context.Context, token string) error {
	return nil
}
func (storyStore) APITokenUser(ctx context.Context, token string) (*store.User, error) {
	return nil, store.ErrNotFound
}

// StoryHttpIssueList drives the real mux for GET /issues, so the
// preview replays the handler's status and security headers too.
func StoryHttpIssueList(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/issues", nil))
}

// StoryHttpNotFound drives the real mux for an unknown path; the
// recorded 404 status is replayed by the preview, unlike print
// stories which always answer 200.
func StoryHttpNotFound(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/no-such-page", nil))
}

// renderFragment prints one named template fragment the way a partial
// POST would return it — the markup partial-submit.js swaps in.
func renderFragment(page, name string, status int, data any) {
	rec := httptest.NewRecorder()
	NewRenderer().renderFragment(rec, page, name, status, data, supportedLangs[0])
	printRec(rec)
}

// StoryIssueCommentFragment is the <li> a successful partial comment
// POST returns for an issue — appended to .comment-list by the swap.
func StoryIssueCommentFragment() {
	renderFragment("issue_detail", "issue_comment", http.StatusOK,
		dict("C", storyComments[1], "Workspace", storyWorkspace))
}

// StoryIssueCommentHiddenFragment is the hidden-state <li> a partial
// hide POST swaps over the visible comment.
func StoryIssueCommentHiddenFragment() {
	renderFragment("issue_detail", "issue_comment", http.StatusOK,
		dict("C", storyCommentsModerated[2], "Workspace", storyWorkspace))
}

// StoryDiscussionDetailModerated renders the discussion detail with a
// hidden thread and a hidden reply (the thread's replies stay visible —
// they are other posts).
func StoryDiscussionDetailModerated() {
	renderDiscussionDetail(http.StatusOK, discussionDetailPage{
		Title:      "#2 How do custom labels get their color?",
		Discussion: storyDiscussion,
		Threads:    storyDThreadsModerated,
		Workspace:  storyWorkspace,
		Section:    "discussions",
	})
}

// StoryIssueCommentFormFragment is the error-marked form a 422 partial
// response swaps back in place of the submitted one.
func StoryIssueCommentFormFragment() {
	renderFragment("issue_detail", "issue_comment_form", http.StatusUnprocessableEntity, issueDetailPage{
		Issue:        storyIssue,
		CommentDraft: "kept as the draft",
		CommentError: "Body is required.",
		Workspace:    storyWorkspace,
	})
}

// StoryDiscussionCommentFragment renders a top-level discussion comment
// <li> — collapsed reply form and qa answer button included.
func StoryDiscussionCommentFragment() {
	renderFragment("discussion_detail", "discussion_comment", http.StatusOK,
		dict("Discussion", storyDiscussion, "C", storyDThreads[1], "Workspace", storyWorkspace))
}

// StoryDiscussionReplyFragment renders the reply <li> appended to the
// parent's .comment-replies by a partial reply POST.
func StoryDiscussionReplyFragment() {
	renderFragment("discussion_detail", "discussion_reply", http.StatusOK,
		dict("Discussion", storyDiscussion, "C", storyDThreads[0].Comments[0], "Workspace", storyWorkspace))
}

// storyPost builds a urlencoded form POST for the replay stories, body
// included — the handler's parseForm populates PostForm like a real
// submission.
func storyPost(target string, form url.Values, partial bool) *http.Request {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if partial {
		req.Header.Set("X-Partial", "1")
	}
	return req
}

// StoryHttpCreateCommentPartial replays a comment POST with X-Partial:
// the mux answers the new comment's fragment instead of a 303 redirect.
func StoryHttpCreateCommentPartial(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/w/default/issues/2/comments", url.Values{"body": {"posted via fetch"}}, true))
}

// StoryHttpCreateCommentRedirect replays the same POST without the
// header — the unchanged full-reload path answering 303 #comment-N.
func StoryHttpCreateCommentRedirect(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/w/default/issues/2/comments", url.Values{"body": {"classic post"}}, false))
}

// StoryHttpCreateCommentPartialError replays a partial post with an
// empty body: the 422 response is the error-marked form fragment.
func StoryHttpCreateCommentPartialError(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/w/default/issues/2/comments", url.Values{"body": {""}}, true))
}

// StoryHttpCreateDiscussionReplyPartial replays a partial reply POST:
// the fragment carries comment-reply markup for .comment-replies.
func StoryHttpCreateDiscussionReplyPartial(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/w/default/discussions/2/comments", url.Values{"body": {"a reply"}, "thread_id": {"1"}}, true))
}

// StoryHttpSetStatusPartial replays a partial status toggle: the mux
// answers the re-rendered issue article — badge, meta and flipped button
// — that replace mode swaps over .issue-detail.
func StoryHttpSetStatusPartial(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/w/default/issues/2/status", url.Values{"status": {"closed"}}, true))
}

// StoryHttpSetStatusRedirect replays the same POST without X-Partial —
// the unchanged 303 PRG path.
func StoryHttpSetStatusRedirect(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/w/default/issues/2/status", url.Values{"status": {"closed"}}, false))
}

// StoryHttpHideCommentPartial replays a partial hide POST: the mux
// answers the hidden-state <li> (reason note + collapsed body) that
// replace mode swaps over the visible comment.
func StoryHttpHideCommentPartial(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/w/default/issues/2/comments/2/hide", url.Values{"reason": {"spam"}}, true))
}

// StoryHttpHideCommentRedirect replays the same POST without X-Partial
// — the unchanged 303 #comment-N path.
func StoryHttpHideCommentRedirect(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/w/default/issues/2/comments/2/hide", url.Values{"reason": {"spam"}}, false))
}

// StoryHttpUnhideCommentPartial replays a partial unhide POST — the
// store reports comment 2 visible again, so the fragment is the normal
// comment markup.
func StoryHttpUnhideCommentPartial(w http.ResponseWriter, r *http.Request) {
	st := newStoryStore()
	_ = st.SetCommentHidden(r.Context(), 2, 2, "spam")
	New(st).ServeHTTP(w, storyPost("/w/default/issues/2/comments/2/unhide", nil, true))
}

// StoryHttpSetCommentAnswerPartial replays a partial answer mark on the
// Q&A fixture: since marking moves the badge, the swap fragment carries
// two <li>s — the newly marked comment 2 and comment 1 losing the mark —
// each self-targeting by id.
func StoryHttpSetCommentAnswerPartial(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/w/default/discussions/2/comments/2/answer", url.Values{"answer": {"1"}}, true))
}

// StoryHttpSetCommentAnswerRedirect replays the same POST without
// X-Partial — the unchanged 303 #comment-N path.
func StoryHttpSetCommentAnswerRedirect(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/w/default/discussions/2/comments/2/answer", url.Values{"answer": {"1"}}, false))
}

// StoryHttpApp routes the real incoming request through the app's mux.
// cmd/storybook mounts it as a catch-all, so the storybook doubles as a
// fake-data instance of the whole app — enough for browser JS like
// partial-submit.js (fetch → fragment → DOM swap) to run end to end
// without a database.
func StoryHttpApp(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, r)
}

// The storybook runs in the single "default" workspace; management
// endpoints are read-only fixtures (create/delete panic like other
// mutating methods do here).
var storyWorkspace = &store.Workspace{ID: 1, Slug: "default", Name: "default", Description: "Storybook workspace."}

func (storyStore) ListWorkspaces(ctx context.Context) ([]*store.Workspace, error) {
	return []*store.Workspace{storyWorkspace}, nil
}
func (storyStore) WorkspaceRefBySlug(ctx context.Context, slug string) (*store.Workspace, error) {
	if slug == storyWorkspace.Slug {
		return storyWorkspace, nil
	}
	return nil, store.ErrNotFound
}
func (storyStore) WorkspaceBySlug(ctx context.Context, slug string) (*store.Workspace, error) {
	if slug == storyWorkspace.Slug {
		return storyWorkspace, nil
	}
	return nil, store.ErrNotFound
}
func (storyStore) WorkspaceByID(ctx context.Context, id int64) (*store.Workspace, error) {
	if id == storyWorkspace.ID {
		return storyWorkspace, nil
	}
	return nil, store.ErrNotFound
}
func (storyStore) CreateWorkspace(ctx context.Context, in store.WorkspaceInput) (int64, error) {
	panic("storybook: CreateWorkspace not implemented")
}
func (storyStore) DeleteWorkspace(ctx context.Context, slug string) error {
	panic("storybook: DeleteWorkspace not implemented")
}

// ---------------------------------------------------------------------------
var storyPullAuthors = []*store.User{
	{ID: 1, Name: "demo"},
	{ID: 2, Name: "taro"},
}

// Pull request fixtures — the storybook nav shows the Pull requests link and
// the pages render without a real git repo: fixtures feed the page models
// directly, and storyStore's PR methods mirror them for replayed requests.
var storyPulls = []*store.PullRequest{
	{ID: 12, BaseRef: "refs/heads/main", HeadRef: "refs/heads/feat/pulls-index",
		Title: "Add the /pulls index page", Body: "Implements the list + tab filters.",
		Status: store.PRStatusOpen, Author: storyPullAuthors[0],
		HeadSHA: "1a2b3c4d5e6f", MergeBaseSHA: "9f8e7d6c5b4a",
		CreatedAt: storyNow().Add(-26 * time.Hour), UpdatedAt: storyNow().Add(-3 * time.Hour)},
	{ID: 11, BaseRef: "refs/heads/main", HeadRef: "refs/heads/fix/nav-order",
		Title: "Reorder nav links", Body: "Issues, Discussions, Pull requests.",
		Status: store.PRStatusMerged, Author: storyPullAuthors[1],
		HeadSHA: "aabbccddeeff", MergeBaseSHA: "112233445566", MergeSHA: "deadbeefcafe",
		MergeMethod: "squash",
		CreatedAt:   storyNow().Add(-96 * time.Hour), UpdatedAt: storyNow().Add(-48 * time.Hour)},
	{ID: 10, BaseRef: "refs/heads/main", HeadRef: "refs/heads/try/sidebar",
		Title: "Experiment: collapsible sidebar", Body: "Abandoned in favor of a top bar.",
		Status: store.PRStatusClosed, Author: storyPullAuthors[0],
		HeadSHA: "feedface1234", MergeBaseSHA: "112233445566",
		CreatedAt: storyNow().Add(-200 * time.Hour), UpdatedAt: storyNow().Add(-150 * time.Hour)},
}

var storyPRComments = []*store.PRComment{
	{ID: 30, PRID: 12, Body: "First pass — the tab bar mirrors the issues one.",
		Author: storyPullAuthors[0], CreatedAt: storyNow().Add(-20 * time.Hour)},
	{ID: 31, PRID: 12, Path: "internal/web/templates/pulls.html", Line: 14, Side: "right",
		AnchorSHA: "1a2b3c4d5e6f", DiffSnippet: "@@ -12,6 +12,8 @@\n <nav>\n+  <a href=\"/pulls\">Pull requests</a>\n </nav>",
		Body:   "The link should live outside the workspace gate.",
		Author: storyPullAuthors[1], CreatedAt: storyNow().Add(-18 * time.Hour)},
	{ID: 32, PRID: 12, Body: "Done — moved next to Discussions.",
		Author: storyPullAuthors[0], CreatedAt: storyNow().Add(-17 * time.Hour)},
}

func (storyStore) ListPullRequests(ctx context.Context, f store.PRListFilter) ([]*store.PullRequest, int, error) {
	return storyPulls, len(storyPulls), nil
}
func (storyStore) GetPullRequest(ctx context.Context, id int64) (*store.PullRequest, error) {
	for _, p := range storyPulls {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, store.ErrNotFound
}
func (storyStore) CreatePullRequest(ctx context.Context, in store.PRInput, authorID int64, pin func(int64) error) (int64, error) {
	return 99, nil
}
func (storyStore) UpdatePullRequest(ctx context.Context, id int64, in store.PRInput) error {
	return nil
}
func (storyStore) ClosePullRequest(ctx context.Context, id int64, headSHA, mergeBaseSHA string) error {
	return nil
}
func (storyStore) ReopenPullRequest(ctx context.Context, id int64) error {
	return nil
}
func (storyStore) MarkMerged(ctx context.Context, id int64, mi store.PRMergeInfo) error {
	return nil
}
func (storyStore) ListPRComments(ctx context.Context, prID int64) ([]*store.PRComment, error) {
	return storyPRComments, nil
}
func (storyStore) CreatePRComment(ctx context.Context, prID int64, in store.PRCommentInput, authorID int64) (int64, error) {
	return 99, nil
}
func (storyStore) UpdatePRComment(ctx context.Context, prID, commentID int64, body string) error {
	return nil
}
func (storyStore) DeletePRComment(ctx context.Context, prID, commentID int64) error {
	return nil
}
func (storyStore) SetPRCommentHidden(ctx context.Context, prID, commentID int64, reason string) error {
	return nil
}

func renderPullList(status int, data pullListPage) {
	rec := httptest.NewRecorder()
	NewRendererWithPulls(true).renderPullList(rec, status, data, supportedLangs[0])
	printRec(rec)
}
func renderPullDetail(status int, data pullDetailPage) {
	rec := httptest.NewRecorder()
	NewRendererWithPulls(true).renderPullDetail(rec, status, data, supportedLangs[0])
	printRec(rec)
}
func renderPullForm(status int, data pullFormPage) {
	rec := httptest.NewRecorder()
	NewRendererWithPulls(true).renderPullForm(rec, status, data, supportedLangs[0])
	printRec(rec)
}
func renderPullFiles(status int, data pullFilesPage) {
	rec := httptest.NewRecorder()
	NewRendererWithPulls(true).renderPullFiles(rec, status, data, supportedLangs[0])
	printRec(rec)
}
func renderPullMerge(status int, data pullMergePage) {
	rec := httptest.NewRecorder()
	NewRendererWithPulls(true).renderPullMerge(rec, status, data, supportedLangs[0])
	printRec(rec)
}

// StoryPullList renders the pull request index with open/merged/closed rows.
func StoryPullList() {
	renderPullList(http.StatusOK, pullListPage{
		Title: "Pull requests", Section: "pulls",
		Filter: store.PRListFilter{Status: "all"},
		Pulls:  storyPulls,
		Total:  3, Page: 1, TotalPages: 1,
		RangeStart: 1, RangeEnd: 3,
	})
}

// StoryPullDetail renders an open PR's conversation: merge-clean badge,
// commits, and an anchored review comment.
func StoryPullDetail() {
	renderPullDetail(http.StatusOK, pullDetailPage{
		Title: storyPulls[0].Title, Section: "pulls",
		PR: storyPulls[0],
		Commits: []gitrepo.Commit{
			{SHA: "1a2b3c4d5e6f7a8b9c0d", ShortSHA: "1a2b3c4", Subject: "List handler + routes", Author: "demo", Date: storyNow().Add(-25 * time.Hour)},
			{SHA: "2b3c4d5e6f7a8b9c0d1e", ShortSHA: "2b3c4d5", Subject: "Tab filter bar", Author: "demo", Date: storyNow().Add(-24 * time.Hour)},
		},
		Merge:    &gitrepo.MergeCheck{Status: gitrepo.MergeClean},
		Comments: storyPRComments,
	})
}

// StoryPullDetailMerged renders the conversation of a merged PR — static
// merge info, no live mergeability probe.
func StoryPullDetailMerged() {
	renderPullDetail(http.StatusOK, pullDetailPage{
		Title: storyPulls[1].Title, Section: "pulls",
		PR:       storyPulls[1],
		Comments: nil,
	})
}

// StoryPullNew renders the create form with base/head selects.
func StoryPullNew() {
	renderPullForm(http.StatusOK, pullFormPage{
		Title: "New pull request", Heading: "Open a pull request",
		Action: "/pulls", Section: "pulls",
		Current: "refs/heads/feat/pulls-index",
		Refs: []gitrepo.RefName{
			{Name: "refs/heads/main", Short: "main"},
			{Name: "refs/heads/feat/pulls-index", Short: "feat/pulls-index"},
			{Name: "refs/heads/feat/comments", Short: "feat/comments"},
		},
	})
}

// StoryPullEdit renders the edit form: refs frozen as text, only title/body.
func StoryPullEdit() {
	renderPullForm(http.StatusOK, pullFormPage{
		Title: "Edit pull request", Heading: "Edit pull request",
		Action: "/pulls/12/edit", Section: "pulls",
		PR:    storyPulls[0],
		Input: store.PRInput{Title: storyPulls[0].Title, Body: storyPulls[0].Body},
	})
}

// StoryPullFiles renders the files-changed page: one modified file with an
// inline comment form and one anchored comment.
func StoryPullFiles() {
	files := []pullFileView{
		{
			Change:       gitrepo.FileChange{Status: "M", NewPath: "internal/web/templates/pulls.html"},
			CommentPath:  "internal/web/templates/pulls.html",
			StatusLetter: "M",
			Anchor:       "file-internal-web-templates-pulls-html",
			Lines: []diffLine{
				{Kind: "meta", Text: "diff --git a/internal/web/templates/pulls.html b/internal/web/templates/pulls.html"},
				{Kind: "hunk", Text: "@@ -12,6 +12,8 @@"},
				{Kind: "ctx", OldNo: 12, NewNo: 12, Text: " <nav>"},
				{Kind: "add", NewNo: 13, Text: "+  <a href=\"/pulls\">Pull requests</a>"},
				{Kind: "del", OldNo: 13, Text: "-  <a href=\"/old\">Old</a>"},
				{Kind: "ctx", OldNo: 14, NewNo: 14, Text: " </nav>"},
			},
			Comments: []*store.PRComment{storyPRComments[1]},
		},
		{
			Change:       gitrepo.FileChange{Status: "A", NewPath: "internal/web/pulls.go"},
			CommentPath:  "internal/web/pulls.go",
			StatusLetter: "A",
			Anchor:       "file-internal-web-pulls-go",
			Lines: []diffLine{
				{Kind: "hunk", Text: "@@ -0,0 +1,3 @@"},
				{Kind: "add", NewNo: 1, Text: "+package web"},
				{Kind: "add", NewNo: 2, Text: "+"},
				{Kind: "add", NewNo: 3, Text: "+// Pull request pages."},
			},
		},
	}
	open := &openLineComment{Path: "internal/web/templates/pulls.html", Line: 13, Side: "right"}
	for i := range files {
		annotateDiffLines(files[i].Lines, storyPulls[0].ID, files[i].Change, files[i].CommentPath, files[i].Anchor, open, &messagesEN)
	}
	renderPullFiles(http.StatusOK, pullFilesPage{
		Title: storyPulls[0].Title, Section: "pulls",
		PR: storyPulls[0], Files: files, Open: open,
	})
}

// StoryPullMerge renders the merge confirmation page on a clean probe.
func StoryPullMerge() {
	renderPullMerge(http.StatusOK, pullMergePage{
		Title: "Merge pull request", Section: "pulls",
		PR:    storyPulls[0],
		Check: &gitrepo.MergeCheck{Status: gitrepo.MergeClean},
	})
}

// StoryPullMergeConflict renders the merge page when the probe reports a
// conflict — the merge button is gone, the file list is shown.
func StoryPullMergeConflict() {
	renderPullMerge(http.StatusOK, pullMergePage{
		Title: "Merge pull request", Section: "pulls",
		PR: storyPulls[0],
		Check: &gitrepo.MergeCheck{Status: gitrepo.MergeConflicting,
			Files: []string{"internal/web/templates/pulls.html"}},
	})
}

func (s storyStore) GetIssueInWorkspace(ctx context.Context, wsID, id int64) (*store.Issue, error) {
	is, err := s.GetIssue(ctx, id)
	if err != nil || is.WorkspaceID != wsID {
		return nil, store.ErrNotFound
	}
	return is, nil
}

func (storyStore) PatchIssueInWorkspace(context.Context, int64, int64, store.IssuePatch) error {
	return nil
}

func (s storyStore) GetComment(_ context.Context, issueID, commentID int64) (*store.Comment, error) {
	for _, c := range s.comments[issueID] {
		if c.ID == commentID {
			return c, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s storyStore) LabelByName(ctx context.Context, wsID int64, kind, name string) (*store.Label, error) {
	labels, _ := s.ListLabels(ctx, wsID, kind)
	var shared *store.Label
	for _, l := range labels {
		if l.Name != name {
			continue
		}
		if l.WorkspaceID == wsID {
			return l, nil
		}
		if l.WorkspaceID == 0 {
			shared = l
		}
	}
	if shared != nil {
		return shared, nil
	}
	return nil, store.ErrNotFound
}

func (storyStore) GetPRComment(_ context.Context, prID, commentID int64) (*store.PRComment, error) {
	for _, c := range storyPRComments {
		if c.PRID == prID && c.ID == commentID {
			return c, nil
		}
	}
	return nil, store.ErrNotFound
}
