// Discussion routes: /discussions mirror the /issues handlers (same PRG
// pattern), minus any state transition — discussions never close. The two
// differences the design memo (docs/sketch/ja/discussions.md) calls out are
// a required single category, one-level threaded comments, and the qa-only
// answer mark.
package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// discussionListPage is the view model for GET /discussions.
type discussionListPage struct {
	Title           string
	Discussions     []*store.Discussion
	Categories      []*store.DiscussionCategory
	Filter          store.DiscussionListFilter
	Total           int
	Page            int
	TotalPages      int
	RangeStart      int
	RangeEnd        int
	HasActiveFilter bool
	Section         string
	User            *store.User
}

// discussionDetailPage is the view model for GET /discussions/{id}.
// CommentDraft/CommentError carry a failed comment submission back into the
// form; ReplyDraftID does the same for a failed reply so the <details> on
// that comment can reopen.
type discussionDetailPage struct {
	Title        string
	Discussion   *store.Discussion
	Comments     []*store.DiscussionComment
	CommentDraft string
	CommentError string
	ReplyDraftID int64 // parent comment ID whose reply failed validation
	Section      string
	User         *store.User
}

// discussionFormPage is the view model for both the new-discussion and
// edit-discussion forms.
type discussionFormPage struct {
	Title       string
	Heading     string
	Action      string
	SubmitLabel string
	Input       store.DiscussionInput
	Categories  []*store.DiscussionCategory
	Errors      discussionFormErrors
	Section     string
	User        *store.User
}

// discussionFormErrors carries per-field validation messages for the
// discussion form.
type discussionFormErrors struct {
	Title    string
	Body     string
	Category string
}

func (e discussionFormErrors) any() bool { return e.Title != "" || e.Body != "" || e.Category != "" }

// discussionDeletePage is the view model for the delete confirmation page.
type discussionDeletePage struct {
	Title      string
	Discussion *store.Discussion
	Section    string
	User       *store.User
}

// listDiscussions handles GET /discussions: category filter, keyword
// search, answered filter, sorting and pagination.
func (h *handler) listDiscussions(w http.ResponseWriter, r *http.Request) {
	f := parseDiscussionFilter(r)

	discussions, total, err := h.store.ListDiscussions(r.Context(), f)
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	// Same page-clamp as the issue list: a page past the end would render
	// the misleading empty state with no way back.
	totalPages := (total + perPage - 1) / perPage
	if total > 0 && f.Page > totalPages {
		f.Page = totalPages
		discussions, total, err = h.store.ListDiscussions(r.Context(), f)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
	}

	categories, err := h.store.ListDiscussionCategories(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	data := discussionListPage{
		Title:       "Discussions",
		Discussions: discussions,
		Categories:  categories,
		Filter:      f,
		Total:       total,
		Page:        f.Page,
		TotalPages:  totalPages,
		HasActiveFilter: f.Query != "" || f.CategorySlug != "" ||
			f.Answered != "" || f.Sort != "",
		Section: "discussions",
		User:    userFromContext(r.Context()),
	}
	if total > 0 {
		data.RangeStart = (f.Page-1)*perPage + 1
		data.RangeEnd = min(f.Page*perPage, total)
	}
	// Same external speculation rules as the issue list; the shared rules
	// file also matches .discussion-list a.issue-title.
	w.Header().Set("Speculation-Rules", `"/static/speculation-rules.json"`)
	h.r.renderDiscussionList(w, http.StatusOK, data)
}

// newDiscussion renders the empty create form.
func (h *handler) newDiscussion(w http.ResponseWriter, r *http.Request) {
	categories, err := h.store.ListDiscussionCategories(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.r.renderDiscussionForm(w, http.StatusOK, discussionFormPage{
		Title:       "New discussion",
		Heading:     "New discussion",
		Action:      "/discussions",
		SubmitLabel: "Create discussion",
		Categories:  categories,
		Section:     "discussions",
		User:        userFromContext(r.Context()),
	})
}

// createDiscussion validates POST /discussions and redirects to the new
// discussion.
func (h *handler) createDiscussion(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r) {
		return
	}
	in, errs := h.validateDiscussionInput(w, r)
	if errs == nil {
		id, err := h.store.CreateDiscussion(r.Context(), *in, authorIDFromContext(r.Context()))
		switch {
		case errors.Is(err, store.ErrInvalidCategory):
			errs = &discussionFormErrors{Category: "Category is required."}
		case err != nil:
			h.internalError(w, r, err)
			return
		case err == nil:
			http.Redirect(w, r, discussionURL(id), http.StatusSeeOther)
			return
		}
	}
	h.renderDiscussionFormError(w, r, "New discussion", "New discussion",
		"/discussions", "Create discussion", *in, *errs)
}

// showDiscussion renders the detail page including threaded comments and
// the comment form.
func (h *handler) showDiscussion(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	discussion, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.discussionError(w, r, err)
		return
	}
	comments, err := h.store.ListDiscussionComments(r.Context(), id)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.r.renderDiscussionDetail(w, http.StatusOK, discussionDetailPage{
		Title:      fmt.Sprintf("#%d %s", discussion.ID, discussion.Title),
		Discussion: discussion,
		Comments:   comments,
		Section:    "discussions",
		User:       userFromContext(r.Context()),
	})
}

