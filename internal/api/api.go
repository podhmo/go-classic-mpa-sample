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
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/gitrepo"
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// Store is the consumer-side persistence contract the API needs.
// *store.Store satisfies it automatically; tests may substitute a fake.
type Store interface {
	ListIssues(ctx context.Context, wsID int64, f store.ListFilter) ([]*store.Issue, int, error)
	GetIssue(ctx context.Context, id int64) (*store.Issue, error)
	GetIssueInWorkspace(ctx context.Context, wsID, id int64) (*store.Issue, error)
	CreateIssue(ctx context.Context, wsID int64, in store.IssueInput, authorID int64) (int64, error)
	UpdateIssue(ctx context.Context, id int64, in store.IssueInput) error
	PatchIssueInWorkspace(ctx context.Context, wsID, id int64, patch store.IssuePatch) error
	SetIssueStatus(ctx context.Context, id int64, status string) error
	DeleteIssue(ctx context.Context, id int64) error
	ListComments(ctx context.Context, issueID int64) ([]*store.Comment, error)
	GetComment(ctx context.Context, issueID, commentID int64) (*store.Comment, error)
	CreateComment(ctx context.Context, issueID int64, body string, authorID int64) (int64, error)
	UpdateComment(ctx context.Context, issueID, commentID int64, body string) error
	UpdateCommentVersion(ctx context.Context, issueID, commentID int64, body string, expectedVersion int64) error
	DeleteComment(ctx context.Context, issueID, commentID int64) error
	SetCommentHidden(ctx context.Context, issueID, commentID int64, reason string) error
	ListLabels(ctx context.Context, wsID int64, kind string) ([]*store.Label, error)
	GetLabel(ctx context.Context, id int64) (*store.Label, error)
	LabelByName(ctx context.Context, wsID int64, kind, name string) (*store.Label, error)
	CreateLabel(ctx context.Context, wsID int64, kind string, in store.LabelInput) (int64, error)
	UpdateLabel(ctx context.Context, id int64, in store.LabelInput) error
	ArchiveLabel(ctx context.Context, id int64, archived bool) error
	ListDiscussions(ctx context.Context, wsID int64, f store.DiscussionListFilter) ([]*store.Discussion, int, error)
	GetDiscussion(ctx context.Context, id int64) (*store.Discussion, error)
	CreateDiscussion(ctx context.Context, wsID int64, in store.DiscussionInput, authorID int64) (int64, error)
	UpdateDiscussion(ctx context.Context, id int64, in store.DiscussionInput) error
	DeleteDiscussion(ctx context.Context, id int64) error
	ListDiscussionThreads(ctx context.Context, discussionID int64) ([]*store.DiscussionThread, error)
	CreateDiscussionThread(ctx context.Context, discussionID int64, body string, authorID int64) (int64, error)
	CreateDiscussionComment(ctx context.Context, discussionID, threadID int64, body string, authorID int64) (int64, error)
	SetThreadAnswer(ctx context.Context, discussionID, threadID int64, answer bool) error
	UpdateDiscussionThread(ctx context.Context, discussionID, threadID int64, body string) error
	UpdateDiscussionThreadVersion(ctx context.Context, discussionID, threadID int64, body string, expectedVersion int64) error
	DeleteDiscussionThread(ctx context.Context, discussionID, threadID int64) error
	SetDiscussionThreadHidden(ctx context.Context, discussionID, threadID int64, reason string) error
	UpdateDiscussionComment(ctx context.Context, discussionID, threadID, commentID int64, body string) error
	UpdateDiscussionCommentVersion(ctx context.Context, discussionID, threadID, commentID int64, body string, expectedVersion int64) error
	DeleteDiscussionComment(ctx context.Context, discussionID, threadID, commentID int64) error
	SetDiscussionCommentHidden(ctx context.Context, discussionID, threadID, commentID int64, reason string) error
	ListDiscussionCategories(ctx context.Context) ([]*store.DiscussionCategory, error)
	ListWorkspaces(ctx context.Context) ([]*store.Workspace, error)
	WorkspaceRefBySlug(ctx context.Context, slug string) (*store.Workspace, error)
	WorkspaceBySlug(ctx context.Context, slug string) (*store.Workspace, error)
	CreateWorkspace(ctx context.Context, in store.WorkspaceInput) (int64, error)
	DeleteWorkspace(ctx context.Context, slug string) error
	APITokenUser(ctx context.Context, token string) (*store.User, error)

	// Pull requests — workspace-independent (docs/sketch/ja/
	// pull-requests.md §6). These mount only with a *gitrepo.Repo.
	ListPullRequests(ctx context.Context, f store.PRListFilter) ([]*store.PullRequest, int, error)
	GetPullRequest(ctx context.Context, id int64) (*store.PullRequest, error)
	CreatePullRequest(ctx context.Context, in store.PRInput, authorID int64, pin func(prID int64) error) (int64, error)
	UpdatePullRequest(ctx context.Context, id int64, in store.PRInput) error
	DeletePullRequest(ctx context.Context, id int64) error
	ClosePullRequest(ctx context.Context, id int64, headSHA, mergeBaseSHA string) error
	ReopenPullRequest(ctx context.Context, id int64) error
	MarkMerged(ctx context.Context, id int64, mi store.PRMergeInfo) error
	ReserveMerge(ctx context.Context, id int64) (int64, error)
	ReleaseMerge(ctx context.Context, id int64, token int64) error
	MarkMergedWithReservation(ctx context.Context, id int64, mi store.PRMergeInfo, token int64) error
	ListPRComments(ctx context.Context, prID int64) ([]*store.PRComment, error)
	GetPRComment(ctx context.Context, prID, commentID int64) (*store.PRComment, error)
	CreatePRComment(ctx context.Context, prID int64, in store.PRCommentInput, authorID int64) (int64, error)
	UpdatePRComment(ctx context.Context, prID, commentID int64, body string) error
	UpdatePRCommentVersion(ctx context.Context, prID, commentID int64, body string, expectedVersion int64) error
	DeletePRComment(ctx context.Context, prID, commentID int64) error
	SetPRCommentHidden(ctx context.Context, prID, commentID int64, reason string) error
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
func New(st Store, envToken string, repo *gitrepo.Repo) http.Handler {
	h := &handler{store: st, repo: repo}
	mux := http.NewServeMux()

	// Routes are registered methodless per path and dispatched on
	// r.Method: method-scoped patterns would only 405 when no catch-all
	// matches, while a methodless catch-all would eat them into a 404.
	// Dispatching here gives both — known path + wrong method → 405,
	// unknown path → JSON 404 via the "/api/v1/" subtree below.
	// Workspace-scoped resources live under /w/{slug}; the workspace-less
	// /issues and /discussions paths are gone (a 404, not a redirect — API
	// callers should be updated, docs/sketch/ja/workspaces.md). Labels are
	// workspace-scoped too (shared master rows ride along); only
	// discussion categories stay global.
	routes := map[string]map[string]http.HandlerFunc{
		"/api/v1/workspaces":            {"GET": h.listWorkspaces, "POST": h.createWorkspace},
		"/api/v1/workspaces/{slug}":     {"GET": h.getWorkspace, "DELETE": h.deleteWorkspace},
		"/api/v1/discussion-categories": {"GET": h.listDiscussionCategories},

		"/api/v1/w/{ws}/labels":      {"GET": h.scoped(h.listLabels), "POST": h.scoped(h.createLabel)},
		"/api/v1/w/{ws}/labels/{id}": {"PATCH": h.scoped(h.updateLabel), "DELETE": h.scoped(h.deleteLabel)},

		"/api/v1/w/{ws}/issues":               {"GET": h.scoped(h.listIssues), "POST": h.scoped(h.createIssue)},
		"/api/v1/w/{ws}/issues/{id}":          {"GET": h.scoped(h.getIssue), "PATCH": h.scoped(h.updateIssue), "DELETE": h.scoped(h.deleteIssue)},
		"/api/v1/w/{ws}/issues/{id}/status":   {"POST": h.scoped(h.setIssueStatus)},
		"/api/v1/w/{ws}/issues/{id}/comments": {"POST": h.scoped(h.createComment)},
		// Comment moderation (internal/api/comments.go): PATCH/DELETE on
		// the comment itself, hide/unhide as explicit action POSTs.
		"/api/v1/w/{ws}/issues/{id}/comments/{cid}":        {"PATCH": h.scoped(h.updateComment), "DELETE": h.scoped(h.deleteComment)},
		"/api/v1/w/{ws}/issues/{id}/comments/{cid}/hide":   {"POST": h.scoped(h.hideComment)},
		"/api/v1/w/{ws}/issues/{id}/comments/{cid}/unhide": {"POST": h.scoped(h.unhideComment)},
		"/api/v1/w/{ws}/issues/{id}/markdown":              {"GET": h.scoped(h.issueMarkdown)},

		"/api/v1/w/{ws}/discussions":                             {"GET": h.scoped(h.listDiscussions), "POST": h.scoped(h.createDiscussion)},
		"/api/v1/w/{ws}/discussions/{id}":                        {"GET": h.scoped(h.getDiscussion), "PATCH": h.scoped(h.updateDiscussion), "DELETE": h.scoped(h.deleteDiscussion)},
		"/api/v1/w/{ws}/discussions/{id}/markdown":               {"GET": h.scoped(h.discussionMarkdown)},
		"/api/v1/w/{ws}/discussions/{id}/threads":                {"POST": h.scoped(h.createDiscussionThread)},
		"/api/v1/w/{ws}/discussions/{id}/threads/{tid}/comments": {"POST": h.scoped(h.createDiscussionComment)},
		"/api/v1/w/{ws}/discussions/{id}/threads/{tid}/answer":   {"POST": h.scoped(h.setThreadAnswer)},
		// Thread and reply moderation — same shapes as issue comments.
		"/api/v1/w/{ws}/discussions/{id}/threads/{tid}":                       {"PATCH": h.scoped(h.updateThread), "DELETE": h.scoped(h.deleteThread)},
		"/api/v1/w/{ws}/discussions/{id}/threads/{tid}/hide":                  {"POST": h.scoped(h.hideThread)},
		"/api/v1/w/{ws}/discussions/{id}/threads/{tid}/unhide":                {"POST": h.scoped(h.unhideThread)},
		"/api/v1/w/{ws}/discussions/{id}/threads/{tid}/comments/{cid}":        {"PATCH": h.scoped(h.updateReply), "DELETE": h.scoped(h.deleteReply)},
		"/api/v1/w/{ws}/discussions/{id}/threads/{tid}/comments/{cid}/hide":   {"POST": h.scoped(h.hideReply)},
		"/api/v1/w/{ws}/discussions/{id}/threads/{tid}/comments/{cid}/unhide": {"POST": h.scoped(h.unhideReply)},
	}
	for path, methods := range routes {
		mux.HandleFunc(path, methodDispatch(methods))
	}

	// Pull requests mount only when the server has a git repo — on a
	// repo-less deployment /api/v1/pulls* falls to the JSON 404 below,
	// matching the HTML nav's hidden link.
	if repo != nil {
		for path, methods := range map[string]map[string]http.HandlerFunc{
			"/api/v1/pulls":                            {"GET": h.listPulls, "POST": h.createPull},
			"/api/v1/pulls/{id}":                       {"GET": h.getPull, "PATCH": h.updatePull, "DELETE": h.deletePull},
			"/api/v1/pulls/{id}/comments":              {"GET": h.listPRComments, "POST": h.createPRComment},
			"/api/v1/pulls/{id}/comments/{cid}":        {"PATCH": h.updatePRComment, "DELETE": h.deletePRComment},
			"/api/v1/pulls/{id}/comments/{cid}/hide":   {"POST": h.hidePRComment},
			"/api/v1/pulls/{id}/comments/{cid}/unhide": {"POST": h.unhidePRComment},
			"/api/v1/pulls/{id}/status":                {"POST": h.setPullStatus},
			"/api/v1/pulls/{id}/merge":                 {"POST": h.mergePull},
			"/api/v1/pulls/{id}/diff":                  {"GET": h.pullDiff},
			"/api/v1/pulls/{id}/markdown":              {"GET": h.pullMarkdown},
		} {
			mux.HandleFunc(path, methodDispatch(methods))
		}
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
	repo  *gitrepo.Repo // nil → /api/v1/pulls* unmounted
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

// writeMarkdown answers a text/markdown document — the thread export from
// internal/export, the API's one non-JSON representation. Errors keep the
// JSON envelope; only 200s carry markdown.
func writeMarkdown(w http.ResponseWriter, filename, doc string) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filename))
	io.WriteString(w, doc)
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

// parseIfMatch reads the optional update precondition. The API exposes
// versions as strong integer entity tags; absent or "*" means
// unconditional. Weak or malformed tags are rejected rather than silently
// downgraded to an unconditional write.
func parseIfMatch(w http.ResponseWriter, r *http.Request) (int64, bool) {
	h := strings.TrimSpace(r.Header.Get("If-Match"))
	if h == "" || h == "*" {
		return 0, true
	}
	if strings.Contains(h, ",") || strings.HasPrefix(h, "W/") || len(h) < 2 || h[0] != '"' || h[len(h)-1] != '"' {
		writeError(w, http.StatusBadRequest, "If-Match must be one strong version tag.", nil)
		return 0, false
	}
	v, err := strconv.ParseInt(h[1:len(h)-1], 10, 64)
	if err != nil || v < 1 {
		writeError(w, http.StatusBadRequest, "If-Match must be one strong version tag.", nil)
		return 0, false
	}
	return v, true
}

// queryInt reads a positive int query parameter with a default.
func queryInt(r *http.Request, name string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil && v > 0 {
		return v
	}
	return def
}

// queryPage reads a page number; absurd values fall back to page 1.
func queryPage(r *http.Request) int {
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 && v <= store.MaxListPage {
		return v
	}
	return 1
}

// scoped resolves the {ws} path segment into its workspace (a JSON 404 on
// a miss) and calls fn with it.
func (h *handler) scoped(fn func(http.ResponseWriter, *http.Request, *store.Workspace)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws, err := h.store.WorkspaceRefBySlug(r.Context(), r.PathValue("ws"))
		if err != nil {
			h.storeError(w, err)
			return
		}
		fn(w, r, ws)
	}
}

