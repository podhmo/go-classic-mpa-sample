// Comment moderation: edit via a dedicated page, delete via a GET
// confirmation page, and hide/unhide as in-place POSTs — the same
// shapes the issue/discussion resources themselves use (ui-rules §1,
// §7). The three comment-like rows (issue comments, discussion
// threads, replies) share one implementation; each handler resolves
// its (resource, id) chain first, so an id that does not line up
// answers 404 like any other missing resource.
package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// commentEditPage is the shared view model of the comment edit page —
// one textarea, one submit; only the post URL differs between issue
// comments, threads and replies.
type commentEditPage struct {
	Title     string
	Heading   string
	Action    string // POST URL
	CancelURL string // back link to the post's anchor
	Body      string // current body, or the rejected draft on a 422
	Version   int64
	Error     string
	Section   string
	Workspace *store.Workspace
	User      *store.User
}

// commentDeletePage is the shared view model of the delete confirmation
// page. Noun names the post ("comment", "thread", "reply"); Replies
// counts the replies a deleted thread takes with it (0 otherwise).
type commentDeletePage struct {
	Title     string
	Heading   string
	Noun      string
	Preview   string // one-line body excerpt for confirmation
	Replies   int
	Action    string // POST URL
	CancelURL string
	Section   string
	Workspace *store.Workspace
	User      *store.User
}

// parseSubID extracts a secondary id path value ({cid}, {tid}); like
// parseID, malformed values report false for a 404.
func parseSubID(r *http.Request, name string) (int64, bool) {
	v, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || v < 1 {
		return 0, false
	}
	return v, true
}

func findComment(comments []*store.Comment, id int64) *store.Comment {
	for _, c := range comments {
		if c.ID == id {
			return c
		}
	}
	return nil
}