// editDiscussion renders the edit form populated with the current values.
func (h *handler) editDiscussion(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	discussion, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.discussionError(w, r, err)
		return
	}
	categories, err := h.store.ListDiscussionCategories(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.r.renderDiscussionForm(w, http.StatusOK, discussionFormPage{
		Title:       fmt.Sprintf("Edit discussion #%d", discussion.ID),
		Heading:     fmt.Sprintf("Edit discussion #%d", discussion.ID),
		Action:      discussionURL(id) + "/edit",
		SubmitLabel: "Save changes",
		Input: store.DiscussionInput{
			Title:      discussion.Title,
			Body:       discussion.Body,
			CategoryID: discussion.Category.ID,
		},
		Categories: categories,
		Section:    "discussions",
		User:       userFromContext(r.Context()),
	})
}

// updateDiscussion validates POST /discussions/{id}/edit and redirects to
// the detail page.
func (h *handler) updateDiscussion(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	// Same rule as updateIssue: ANY POST to a missing discussion is a 404,
	// even one that would fail validation.
	if _, err := h.store.GetDiscussion(r.Context(), id); err != nil {
		h.discussionError(w, r, err)
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	in, errs := h.validateDiscussionInput(w, r)
	if errs == nil {
		err := h.store.UpdateDiscussion(r.Context(), id, *in)
		switch {
		case errors.Is(err, store.ErrInvalidCategory):
			errs = &discussionFormErrors{Category: "Category is required."}
		case err != nil:
			h.discussionError(w, r, err)
			return
		case err == nil:
			http.Redirect(w, r, discussionURL(id), http.StatusSeeOther)
			return
		}
	}
	h.renderDiscussionFormError(w, r,
		fmt.Sprintf("Edit discussion #%d", id), fmt.Sprintf("Edit discussion #%d", id),
		discussionURL(id)+"/edit", "Save changes", *in, *errs)
}

// confirmDeleteDiscussion renders the delete confirmation page. Being a
// GET, it must never delete anything itself.
func (h *handler) confirmDeleteDiscussion(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	discussion, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.discussionError(w, r, err)
		return
	}
	h.r.renderDiscussionDelete(w, http.StatusOK, discussionDeletePage{
		Title:      fmt.Sprintf("Delete discussion #%d", discussion.ID),
		Discussion: discussion,
		Section:    "discussions",
		User:       userFromContext(r.Context()),
	})
}

