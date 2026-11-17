// Package web implements the HTTP layer of the issue tracker: a classic
// multi-page application rendered server-side with html/template and no
// JavaScript. All state changes happen through HTML forms following the
// POST → Redirect → GET (PRG) pattern.
package web

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"log"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/api"
	"github.com/podhmo/go-classic-mpa-sample/internal/events"
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Store is the persistence contract the web layer depends on. It matches the
// methods of *store.Store, which satisfies this interface automatically.
// Tests provide an in-memory implementation instead.
type Store interface {
	ListIssues(ctx context.Context, wsID int64, f store.ListFilter) ([]*store.Issue, int, error)
	GetIssue(ctx context.Context, id int64) (*store.Issue, error)
	CreateIssue(ctx context.Context, wsID int64, in store.IssueInput, authorID int64) (int64, error)
	UpdateIssue(ctx context.Context, id int64, in store.IssueInput) error
	SetIssueStatus(ctx context.Context, id int64, status string) error
	DeleteIssue(ctx context.Context, id int64) error
	ListComments(ctx context.Context, issueID int64) ([]*store.Comment, error)
	CreateComment(ctx context.Context, issueID int64, body string, authorID int64) (int64, error)
	ListLabels(ctx context.Context) ([]*store.Label, error)
	ListDiscussions(ctx context.Context, wsID int64, f store.DiscussionListFilter) ([]*store.Discussion, int, error)
	GetDiscussion(ctx context.Context, id int64) (*store.Discussion, error)
	CreateDiscussion(ctx context.Context, wsID int64, in store.DiscussionInput, authorID int64) (int64, error)
	UpdateDiscussion(ctx context.Context, id int64, in store.DiscussionInput) error
	DeleteDiscussion(ctx context.Context, id int64) error
	ListDiscussionThreads(ctx context.Context, discussionID int64) ([]*store.DiscussionThread, error)
	CreateDiscussionThread(ctx context.Context, discussionID int64, body string, authorID int64) (int64, error)
	CreateDiscussionComment(ctx context.Context, discussionID, threadID int64, body string, authorID int64) (int64, error)
	SetThreadAnswer(ctx context.Context, discussionID, threadID int64, answer bool) error
	ListDiscussionCategories(ctx context.Context) ([]*store.DiscussionCategory, error)
	ListWorkspaces(ctx context.Context) ([]*store.Workspace, error)
	WorkspaceBySlug(ctx context.Context, slug string) (*store.Workspace, error)
	WorkspaceByID(ctx context.Context, id int64) (*store.Workspace, error)
	CreateWorkspace(ctx context.Context, in store.WorkspaceInput) (int64, error)
	DeleteWorkspace(ctx context.Context, slug string) error
	CreateUser(ctx context.Context, name, passwordHash string) (int64, error)
	AuthenticateUser(ctx context.Context, name, password string) (*store.User, error)
	CreateSession(ctx context.Context, userID int64, ttl time.Duration) (string, error)
	SessionUser(ctx context.Context, token string, ttl time.Duration) (*store.User, error)
	DeleteSession(ctx context.Context, token string) error
	APITokenUser(ctx context.Context, token string) (*store.User, error)
}

const (
	// maxFormBytes caps POST request bodies (forms only send a few fields).
	maxFormBytes = 1 << 20 // 1 MiB
	perPage      = 20
)

// Option configures New.
type Option func(*config)

type config struct {
	apiToken   string
	apiEnabled bool
	hub        *events.Hub
}

// WithAPIToken mounts the JSON API (docs/sketch/ja/cli-and-web-api.md)
// under /api/v1 guarded by the given shared Bearer token. A non-empty
// token also enables the API; per-user api_tokens credentials work
// alongside it. An empty token alone keeps /api/v1 unmounted — the API is
// fail-closed: no usable credential, no endpoint.
func WithAPIToken(token string) Option {
	return func(c *config) {
		c.apiToken = token
		if token != "" {
			c.apiEnabled = true
		}
	}
}

