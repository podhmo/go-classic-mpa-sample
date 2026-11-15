// Package api implements the JSON Web API under /api/v1
// (docs/sketch/ja/cli-and-web-api.md, Phase 2). It is a thin machine-facing
// surface over the same store the HTML app uses: DTOs are wire types kept
// separate from the store's domain types, and authentication is a single
// Bearer token mounted fail-closed (no token, no /api/v1).
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// Store is the consumer-side persistence contract the API needs.
// *store.Store satisfies it automatically; tests may substitute a fake.
type Store interface {
	ListIssues(ctx context.Context, f store.ListFilter) ([]*store.Issue, int, error)
	GetIssue(ctx context.Context, id int64) (*store.Issue, error)
	CreateIssue(ctx context.Context, in store.IssueInput, authorID int64) (int64, error)
	UpdateIssue(ctx context.Context, id int64, in store.IssueInput) error
	SetIssueStatus(ctx context.Context, id int64, status string) error
	DeleteIssue(ctx context.Context, id int64) error
	ListComments(ctx context.Context, issueID int64) ([]*store.Comment, error)
	CreateComment(ctx context.Context, issueID int64, body string, authorID int64) (int64, error)
	ListLabels(ctx context.Context) ([]*store.Label, error)
	ListDiscussions(ctx context.Context, f store.DiscussionListFilter) ([]*store.Discussion, int, error)
	GetDiscussion(ctx context.Context, id int64) (*store.Discussion, error)
	CreateDiscussion(ctx context.Context, in store.DiscussionInput, authorID int64) (int64, error)
	UpdateDiscussion(ctx context.Context, id int64, in store.DiscussionInput) error
	DeleteDiscussion(ctx context.Context, id int64) error
	ListDiscussionComments(ctx context.Context, discussionID int64) ([]*store.DiscussionComment, error)
	CreateDiscussionComment(ctx context.Context, discussionID, parentID int64, body string, authorID int64) (int64, error)
	SetCommentAnswer(ctx context.Context, discussionID, commentID int64, answer bool) error
	ListDiscussionCategories(ctx context.Context) ([]*store.DiscussionCategory, error)
}

const (
	// maxBodyBytes caps JSON request bodies, matching the HTML form limit.
	maxBodyBytes = 1 << 20 // 1 MiB
	// defaultPerPage matches the HTML list's fixed page size.
	defaultPerPage = 20
	// maxPerPage caps the ?per_page= query parameter.
	maxPerPage = 100
)

// New returns the /api/v1 handler; every request must carry
// "Authorization: Bearer <token>". The token is compared in constant time.
func New(st Store, token string) http.Handler {
	h := &handler{store: st}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/issues", h.listIssues)
	mux.HandleFunc("POST /api/v1/issues", h.createIssue)
	mux.HandleFunc("GET /api/v1/issues/{id}", h.getIssue)
	mux.HandleFunc("PATCH /api/v1/issues/{id}", h.updateIssue)
	mux.HandleFunc("DELETE /api/v1/issues/{id}", h.deleteIssue)
	mux.HandleFunc("POST /api/v1/issues/{id}/status", h.setIssueStatus)
	mux.HandleFunc("POST /api/v1/issues/{id}/comments", h.createComment)
	mux.HandleFunc("GET /api/v1/labels", h.listLabels)

	mux.HandleFunc("GET /api/v1/discussions", h.listDiscussions)
	mux.HandleFunc("POST /api/v1/discussions", h.createDiscussion)
	mux.HandleFunc("GET /api/v1/discussions/{id}", h.getDiscussion)
	mux.HandleFunc("PATCH /api/v1/discussions/{id}", h.updateDiscussion)
	mux.HandleFunc("DELETE /api/v1/discussions/{id}", h.deleteDiscussion)
	mux.HandleFunc("POST /api/v1/discussions/{id}/comments", h.createDiscussionComment)
	mux.HandleFunc("POST /api/v1/discussions/{id}/comments/{cid}/answer", h.setCommentAnswer)
	mux.HandleFunc("GET /api/v1/discussion-categories", h.listDiscussionCategories)

	// Anything else under /api/v1 is a JSON 404 (HTML 404s would confuse
	// non-browser clients).
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Not found.", nil)
	})

	return bearerAuth(token, mux)
}

// bearerAuth rejects requests whose Authorization header is not
// "Bearer <token>". Compared in constant time; the header check is
// case-insensitive per RFC 9110 field-name and auth-scheme rules.
func bearerAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			writeError(w, http.StatusUnauthorized, "Unauthorized.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type handler struct {
	store Store
}

// --- wire types (DTOs), kept separate from store's domain types ---

