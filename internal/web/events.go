// Live updates: per-resource SSE channels plus the fragment endpoints the
// client refetches after a change hint. Design: docs/sketch/ja/live-updates.md.
//
// Events are invalidation-only — {kind, target, parent} — so a subscriber
// always re-fetches server-rendered fragments; markdown truth stays
// server-side. The hub is fed by (*store.Store).WatchChanges, which tails
// the changes table and so covers every write path (HTML forms, /api/v1,
// and the local CLI running in its own process) in one place.
package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/events"
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// sseHeartbeat keeps connections alive through idle proxies and lets the
// server notice dead clients.
const sseHeartbeat = 25 * time.Second

// issueEvents streams changes for one issue as text/event-stream.
func (h *handler) issueEvents(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	i, ok := h.requireIssue(w, r, ws)
	if !ok {
		return
	}
	h.streamEvents(w, r, events.IssueTopic(i.ID))
}

// discussionEvents streams changes for one discussion.
func (h *handler) discussionEvents(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	d, ok := h.requireDiscussion(w, r, ws)
	if !ok {
		return
	}
	h.streamEvents(w, r, events.DiscussionTopic(d.ID))
}

// streamEvents writes the SSE stream for topic until the client
// disconnects. main.go sets WriteTimeout: 15s for the whole server —
// adequate for normal responses but fatal to a long-lived stream — so the
// per-response write deadline is lifted here (a zero deadline means none).
func (h *handler) streamEvents(w http.ResponseWriter, r *http.Request, topic string) {
	hd := w.Header()
	hd.Set("Content-Type", "text/event-stream")
	hd.Set("Cache-Control", "no-cache")
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		// Without the lifted deadline the server WriteTimeout cuts the
		// stream at 15s. Better to run degraded than refuse outright.
		log.Printf("web: sse %s: cannot lift write deadline: %v", topic, err)
	}

	ch, unsub := h.hub.Subscribe(topic)
	defer unsub()

	// Send the retry directive immediately so the client's onopen fires
	// and its reconnect delay is ours, not the browser default.
	if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		return
	}
	ping := time.NewTicker(sseHeartbeat)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case c := <-ch:
			if _, err := writeSSE(w, c); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}

// writeSSE formats one change as an SSE frame: id carries the hub sequence
// (EventSource's Last-Event-ID), event the kind.
func writeSSE(w http.ResponseWriter, c events.Change) (int, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return 0, err
	}
	return fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", c.Seq, c.Kind, data)
}

// parseIDSet reads the ?ids=1,2,3 filter. An absent parameter returns nil
// (the whole list — the resync path); an empty one filters to nothing.
func parseIDSet(r *http.Request) (map[int64]bool, error) {
	raw, present := r.URL.Query()["ids"]
	if !present {
		return nil, nil
	}
	set := map[int64]bool{}
	for _, v := range strings.Split(strings.Join(raw, ","), ",") {
		if v == "" {
			continue
		}
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 1 {
			return nil, fmt.Errorf("bad id %q", v)
		}
		set[id] = true
	}
	return set, nil
}

// filterByID applies a parseIDSet result in place. nil ids mean unfiltered.
func filterByID[T any](items []T, ids map[int64]bool, id func(T) int64) []T {
	if ids == nil {
		return items
	}
	out := items[:0]
	for _, item := range items {
		if ids[id(item)] {
			out = append(out, item)
		}
	}
	return out
}

// issueCommentsFragment serves GET /issues/{id}/fragments/comments:
// ?ids= filters to those comments, otherwise the whole list (resync).
// The fragment leads with the issue meta as a data-swap node — a new
// comment bumps updated_at.
func (h *handler) issueCommentsFragment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	ids, err := parseIDSet(r)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, msgFor(r).ErrBadRequestH, msgFor(r).ErrInvalidIDs)
		return
	}
	issue, ok := h.requireIssue(w, r, ws)
	if !ok {
		return
	}
	comments, err := h.store.ListComments(r.Context(), issue.ID)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	out := filterByID(comments, ids, func(c *store.Comment) int64 { return c.ID })
	h.r.renderFragment(w, "issue_detail", "issue_comments_update", http.StatusOK, dict("Issue", issue, "Comments", out, "Workspace", ws), localeFromContext(r.Context()))

}

// issueArticleFragment serves GET /issues/{id}/fragments/article — the
// whole issue card for status/edit live swaps.
func (h *handler) issueArticleFragment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	issue, ok := h.requireIssue(w, r, ws)
	if !ok {
		return
	}
	h.r.renderFragment(w, "issue_detail", "issue_article", http.StatusOK, issueDetailPage{
		Issue:     issue,
		Workspace: ws,
		User:      userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}

// discussionThreadsFragment serves GET /discussions/{id}/fragments/threads
// — ?ids= picks specific threads (also how answer-move swaps fetch the two
// changed <li>s), no ids returns the whole list.
func (h *handler) discussionThreadsFragment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	ids, err := parseIDSet(r)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, msgFor(r).ErrBadRequestH, msgFor(r).ErrInvalidIDs)
		return
	}
	discussion, ok := h.requireDiscussion(w, r, ws)
	if !ok {
		return
	}
	threads, err := h.store.ListDiscussionThreads(r.Context(), discussion.ID)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	out := filterByID(threads, ids, func(t *store.DiscussionThread) int64 { return t.ID })
	h.r.renderFragment(w, "discussion_detail", "discussion_threads_update", http.StatusOK, dict("Discussion", discussion, "Threads", out, "Workspace", ws), localeFromContext(r.Context()))

}

// discussionRepliesFragment serves
// GET /discussions/{id}/fragments/replies?thread=T(&ids=…): the reply
// <li>s of one thread, for appending pushed replies without touching the
// thread's open reply form.
func (h *handler) discussionRepliesFragment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	threadID, err := strconv.ParseInt(r.URL.Query().Get("thread"), 10, 64)
	if err != nil || threadID < 1 {
		h.renderError(w, r, http.StatusBadRequest, msgFor(r).ErrBadRequestH, msgFor(r).ErrInvalidThreadID)
		return
	}
	ids, err := parseIDSet(r)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, msgFor(r).ErrBadRequestH, msgFor(r).ErrInvalidIDs)
		return
	}
	discussion, ok := h.requireDiscussion(w, r, ws)
	if !ok {
		return
	}
	threads, err := h.store.ListDiscussionThreads(r.Context(), discussion.ID)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	var thread *store.DiscussionThread
	for _, t := range threads {
		if t.ID == threadID {
			thread = t
			break
		}
	}
	if thread == nil {
		h.notFound(w, r)
		return
	}
	out := filterByID(thread.Comments, ids, func(c *store.DiscussionComment) int64 { return c.ID })
	h.r.renderFragment(w, "discussion_detail", "discussion_replies_update", http.StatusOK, dict("Discussion", discussion, "Comments", out, "Workspace", ws), localeFromContext(r.Context()))

}

// discussionArticleFragment serves GET /discussions/{id}/fragments/article
// — the whole discussion card for edit live swaps.
func (h *handler) discussionArticleFragment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	discussion, ok := h.requireDiscussion(w, r, ws)
	if !ok {
		return
	}
	h.r.renderFragment(w, "discussion_detail", "discussion_article", http.StatusOK, discussionDetailPage{
		Discussion: discussion,
		Workspace:  ws,
		User:       userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}
