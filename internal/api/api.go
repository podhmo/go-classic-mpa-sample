// Package api implements the JSON Web API under /api/v1
// (docs/sketch/ja/cli-and-web-api.md, Phase 2). It is a thin machine-facing
// surface over the same store the HTML app uses: DTOs are wire types kept
// separate from the store's domain types, and authentication is Bearer
// tokens mounted fail-closed (no usable token, no /api/v1). Two credential
// kinds are accepted: a single shared env token (writes stay anonymous)
// and per-user api_tokens rows (writes carry the user's author_id).
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
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
	APITokenUser(ctx context.Context, token string) (*store.User, error)
}

const (
	// maxBodyBytes caps JSON request bodies, matching the HTML form limit.
	maxBodyBytes = 1 << 20 // 1 MiB
	// defaultPerPage matches the HTML list's fixed page size.
	defaultPerPage = 20
	maxPerPage     = 100
)

// New returns the /api/v1 handler; every request must carry
// "Authorization: Bearer <token>". envToken is the optional shared
// credential (constant-time compared; its requests are anonymous); any
// other token is resolved through the store's api_tokens table and its
// requests run as that user.
func New(st Store, envToken string) http.Handler {
	h := &handler{store: st}
	mux := http.NewServeMux()

	// Routes are registered methodless per path and dispatched on
	// r.Method: method-scoped patterns would only 405 when no catch-all
	// matches, while a methodless catch-all would eat them into a 404.
	// Dispatching here gives both — known path + wrong method → 405,
	// unknown path → JSON 404 via the "/api/v1/" subtree below.
	routes := map[string]map[string]http.HandlerFunc{
		"/api/v1/issues":               {"GET": h.listIssues, "POST": h.createIssue},
		"/api/v1/issues/{id}":          {"GET": h.getIssue, "PATCH": h.updateIssue, "DELETE": h.deleteIssue},
		"/api/v1/issues/{id}/status":   {"POST": h.setIssueStatus},
		"/api/v1/issues/{id}/comments": {"POST": h.createComment},
		"/api/v1/labels":               {"GET": h.listLabels},

		"/api/v1/discussions":                            {"GET": h.listDiscussions, "POST": h.createDiscussion},
		"/api/v1/discussions/{id}":                       {"GET": h.getDiscussion, "PATCH": h.updateDiscussion, "DELETE": h.deleteDiscussion},
		"/api/v1/discussions/{id}/comments":              {"POST": h.createDiscussionComment},
		"/api/v1/discussions/{id}/comments/{cid}/answer": {"POST": h.setCommentAnswer},
		"/api/v1/discussion-categories":                  {"GET": h.listDiscussionCategories},
	}
	for path, methods := range routes {
		mux.HandleFunc(path, methodDispatch(methods))
	}

	// Anything else under /api/v1 is a JSON 404 (HTML 404s would confuse
	// non-browser clients).
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Not found.", nil)
	})

	return bearerAuth(envToken, st, mux)
}

// methodDispatch runs the handler registered for r.Method, or answers 405
// with an Allow header when the path exists but not the method.
func methodDispatch(methods map[string]http.HandlerFunc) http.HandlerFunc {
	var names []string
	for m := range methods {
		names = append(names, m)
	}
	// HEAD is accepted wherever GET is (mapped below) — list it like the
	// std mux does so the Allow header tells the truth.
	if _, ok := methods[http.MethodGet]; ok {
		names = append(names, http.MethodHead)
	}
	slices.Sort(names)
	allow := strings.Join(names, ", ")
	return func(w http.ResponseWriter, r *http.Request) {
		// HEAD rides on GET, like the std mux's method-scoped patterns;
		// the server strips the body on the way out.
		m := r.Method
		if m == http.MethodHead {
			m = http.MethodGet
		}
		if fn, ok := methods[m]; ok {
			fn(w, r)
			return
		}
		w.Header().Set("Allow", allow)
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed.", nil)
	}
}

// bearerAuth rejects requests whose Authorization header is not
// "Bearer <token>". The shared env token is compared in constant time and
// authenticates as anonymous (actor id 0); anything else is looked up in
// api_tokens, where the row's user becomes the request's actor. The header
// check is case-insensitive per RFC 9110 field-name and auth-scheme rules.
func bearerAuth(envToken string, st Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || got == "" {
			writeError(w, http.StatusUnauthorized, "Unauthorized.", nil)
			return
		}
		var actorID int64
		if envToken != "" && subtle.ConstantTimeCompare([]byte(got), []byte(envToken)) == 1 {
			actorID = 0 // shared token: no user identity, writes stay anonymous
		} else {
			u, err := st.APITokenUser(r.Context(), got)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					writeError(w, http.StatusUnauthorized, "Unauthorized.", nil)
				} else {
					writeError(w, http.StatusInternalServerError, "Internal server error.", nil)
				}
				return
			}
			actorID = u.ID
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, actorID)))
	})
}

// actorKey is the context key under which bearerAuth stores the resolved
// user id (0 = anonymous).
type actorKey struct{}

// actorID returns the authenticated user id for the request, or 0 when the
// request used the shared env token (anonymous writes).
func actorID(r *http.Request) int64 {
	id, _ := r.Context().Value(actorKey{}).(int64)
	return id
}

type handler struct {
	store Store
}

type User struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func userDTO(u *store.User) *User {
	if u == nil {
		return nil
	}
	return &User{ID: u.ID, Name: u.Name}
}

func fmtTime(t time.Time) string { return t.Format(time.RFC3339) }

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
	// A valid first object must be the whole body — extra JSON or garbage
	// after it is rejected rather than silently dropped.
	if dec.More() {
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
