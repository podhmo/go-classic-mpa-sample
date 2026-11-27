// Discussion routes: /discussions mirror the /issues handlers (same PRG
// pattern), minus any state transition — discussions never close. The two
// differences the design memo (docs/sketch/ja/discussions.md) calls out are
// a required single category, one-level comment threads, and the qa-only
// answer mark.
package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/export"
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
	Workspace       *store.Workspace
	User            *store.User
}

// discussionDetailPage is the view model for GET /discussions/{id}.
// CommentDraft/CommentError carry a failed thread-post submission back
// into the form; ThreadDraftID does the same for a failed reply so that
// thread's reply form keeps the draft and the error.
type discussionDetailPage struct {
	Title         string
	Discussion    *store.Discussion
	Threads       []*store.DiscussionThread
	CommentDraft  string
	CommentError  string
	ThreadDraftID int64 // thread ID whose reply failed validation
	Section       string
	Workspace     *store.Workspace
	User          *store.User
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
	Labels      []*store.Label
	Errors      discussionFormErrors
	Section     string
	Workspace   *store.Workspace
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
	Workspace  *store.Workspace
	User       *store.User
}

// listDiscussions handles GET /discussions: category filter, keyword
// search, answered filter, sorting and pagination.
func (h *handler) listDiscussions(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	f := parseDiscussionFilter(r)

	discussions, total, err := h.store.ListDiscussions(r.Context(), ws.ID, f)
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	// Same page-clamp as the issue list: a page past the end would render
	// the misleading empty state with no way back.
	totalPages := (total + perPage - 1) / perPage
	if total > 0 && f.Page > totalPages {
		f.Page = totalPages
		discussions, total, err = h.store.ListDiscussions(r.Context(), ws.ID, f)
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
		Title:       msgFor(r).DiscussionsTitle,
		Discussions: discussions,
		Categories:  categories,
		Filter:      f,
		Total:       total,
		Page:        f.Page,
		TotalPages:  totalPages,
		HasActiveFilter: f.Query != "" || f.CategorySlug != "" ||
			f.Answered != "" || f.Sort != "",
		Section:   "discussions",
		Workspace: ws,
		User:      userFromContext(r.Context()),
	}
	if total > 0 {
		data.RangeStart = (f.Page-1)*perPage + 1
		data.RangeEnd = min(f.Page*perPage, total)
	}
	// Same external speculation rules as the issue list; the shared rules
	// file also matches .discussion-list a.issue-title.
	w.Header().Set("Speculation-Rules", strconv.Quote(staticURL("speculation-rules.json")))
	h.r.renderDiscussionList(w, http.StatusOK, data, localeFromContext(r.Context()))
}