func findThread(threads []*store.DiscussionThread, id int64) *store.DiscussionThread {
	for _, t := range threads {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func findReply(t *store.DiscussionThread, id int64) *store.DiscussionComment {
	for _, c := range t.Comments {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// requireComment resolves /issues/{id}/comments/{cid}: the issue must
// exist and be in this workspace, the comment must belong to it. Any
// failure already wrote its response; the bool reports success.
func (h *handler) requireComment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) (*store.Issue, *store.Comment, bool) {
	id, ok1 := parseID(r)
	cid, ok2 := parseSubID(r, "cid")
	if !ok1 || !ok2 {
		h.notFound(w, r)
		return nil, nil, false
	}
	issue, err := h.store.GetIssue(r.Context(), id)
	if err != nil {
		h.issueError(w, r, err)
		return nil, nil, false
	}
	if !h.requireScope(w, r, ws, issue.WorkspaceID) {
		return nil, nil, false
	}
	comments, err := h.store.ListComments(r.Context(), id)
	if err != nil {
		h.internalError(w, r, err)
		return nil, nil, false
	}
	c := findComment(comments, cid)
	if c == nil {
		h.notFound(w, r)
		return nil, nil, false
	}
	return issue, c, true
}

// requireThread resolves /discussions/{id}/threads/{tid} the same way;
// the returned thread carries its replies (the delete-confirm page
// counts them, and the fragment swap re-renders them).
func (h *handler) requireThread(w http.ResponseWriter, r *http.Request, ws *store.Workspace) (*store.Discussion, *store.DiscussionThread, bool) {
	id, ok1 := parseID(r)
	tid, ok2 := parseSubID(r, "tid")
	if !ok1 || !ok2 {
		h.notFound(w, r)
		return nil, nil, false
	}
	discussion, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.discussionError(w, r, err)
		return nil, nil, false
	}
	if !h.requireScope(w, r, ws, discussion.WorkspaceID) {
		return nil, nil, false
	}
	threads, err := h.store.ListDiscussionThreads(r.Context(), id)
	if err != nil {
		h.internalError(w, r, err)
		return nil, nil, false
	}
	t := findThread(threads, tid)
	if t == nil {
		h.notFound(w, r)
		return nil, nil, false
	}
	return discussion, t, true
}

// requireReply resolves /discussions/{id}/threads/{tid}/comments/{cid}.
func (h *handler) requireReply(w http.ResponseWriter, r *http.Request, ws *store.Workspace) (*store.Discussion, *store.DiscussionThread, *store.DiscussionComment, bool) {
	discussion, t, ok := h.requireThread(w, r, ws)
	if !ok {
		return nil, nil, nil, false
	}
	cid, ok := parseSubID(r, "cid")
	if !ok {
		h.notFound(w, r)
		return nil, nil, nil, false
	}
	c := findReply(t, cid)
	if c == nil {
		h.notFound(w, r)
		return nil, nil, nil, false
	}
	return discussion, t, c, true
}

// excerpt bounds the delete-confirm preview to one short line — the
// body itself stays in the store, the page only needs enough of it to
// recognize the post.
func excerpt(s string) string {
	const max = 160
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}

// editCommentForm renders the comment's edit page.
func (h *handler) editCommentForm(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	issue, c, ok := h.requireComment(w, r, ws)
	if !ok {
		return
	}
	h.r.renderCommentEdit(w, http.StatusOK, commentEditPage{
		Title:     fmt.Sprintf(msgFor(r).EditComment, c.ID),
		Heading:   fmt.Sprintf(msgFor(r).EditComment, c.ID),
		Action:    fmt.Sprintf("%s/comments/%d/edit", issueURL(ws, issue.ID), c.ID),
		CancelURL: fmt.Sprintf("%s#comment-%d", issueURL(ws, issue.ID), c.ID),
		Body:      c.Body,
		Version:   c.Version,
		Section:   "issues",
		Workspace: ws,
		User:      userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}

// updateComment validates and applies the edit, then redirects back to
// the comment's anchor. The body is required — a blanked-out comment
// is not a deletion, just a 422.
func (h *handler) updateComment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	issue, c, ok := h.requireComment(w, r, ws)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	body := strings.TrimSpace(r.PostFormValue("body"))
	cancelURL := fmt.Sprintf("%s#comment-%d", issueURL(ws, issue.ID), c.ID)
	if fe := store.ValidateCommentBody(body); fe.Code != "" {
		h.r.renderCommentEdit(w, http.StatusUnprocessableEntity, commentEditPage{
			Title:     fmt.Sprintf(msgFor(r).EditComment, c.ID),
			Heading:   fmt.Sprintf(msgFor(r).EditComment, c.ID),
			Action:    fmt.Sprintf("%s/comments/%d/edit", issueURL(ws, issue.ID), c.ID),
			CancelURL: cancelURL,
			Body:      body,
			Version:   formVersion(r),
			Error:     fieldErrText(localeFromContext(r.Context()), "body", fe),
			Section:   "issues",
			Workspace: ws,
			User:      userFromContext(r.Context()),
		}, localeFromContext(r.Context()))

		return
	}
	if err := h.store.UpdateCommentVersion(r.Context(), issue.ID, c.ID, body, formVersion(r)); err != nil {
		h.issueError(w, r, err)
		return
	}
	redirectSeeOther(w, r, cancelURL)
}

// confirmDeleteComment renders the delete confirmation page. Being a
// GET, it must never delete anything itself.
func (h *handler) confirmDeleteComment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	issue, c, ok := h.requireComment(w, r, ws)
	if !ok {
		return
	}
	h.r.renderCommentDelete(w, http.StatusOK, commentDeletePage{
		Title:     fmt.Sprintf(msgFor(r).DeleteComment, c.ID),
		Heading:   fmt.Sprintf(msgFor(r).DeleteComment, c.ID),
		Noun:      "comment",
		Preview:   excerpt(c.Body),
		Action:    fmt.Sprintf("%s/comments/%d/delete", issueURL(ws, issue.ID), c.ID),
		CancelURL: fmt.Sprintf("%s#comment-%d", issueURL(ws, issue.ID), c.ID),
		Section:   "issues",
		Workspace: ws,
		User:      userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}

// deleteComment performs the deletion and returns to the detail page —
// the anchor is gone, so no fragment.
func (h *handler) deleteComment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	issue, c, ok := h.requireComment(w, r, ws)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	if err := h.store.DeleteComment(r.Context(), issue.ID, c.ID); err != nil {
		h.issueError(w, r, err)
		return
	}
	redirectSeeOther(w, r, issueURL(ws, issue.ID))
}

func (h *handler) hideComment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	h.setCommentHidden(w, r, ws, true)
}

func (h *handler) unhideComment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	h.setCommentHidden(w, r, ws, false)
}

// setCommentHidden is the shared body of hideComment/unhideComment.
// The whole <li> changes state, so a partial request swaps in the
// re-rendered fragment (self-targeting by id, like an answer mark).
func (h *handler) setCommentHidden(w http.ResponseWriter, r *http.Request, ws *store.Workspace, hide bool) {
	issue, c, ok := h.requireComment(w, r, ws)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	reason := ""
	if hide {
		reason = r.PostFormValue("reason")
		if !store.ValidHideReason(reason) {
			h.renderError(w, r, http.StatusBadRequest, msgFor(r).ErrBadRequestH, msgFor(r).ErrHideReason)
			return
		}
	}
	if err := h.store.SetCommentHidden(r.Context(), issue.ID, c.ID, reason); err != nil {
		h.issueError(w, r, err)
		return
	}
	if wantsPartial(r) {
		comments, err := h.store.ListComments(r.Context(), issue.ID)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		nc := findComment(comments, c.ID)
		if nc == nil {
			// A concurrent delete removed the row between requireComment
			// and this re-list — render would template-error on nil.
			h.notFound(w, r)
			return
		}
		h.r.renderFragment(w, "issue_detail", "issue_comment", http.StatusOK, dict("C", nc, "Workspace", ws), localeFromContext(r.Context()))

		return
	}
	redirectSeeOther(w, r, fmt.Sprintf("%s#comment-%d", issueURL(ws, issue.ID), c.ID))
}

// editThreadForm renders the thread's (the opening post's) edit page.
func (h *handler) editThreadForm(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	discussion, t, ok := h.requireThread(w, r, ws)
	if !ok {
		return
	}
	h.r.renderCommentEdit(w, http.StatusOK, commentEditPage{
		Title:     fmt.Sprintf(msgFor(r).EditThread, t.ID),
		Heading:   fmt.Sprintf(msgFor(r).EditThread, t.ID),
		Action:    fmt.Sprintf("%s/threads/%d/edit", discussionURL(ws, discussion.ID), t.ID),
		CancelURL: fmt.Sprintf("%s#thread-%d", discussionURL(ws, discussion.ID), t.ID),
		Body:      t.Body,
		Version:   t.Version,
		Section:   "discussions",
		Workspace: ws,
		User:      userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}

// updateThread validates and applies the thread-body edit.
func (h *handler) updateThread(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	discussion, t, ok := h.requireThread(w, r, ws)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	body := strings.TrimSpace(r.PostFormValue("body"))
	cancelURL := fmt.Sprintf("%s#thread-%d", discussionURL(ws, discussion.ID), t.ID)
	if fe := store.ValidateCommentBody(body); fe.Code != "" {
		h.r.renderCommentEdit(w, http.StatusUnprocessableEntity, commentEditPage{
			Title:     fmt.Sprintf(msgFor(r).EditThread, t.ID),
			Heading:   fmt.Sprintf(msgFor(r).EditThread, t.ID),
			Action:    fmt.Sprintf("%s/threads/%d/edit", discussionURL(ws, discussion.ID), t.ID),
			CancelURL: cancelURL,
			Body:      body,
			Version:   formVersion(r),
			Error:     fieldErrText(localeFromContext(r.Context()), "body", fe),
			Section:   "discussions",
			Workspace: ws,
			User:      userFromContext(r.Context()),
		}, localeFromContext(r.Context()))

		return
	}
	if err := h.store.UpdateDiscussionThreadVersion(r.Context(), discussion.ID, t.ID, body, formVersion(r)); err != nil {
		h.discussionError(w, r, err)
		return
	}
	redirectSeeOther(w, r, cancelURL)
}

// confirmDeleteThread renders the thread delete confirmation page;
// the reply count warns that a deleted thread takes its replies along.
func (h *handler) confirmDeleteThread(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	discussion, t, ok := h.requireThread(w, r, ws)
	if !ok {
		return
	}
	h.r.renderCommentDelete(w, http.StatusOK, commentDeletePage{
		Title:     fmt.Sprintf(msgFor(r).DeleteThread, t.ID),
		Heading:   fmt.Sprintf(msgFor(r).DeleteThread, t.ID),
		Noun:      "thread",
		Preview:   excerpt(t.Body),
		Replies:   len(t.Comments),
		Action:    fmt.Sprintf("%s/threads/%d/delete", discussionURL(ws, discussion.ID), t.ID),
		CancelURL: fmt.Sprintf("%s#thread-%d", discussionURL(ws, discussion.ID), t.ID),
		Section:   "discussions",
		Workspace: ws,
		User:      userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}

// deleteThread removes the thread and its replies (CASCADE).
func (h *handler) deleteThread(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	discussion, t, ok := h.requireThread(w, r, ws)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	if err := h.store.DeleteDiscussionThread(r.Context(), discussion.ID, t.ID); err != nil {
		h.discussionError(w, r, err)
		return
	}
	redirectSeeOther(w, r, discussionURL(ws, discussion.ID))
}

func (h *handler) hideThread(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	h.setThreadHidden(w, r, ws, true)
}

func (h *handler) unhideThread(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	h.setThreadHidden(w, r, ws, false)
}

// setThreadHidden is the shared body of hideThread/unhideThread. The
// swap fragment is the whole thread <li> — it re-renders the replies
// too, so the minimized state and the still-visible replies stay in
// sync.
func (h *handler) setThreadHidden(w http.ResponseWriter, r *http.Request, ws *store.Workspace, hide bool) {
	discussion, t, ok := h.requireThread(w, r, ws)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	reason := ""
	if hide {
		reason = r.PostFormValue("reason")
		if !store.ValidHideReason(reason) {
			h.renderError(w, r, http.StatusBadRequest, msgFor(r).ErrBadRequestH, msgFor(r).ErrHideReason)
			return
		}
	}
	if err := h.store.SetDiscussionThreadHidden(r.Context(), discussion.ID, t.ID, reason); err != nil {
		h.discussionError(w, r, err)
		return
	}
	if wantsPartial(r) {
		threads, err := h.store.ListDiscussionThreads(r.Context(), discussion.ID)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		nt := findThread(threads, t.ID)
		if nt == nil {
			h.notFound(w, r)
			return
		}
		h.r.renderFragment(w, "discussion_detail", "discussion_comment", http.StatusOK, dict("Discussion", discussion, "C", nt, "Workspace", ws), localeFromContext(r.Context()))

		return
	}
	redirectSeeOther(w, r, fmt.Sprintf("%s#thread-%d", discussionURL(ws, discussion.ID), t.ID))
}

// editReplyForm renders the reply's edit page.
func (h *handler) editReplyForm(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	discussion, t, c, ok := h.requireReply(w, r, ws)
	if !ok {
		return
	}
	h.r.renderCommentEdit(w, http.StatusOK, commentEditPage{
		Title:     fmt.Sprintf(msgFor(r).EditComment, c.ID),
		Heading:   fmt.Sprintf(msgFor(r).EditComment, c.ID),
		Action:    fmt.Sprintf("%s/threads/%d/comments/%d/edit", discussionURL(ws, discussion.ID), t.ID, c.ID),
		CancelURL: fmt.Sprintf("%s#comment-%d", discussionURL(ws, discussion.ID), c.ID),
		Body:      c.Body,
		Version:   c.Version,
		Section:   "discussions",
		Workspace: ws,
		User:      userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}

// updateReply validates and applies the reply edit.
func (h *handler) updateReply(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	discussion, t, c, ok := h.requireReply(w, r, ws)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	body := strings.TrimSpace(r.PostFormValue("body"))
	cancelURL := fmt.Sprintf("%s#comment-%d", discussionURL(ws, discussion.ID), c.ID)
	if fe := store.ValidateCommentBody(body); fe.Code != "" {
		h.r.renderCommentEdit(w, http.StatusUnprocessableEntity, commentEditPage{
			Title:     fmt.Sprintf(msgFor(r).EditComment, c.ID),
			Heading:   fmt.Sprintf(msgFor(r).EditComment, c.ID),
			Action:    fmt.Sprintf("%s/threads/%d/comments/%d/edit", discussionURL(ws, discussion.ID), t.ID, c.ID),
			CancelURL: cancelURL,
			Body:      body,
			Version:   formVersion(r),
			Error:     fieldErrText(localeFromContext(r.Context()), "body", fe),
			Section:   "discussions",
			Workspace: ws,
			User:      userFromContext(r.Context()),
		}, localeFromContext(r.Context()))

		return
	}
	if err := h.store.UpdateDiscussionCommentVersion(r.Context(), discussion.ID, t.ID, c.ID, body, formVersion(r)); err != nil {
		h.discussionError(w, r, err)
		return
	}
	redirectSeeOther(w, r, cancelURL)
}

// confirmDeleteReply renders the reply delete confirmation page.
func (h *handler) confirmDeleteReply(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	discussion, t, c, ok := h.requireReply(w, r, ws)
	if !ok {
		return
	}
	h.r.renderCommentDelete(w, http.StatusOK, commentDeletePage{
		Title:     fmt.Sprintf(msgFor(r).DeleteComment, c.ID),
		Heading:   fmt.Sprintf(msgFor(r).DeleteComment, c.ID),
		Noun:      "comment",
		Preview:   excerpt(c.Body),
		Action:    fmt.Sprintf("%s/threads/%d/comments/%d/delete", discussionURL(ws, discussion.ID), t.ID, c.ID),
		CancelURL: fmt.Sprintf("%s#comment-%d", discussionURL(ws, discussion.ID), c.ID),
		Section:   "discussions",
		Workspace: ws,
		User:      userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}

// deleteReply removes the reply; the redirect keeps the thread anchor.
func (h *handler) deleteReply(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	discussion, t, c, ok := h.requireReply(w, r, ws)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	if err := h.store.DeleteDiscussionComment(r.Context(), discussion.ID, t.ID, c.ID); err != nil {
		h.discussionError(w, r, err)
		return
	}
	redirectSeeOther(w, r, fmt.Sprintf("%s#thread-%d", discussionURL(ws, discussion.ID), t.ID))
}

func (h *handler) hideReply(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	h.setReplyHidden(w, r, ws, true)
}

func (h *handler) unhideReply(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	h.setReplyHidden(w, r, ws, false)
}

// setReplyHidden is the shared body of hideReply/unhideReply — the
// reply <li> re-renders for the partial swap.
func (h *handler) setReplyHidden(w http.ResponseWriter, r *http.Request, ws *store.Workspace, hide bool) {
	discussion, t, c, ok := h.requireReply(w, r, ws)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	reason := ""
	if hide {
		reason = r.PostFormValue("reason")
		if !store.ValidHideReason(reason) {
			h.renderError(w, r, http.StatusBadRequest, msgFor(r).ErrBadRequestH, msgFor(r).ErrHideReason)
			return
		}
	}
	if err := h.store.SetDiscussionCommentHidden(r.Context(), discussion.ID, t.ID, c.ID, reason); err != nil {
		h.discussionError(w, r, err)
		return
	}
	if wantsPartial(r) {
		threads, err := h.store.ListDiscussionThreads(r.Context(), discussion.ID)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		var reply *store.DiscussionComment
		if nt := findThread(threads, t.ID); nt != nil {
			reply = findReply(nt, c.ID)
		}
		if reply == nil {
			h.notFound(w, r)
			return
		}
		h.r.renderFragment(w, "discussion_detail", "discussion_reply", http.StatusOK, dict("Discussion", discussion, "C", reply, "Workspace", ws), localeFromContext(r.Context()))

		return
	}
	redirectSeeOther(w, r, fmt.Sprintf("%s#comment-%d", discussionURL(ws, discussion.ID), c.ID))
}