// deleteDiscussion performs the deletion and returns to the list.
func (h *handler) deleteDiscussion(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	if err := h.store.DeleteDiscussion(r.Context(), id); err != nil {
		h.discussionError(w, r, err)
		return
	}
	http.Redirect(w, r, "/discussions", http.StatusSeeOther)
}

// createDiscussionComment validates POST /discussions/{id}/comments — a
// top-level comment, or a reply when the hidden parent_id is present — and
// redirects back to the detail page anchored at the new comment.
func (h *handler) createDiscussionComment(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	body := strings.TrimSpace(r.PostFormValue("body"))
	var parentID int64
	if v := r.PostFormValue("parent_id"); v != "" {
		if pid, err := strconv.ParseInt(v, 10, 64); err == nil && pid > 0 {
			parentID = pid
		}
	}

	var errMsg string
	switch {
	case body == "":
		errMsg = "Comment is required."
	case utf8.RuneCountInString(body) > maxBodyRunes:
		errMsg = fmt.Sprintf("Comment must be %d characters or fewer.", maxBodyRunes)
	}

	commentID, err := h.store.CreateDiscussionComment(r.Context(), id, parentID, body, authorIDFromContext(r.Context()))
	switch {
	case err == nil:
		http.Redirect(w, r, fmt.Sprintf("%s#comment-%d", discussionURL(id), commentID), http.StatusSeeOther)
		return
	case errors.Is(err, store.ErrInvalidParent):
		errMsg = "Replies can only be added to top-level comments."
	case errors.Is(err, store.ErrNotFound):
		h.notFound(w, r)
		return
	default:
		h.internalError(w, r, err)
		return
	}

	// Re-render the detail page with the error. ReplyDraftID reopens the
	// <details> on the parent comment when the failed post was a reply.
	discussion, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.discussionError(w, r, err)
		return
	}
	comments, err := h.store.ListDiscussionComments(r.Context(), id)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	// When the failed reply's parent is not a displayed top-level comment
	// (a nested reply, or a comment of another discussion), no per-comment
	// form can hold the draft — drop ReplyDraftID so the bottom comment
	// form keeps the text instead of losing it.
	if parentID != 0 {
		found := false
		for _, c := range comments {
			if c.ID == parentID {
				found = true
				break
			}
		}
		if !found {
			parentID = 0
		}
	}
	h.r.renderDiscussionDetail(w, http.StatusUnprocessableEntity, discussionDetailPage{
		Title:        fmt.Sprintf("#%d %s", discussion.ID, discussion.Title),
		Discussion:   discussion,
		Comments:     comments,
		CommentDraft: body,
		CommentError: errMsg,
		ReplyDraftID: parentID,
		Section:      "discussions",
		User:         userFromContext(r.Context()),
	})
}

// setCommentAnswer handles POST /discussions/{id}/comments/{cid}/answer.
// The target state arrives as a hidden field ("1" to mark, "0" to unmark) —
// the same hidden-field toggle pattern as setStatus.
func (h *handler) setCommentAnswer(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	cid, err := strconv.ParseInt(r.PathValue("cid"), 10, 64)
	if err != nil || cid < 1 {
		h.notFound(w, r)
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	var answer bool
	switch r.PostFormValue("answer") {
	case "1":
		answer = true
	case "0":
	default:
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "Invalid answer value.")
		return
	}
	err = h.store.SetCommentAnswer(r.Context(), id, cid, answer)
	switch {
	case err == nil:
		http.Redirect(w, r, fmt.Sprintf("%s#comment-%d", discussionURL(id), cid), http.StatusSeeOther)
	case errors.Is(err, store.ErrNotFound):
		h.notFound(w, r)
	case errors.Is(err, store.ErrNotAnswerable):
		h.renderError(w, r, http.StatusUnprocessableEntity, "Cannot mark as answer",
			"Only top-level comments on a Q&A discussion can be marked as the answer.")
	default:
		h.internalError(w, r, err)
	}
}