// requireScope replies 404 when the resource belongs to another
// workspace — the API's slug-mismatch contract (no redirect like HTML).
func (h *handler) requireScope(w http.ResponseWriter, ws *store.Workspace, realWSID int64) bool {
	if realWSID != ws.ID {
		writeError(w, http.StatusNotFound, "Not found.", nil)
		return false
	}
	return true
}

// storeError maps store sentinel errors to HTTP statuses.
func (h *handler) storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Not found.", nil)
	case errors.Is(err, store.ErrVersionConflict):
		writeError(w, http.StatusPreconditionFailed, "Version conflict.", nil)
	case errors.Is(err, store.ErrDuplicateSlug):
		writeError(w, http.StatusUnprocessableEntity, "Validation failed.", map[string]string{"slug": "This slug is already taken."})
	case errors.Is(err, store.ErrLastWorkspace):
		writeError(w, http.StatusUnprocessableEntity, "The last remaining workspace cannot be deleted.", nil)
	case errors.Is(err, store.ErrInvalidCategory):
		writeError(w, http.StatusUnprocessableEntity, "Unknown discussion category.", map[string]string{"category": "Unknown category."})
	case errors.Is(err, store.ErrInvalidThread):
		writeError(w, http.StatusUnprocessableEntity, "Replies can only be added to a thread of this discussion.", map[string]string{"thread_id": "Invalid reply target."})
	case errors.Is(err, store.ErrNotAnswerable):
		writeError(w, http.StatusUnprocessableEntity, "This thread cannot be marked as the answer.", nil)
	case errors.Is(err, store.ErrInvalidHideReason):
		writeError(w, http.StatusUnprocessableEntity, "Validation failed.", hideReasonField())
	default:
		writeError(w, http.StatusInternalServerError, "Internal server error.", nil)
	}
}

// fields converts store.FieldErrors into the wire map (nil-safe).
func fields(fe store.FieldErrors) map[string]string {
	if len(fe) == 0 {
		return nil
	}
	m := make(map[string]string, len(fe))
	for k, v := range fe {
		m[k] = v.Message
	}
	return m
}