// WithAPIEnabled mounts /api/v1 even without a shared token, for the case
// where per-user api_tokens rows are the only credential. Callers decide
// this once at startup (e.g. store.HasAPITokens); a token issued later
// while the server runs does not remount the API.
func WithAPIEnabled(on bool) Option {
	return func(c *config) { c.apiEnabled = on }
}

// WithHub supplies the change hub the /{issues,discussions}/{id}/events
// SSE endpoints broadcast from. It must be the same hub the store
// publishes to — main wires both sides via
// (*store.Store).SetListener(hub.Publish). Callers that skip the option
// (tests, the storybook) get a private quiet hub: the endpoints exist
// but never emit.
func WithHub(hub *events.Hub) Option {
	return func(c *config) { c.hub = hub }
}

// New returns the root http.Handler for the application.
func New(st Store, opts ...Option) http.Handler {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.hub == nil {
		cfg.hub = events.NewHub()
	}
	h := &handler{store: st, r: NewRenderer(), hub: cfg.hub}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.home)
	mux.HandleFunc("GET /register", h.registerForm)
	mux.HandleFunc("POST /register", h.register)
	mux.HandleFunc("GET /login", h.loginForm)
	mux.HandleFunc("POST /login", h.login)
	mux.HandleFunc("POST /logout", h.logout)
	// Workspace management (docs/sketch/ja/workspaces.md).
	mux.HandleFunc("GET /workspaces", h.listWorkspaces)
	mux.HandleFunc("GET /workspaces/new", h.newWorkspace)
	mux.HandleFunc("POST /workspaces", h.createWorkspace)
	mux.HandleFunc("GET /w/{ws}/delete", h.scoped(h.confirmDeleteWorkspace))
	mux.HandleFunc("POST /w/{ws}/delete", h.scoped(h.deleteWorkspace))
	// Canonical resource URLs live under /w/{slug}; the workspace-less
	// /issues and /discussions paths stay as redirects at the bottom.
	mux.HandleFunc("GET /w/{ws}/issues", h.scoped(h.listIssues))
	mux.HandleFunc("GET /w/{ws}/issues/new", h.scoped(h.newIssue))
	mux.HandleFunc("POST /w/{ws}/issues", h.scoped(h.createIssue))
	mux.HandleFunc("GET /w/{ws}/issues/{id}", h.scoped(h.showIssue))
	mux.HandleFunc("GET /w/{ws}/issues/{id}/edit", h.scoped(h.editIssue))
	mux.HandleFunc("POST /w/{ws}/issues/{id}/edit", h.scoped(h.updateIssue))
	mux.HandleFunc("POST /w/{ws}/issues/{id}/status", h.scoped(h.setStatus))
	mux.HandleFunc("GET /w/{ws}/issues/{id}/delete", h.scoped(h.confirmDelete))
	mux.HandleFunc("POST /w/{ws}/issues/{id}/delete", h.scoped(h.deleteIssue))
	mux.HandleFunc("POST /w/{ws}/issues/{id}/comments", h.scoped(h.createComment))
	mux.HandleFunc("GET /w/{ws}/discussions", h.scoped(h.listDiscussions))
	mux.HandleFunc("GET /w/{ws}/discussions/new", h.scoped(h.newDiscussion))
	mux.HandleFunc("POST /w/{ws}/discussions", h.scoped(h.createDiscussion))
	mux.HandleFunc("GET /w/{ws}/discussions/{id}", h.scoped(h.showDiscussion))
	mux.HandleFunc("GET /w/{ws}/discussions/{id}/edit", h.scoped(h.editDiscussion))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/edit", h.scoped(h.updateDiscussion))
	mux.HandleFunc("GET /w/{ws}/discussions/{id}/delete", h.scoped(h.confirmDeleteDiscussion))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/delete", h.scoped(h.deleteDiscussion))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/comments", h.scoped(h.createDiscussionComment))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/comments/{cid}/answer", h.scoped(h.setCommentAnswer))
	// Live updates (docs/sketch/ja/live-updates.md): SSE change channels
	// plus the fragment endpoints live-updates.js refetches after a hint.
	mux.HandleFunc("GET /w/{ws}/issues/{id}/events", h.scoped(h.issueEvents))
	mux.HandleFunc("GET /w/{ws}/issues/{id}/fragments/comments", h.scoped(h.issueCommentsFragment))
	mux.HandleFunc("GET /w/{ws}/issues/{id}/fragments/article", h.scoped(h.issueArticleFragment))
	mux.HandleFunc("GET /w/{ws}/discussions/{id}/events", h.scoped(h.discussionEvents))
	mux.HandleFunc("GET /w/{ws}/discussions/{id}/fragments/threads", h.scoped(h.discussionThreadsFragment))
	mux.HandleFunc("GET /w/{ws}/discussions/{id}/fragments/replies", h.scoped(h.discussionRepliesFragment))
	mux.HandleFunc("GET /w/{ws}/discussions/{id}/fragments/article", h.scoped(h.discussionArticleFragment))
	// Legacy workspace-less URLs redirect into a workspace: collections
	// land in the default workspace, {id} paths in the resource's real
	// one (GET/HEAD → 302, other methods → 307 keeps method and body). They must be
	// method-scoped like the 404 catch-alls below: a method-less
	// "/issues/{rest...}" would conflict with "GET /" (more general path,
	// fewer methods). GET covers HEAD; other verbs get the mux's 405.
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		mux.HandleFunc(m+" /issues", h.legacyCollection("issues"))
		mux.HandleFunc(m+" /issues/{rest...}", h.legacySub("issues"))
		mux.HandleFunc(m+" /discussions", h.legacyCollection("discussions"))
		mux.HandleFunc(m+" /discussions/{rest...}", h.legacySub("discussions"))
	}
	if cfg.apiEnabled {
		// Method-scoped subtree patterns: a method-less "/api/v1/" would
		// conflict with the catch-all "GET /" (more general path, fewer
		// methods). Other verbs (PUT, OPTIONS, ...) keep the mux's 405.
		apiHandler := api.New(st, cfg.apiToken)
		for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
			mux.Handle(method+" /api/v1/", apiHandler)
		}
	}
	// Serve the single stylesheet explicitly: a bare "GET /static/" file
	// server would expose a directory listing and unstyled plaintext 404s.
	mux.HandleFunc("GET /static/app.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/app.css")
	})
	// External speculation rules for the issue list (Speculation-Rules
	// response header). The spec requires this exact media type; .json's
	// application/json default would make the browser ignore the file.
	mux.HandleFunc("GET /static/speculation-rules.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/speculationrules+json")
		http.ServeFileFS(w, r, staticFS, "static/speculation-rules.json")
	})
	// Search-term highlighting on the list page (CSS Custom Highlight API).
	mux.HandleFunc("GET /static/search-highlight.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/search-highlight.js")
	})
	// Comment permalink copy buttons on the detail page.
	mux.HandleFunc("GET /static/copy-permalink.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/copy-permalink.js")
	})
	// Ctrl/Cmd+Enter form submission from textareas.
	mux.HandleFunc("GET /static/enter-submit.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/enter-submit.js")
	})
	// Direction-tagging for cross-document view transitions between
	// paginated list pages.
	mux.HandleFunc("GET /static/pagination-transition.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/pagination-transition.js")
	})
	// aria-invalid ↔ :user-invalid sync + polite error announcements on
	// pages with forms (detail comment form, issue create/edit form).
	mux.HandleFunc("GET /static/form-errors.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/form-errors.js")
	})
	// Shared polite live region used by the form-error and partial-submit
	// modules (imported, not loaded as a script tag).
	mux.HandleFunc("GET /static/announcer.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/announcer.js")
	})
	// Comment forms' partial-update submission (fetch → fragment → DOM
	// swap) on the issue/discussion detail pages.
	mux.HandleFunc("GET /static/partial-submit.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/partial-submit.js")
	})
	// Visual toast notifications shown on partial-post success (imported
	// by partial-submit.js, not loaded as a script tag).
	mux.HandleFunc("GET /static/toast.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/toast.js")
	})
	// Disable comment submit buttons while the textarea is empty (module;
	// also imported by partial-submit.js to re-sync after a partial post).
	mux.HandleFunc("GET /static/comment-submit-disabled.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/comment-submit-disabled.js")
	})
	// beforeunload guard against losing unsaved form input.
	mux.HandleFunc("GET /static/leave-guard.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/leave-guard.js")
	})
	// Shared element-lifecycle registry (attach/detach on DOM mutations) —
	// a module, fetched only as an import dependency.
	mux.HandleFunc("GET /static/dom-hooks.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/dom-hooks.js")
	})
	// Fragment parsing + scoped-transition helpers shared by
	// partial-submit.js and live-updates.js (imported, not a script tag).
	mux.HandleFunc("GET /static/fragments.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/fragments.js")
	})
	// EventSource client that refetches fragments on pushed change hints
	// (imported by page-detail.js).
	mux.HandleFunc("GET /static/live-updates.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/live-updates.js")
	})
	// Per-page-type module entries: the single <script type="module"> tag
	// on the detail pages and the create/edit form pages.
	mux.HandleFunc("GET /static/page-detail.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/page-detail.js")
	})
	mux.HandleFunc("GET /static/page-form.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/page-form.js")
	})
	// Catch-alls: unknown GET/POST paths render the styled 404 page instead
	// of the mux's plaintext default. They are method-scoped on purpose — a
	// method-less "/" pattern would match PUT/DELETE too and swallow the
	// mux's automatic 405 Method Not Allowed.
	mux.HandleFunc("GET /", h.notFound)
	mux.HandleFunc("POST /", h.notFound)

	// Requests run under withUser (session-cookie → context auth resolution)
	// and then the stdlib CrossOriginProtection middleware, which rejects
	// cross-site state-changing browser requests (CSRF) via Sec-Fetch-Site /
	// Origin headers.
	return securityHeaders(http.NewCrossOriginProtection().Handler(withUser(h, mux)))
}

