package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// issueListPage is the view model for GET /issues.
type issueListPage struct {
	Title           string
	Issues          []*store.Issue
	Labels          []*store.Label
	Filter          store.ListFilter
	Total           int
	Page            int
	TotalPages      int
	RangeStart      int
	RangeEnd        int
	HasActiveFilter bool
	Section         string
	Workspace       *store.Workspace
	User            *store.User
}

// issueDetailPage is the view model for GET /issues/{id}. CommentDraft and
// CommentError are only populated when re-rendering after a failed comment
// submission.
type issueDetailPage struct {
	Title        string
	Issue        *store.Issue
	Comments     []*store.Comment
	CommentDraft string
	CommentError string
	Section      string
	Workspace    *store.Workspace
	User         *store.User
}

// issueFormPage is the view model for both the new-issue and edit-issue
// forms.
type issueFormPage struct {
	Title       string
	Heading     string
	Action      string
	SubmitLabel string
	Input       store.IssueInput
	Labels      []*store.Label
	Errors      issueFormErrors
	Section     string
	Workspace   *store.Workspace
	User        *store.User
}

// issueFormErrors carries per-field validation messages for the issue form.
type issueFormErrors struct {
	Title string
	Body  string
}

func (e issueFormErrors) any() bool { return e.Title != "" || e.Body != "" }

// issueDeletePage is the view model for the delete confirmation page.
type issueDeletePage struct {
	Title     string
	Issue     *store.Issue
	Section   string
	Workspace *store.Workspace
	User      *store.User
}