// newDiscussion renders the empty create form.
func (h *handler) newDiscussion(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	categories, labels, err := h.discussionFormChoices(r, ws)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.r.renderDiscussionForm(w, http.StatusOK, discussionFormPage{
		Title:       msgFor(r).NewDiscussion,
		Heading:     msgFor(r).NewDiscussion,
		Action:      "/w/" + ws.Slug + "/discussions",
		SubmitLabel: msgFor(r).CreateDiscussion,
		Categories:  categories,
		Labels:      labels,
		Section:     "discussions",
		Workspace:   ws,
		User:        userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}

// discussionFormChoices loads the category list and the workspace's
// discussion-kind labels the form's picker needs.
func (h *handler) discussionFormChoices(r *http.Request, ws *store.Workspace) ([]*store.DiscussionCategory, []*store.Label, error) {
	categories, err := h.store.ListDiscussionCategories(r.Context())
	if err != nil {
		return nil, nil, err
	}
	labels, err := h.store.ListLabels(r.Context(), ws.ID, store.KindLabelDiscussion)
	if err != nil {
		return nil, nil, err
	}
	return categories, labels, nil
}

// createDiscussion validates POST /discussions and redirects to the new
// discussion.
func (h *handler) createDiscussion(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	if !h.parseForm(w, r) {
		return
	}
	in, errs := h.validateDiscussionInput(w, r)
	if errs == nil {
		id, err := h.store.CreateDiscussion(r.Context(), ws.ID, *in, authorIDFromContext(r.Context()))
		switch {
		case errors.Is(err, store.ErrInvalidCategory):
			errs = &discussionFormErrors{Category: msgFor(r).CategoryRequired}
		case err != nil:
			h.internalError(w, r, err)
			return
		case err == nil:
			http.Redirect(w, r, discussionURL(ws, id), http.StatusSeeOther)
			return
		}
	}
	h.renderDiscussionFormError(w, r, ws, msgFor(r).NewDiscussion, msgFor(r).NewDiscussion,
		"/w/"+ws.Slug+"/discussions", msgFor(r).CreateDiscussion, *in, *errs)
}

// showDiscussion renders the detail page including threaded comments and
// the comment form.
func (h *handler) showDiscussion(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
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
	if !h.requireScope(w, r, ws, discussion.WorkspaceID) {
		return
	}
	threads, err := h.store.ListDiscussionThreads(r.Context(), id)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.r.renderDiscussionDetail(w, http.StatusOK, discussionDetailPage{
		Title:      fmt.Sprintf("#%d %s", discussion.ID, discussion.Title),
		Discussion: discussion,
		Threads:    threads,
		Section:    "discussions",
		Workspace:  ws,
		User:       userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}

// discussionMarkdown serves GET /discussions/{id}/markdown — the cookie-
// session twin of the API endpoint, fetched by the "Copy as markdown"
// action.
func (h *handler) discussionMarkdown(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
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
	if !h.requireScope(w, r, ws, discussion.WorkspaceID) {
		return
	}
	threads, err := h.store.ListDiscussionThreads(r.Context(), id)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeMarkdown(w, fmt.Sprintf("discussion-%d.md", id), export.DiscussionMarkdown(discussion, threads, ws.Slug))
}

// editDiscussion renders the edit form populated with the current values.
func (h *handler) editDiscussion(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
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
	if !h.requireScope(w, r, ws, discussion.WorkspaceID) {
		return
	}
	categories, labels, err := h.discussionFormChoices(r, ws)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	in := store.DiscussionInput{
		Title:      discussion.Title,
		Body:       discussion.Body,
		CategoryID: discussion.Category.ID,
		LabelIDs:   make([]int64, 0, len(discussion.Labels)),
	}
	for _, l := range discussion.Labels {
		in.LabelIDs = append(in.LabelIDs, l.ID)
	}
	h.r.renderDiscussionForm(w, http.StatusOK, discussionFormPage{
		Title:       fmt.Sprintf(msgFor(r).EditDiscussion, discussion.ID),
		Heading:     fmt.Sprintf(msgFor(r).EditDiscussion, discussion.ID),
		Action:      discussionURL(ws, id) + "/edit",
		SubmitLabel: msgFor(r).BtnSaveChanges,
		Input:       in,
		Categories:  categories,
		Labels:      labels,
		Section:     "discussions",
		Workspace:   ws,
		User:        userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}

// updateDiscussion validates POST /discussions/{id}/edit and redirects to
// the detail page.
func (h *handler) updateDiscussion(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	// Same rule as updateIssue: ANY POST to a missing discussion is a 404,
	// even one that would fail validation.
	discussion, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.discussionError(w, r, err)
		return
	}
	if !h.requireScope(w, r, ws, discussion.WorkspaceID) {
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
			errs = &discussionFormErrors{Category: msgFor(r).CategoryRequired}
		case err != nil:
			h.discussionError(w, r, err)
			return
		case err == nil:
			http.Redirect(w, r, discussionURL(ws, id), http.StatusSeeOther)
			return
		}
	}
	h.renderDiscussionFormError(w, r, ws,
		fmt.Sprintf(msgFor(r).EditDiscussion, id), fmt.Sprintf(msgFor(r).EditDiscussion, id),
		discussionURL(ws, id)+"/edit", msgFor(r).BtnSaveChanges, *in, *errs)
}

// confirmDeleteDiscussion renders the delete confirmation page. Being a
// GET, it must never delete anything itself.
func (h *handler) confirmDeleteDiscussion(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
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
	if !h.requireScope(w, r, ws, discussion.WorkspaceID) {
		return
	}
	h.r.renderDiscussionDelete(w, http.StatusOK, discussionDeletePage{
		Title:      fmt.Sprintf(msgFor(r).DeleteDiscussion, discussion.ID),
		Discussion: discussion,
		Section:    "discussions",
		Workspace:  ws,
		User:       userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}

// deleteDiscussion performs the deletion and returns to the list.
func (h *handler) deleteDiscussion(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
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
	if !h.requireScope(w, r, ws, discussion.WorkspaceID) {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	if err := h.store.DeleteDiscussion(r.Context(), id); err != nil {
		h.discussionError(w, r, err)
		return
	}
	http.Redirect(w, r, "/w/"+ws.Slug+"/discussions", http.StatusSeeOther)
}

// createDiscussionComment validates POST /discussions/{id}/comments — a
// new thread, or a reply when the hidden thread_id is present — and
// redirects back to the detail page anchored at the new comment. With
// X-Partial it answers a fragment instead: the new <li> on success, or the
// error-marked form (the thread's reply form, or the bottom form)
// on a validation failure (422).
func (h *handler) createDiscussionComment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	discussion0, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.discussionError(w, r, err)
		return
	}
	if !h.requireScope(w, r, ws, discussion0.WorkspaceID) {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	body := strings.TrimSpace(r.PostFormValue("body"))
	partial := wantsPartial(r)
	var threadID int64
	var errMsg string
	if v := r.PostFormValue("thread_id"); v != "" {
		// A present but malformed thread_id must not degrade to a
		// new thread — the poster asked for a reply.
		if tid, err := strconv.ParseInt(v, 10, 64); err == nil && tid > 0 {
			threadID = tid
		} else {
			errMsg = msgFor(r).ErrReplyTarget
		}
	}
	if errMsg == "" {
		errMsg = fieldErrText(localeFromContext(r.Context()), "body", store.ValidateCommentBody(body))
	}

	if errMsg == "" {
		var commentID int64
		var err error
		if threadID == 0 {
			commentID, err = h.store.CreateDiscussionThread(r.Context(), id, body, authorIDFromContext(r.Context()))
		} else {
			commentID, err = h.store.CreateDiscussionComment(r.Context(), id, threadID, body, authorIDFromContext(r.Context()))
		}
		switch {
		case err == nil:
			if partial {
				h.renderNewDiscussionComment(w, r, ws, id, threadID, commentID, body)
				return
			}
			anchor := "comment"
			if threadID == 0 {
				anchor = "thread"
			}
			http.Redirect(w, r, fmt.Sprintf("%s#%s-%d", discussionURL(ws, id), anchor, commentID), http.StatusSeeOther)
			return
		case errors.Is(err, store.ErrInvalidThread):
			errMsg = msgFor(r).ErrThreadOnly
		case errors.Is(err, store.ErrNotFound):
			h.notFound(w, r)
			return
		default:
			h.internalError(w, r, err)
			return
		}
	}

	// Re-render the detail page with the error. ThreadDraftID points the
	// failed reply's draft and error at that thread's reply form.
	discussion, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.discussionError(w, r, err)
		return
	}
	threads, err := h.store.ListDiscussionThreads(r.Context(), id)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	// When the failed reply's thread is not among the displayed threads
	// (another discussion's thread), no per-thread form can hold the draft
	// — drop ThreadDraftID so the bottom comment form keeps the text
	// instead of losing it.
	var thread *store.DiscussionThread
	if threadID != 0 {
		for _, t := range threads {
			if t.ID == threadID {
				thread = t
				break
			}
		}
		if thread == nil {
			threadID = 0
		}
	}
	if partial {
		if thread != nil {
			h.r.renderFragment(w, "discussion_detail", "discussion_reply_form", http.StatusUnprocessableEntity, dict("Discussion", discussion, "C", thread, "Workspace", ws,
				"ThreadDraftID", threadID, "CommentDraft", body, "CommentError", errMsg), localeFromContext(r.Context()))

			return
		}
		h.r.renderFragment(w, "discussion_detail", "discussion_comment_form", http.StatusUnprocessableEntity, discussionDetailPage{
			Discussion:   discussion,
			CommentDraft: body,
			CommentError: errMsg,
			Workspace:    ws,
			User:         userFromContext(r.Context()),
		}, localeFromContext(r.Context()))

		return
	}
	h.r.renderDiscussionDetail(w, http.StatusUnprocessableEntity, discussionDetailPage{
		Title:         fmt.Sprintf("#%d %s", discussion.ID, discussion.Title),
		Discussion:    discussion,
		Threads:       threads,
		CommentDraft:  body,
		CommentError:  errMsg,
		ThreadDraftID: threadID,
		Section:       "discussions",
		Workspace:     ws,
		User:          userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}

// renderNewDiscussionComment answers a partial POST with the new post's
// <li>: a reply fragment when threadID is set, a thread fragment
// otherwise (the latter also carries the reply form and the qa
// answer button, so it needs the parent discussion's category). Either
// way the fragment starts with the header meta as a data-swap node — the
// post bumped discussion.updated_at.
func (h *handler) renderNewDiscussionComment(w http.ResponseWriter, r *http.Request, ws *store.Workspace, id, threadID, commentID int64, body string) {
	discussion, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.discussionError(w, r, err)
		return
	}
	if threadID != 0 {
		comment := &store.DiscussionComment{
			ID:        commentID,
			ThreadID:  threadID,
			Body:      body,
			Author:    userFromContext(r.Context()),
			CreatedAt: time.Now(),
		}
		h.r.renderFragment(w, "discussion_detail", "discussion_reply_append", http.StatusOK, dict("Discussion", discussion, "C", comment, "Workspace", ws), localeFromContext(r.Context()))

		return
	}
	thread := &store.DiscussionThread{
		ID:           commentID,
		DiscussionID: id,
		Body:         body,
		Author:       userFromContext(r.Context()),
		CreatedAt:    time.Now(),
	}
	h.r.renderFragment(w, "discussion_detail", "discussion_comment_append", http.StatusOK, dict("Discussion", discussion, "C", thread, "Workspace", ws), localeFromContext(r.Context()))

}

// setCommentAnswer handles POST /discussions/{id}/comments/{cid}/answer.
// The {cid} segment names the thread — a thread is the only markable
// post, so the route predating the rename still reads naturally. The
// target state arrives as a hidden field ("1" to mark, "0" to unmark) —
// the same hidden-field toggle pattern as setStatus.
func (h *handler) setCommentAnswer(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
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
	discussion0, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.discussionError(w, r, err)
		return
	}
	if !h.requireScope(w, r, ws, discussion0.WorkspaceID) {
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
		h.renderError(w, r, http.StatusBadRequest, msgFor(r).ErrBadRequestH, msgFor(r).ErrAnswerValue)
		return
	}
	// A partial request must know the pre-update mark holder: marking moves
	// the badge, so the swap fragment re-renders the previous answer's <li>
	// too. Only the holder's id survives the write — fake stores share
	// comment pointers (storybook's does), so IsAnswer flags are
	// post-mutation by the time the response renders.
	var prevID int64
	if wantsPartial(r) {
		ts, err := h.store.ListDiscussionThreads(r.Context(), id)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		for _, t := range ts {
			if t.IsAnswer {
				prevID = t.ID
			}
		}
	}
	err = h.store.SetThreadAnswer(r.Context(), id, cid, answer)
	switch {
	case err == nil:
		if wantsPartial(r) {
			h.renderAnswerSwap(w, r, ws, id, cid, prevID)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("%s#thread-%d", discussionURL(ws, id), cid), http.StatusSeeOther)
	case errors.Is(err, store.ErrNotFound):
		h.notFound(w, r)
	case errors.Is(err, store.ErrNotAnswerable):
		h.renderError(w, r, http.StatusUnprocessableEntity, msgFor(r).ErrAnswerQAH, msgFor(r).ErrAnswerQAM)
	default:
		h.internalError(w, r, err)
	}
}

// renderAnswerSwap answers a partial answer-mark POST with every <li>
// that changed: the toggled thread plus, when the mark moved, the
// thread that carried it before (prevID, 0 when nothing was marked).
// Each <li> self-targets by id in the client's replace swap.
func (h *handler) renderAnswerSwap(w http.ResponseWriter, r *http.Request, ws *store.Workspace, id, cid, prevID int64) {
	discussion, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.discussionError(w, r, err)
		return
	}
	ts, err := h.store.ListDiscussionThreads(r.Context(), id)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	var nodes []*store.DiscussionThread
	for _, t := range ts {
		if t.ID == cid || (prevID != 0 && t.ID == prevID) {
			nodes = append(nodes, t)
		}
	}
	if len(nodes) == 0 {
		h.notFound(w, r)
		return
	}
	h.r.renderFragment(w, "discussion_detail", "discussion_comment_swap", http.StatusOK, dict("Discussion", discussion, "Comments", nodes, "Workspace", ws), localeFromContext(r.Context()))

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
func (h *handler) renderDiscussionFormError(w http.ResponseWriter, r *http.Request, ws *store.Workspace,
	title, heading, action, submitLabel string, in store.DiscussionInput, errs discussionFormErrors) {
	categories, labels, err := h.discussionFormChoices(r, ws)
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
		Labels:      labels,
		Errors:      errs,
		Section:     "discussions",
		Workspace:   ws,
		User:        userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}

func discussionURL(ws *store.Workspace, id int64) string {
	return fmt.Sprintf("/w/%s/discussions/%d", ws.Slug, id)
}

// validateDiscussionInput extracts and validates the discussion form
// fields; nil errors mean the input is usable. It only reports validation
// problems — the caller must have parsed the form already.
func (h *handler) validateDiscussionInput(_ http.ResponseWriter, r *http.Request) (*store.DiscussionInput, *discussionFormErrors) {
	in := store.DiscussionInput{
		Title: strings.TrimSpace(r.PostFormValue("title")),
		Body:  strings.TrimSpace(r.PostFormValue("body")),
	}
	if v := r.PostFormValue("category_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			in.CategoryID = id
		}
	}
	for _, v := range r.PostForm["label_id"] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			in.LabelIDs = append(in.LabelIDs, id)
		}
	}
	// A discussion must belong to exactly one category — the select's
	// first option is a disabled placeholder, so a missing/zero value is a
	// validation failure, not a default.
	fe := store.ValidateDiscussionInput(in)
	if fe.Any() {
		lang := localeFromContext(r.Context())
		return &in, &discussionFormErrors{Title: fieldErrText(lang, "title", fe["title"]), Body: fieldErrText(lang, "body", fe["body"]), Category: fieldErrText(lang, "category", fe["category"])}
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
	f.Page = parsePageQuery(q.Get("page"))
	return f
}

// discussionsURL builds the workspace's /discussions URLs carrying the
// current filter state, so pagination and category links preserve active
// filters.
func discussionsURL(ws *store.Workspace, f store.DiscussionListFilter) string {
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
		return "/w/" + ws.Slug + "/discussions?" + enc
	}
	return "/w/" + ws.Slug + "/discussions"
}

// dListURL keeps the filter but moves to another page.
func dListURL(ws *store.Workspace, f store.DiscussionListFilter, page int) string {
	f.Page = page
	return discussionsURL(ws, f)
}
