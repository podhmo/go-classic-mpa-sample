// Comment moderation endpoints — PATCH/DELETE on the post itself plus
// hide/unhide action POSTs, for the three comment-like rows (issue
// comments, discussion threads, replies). Each handler resolves the
// (resource, id) chain and the workspace scope first, so an id that
// does not line up is the same 404 as any missing resource.
package api

import (
	"net/http"
	"strings"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// hideReasonField is the shared 422 field map for an out-of-set reason
// — the allowed values ride along so CLI/API callers can self-correct.
func hideReasonField() map[string]string {
	return map[string]string{"reason": "Unknown hide reason. Valid values: " + strings.Join(store.HideReasons, ", ")}
}

// Moderation is the moderation triple embedded inline in the three
// post DTOs: edited_at appears once the body changed, hidden +
// hidden_reason describe the minimized state. Exported so the CLI's
// --json views can build the same wire shape from store rows.
type Moderation struct {
	EditedAt     *string `json:"edited_at,omitempty"`
	Hidden       bool    `json:"hidden"`
	HiddenReason string  `json:"hidden_reason,omitempty"`
}

func moderationDTO(m store.Moderation) Moderation {
	var out Moderation
	if m.Edited() {
		s := fmtTime(m.EditedAt)
		out.EditedAt = &s
	}
	out.Hidden = m.Hidden
	out.HiddenReason = m.HiddenReason
	return out
}

// findIssueComment re-reads one comment after a mutation (the DTO the
// response carries); nil when the list cannot be read or the id is gone.
func (h *handler) findIssueComment(r *http.Request, issueID, commentID int64) *store.Comment {
	comments, err := h.store.ListComments(r.Context(), issueID)
	if err != nil {
		return nil
	}
	for _, c := range comments {
		if c.ID == commentID {
			return c
		}
	}
	return nil
}

// requireIssueComment resolves /issues/{id}/comments/{cid}: parent
// issue, workspace scope, and the comment itself. Any failure already
// wrote its response.
func (h *handler) requireIssueComment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) (*store.Comment, bool) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return nil, false
	}
	cid, ok := h.pathID(w, r, "cid")
	if !ok {
		return nil, false
	}
	issue, err := h.store.GetIssue(r.Context(), id)
	if err != nil {
		h.storeError(w, err)
		return nil, false
	}
	if !h.requireScope(w, ws, issue.WorkspaceID) {
		return nil, false
	}
	c := h.findIssueComment(r, id, cid)
	if c == nil {
		writeError(w, http.StatusNotFound, "Not found.", nil)
		return nil, false
	}
	return c, true
}

// requireThread resolves /discussions/{id}/threads/{tid} the same way
// and returns the discussion id for the follow-up store call.
func (h *handler) requireThread(w http.ResponseWriter, r *http.Request, ws *store.Workspace) (int64, *store.DiscussionThread, bool) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return 0, nil, false
	}
	tid, ok := h.pathID(w, r, "tid")
	if !ok {
		return 0, nil, false
	}
	d, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.storeError(w, err)
		return 0, nil, false
	}
	if !h.requireScope(w, ws, d.WorkspaceID) {
		return 0, nil, false
	}
	t := findDiscussionThread(h.store, r, id, tid)
	if t == nil {
		writeError(w, http.StatusNotFound, "Not found.", nil)
		return 0, nil, false
	}
	return id, t, true
}

// requireReply resolves /discussions/{id}/threads/{tid}/comments/{cid}.
func (h *handler) requireReply(w http.ResponseWriter, r *http.Request, ws *store.Workspace) (int64, *store.DiscussionComment, bool) {
	id, t, ok := h.requireThread(w, r, ws)
	if !ok {
		return 0, nil, false
	}
	cid, ok := h.pathID(w, r, "cid")
	if !ok {
		return 0, nil, false
	}
	c := findDiscussionComment(t, cid)
	if c == nil {
		writeError(w, http.StatusNotFound, "Not found.", nil)
		return 0, nil, false
	}
	return id, c, true
}

// decodeHideBody reads {"reason": "..."} for a hide POST; an invalid
// reason is a 422 — the store would also reject it, but the wire error
// names the field.
func decodeHideBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	var in struct {
		Reason string `json:"reason"`
	}
	if !decodeBody(w, r, &in) {
		return "", false
	}
	if !store.ValidHideReason(in.Reason) {
		writeError(w, http.StatusUnprocessableEntity, "Validation failed.", hideReasonField())
		return "", false
	}
	return in.Reason, true
}

// decodeEditBody reads {"body": "..."} for a moderation PATCH.
func decodeEditBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	var in struct {
		Body string `json:"body"`
	}
	if !decodeBody(w, r, &in) {
		return "", false
	}
	body := strings.TrimSpace(in.Body)
	if fe := store.ValidateCommentBody(body); fe.Code != "" {
		writeError(w, http.StatusUnprocessableEntity, "Validation failed.", map[string]string{"body": fe.Message})
		return "", false
	}
	return body, true
}

