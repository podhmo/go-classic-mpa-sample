// Discussion endpoints — same shapes as the issue endpoints, plus
// category and answer-mark handling.
package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// GET /api/v1/discussions?category=&answered=&q=&sort=&page=&per_page=
// Response: {"discussions": [...], "total": N, "page": P, "per_page": PP}
func (h *handler) listDiscussions(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	q := r.URL.Query()
	f := store.DiscussionListFilter{
		CategorySlug: q.Get("category"),
		Answered:     q.Get("answered"),
		Query:        strings.TrimSpace(q.Get("q")),
		Sort:         q.Get("sort"),
		Page:         queryInt(r, "page", 1),
		PerPage:      min(queryInt(r, "per_page", defaultPerPage), maxPerPage),
	}
	discussions, total, err := h.store.ListDiscussions(r.Context(), ws.ID, f)
	if err != nil {
		h.storeError(w, err)
		return
	}
	out := []Discussion{}
	for _, d := range discussions {
		out = append(out, discussionDTO(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"discussions": out, "total": total, "page": f.Page, "per_page": f.PerPage,
	})
}

// GET /api/v1/discussions/{id} → {"discussion": {...}, "threads": [...]}
// Each thread — itself the opening post — nests its replies under
// "comments".
func (h *handler) getDiscussion(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	d, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.storeError(w, err)
		return
	}
	if !h.requireScope(w, ws, d.WorkspaceID) {
		return
	}
	threads, err := h.store.ListDiscussionThreads(r.Context(), id)
	if err != nil {
		h.storeError(w, err)
		return
	}
	out := []DiscussionThread{}
	for _, t := range threads {
		out = append(out, discussionThreadDTO(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionDTO(d), "threads": out})
}

type discussionBody struct {
	Title      *string `json:"title"`
	Body       *string `json:"body"`
	CategoryID *int64  `json:"category_id"`
	Category   string  `json:"category"` // category slug, resolved via ListDiscussionCategories
}

// POST /api/v1/discussions {"title","body","category_id"|"category"} → 201
func (h *handler) createDiscussion(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	var in discussionBody
	if !decodeBody(w, r, &in) {
		return
	}
	catID := derefInt(in.CategoryID)
	resolved, ferr, err := h.resolveCategoryID(r, catID, in.Category)
	if err != nil {
		h.storeError(w, err)
		return
	}
	if ferr != nil {
		writeError(w, http.StatusUnprocessableEntity, "Validation failed.", ferr)
		return
	}
	input := store.DiscussionInput{
		Title: strings.TrimSpace(deref(in.Title)), Body: strings.TrimSpace(deref(in.Body)), CategoryID: resolved,
	}
	if fe := store.ValidateDiscussionInput(input); fe.Any() {
		writeError(w, http.StatusUnprocessableEntity, "Validation failed.", fields(fe))
		return
	}
	id, err := h.store.CreateDiscussion(r.Context(), ws.ID, input, actorID(r))
	if err != nil {
		h.storeError(w, err)
		return
	}
	d, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"discussion": discussionDTO(d)})
}

// PATCH /api/v1/discussions/{id} — partial update, same semantics as the
// issue PATCH.
func (h *handler) updateDiscussion(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	var in discussionBody
	if !decodeBody(w, r, &in) {
		return
	}
	d, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.storeError(w, err)
		return
	}
	if !h.requireScope(w, ws, d.WorkspaceID) {
		return
	}
	input := store.DiscussionInput{Title: d.Title, Body: d.Body, CategoryID: d.Category.ID}
	if in.Title != nil {
		input.Title = strings.TrimSpace(*in.Title)
	}
	if in.Body != nil {
		input.Body = strings.TrimSpace(*in.Body)
	}
	if in.CategoryID != nil || in.Category != "" {
		resolved, ferr, err := h.resolveCategoryID(r, derefInt(in.CategoryID), in.Category)
		if err != nil {
			h.storeError(w, err)
			return
		}
		if ferr != nil {
			writeError(w, http.StatusUnprocessableEntity, "Validation failed.", ferr)
			return
		}
		input.CategoryID = resolved
	}
	if fe := store.ValidateDiscussionInput(input); fe.Any() {
		writeError(w, http.StatusUnprocessableEntity, "Validation failed.", fields(fe))
		return
	}
	if err := h.store.UpdateDiscussion(r.Context(), id, input); err != nil {
		h.storeError(w, err)
		return
	}
	d, err = h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionDTO(d)})
}

// DELETE /api/v1/discussions/{id} → 204
func (h *handler) deleteDiscussion(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	d, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.storeError(w, err)
		return
	}
	if !h.requireScope(w, ws, d.WorkspaceID) {
		return
	}
	if err := h.store.DeleteDiscussion(r.Context(), id); err != nil {
		h.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/discussions/{id}/threads {"body"} → 201 — opens a thread
// on the discussion (the post a "comment" form submits).
func (h *handler) createDiscussionThread(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	body := strings.TrimSpace(in.Body)
	if msg := store.ValidateCommentBody(body); msg != "" {
		writeError(w, http.StatusUnprocessableEntity, "Validation failed.", map[string]string{"body": msg})
		return
	}
	d, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.storeError(w, err)
		return
	}
	if !h.requireScope(w, ws, d.WorkspaceID) {
		return
	}
	tid, err := h.store.CreateDiscussionThread(r.Context(), id, body, actorID(r))
	if err != nil {
		h.storeError(w, err)
		return
	}
	t := findDiscussionThread(h.store, r, id, tid)
	if t == nil {
		writeJSON(w, http.StatusCreated, map[string]any{"thread": map[string]any{"id": tid}})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"thread": discussionThreadDTO(t)})
}

