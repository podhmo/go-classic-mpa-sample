package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

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
}

// issueFormErrors carries per-field validation messages for the issue form.
type issueFormErrors struct {
	Title string
	Body  string
}

func (e issueFormErrors) any() bool { return e.Title != "" || e.Body != "" }

// issueDeletePage is the view model for the delete confirmation page.
type issueDeletePage struct {
	Title string
	Issue *store.Issue
}

// home redirects the bare root to the issue list.
func (h *handler) home(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/issues", http.StatusSeeOther)
}

// listIssues handles GET /issues: status tabs, keyword search, label
// filtering, sorting and pagination.
func (h *handler) listIssues(w http.ResponseWriter, r *http.Request) {
	f := parseListFilter(r)

	issues, total, err := h.store.ListIssues(r.Context(), f)
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
		issues, total, err = h.store.ListIssues(r.Context(), f)
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
	}
	if total > 0 {
		data.RangeStart = (f.Page-1)*perPage + 1
		data.RangeEnd = min(f.Page*perPage, total)
	}
	h.r.renderIssueList(w, http.StatusOK, data)
}

// newIssue renders the empty create form.
func (h *handler) newIssue(w http.ResponseWriter, r *http.Request) {
	labels, err := h.store.ListLabels(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.r.renderIssueForm(w, http.StatusOK, issueFormPage{
		Title:       "New issue",
		Heading:     "New issue",
		Action:      "/issues",
		SubmitLabel: "Create issue",
		Labels:      labels,
	})
}

// createIssue validates POST /issues and redirects to the new issue.
func (h *handler) createIssue(w http.ResponseWriter, r *http.Request) {
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
			Action:      "/issues",
			SubmitLabel: "Create issue",
			Input:       in,
			Labels:      labels,
			Errors:      errs,
		})
		return
	}
	id, err := h.store.CreateIssue(r.Context(), in)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	http.Redirect(w, r, issueURL(id), http.StatusSeeOther)
}

// showIssue renders the detail page including comments and the comment form.
func (h *handler) showIssue(w http.ResponseWriter, r *http.Request) {
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
	comments, err := h.store.ListComments(r.Context(), id)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.r.renderIssueDetail(w, http.StatusOK, issueDetailPage{
		Title:    fmt.Sprintf("#%d %s", issue.ID, issue.Title),
		Issue:    issue,
		Comments: comments,
	})
}

// editIssue renders the edit form populated with the current values.
func (h *handler) editIssue(w http.ResponseWriter, r *http.Request) {
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
		Action:      issueURL(id) + "/edit",
		SubmitLabel: "Save changes",
		Input:       in,
		Labels:      labels,
	})
}

// updateIssue validates POST /issues/{id}/edit and redirects to the detail.
func (h *handler) updateIssue(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	// Fetch the issue up front so ANY POST to a missing issue — including
	// one that fails validation — is a 404 rather than a 422 edit form.
	if _, err := h.store.GetIssue(r.Context(), id); err != nil {
		h.issueError(w, r, err)
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
			Action:      issueURL(id) + "/edit",
			SubmitLabel: "Save changes",
			Input:       in,
			Labels:      labels,
			Errors:      errs,
		})
		return
	}
	if err := h.store.UpdateIssue(r.Context(), id, in); err != nil {
		h.issueError(w, r, err)
		return
	}
	http.Redirect(w, r, issueURL(id), http.StatusSeeOther)
}

// setStatus handles POST /issues/{id}/status. The target status arrives as a
// hidden form field rendered by the detail page ("closed" on an open issue,
// "open" on a closed one).
func (h *handler) setStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	status := r.PostFormValue("status")
	if status != store.StatusOpen && status != store.StatusClosed {
		h.r.renderError(w, http.StatusBadRequest, "Bad request", "Invalid status value.")
		return
	}
	if err := h.store.SetIssueStatus(r.Context(), id, status); err != nil {
		h.issueError(w, r, err)
		return
	}
	http.Redirect(w, r, issueURL(id), http.StatusSeeOther)
}

// confirmDelete renders the delete confirmation page. Being a GET, it must
// never delete anything itself.
func (h *handler) confirmDelete(w http.ResponseWriter, r *http.Request) {
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
	h.r.renderIssueDelete(w, http.StatusOK, issueDeletePage{
		Title: fmt.Sprintf("Delete issue #%d", issue.ID),
		Issue: issue,
	})
}

// deleteIssue performs the deletion and returns to the list.
func (h *handler) deleteIssue(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	if err := h.store.DeleteIssue(r.Context(), id); err != nil {
		h.issueError(w, r, err)
		return
	}
	http.Redirect(w, r, "/issues", http.StatusSeeOther)
}

// createComment validates POST /issues/{id}/comments and redirects back to
// the detail page anchored at the comment list.
func (h *handler) createComment(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	body := strings.TrimSpace(r.PostFormValue("body"))

	var errMsg string
	switch {
	case body == "":
		errMsg = "Comment is required."
	case utf8.RuneCountInString(body) > maxBodyRunes:
		errMsg = fmt.Sprintf("Comment must be %d characters or fewer.", maxBodyRunes)
	}
	if errMsg != "" {
		issue, err := h.store.GetIssue(r.Context(), id)
		if err != nil {
			h.issueError(w, r, err)
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
		})
		return
	}

	if _, err := h.store.CreateComment(r.Context(), id, body); err != nil {
		h.issueError(w, r, err)
		return
	}
	http.Redirect(w, r, issueURL(id)+"#comments", http.StatusSeeOther)
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

func issueURL(id int64) string {
	return fmt.Sprintf("/issues/%d", id)
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

	var errs issueFormErrors
	switch {
	case in.Title == "":
		errs.Title = "Title is required."
	case utf8.RuneCountInString(in.Title) > maxTitleRunes:
		errs.Title = fmt.Sprintf("Title must be %d characters or fewer.", maxTitleRunes)
	}
	if utf8.RuneCountInString(in.Body) > maxBodyRunes {
		errs.Body = fmt.Sprintf("Body must be %d characters or fewer.", maxBodyRunes)
	}
	return in, errs
}