// PATCH /api/v1/w/{ws}/issues/{id}/comments/{cid} {"body"} → 200 {"comment"}
// Like updateIssue the body decodes before the resource chain resolves:
// a malformed payload is a 400/422, not a 404.
func (h *handler) updateComment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	body, ok := decodeEditBody(w, r)
	if !ok {
		return
	}
	c, ok := h.requireIssueComment(w, r, ws)
	if !ok {
		return
	}
	if err := h.store.UpdateComment(r.Context(), c.IssueID, c.ID, body); err != nil {
		h.storeError(w, err)
		return
	}
	if c := h.findIssueComment(r, c.IssueID, c.ID); c != nil {
		writeJSON(w, http.StatusOK, map[string]any{"comment": commentDTO(c)})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/v1/w/{ws}/issues/{id}/comments/{cid} → 204
func (h *handler) deleteComment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	c, ok := h.requireIssueComment(w, r, ws)
	if !ok {
		return
	}
	if err := h.store.DeleteComment(r.Context(), c.IssueID, c.ID); err != nil {
		h.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST .../comments/{cid}/hide {"reason"} → 200 {"comment"}
func (h *handler) hideComment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	h.setCommentHidden(w, r, ws, true)
}

// POST .../comments/{cid}/unhide → 200 {"comment"}
func (h *handler) unhideComment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	h.setCommentHidden(w, r, ws, false)
}

func (h *handler) setCommentHidden(w http.ResponseWriter, r *http.Request, ws *store.Workspace, hide bool) {
	reason := ""
	if hide {
		var ok bool
		if reason, ok = decodeHideBody(w, r); !ok {
			return
		}
	}
	c, ok := h.requireIssueComment(w, r, ws)
	if !ok {
		return
	}
	if err := h.store.SetCommentHidden(r.Context(), c.IssueID, c.ID, reason); err != nil {
		h.storeError(w, err)
		return
	}
	if c := h.findIssueComment(r, c.IssueID, c.ID); c != nil {
		writeJSON(w, http.StatusOK, map[string]any{"comment": commentDTO(c)})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/v1/w/{ws}/discussions/{id}/threads/{tid} {"body"} → 200 {"thread"}
func (h *handler) updateThread(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	body, ok := decodeEditBody(w, r)
	if !ok {
		return
	}
	id, t, ok := h.requireThread(w, r, ws)
	if !ok {
		return
	}
	if err := h.store.UpdateDiscussionThread(r.Context(), id, t.ID, body); err != nil {
		h.storeError(w, err)
		return
	}
	if t := findDiscussionThread(h.store, r, id, t.ID); t != nil {
		writeJSON(w, http.StatusOK, map[string]any{"thread": discussionThreadDTO(t)})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/v1/w/{ws}/discussions/{id}/threads/{tid} → 204 — the
// thread's replies go with it (ON DELETE CASCADE).
func (h *handler) deleteThread(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, t, ok := h.requireThread(w, r, ws)
	if !ok {
		return
	}
	if err := h.store.DeleteDiscussionThread(r.Context(), id, t.ID); err != nil {
		h.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST .../threads/{tid}/hide {"reason"} → 200 {"thread"}
func (h *handler) hideThread(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	h.setThreadHidden(w, r, ws, true)
}

// POST .../threads/{tid}/unhide → 200 {"thread"}
func (h *handler) unhideThread(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	h.setThreadHidden(w, r, ws, false)
}

func (h *handler) setThreadHidden(w http.ResponseWriter, r *http.Request, ws *store.Workspace, hide bool) {
	reason := ""
	if hide {
		var ok bool
		if reason, ok = decodeHideBody(w, r); !ok {
			return
		}
	}
	id, t, ok := h.requireThread(w, r, ws)
	if !ok {
		return
	}
	if err := h.store.SetDiscussionThreadHidden(r.Context(), id, t.ID, reason); err != nil {
		h.storeError(w, err)
		return
	}
	if t := findDiscussionThread(h.store, r, id, t.ID); t != nil {
		writeJSON(w, http.StatusOK, map[string]any{"thread": discussionThreadDTO(t)})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PATCH .../threads/{tid}/comments/{cid} {"body"} → 200 {"comment"}
func (h *handler) updateReply(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	body, ok := decodeEditBody(w, r)
	if !ok {
		return
	}
	id, c, ok := h.requireReply(w, r, ws)
	if !ok {
		return
	}
	if err := h.store.UpdateDiscussionComment(r.Context(), id, c.ThreadID, c.ID, body); err != nil {
		h.storeError(w, err)
		return
	}
	if t := findDiscussionThread(h.store, r, id, c.ThreadID); t != nil {
		if c := findDiscussionComment(t, c.ID); c != nil {
			writeJSON(w, http.StatusOK, map[string]any{"comment": discussionCommentDTO(c)})
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE .../threads/{tid}/comments/{cid} → 204
func (h *handler) deleteReply(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, c, ok := h.requireReply(w, r, ws)
	if !ok {
		return
	}
	if err := h.store.DeleteDiscussionComment(r.Context(), id, c.ThreadID, c.ID); err != nil {
		h.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST .../threads/{tid}/comments/{cid}/hide {"reason"} → 200 {"comment"}
func (h *handler) hideReply(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	h.setReplyHidden(w, r, ws, true)
}

// POST .../threads/{tid}/comments/{cid}/unhide → 200 {"comment"}
func (h *handler) unhideReply(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	h.setReplyHidden(w, r, ws, false)
}

func (h *handler) setReplyHidden(w http.ResponseWriter, r *http.Request, ws *store.Workspace, hide bool) {
	reason := ""
	if hide {
		var ok bool
		if reason, ok = decodeHideBody(w, r); !ok {
			return
		}
	}
	id, c, ok := h.requireReply(w, r, ws)
	if !ok {
		return
	}
	if err := h.store.SetDiscussionCommentHidden(r.Context(), id, c.ThreadID, c.ID, reason); err != nil {
		h.storeError(w, err)
		return
	}
	if t := findDiscussionThread(h.store, r, id, c.ThreadID); t != nil {
		if c := findDiscussionComment(t, c.ID); c != nil {
			writeJSON(w, http.StatusOK, map[string]any{"comment": discussionCommentDTO(c)})
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