// discussionError maps store.ErrNotFound to a 404 page and delegates
// everything else to the generic 500 handler.
func (h *handler) discussionError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	h.internalError(w, r, err)
}

// renderDiscussionFormError re-renders the discussion form with 422.
func (h *handler) renderDiscussionFormError(w http.ResponseWriter, r *http.Request,
	title, heading, action, submitLabel string, in store.DiscussionInput, errs discussionFormErrors) {
	categories, err := h.store.ListDiscussionCategories(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.r.renderDiscussionForm(w, http.StatusUnprocessableEntity, discussionFormPage{
		Title:       title,
		Heading:     heading,
		Action:      action,
		SubmitLabel: submitLabel,
		Input:       in,
		Categories:  categories,
		Errors:      errs,
		Section:     "discussions",
		User:        userFromContext(r.Context()),
	})
}

func discussionURL(id int64) string {
	return fmt.Sprintf("/discussions/%d", id)
}

// validateDiscussionInput extracts and validates the discussion form
// fields; nil errors mean the input is usable. It only reports validation
// problems — the caller must have parsed the form already.
func (h *handler) validateDiscussionInput(_ http.ResponseWriter, r *http.Request) (*store.DiscussionInput, *discussionFormErrors) {
	in := store.DiscussionInput{
		Title: strings.TrimSpace(r.PostFormValue("title")),
		Body:  strings.TrimSpace(r.PostFormValue("body")),
	}
	var errs discussionFormErrors
	switch {
	case in.Title == "":
		errs.Title = "Title is required."
	case utf8.RuneCountInString(in.Title) > maxTitleRunes:
		errs.Title = fmt.Sprintf("Title must be %d characters or fewer.", maxTitleRunes)
	}
	if utf8.RuneCountInString(in.Body) > maxBodyRunes {
		errs.Body = fmt.Sprintf("Body must be %d characters or fewer.", maxBodyRunes)
	}
	if v := r.PostFormValue("category_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			in.CategoryID = id
		}
	}
	// A discussion must belong to exactly one category — the select's
	// first option is a disabled placeholder, so a missing/zero value is a
	// validation failure, not a default.
	if in.CategoryID == 0 {
		errs.Category = "Category is required."
	}
	if errs.any() {
		return &in, &errs
	}
	return &in, nil
}

// parseDiscussionFilter reads and normalizes the discussion list query
// parameters.
func parseDiscussionFilter(r *http.Request) store.DiscussionListFilter {
	q := r.URL.Query()
	f := store.DiscussionListFilter{
		CategorySlug: q.Get("category"),
		Answered:     q.Get("answered"),
		Query:        strings.TrimSpace(q.Get("q")),
		Sort:         q.Get("sort"),
		Page:         1,
		PerPage:      perPage,
	}
	if f.Answered != "yes" && f.Answered != "no" {
		f.Answered = ""
	}
	if f.Sort != "oldest" {
		f.Sort = ""
	}
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 0 && n <= 1_000_000 {
		f.Page = n
	}
	return f
}

// discussionsURL builds /discussions URLs carrying the current filter
// state, so pagination and category links preserve active filters.
func discussionsURL(f store.DiscussionListFilter) string {
	v := url.Values{}
	if f.CategorySlug != "" {
		v.Set("category", f.CategorySlug)
	}
	if f.Answered != "" {
		v.Set("answered", f.Answered)
	}
	if f.Query != "" {
		v.Set("q", f.Query)
	}
	if f.Sort != "" {
		v.Set("sort", f.Sort)
	}
	if f.Page > 1 {
		v.Set("page", strconv.Itoa(f.Page))
	}
	if enc := v.Encode(); enc != "" {
		return "/discussions?" + enc
	}
	return "/discussions"
}

// dListURL keeps the filter but moves to another page.
func dListURL(f store.DiscussionListFilter, page int) string {
	f.Page = page
	return discussionsURL(f)
}