// contentSecurityPolicy is the enforced CSP for every response. The app
// serves every byte itself — one stylesheet plus a data: favicon — so the
// policy can be near-default-deny. script-src is 'self' rather than 'none':
// JavaScript is allowed once its behavior is verifiable via the storybook
// (see docs/ja/decisions.md), but only from same-origin files under
// /static/ — inline <script> stays blocked.
//
// The one deliberate hole is style-src-attr 'unsafe-inline': label badges
// carry their DB-driven color in a style="--label-color: ..." attribute, and
// CSP offers no nonce/hash mechanism for style attribute values. The hole is
// narrow — <style> elements stay blocked under style-src 'self', and CSS
// url() loads can't exfiltrate because img-src allows no remote origin.
// Browsers too old for style-src-attr fall back to style-src for the
// attribute and simply render badges in the muted fallback color instead of
// breaking layout.
//
// prefetch-src is deliberately absent: Chromium does not implement that
// draft directive (it logs "Unrecognized Content-Security-Policy
// directive"), and same-origin document prefetches triggered by the
// Speculation-Rules header are not blocked by default-src 'none' —
// verified in real Chromium on the issue list.
//
// img-src is 'self' data: on purpose: goldmark renders user-posted
// ![i](https://...) as <img>, and remote images in user content are a
// tracking-pixel channel. data: is needed only for our own favicon.
//
// Deliberately absent: require-trusted-types-for (the app has no DOM
// script sinks to guard), upgrade-insecure-requests (the app is served over
// plain HTTP and the directive would rewrite every URL to https), report-to
// (no reporting endpoint exists), and HSTS (ignored over HTTP; this is a
// localhost app with no TLS).
//
// connect-src 'self' covers the fetch() calls partial-submit.js makes for
// comment posting; without it the request is blocked before reaching the
// network (fetch obeys connect-src, not script-src).
const contentSecurityPolicy = "default-src 'none'; " +
	"script-src 'self'; " +
	"connect-src 'self'; " +
	"style-src 'self'; " +
	// style-src-elem 'unsafe-inline' allows inline <style> elements: layout.html
	// carries the @view-transition navigation:auto opt-in there because Chrome
	// may check the opt-in before the external stylesheet finishes loading,
	// and Chrome also materializes a transition's capture styles as <style>
	// at runtime. Attribute styles stay governed by style-src-attr below.
	"style-src-elem 'self' 'unsafe-inline'; " +
	"style-src-attr 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// permissionsPolicy denies powerful features the app never uses; CSP has no
