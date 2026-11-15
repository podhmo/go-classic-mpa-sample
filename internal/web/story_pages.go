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

	storyCategories = []*store.DiscussionCategory{
		{ID: 1, Name: "Announcements", Slug: "announcements", Format: store.CategoryFormatOpen, Description: "Updates from the maintainers."},
		{ID: 2, Name: "General", Slug: "general", Format: store.CategoryFormatOpen, Description: "Anything related to the project."},
		{ID: 3, Name: "Ideas", Slug: "ideas", Format: store.CategoryFormatOpen, Description: "Share ideas for new features."},
		{ID: 4, Name: "Q&A", Slug: "qa", Format: store.CategoryFormatQA, Description: "Ask the community for help."},
	}

	storyDiscussions = []*store.Discussion{
		{
			ID: 3, Title: "Release v0.3 is out",
			Body:      "Small announcement.",
			Category:  *storyCategories[0],
			CreatedAt: storyTime2, UpdatedAt: storyTime2,
		},
		{
			ID: 2, Title: "How do custom labels get their color?",
			Body:      "I cannot find where the palette is defined.",
			Category:  *storyCategories[3],
			CreatedAt: storyTime, UpdatedAt: storyTime2,
			Answered: true,
		},
		{
			ID: 1, Title: "Idea: keyboard navigation for the filter bar",
			Body:      "It would be nice to move between filters with arrow keys.",
			Category:  *storyCategories[2],
			CreatedAt: storyTime, UpdatedAt: storyTime,
		},
	}

	storyDiscussion = &store.Discussion{
		ID: 2, Title: "How do custom labels get their color?",
		Body:      "I cannot find where the **palette** is defined.",
		Category:  *storyCategories[3],
		CreatedAt: storyTime, UpdatedAt: storyTime2,
		Answered: true,
	}

	storyDComments = []*store.DiscussionComment{
		{
			ID: 1, DiscussionID: 2, Body: "Look at `label_color` in the seed data.", CreatedAt: storyTime,
			IsAnswer: true,
			Replies: []*store.DiscussionComment{
				{ID: 3, DiscussionID: 2, ParentID: 1, Body: "That was it — thank you!", CreatedAt: storyTime2},
			},
		},
		{ID: 2, DiscussionID: 2, Body: "Same question on Firefox.", CreatedAt: storyTime2},
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

func renderDiscussionList(status int, data discussionListPage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderDiscussionList(rec, status, data)
	printRec(rec)
}

func renderDiscussionDetail(status int, data discussionDetailPage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderDiscussionDetail(rec, status, data)
	printRec(rec)
}

func renderDiscussionForm(status int, data discussionFormPage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderDiscussionForm(rec, status, data)
	printRec(rec)
}

func renderDiscussionDelete(status int, data discussionDeletePage) {
	rec := httptest.NewRecorder()
	NewRenderer().renderDiscussionDelete(rec, status, data)
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
		Section:         "issues",
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
		Section:         "issues",
	})
}

// StoryIssueDetail renders the issue detail page with comments.
func StoryIssueDetail() {
	renderIssueDetail(http.StatusOK, issueDetailPage{
		Title:    "#2 Improve responsive layout",
		Issue:    storyIssue,
		Comments: storyComments,
		Section:  "issues",
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
		Section:  "issues",
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
		Section:      "issues",
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
		Section:     "issues",
	})
}

// StoryIssueDelete renders the delete confirmation page.
func StoryIssueDelete() {
	renderIssueDelete(http.StatusOK, issueDeletePage{
		Title:   "Delete issue #2",
		Issue:   storyIssue,
		Section: "issues",
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
		Section:         "discussions",
	})
}

// StoryDiscussionDetail renders the Q&A detail page: an answer-marked
// top-level comment with a nested reply.
func StoryDiscussionDetail() {
	renderDiscussionDetail(http.StatusOK, discussionDetailPage{
		Title:        "#2 How do custom labels get their color?",
		Discussion:   storyDiscussion,
		Comments:     storyDComments,
		CommentCount: countDiscussionComments(storyDComments),
		Section:      "discussions",
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
		Errors:      discussionFormErrors{Title: "Title is required.", Category: "Please pick a category."},
		Section:     "discussions",
	})
}

// StoryDiscussionDelete renders the delete confirmation page.
func StoryDiscussionDelete() {
	renderDiscussionDelete(http.StatusOK, discussionDeletePage{
		Title:      "Delete discussion #2",
		Discussion: storyDiscussion,
		Section:    "discussions",
	})
}