type User struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Label struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Issue struct {
	ID        int64   `json:"id"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	Status    string  `json:"status"`
	Labels    []Label `json:"labels"`
	Author    *User   `json:"author"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

type Comment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	Author    *User  `json:"author"`
	CreatedAt string `json:"created_at"`
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

type DiscussionComment struct {
	ID        int64               `json:"id"`
	ParentID  int64               `json:"parent_id"`
	IsAnswer  bool                `json:"is_answer"`
	Body      string              `json:"body"`
	Author    *User               `json:"author"`
	CreatedAt string              `json:"created_at"`
	Replies   []DiscussionComment `json:"replies,omitempty"`
}

// --- store → DTO conversion ---

func userDTO(u *store.User) *User {
	if u == nil {
		return nil
	}
	return &User{ID: u.ID, Name: u.Name}
}

func labelDTO(l store.Label) Label {
	return Label{ID: l.ID, Name: l.Name, Color: l.Color}
}

func fmtTime(t time.Time) string { return t.Format(time.RFC3339) }

func issueDTO(i *store.Issue) Issue {
	out := Issue{
		ID: i.ID, Title: i.Title, Body: i.Body, Status: i.Status,
		Author: userDTO(i.Author), CreatedAt: fmtTime(i.CreatedAt), UpdatedAt: fmtTime(i.UpdatedAt),
		Labels: []Label{},
	}
	for _, l := range i.Labels {
		out.Labels = append(out.Labels, labelDTO(l))
	}
	return out
}

func commentDTO(c *store.Comment) Comment {
	return Comment{ID: c.ID, Body: c.Body, Author: userDTO(c.Author), CreatedAt: fmtTime(c.CreatedAt)}
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

func discussionCommentDTO(c *store.DiscussionComment) DiscussionComment {
	out := DiscussionComment{
		ID: c.ID, ParentID: c.ParentID, IsAnswer: c.IsAnswer, Body: c.Body,
		Author: userDTO(c.Author), CreatedAt: fmtTime(c.CreatedAt),
	}
	for _, rep := range c.Replies {
		out.Replies = append(out.Replies, discussionCommentDTO(rep))
	}
	return out
}

// --- request/response helpers ---

// writeJSON answers with a JSON body. All API responses use the same
// envelope style: resources under their plural name, errors under "error".
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError emits {"error":{"message":..., "fields":{...}}} where fields
// is only present for per-field validation failures.
func writeError(w http.ResponseWriter, status int, message string, fields map[string]string) {
	e := map[string]any{"message": message}
	if len(fields) > 0 {
		e["fields"] = fields
	}
	writeJSON(w, status, map[string]any{"error": e})
}

// decodeBody reads a JSON object body; oversized or malformed bodies are a
// 400.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.", nil)
		return false
	}
	return true
}

// pathID parses a {id}-style path value; non-numeric or non-positive ids
// are a 404, matching the HTML handlers' treatment of parseID failures.
func (h *handler) pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "Not found.", nil)
		return 0, false
	}
	return id, true
}

// queryInt reads a positive int query parameter with a default.
func queryInt(r *http.Request, name string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil && v > 0 {
		return v
	}
	return def
}

// storeError maps store sentinel errors to HTTP statuses.
func (h *handler) storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Not found.", nil)
	case errors.Is(err, store.ErrInvalidCategory):
		writeError(w, http.StatusUnprocessableEntity, "Unknown discussion category.", map[string]string{"category": "Unknown category."})
	case errors.Is(err, store.ErrInvalidParent):
		writeError(w, http.StatusUnprocessableEntity, "Replies can only be added to top-level comments.", map[string]string{"parent_id": "Invalid reply target."})
	case errors.Is(err, store.ErrNotAnswerable):
		writeError(w, http.StatusUnprocessableEntity, "This comment cannot be marked as the answer.", nil)
	default:
		writeError(w, http.StatusInternalServerError, "Internal server error.", nil)
	}
}

// fields converts store.FieldErrors into the wire map (nil-safe).
func fields(fe store.FieldErrors) map[string]string {
	if len(fe) == 0 {
		return nil
	}
	return map[string]string(fe)
}

// resolveLabelIDs resolves label names ("labels") into ids, merged with
// explicit "label_ids". Unknown names are a 422 field error.
func (h *handler) resolveLabelIDs(r *http.Request, ids []int64, names []string) ([]int64, map[string]string) {
	if len(names) == 0 {
		return ids, nil
	}
	all, err := h.store.ListLabels(r.Context())
	if err != nil {
		return nil, nil
	}
	byName := map[string]int64{}
	for _, l := range all {
		byName[l.Name] = l.ID
	}
	for _, n := range names {
		id, ok := byName[n]
		if !ok {
			return nil, map[string]string{"labels": fmt.Sprintf("Unknown label: %s", n)}
		}
		ids = append(ids, id)
	}
	return ids, nil
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