// directive covering them.
const permissionsPolicy = "accelerometer=(), camera=(), geolocation=(), " +
	"gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"

// securityHeaders adds defense-in-depth response headers. Vary mirrors the
// request headers CrossOriginProtection inspects so shared caches cannot
// serve a response keyed on another request's fetch metadata.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin-allow-popups")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", permissionsPolicy)
		h.Add("Vary", "Sec-Fetch-Site")
		h.Add("Vary", "Origin")
		next.ServeHTTP(w, r)
	})
}

// handler bundles the dependencies shared by all route handlers.
type handler struct {
	store Store
	r     *Renderer
	hub   *events.Hub
}

var funcMap = template.FuncMap{
	"fmtTime":     fmtTime,
	"markdown":    renderMarkdown,
	"add":         func(a, b int) int { return a + b },
	"sub":         func(a, b int) int { return a - b },
	"isChecked":   isChecked,
	"authorName":  authorName,
	"listURL":     listURL,
	"dListURL":    dListURL,
	"statusURL":   statusURL,
	"labelURL":    labelURL,
	"maxTitleLen": func() int { return store.MaxTitleRunes },
	"dict":        dict,
}

// dict builds a map from alternating key/value pairs so templates can pass
// several values into one {{template}} call (the detail pages use it to
// hand page-level state into fragment defines).
func dict(pairs ...any) map[string]any {
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		key, _ := pairs[i].(string)
		m[key] = pairs[i+1]
	}
	return m
}