// storyStore implements the Store contract with story fixtures so
// handler-shaped stories drive the real mux — routing, security
// headers, status codes — without a database. newStoryStore gives each
// replayed request its own deep copy of the comment fixtures so
// mutations (e.g. answer marks) are visible inside that request but
// never leak into the shared fixtures.
type storyStore struct {
	issues      map[int64]*store.Issue
	discussions map[int64]*store.Discussion
	dcomments   map[int64][]*store.DiscussionComment
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
	return storyStore{
		issues:      issues,
		discussions: discussions,
		dcomments: map[int64][]*store.DiscussionComment{
			storyDiscussion.ID: cloneDComments(storyDComments),
		},
	}
}

func cloneDComments(src []*store.DiscussionComment) []*store.DiscussionComment {
	out := make([]*store.DiscussionComment, len(src))
	for i, c := range src {
		cc := *c
		cc.Replies = cloneDComments(c.Replies)
		out[i] = &cc
	}
	return out
}

func (storyStore) ListIssues(ctx context.Context, f store.ListFilter) ([]*store.Issue, int, error) {
	return storyIssues, len(storyIssues), nil
}
func (s storyStore) GetIssue(ctx context.Context, id int64) (*store.Issue, error) {
	if is, ok := s.issues[id]; ok {
		return is, nil
	}
	return nil, store.ErrNotFound
}
func (storyStore) CreateIssue(ctx context.Context, in store.IssueInput, authorID int64) (int64, error) {
	return 99, nil
}
func (storyStore) UpdateIssue(ctx context.Context, id int64, in store.IssueInput) error {
	return nil
}
func (s storyStore) SetIssueStatus(ctx context.Context, id int64, status string) error {
	is, ok := s.issues[id]
	if !ok {
		return store.ErrNotFound
	}
	// Same contract as the real store: updated_at bumps only on change.
	if is.Status != status {
		is.Status = status
		is.UpdatedAt = time.Now()
	}
	return nil
}
func (storyStore) DeleteIssue(ctx context.Context, id int64) error {
	return nil
}
func (storyStore) ListComments(ctx context.Context, issueID int64) ([]*store.Comment, error) {
	return storyComments, nil
}
func (s storyStore) CreateComment(ctx context.Context, issueID int64, body string, authorID int64) (int64, error) {
	// An id outside the fixture range so a fragment inserted by the app
	// mount is distinguishable from fixture comments.
	// Same contract as the real store: a new comment bumps updated_at,
	// which the append fragment's meta node reflects.
	if is, ok := s.issues[issueID]; ok {
		is.UpdatedAt = time.Now()
	}
	return 99, nil
}
func (storyStore) ListLabels(ctx context.Context) ([]*store.Label, error) {
	return storyLabels, nil
}

func (storyStore) ListDiscussions(ctx context.Context, f store.DiscussionListFilter) ([]*store.Discussion, int, error) {
	return storyDiscussions, len(storyDiscussions), nil
}
func (s storyStore) GetDiscussion(ctx context.Context, id int64) (*store.Discussion, error) {
	if d, ok := s.discussions[id]; ok {
		return d, nil
	}
	return nil, store.ErrNotFound
}
func (storyStore) CreateDiscussion(ctx context.Context, in store.DiscussionInput, authorID int64) (int64, error) {
	return 99, nil
}
func (storyStore) UpdateDiscussion(ctx context.Context, id int64, in store.DiscussionInput) error {
	return nil
}
func (storyStore) DeleteDiscussion(ctx context.Context, id int64) error {
	return nil
}
func (s storyStore) ListDiscussionComments(ctx context.Context, discussionID int64) ([]*store.DiscussionComment, error) {
	if cs, ok := s.dcomments[discussionID]; ok {
		return cs, nil
	}
	return storyDComments, nil
}
func (s storyStore) CreateDiscussionComment(ctx context.Context, discussionID, parentID int64, body string, authorID int64) (int64, error) {
	// Same contract as the real store: a new comment bumps updated_at.
	if d, ok := s.discussions[discussionID]; ok {
		d.UpdatedAt = time.Now()
	}
	return 99, nil
}
func (s storyStore) SetCommentAnswer(ctx context.Context, discussionID, commentID int64, answer bool) error {
	cs := s.dcomments[discussionID]
	if cs == nil {
		return store.ErrNotFound
	}
	var target *store.DiscussionComment
	for _, c := range cs {
		if c.ID == commentID && c.ParentID == 0 {
			target = c
		}
	}
	if target == nil {
		return store.ErrNotFound
	}
	if answer {
		for _, c := range cs {
			c.IsAnswer = false
		}
	}
	target.IsAnswer = answer
	// Same contract as the real store: an answer mark bumps the
	// discussion's updated_at, which the swap fragment reflects.
	if d, ok := s.discussions[discussionID]; ok {
		d.UpdatedAt = time.Now()
	}
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
	NewRenderer().renderFragment(rec, page, name, status, data)
	printRec(rec)
}