// listIssues handles GET /issues: status tabs, keyword search, label
// filtering, sorting and pagination.
func (h *handler) listIssues(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	f := parseListFilter(r)

	issues, total, err := h.store.ListIssues(r.Context(), ws.ID, f)
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	// A page past the end would render the misleading empty state with no
	// pagination to navigate back; clamp to the last page and re-fetch so
	// the user lands on real results instead.
	totalPages := (total + perPage - 1) / perPage
	if total > 0 && f.Page > totalPages {
		f.Page = totalPages
		issues, total, err = h.store.ListIssues(r.Context(), ws.ID, f)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
	}

	labels, err := h.store.ListLabels(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	data := issueListPage{
		Title:      "Issues",
		Issues:     issues,
		Labels:     labels,
		Filter:     f,
		Total:      total,
		Page:       f.Page,
		TotalPages: totalPages,
		HasActiveFilter: f.Query != "" || f.LabelID != 0 ||
			f.Status != store.StatusOpen || f.Sort != "",
		Section:   "issues",
		Workspace: ws,
		User:      userFromContext(r.Context()),
	}
	if total > 0 {
		data.RangeStart = (f.Page-1)*perPage + 1
		data.RangeEnd = min(f.Page*perPage, total)
	}
	// External speculation rules (issue titles + pagination, moderate
	// eagerness): the Speculation-Rules header delivery keeps the no-JS
	// markup free of <script> elements, which CSP script-src 'self'
	// would block even for the inert speculationrules JSON type. The
	// header value is an sf-string — the quotes are mandatory.
	w.Header().Set("Speculation-Rules", `"/static/speculation-rules.json"`)
	h.r.renderIssueList(w, http.StatusOK, data)
}

// newIssue renders the empty create form.
func (h *handler) newIssue(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	labels, err := h.store.ListLabels(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.r.renderIssueForm(w, http.StatusOK, issueFormPage{
		Title:       "New issue",
		Heading:     "New issue",
		Action:      "/w/" + ws.Slug + "/issues",
		SubmitLabel: "Create issue",
		Labels:      labels,
		Section:     "issues",
		Workspace:   ws,
		User:        userFromContext(r.Context()),
	})
}

// createIssue validates POST /issues and redirects to the new issue.
func (h *handler) createIssue(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	if !h.parseForm(w, r) {
		return
	}
	in, errs := validateIssueInput(r)
	if errs.any() {
		labels, err := h.store.ListLabels(r.Context())
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		h.r.renderIssueForm(w, http.StatusUnprocessableEntity, issueFormPage{
			Title:       "New issue",
			Heading:     "New issue",
			Action:      "/w/" + ws.Slug + "/issues",
			SubmitLabel: "Create issue",
			Input:       in,
			Labels:      labels,
			Errors:      errs,
			Section:     "issues",
			Workspace:   ws,
			User:        userFromContext(r.Context()),
		})
		return
	}
	id, err := h.store.CreateIssue(r.Context(), ws.ID, in, authorIDFromContext(r.Context()))
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	http.Redirect(w, r, issueURL(ws, id), http.StatusSeeOther)
}

// showIssue renders the detail page including comments and the comment form.
func (h *handler) showIssue(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	issue, err := h.store.GetIssue(r.Context(), id)
	if err != nil {
		h.issueError(w, r, err)
		return
	}
	if !h.requireScope(w, r, ws, issue.WorkspaceID) {
		return
	}
	comments, err := h.store.ListComments(r.Context(), id)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.r.renderIssueDetail(w, http.StatusOK, issueDetailPage{
		Title:     fmt.Sprintf("#%d %s", issue.ID, issue.Title),
		Issue:     issue,
		Comments:  comments,
		Section:   "issues",
		Workspace: ws,
		User:      userFromContext(r.Context()),
	})
}

// editIssue renders the edit form populated with the current values.
func (h *handler) editIssue(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	issue, err := h.store.GetIssue(r.Context(), id)
	if err != nil {
		h.issueError(w, r, err)
		return
	}
	if !h.requireScope(w, r, ws, issue.WorkspaceID) {
		return
	}
	labels, err := h.store.ListLabels(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	in := store.IssueInput{
		Title:    issue.Title,
		Body:     issue.Body,
		LabelIDs: make([]int64, 0, len(issue.Labels)),
	}
	for _, l := range issue.Labels {
		in.LabelIDs = append(in.LabelIDs, l.ID)
	}
	h.r.renderIssueForm(w, http.StatusOK, issueFormPage{
		Title:       fmt.Sprintf("Edit issue #%d", issue.ID),
		Heading:     fmt.Sprintf("Edit issue #%d", issue.ID),
		Action:      issueURL(ws, id) + "/edit",
		SubmitLabel: "Save changes",
		Input:       in,
		Labels:      labels,
		Section:     "issues",
		Workspace:   ws,
		User:        userFromContext(r.Context()),
	})
}

// updateIssue validates POST /issues/{id}/edit and redirects to the detail.
func (h *handler) updateIssue(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	// Fetch the issue up front so ANY POST to a missing issue — including
	// one that fails validation — is a 404 rather than a 422 edit form.
	issue, err := h.store.GetIssue(r.Context(), id)
	if err != nil {
		h.issueError(w, r, err)
		return
	}
	if !h.requireScope(w, r, ws, issue.WorkspaceID) {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	in, errs := validateIssueInput(r)
	if errs.any() {
		labels, err := h.store.ListLabels(r.Context())
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		h.r.renderIssueForm(w, http.StatusUnprocessableEntity, issueFormPage{
			Title:       fmt.Sprintf("Edit issue #%d", id),
			Heading:     fmt.Sprintf("Edit issue #%d", id),
			Action:      issueURL(ws, id) + "/edit",
			SubmitLabel: "Save changes",
			Input:       in,
			Labels:      labels,
			Errors:      errs,
			Section:     "issues",
			Workspace:   ws,
			User:        userFromContext(r.Context()),
		})
		return
	}
	if err := h.store.UpdateIssue(r.Context(), id, in); err != nil {
		h.issueError(w, r, err)
		return
	}
	http.Redirect(w, r, issueURL(ws, id), http.StatusSeeOther)
}

// setStatus handles POST /issues/{id}/status. The target status arrives as a
// hidden form field rendered by the detail page ("closed" on an open issue,
// "open" on a closed one).
func (h *handler) setStatus(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	issue0, err := h.store.GetIssue(r.Context(), id)
	if err != nil {
		h.issueError(w, r, err)
		return
	}
	if !h.requireScope(w, r, ws, issue0.WorkspaceID) {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	status := r.PostFormValue("status")
	if status != store.StatusOpen && status != store.StatusClosed {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "Invalid status value.")
		return
	}
	if err := h.store.SetIssueStatus(r.Context(), id, status); err != nil {
		h.issueError(w, r, err)
		return
	}
	if wantsPartial(r) {
		// The swap fragment is the whole article — badge, timestamps and
		// the flipped toggle button all change at once. GetIssue returns
		// the true post-state: the store bumps updated_at only when the
		// status actually changes, so a no-op toggle must echo the stored
		// timestamp, not time.Now().
		issue, err := h.store.GetIssue(r.Context(), id)
		if err != nil {
			h.issueError(w, r, err)
			return
		}
		h.r.renderFragment(w, "issue_detail", "issue_article", http.StatusOK, issueDetailPage{
			Issue:     issue,
			Workspace: ws,
			User:      userFromContext(r.Context()),
		})
		return
	}
	http.Redirect(w, r, issueURL(ws, id), http.StatusSeeOther)
}

// confirmDelete renders the delete confirmation page. Being a GET, it must
// never delete anything itself.
func (h *handler) confirmDelete(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	issue, err := h.store.GetIssue(r.Context(), id)
	if err != nil {
		h.issueError(w, r, err)
		return
	}
	if !h.requireScope(w, r, ws, issue.WorkspaceID) {
		return
	}
	h.r.renderIssueDelete(w, http.StatusOK, issueDeletePage{
		Title:     fmt.Sprintf("Delete issue #%d", issue.ID),
		Issue:     issue,
		Section:   "issues",
		Workspace: ws,
		User:      userFromContext(r.Context()),
	})
}

// deleteIssue performs the deletion and returns to the list.
func (h *handler) deleteIssue(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	issue0, err := h.store.GetIssue(r.Context(), id)
	if err != nil {
		h.issueError(w, r, err)
		return
	}
	if !h.requireScope(w, r, ws, issue0.WorkspaceID) {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	if err := h.store.DeleteIssue(r.Context(), id); err != nil {
		h.issueError(w, r, err)
		return
	}
	http.Redirect(w, r, "/w/"+ws.Slug+"/issues", http.StatusSeeOther)
}

// createComment validates POST /issues/{id}/comments and redirects back to
// the detail page anchored at the new comment's permalink. A request with
// X-Partial gets a fragment instead: the new comment's <li> on success, or
// the error-marked form on a validation failure (422) — the same defines
// the detail page uses, so the DOM swap is markup-identical to a reload.
func (h *handler) createComment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	body := strings.TrimSpace(r.PostFormValue("body"))
	partial := wantsPartial(r)

	issue, err := h.store.GetIssue(r.Context(), id)
	if err != nil {
		h.issueError(w, r, err)
		return
	}
	if !h.requireScope(w, r, ws, issue.WorkspaceID) {
		return
	}

	errMsg := store.ValidateCommentBody(body)
	if errMsg != "" {
		// A partial request swaps only the form; no list reload.
		if partial {
			h.r.renderFragment(w, "issue_detail", "issue_comment_form", http.StatusUnprocessableEntity, issueDetailPage{
				Issue:        issue,
				CommentDraft: body,
				CommentError: errMsg,
				Workspace:    ws,
				User:         userFromContext(r.Context()),
			})
			return
		}
		comments, err := h.store.ListComments(r.Context(), id)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		h.r.renderIssueDetail(w, http.StatusUnprocessableEntity, issueDetailPage{
			Title:        fmt.Sprintf("#%d %s", issue.ID, issue.Title),
			Issue:        issue,
			Comments:     comments,
			CommentDraft: body,
			CommentError: errMsg,
			Section:      "issues",
			Workspace:    ws,
			User:         userFromContext(r.Context()),
		})
		return
	}

	commentID, err := h.store.CreateComment(r.Context(), id, body, authorIDFromContext(r.Context()))
	if err != nil {
		h.issueError(w, r, err)
		return
	}
	if partial {
		// The fragment also carries the header meta as a data-swap node —
		// the comment bumped updated_at, so the stale "Last updated" line
		// must be re-rendered from the post-write issue.
		issue, err = h.store.GetIssue(r.Context(), id)
		if err != nil {
			h.issueError(w, r, err)
			return
		}
		h.r.renderFragment(w, "issue_detail", "issue_comment_append", http.StatusOK, dict(
			"Workspace", ws,
			"Issue", issue,
			"Comment", &store.Comment{
				ID:        commentID,
				IssueID:   id,
				Body:      body,
				Author:    userFromContext(r.Context()),
				CreatedAt: time.Now(),
			},
		))
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s#comment-%d", issueURL(ws, id), commentID), http.StatusSeeOther)
}

// issueError maps store.ErrNotFound to a 404 page and delegates everything
// else to the generic 500 handler.
func (h *handler) issueError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	h.internalError(w, r, err)
}

func issueURL(ws *store.Workspace, id int64) string {
	return fmt.Sprintf("/w/%s/issues/%d", ws.Slug, id)
}

// validateIssueInput extracts and validates the issue form fields. It only
// reports validation problems; form parsing itself happened earlier.
func validateIssueInput(r *http.Request) (store.IssueInput, issueFormErrors) {
	in := store.IssueInput{
		Title: strings.TrimSpace(r.PostFormValue("title")),
		Body:  strings.TrimSpace(r.PostFormValue("body")),
	}
	for _, v := range r.PostForm["label_id"] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			in.LabelIDs = append(in.LabelIDs, id)
		}
	}

	fe := store.ValidateIssueInput(in)
	return in, issueFormErrors{Title: fe["title"], Body: fe["body"]}
}

// isChecked reports whether id is among the selected label IDs.
func isChecked(id int64, ids []int64) bool {
	return slices.Contains(ids, id)
}

// issuesURL builds the workspace's issue-list URLs carrying the current
// filter state, so pagination, tabs and label links preserve active
// filters.
func issuesURL(ws *store.Workspace, f store.ListFilter) string {
	v := url.Values{}
	// "open" is the default filter, so keep URLs clean by omitting it.
	if f.Status != "" && f.Status != store.StatusOpen {
		v.Set("status", f.Status)
	}
	if f.Query != "" {
		v.Set("q", f.Query)
	}
	if f.LabelID != 0 {
		v.Set("label", strconv.FormatInt(f.LabelID, 10))
	}
	if f.Sort != "" {
		v.Set("sort", f.Sort)
	}
	if f.Page > 1 {
		v.Set("page", strconv.Itoa(f.Page))
	}
	if enc := v.Encode(); enc != "" {
		return "/w/" + ws.Slug + "/issues?" + enc
	}
	return "/w/" + ws.Slug + "/issues"
}

// listURL keeps the filter but moves to another page.
func listURL(ws *store.Workspace, f store.ListFilter, page int) string {
	f.Page = page
	return issuesURL(ws, f)
}

// statusURL keeps the filter but switches the status tab and resets paging.
func statusURL(ws *store.Workspace, f store.ListFilter, status string) string {
	f.Status = status
	f.Page = 1
	return issuesURL(ws, f)
}

// labelURL keeps the filter but selects a label and resets paging.
func labelURL(ws *store.Workspace, f store.ListFilter, labelID int64) string {
	f.LabelID = labelID
	f.Page = 1
	return issuesURL(ws, f)
}

// parseListFilter reads and normalizes the list query parameters.
func parseListFilter(r *http.Request) store.ListFilter {
	q := r.URL.Query()
	f := store.ListFilter{
		Status:  q.Get("status"),
		Query:   strings.TrimSpace(q.Get("q")),
		Sort:    q.Get("sort"),
		Page:    1,
		PerPage: perPage,
	}
	switch f.Status {
	case store.StatusOpen, store.StatusClosed, "all":
	default:
		f.Status = store.StatusOpen
	}
	if f.Sort != "oldest" {
		f.Sort = ""
	}

	// Reject absurd page numbers: huge values can overflow the store's
	// (page-1)*perPage offset arithmetic and are never meaningful anyway.
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 0 && n <= 1_000_000 {
		f.Page = n
	}
	if v := q.Get("label"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			f.LabelID = id
		}
	}
	return f
}