// wantsPartial reports whether the client asked for an HTML fragment
// response: partial-submit.js sets X-Partial on its fetch. Everything else
// gets the classic full-page PRG behavior.
func wantsPartial(r *http.Request) bool {
	return r.Header.Get("X-Partial") == "1"
}

// fmtTime renders a timestamp in a compact human-readable form.
func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("Jan 2, 2006 15:04")
}

// authorName renders a post's author; a nil author is an anonymous post.
func authorName(u *store.User) string {
	if u == nil {
		return "anonymous"
	}
	return u.Name
}

// renderError renders the styled error page carrying the request's user so
// the header keeps the logged-in state.
func (h *handler) renderError(w http.ResponseWriter, r *http.Request, status int, heading, message string) {
	h.r.renderErrorFor(w, status, heading, message, userFromContext(r.Context()))
}

// notFound serves the styled 404 page. It is registered as a route handler for
// the catch-all patterns, so it keeps the http.HandlerFunc signature; the
// request itself is only used to resolve the current user.
func (h *handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.renderError(w, r, http.StatusNotFound, "Not found",
		"The page or issue you are looking for does not exist or may have been deleted.")
}

// internalError logs the underlying error but renders a generic page so no
// internals leak to the client.
func (h *handler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("web: %s %s: %v", r.Method, r.URL.Path, err)
	h.renderError(w, r, http.StatusInternalServerError, "Internal server error",
		"Something went wrong on our side. Please try again.")
}

// parseForm installs a body size cap and parses the submitted form. On
// failure it writes the error response itself and reports false.
func (h *handler) parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	// ParseForm marks r.Form parsed without reading a multipart body, so a
	// later PostFormValue would never trigger ParseMultipartForm and every
	// field would read "". Dispatch on the media type instead.
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	var err error
	if mediaType == "multipart/form-data" {
		err = r.ParseMultipartForm(maxFormBytes)
	} else {
		err = r.ParseForm()
	}
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			h.renderError(w, r, http.StatusRequestEntityTooLarge, "Form too large",
				"The submitted form exceeded the size limit.")
			return false
		}
		h.renderError(w, r, http.StatusBadRequest, "Bad request",
			"The submitted form could not be read.")
		return false
	}
	return true
}

// parseID extracts the {id} path value. Non-numeric or non-positive IDs are
// reported as "not found" by the caller.
func parseID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, false
	}
	return id, true
}

// home redirects the bare root to the default workspace's issue list.
// Defined in workspaces.go with the other workspace helpers.