// StoryIssueCommentFragment is the <li> a successful partial comment
// POST returns for an issue — appended to .comment-list by the swap.
func StoryIssueCommentFragment() {
	renderFragment("issue_detail", "issue_comment", http.StatusOK, storyComments[1])
}

// StoryIssueCommentFormFragment is the error-marked form a 422 partial
// response swaps back in place of the submitted one.
func StoryIssueCommentFormFragment() {
	renderFragment("issue_detail", "issue_comment_form", http.StatusUnprocessableEntity, issueDetailPage{
		Issue:        storyIssue,
		CommentDraft: "kept as the draft",
		CommentError: "Comment is required.",
	})
}

// StoryDiscussionCommentFragment renders a top-level discussion comment
// <li> — collapsed reply form and qa answer button included.
func StoryDiscussionCommentFragment() {
	renderFragment("discussion_detail", "discussion_comment", http.StatusOK,
		dict("Discussion", storyDiscussion, "C", storyDComments[1]))
}

// StoryDiscussionReplyFragment renders the reply <li> appended to the
// parent's .comment-replies by a partial reply POST.
func StoryDiscussionReplyFragment() {
	renderFragment("discussion_detail", "discussion_reply", http.StatusOK, storyDComments[0].Replies[0])
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
	New(newStoryStore()).ServeHTTP(w, storyPost("/issues/2/comments", url.Values{"body": {"posted via fetch"}}, true))
}

// StoryHttpCreateCommentRedirect replays the same POST without the
// header — the unchanged full-reload path answering 303 #comment-N.
func StoryHttpCreateCommentRedirect(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/issues/2/comments", url.Values{"body": {"classic post"}}, false))
}

// StoryHttpCreateCommentPartialError replays a partial post with an
// empty body: the 422 response is the error-marked form fragment.
func StoryHttpCreateCommentPartialError(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/issues/2/comments", url.Values{"body": {""}}, true))
}

// StoryHttpCreateDiscussionReplyPartial replays a partial reply POST:
// the fragment carries comment-reply markup for .comment-replies.
func StoryHttpCreateDiscussionReplyPartial(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/discussions/2/comments", url.Values{"body": {"a reply"}, "parent_id": {"1"}}, true))
}

// StoryHttpSetStatusPartial replays a partial status toggle: the mux
// answers the re-rendered issue article — badge, meta and flipped button
// — that replace mode swaps over .issue-detail.
func StoryHttpSetStatusPartial(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/issues/2/status", url.Values{"status": {"closed"}}, true))
}

// StoryHttpSetStatusRedirect replays the same POST without X-Partial —
// the unchanged 303 PRG path.
func StoryHttpSetStatusRedirect(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/issues/2/status", url.Values{"status": {"closed"}}, false))
}

// StoryHttpSetCommentAnswerPartial replays a partial answer mark on the
// Q&A fixture: since marking moves the badge, the swap fragment carries
// two <li>s — the newly marked comment 2 and comment 1 losing the mark —
// each self-targeting by id.
func StoryHttpSetCommentAnswerPartial(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/discussions/2/comments/2/answer", url.Values{"answer": {"1"}}, true))
}

// StoryHttpSetCommentAnswerRedirect replays the same POST without
// X-Partial — the unchanged 303 #comment-N path.
func StoryHttpSetCommentAnswerRedirect(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, storyPost("/discussions/2/comments/2/answer", url.Values{"answer": {"1"}}, false))
}

// StoryHttpApp routes the real incoming request through the app's mux.
// cmd/storybook mounts it as a catch-all, so the storybook doubles as a
// fake-data instance of the whole app — enough for browser JS like
// partial-submit.js (fetch → fragment → DOM swap) to run end to end
// without a database.
func StoryHttpApp(w http.ResponseWriter, r *http.Request) {
	New(newStoryStore()).ServeHTTP(w, r)
}