// POST /api/v1/discussions/{id}/threads/{tid}/comments {"body"} → 201 —
// appends a reply to the thread.
func (h *handler) createDiscussionComment(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	tid, ok := h.pathID(w, r, "tid")
	if !ok {
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	body := strings.TrimSpace(in.Body)
	if msg := store.ValidateCommentBody(body); msg != "" {
		writeError(w, http.StatusUnprocessableEntity, "Validation failed.", map[string]string{"body": msg})
		return
	}
	d0, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.storeError(w, err)
		return
	}
	if !h.requireScope(w, ws, d0.WorkspaceID) {
		return
	}
	cid, err := h.store.CreateDiscussionComment(r.Context(), id, tid, body, actorID(r))
	if err != nil {
		h.storeError(w, err)
		return
	}
	t := findDiscussionThread(h.store, r, id, tid)
	if c := findDiscussionComment(t, cid); c != nil {
		writeJSON(w, http.StatusCreated, map[string]any{"comment": discussionCommentDTO(c)})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"comment": map[string]any{"id": cid}})
}

func findDiscussionThread(st Store, r *http.Request, discussionID, threadID int64) *store.DiscussionThread {
	threads, err := st.ListDiscussionThreads(r.Context(), discussionID)
	if err != nil {
		return nil
	}
	for _, t := range threads {
		if t.ID == threadID {
			return t
		}
	}
	return nil
}

func findDiscussionComment(t *store.DiscussionThread, id int64) *store.DiscussionComment {
	if t == nil {
		return nil
	}
	for _, c := range t.Comments {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// POST /api/v1/discussions/{id}/threads/{tid}/answer {"answer": bool} → 204.
// answer is required: an omitted field would decode as false and silently
// clear the mark.
func (h *handler) setThreadAnswer(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	tid, ok := h.pathID(w, r, "tid")
	if !ok {
		return
	}
	var in struct {
		Answer *bool `json:"answer"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if in.Answer == nil {
		writeError(w, http.StatusUnprocessableEntity, "Validation failed.", map[string]string{"answer": "Answer is required."})
		return
	}
	d, err := h.store.GetDiscussion(r.Context(), id)
	if err != nil {
		h.storeError(w, err)
		return
	}
	if !h.requireScope(w, ws, d.WorkspaceID) {
		return
	}
	if err := h.store.SetThreadAnswer(r.Context(), id, tid, *in.Answer); err != nil {
		h.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/discussion-categories → {"categories": [...]}
func (h *handler) listDiscussionCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := h.store.ListDiscussionCategories(r.Context())
	if err != nil {
		h.storeError(w, err)
		return
	}
	out := []DiscussionCategory{}
	for _, c := range cats {
		out = append(out, categoryDTO(*c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": out})
}

func derefInt(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

type DiscussionCategory struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Format      string `json:"format"`
}

type Discussion struct {
	ID        int64              `json:"id"`
	Title     string             `json:"title"`
	Body      string             `json:"body"`
	Category  DiscussionCategory `json:"category"`
	Author    *User              `json:"author"`
	Answered  bool               `json:"answered"`
	CreatedAt string             `json:"created_at"`
	UpdatedAt string             `json:"updated_at"`
}

type DiscussionThread struct {
	ID        int64               `json:"id"`
	IsAnswer  bool                `json:"is_answer"`
	Body      string              `json:"body"`
	Author    *User               `json:"author"`
	CreatedAt string              `json:"created_at"`
	Comments  []DiscussionComment `json:"comments,omitempty"`
}

type DiscussionComment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	Author    *User  `json:"author"`
	CreatedAt string `json:"created_at"`
}

func categoryDTO(c store.DiscussionCategory) DiscussionCategory {
	return DiscussionCategory{ID: c.ID, Name: c.Name, Slug: c.Slug, Description: c.Description, Format: c.Format}
}

func discussionDTO(d *store.Discussion) Discussion {
	return Discussion{
		ID: d.ID, Title: d.Title, Body: d.Body, Category: categoryDTO(d.Category),
		Author: userDTO(d.Author), Answered: d.Answered,
		CreatedAt: fmtTime(d.CreatedAt), UpdatedAt: fmtTime(d.UpdatedAt),
	}
}

func discussionThreadDTO(t *store.DiscussionThread) DiscussionThread {
	out := DiscussionThread{
		ID: t.ID, IsAnswer: t.IsAnswer, Body: t.Body,
		Author: userDTO(t.Author), CreatedAt: fmtTime(t.CreatedAt),
	}
	for _, c := range t.Comments {
		out.Comments = append(out.Comments, discussionCommentDTO(c))
	}
	return out
}

func discussionCommentDTO(c *store.DiscussionComment) DiscussionComment {
	return DiscussionComment{
		ID: c.ID, Body: c.Body,
		Author: userDTO(c.Author), CreatedAt: fmtTime(c.CreatedAt),
	}
}

// resolveCategoryID accepts either category_id or a category slug and
// returns the id; an unknown slug is a field error.
func (h *handler) resolveCategoryID(r *http.Request, id int64, slug string) (int64, map[string]string, error) {
	if slug == "" {
		return id, nil, nil
	}
	cats, err := h.store.ListDiscussionCategories(r.Context())
	if err != nil {
		return 0, nil, err
	}
	for _, c := range cats {
		if c.Slug == slug {
			return c.ID, nil, nil
		}
	}
	return 0, map[string]string{"category": fmt.Sprintf("Unknown category: %s", slug)}, nil
}
