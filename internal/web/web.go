// Package web implements the HTTP layer of the issue tracker: a classic
// multi-page application rendered server-side with html/template and no
// JavaScript. All state changes happen through HTML forms following the
// POST → Redirect → GET (PRG) pattern.
package web

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/api"
	"github.com/podhmo/go-classic-mpa-sample/internal/events"
	"github.com/podhmo/go-classic-mpa-sample/internal/gitrepo"
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type staticAsset struct {
	body        []byte
	gzipBody    []byte
	etag        string
	gzipETag    string
	contentType string
}

var staticAssets sync.Map

var staticManifest struct {
	once    sync.Once
	version string
	jsNames []string
	imports map[string][]string
}

// loadStaticManifest hashes every embedded asset into one deployment
// version. Entrypoint URLs use that version in a query string; keeping the
// file path stable preserves the storybook's live /static/ overlay, while
// changing any asset still busts the browser cache for the whole module
// graph.
func loadStaticManifest() {
	entries, err := staticFS.ReadDir("static")
	if err != nil {
		panic(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	h := sha256.New()
	jsNames := make([]string, 0, len(names))
	bodies := make(map[string][]byte, len(names))
	for _, name := range names {
		body, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			panic(err)
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(body)
		h.Write([]byte{0})
		if path.Ext(name) == ".js" {
			jsNames = append(jsNames, name)
			bodies[name] = body
		}
	}
	imports := make(map[string][]string, len(jsNames))
	for _, name := range jsNames {
		seen := map[string]bool{}
		for _, line := range bytes.Split(bodies[name], []byte("\n")) {
			line = bytes.TrimSpace(line)
			if !bytes.HasPrefix(line, []byte("import ")) && !bytes.HasPrefix(line, []byte("export ")) {
				continue
			}
			for _, dep := range jsNames {
				if name == dep || seen[dep] {
					continue
				}
				if bytes.Contains(line, []byte(`"./`+dep+`"`)) || bytes.Contains(line, []byte(`'./`+dep+`'`)) {
					seen[dep] = true
					imports[name] = append(imports[name], dep)
				}
			}
		}
	}
	sum := h.Sum(nil)
	staticManifest.version = hex.EncodeToString(sum[:8])
	staticManifest.jsNames = jsNames
	staticManifest.imports = imports
}

// modulePreloads returns the direct and transitive relative imports of an
// entry module in deterministic dependency-first order. The static directory
// is flat, so matching embedded "./name.js" specifiers is enough for the
// module graph this app serves.
func modulePreloads(entry string) []string {
	staticBuildVersion()
	seen := map[string]bool{}
	var out []string
	var visit func(string)
	visit = func(name string) {
		for _, dep := range staticManifest.imports[name] {
			if seen[dep] {
				continue
			}
			seen[dep] = true
			visit(dep)
			out = append(out, dep)
		}
	}
	visit(entry)
	return out
}

func staticBuildVersion() string {
	staticManifest.once.Do(loadStaticManifest)
	return staticManifest.version
}

func staticURL(name string) string {
	return "/static/" + name + "?v=" + staticBuildVersion()
}

func versionedStaticBody(name string, body []byte) []byte {
	if path.Ext(name) != ".js" {
		return body
	}
	staticBuildVersion()
	out := append([]byte(nil), body...)
	for _, dep := range staticManifest.jsNames {
		for _, quote := range []string{`"`, `'`} {
			plain := quote + "./" + dep + quote
			versioned := quote + "./" + dep + "?v=" + staticManifest.version + quote
			out = bytes.ReplaceAll(out, []byte(plain), []byte(versioned))
		}
	}
	return out
}

// loadStaticAsset reads and pre-compresses an embedded static file once.
// The embedded files change only across deploys, so their hashes are stable
// strong ETags for the process lifetime. Versioned module entrypoints get
// version-pinned imports; unversioned fallback URLs keep the raw source so
// an old cached fallback graph cannot start requesting rejected stale
// versions after a deploy.
func loadStaticAsset(name string, versioned bool) (*staticAsset, error) {
	cacheKey := name
	if versioned {
		cacheKey += "\x00versioned"
	}
	if cached, ok := staticAssets.Load(cacheKey); ok {
		return cached.(*staticAsset), nil
	}
	body, err := staticFS.ReadFile("static/" + name)
	if err != nil {
		return nil, err
	}
	if versioned {
		body = versionedStaticBody(name, body)
	}
	sum := sha256.Sum256(body)
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(body); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	gzipSum := sha256.Sum256(compressed.Bytes())
	asset := &staticAsset{
		body:        body,
		gzipBody:    compressed.Bytes(),
		etag:        `"` + hex.EncodeToString(sum[:16]) + `"`,
		gzipETag:    `"` + hex.EncodeToString(gzipSum[:16]) + `"`,
		contentType: mime.TypeByExtension(path.Ext(name)),
	}
	staticAssets.Store(cacheKey, asset)
	return asset, nil
}

func acceptsGzip(header string) bool {
	for part := range strings.SplitSeq(header, ",") {
		name, params, _ := strings.Cut(part, ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		q := 1.0
		for param := range strings.SplitSeq(params, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(param), "=")
			if ok && strings.EqualFold(k, "q") {
				parsed, err := strconv.ParseFloat(v, 64)
				if err != nil {
					q = 0
				} else {
					q = parsed
				}
			}
		}
		if q > 0 {
			return true
		}
	}
	return false
}

func ifNoneMatch(header, etag string) bool {
	for part := range strings.SplitSeq(header, ",") {
		candidate := strings.TrimSpace(part)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}

// Store is the persistence contract the web layer depends on. It matches the
// methods of *store.Store, which satisfies this interface automatically.
// Tests provide an in-memory implementation instead.
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
	ListArchivedLabels(ctx context.Context, wsID int64, kind string) ([]*store.Label, error)
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
	WorkspaceByID(ctx context.Context, id int64) (*store.Workspace, error)
	CreateWorkspace(ctx context.Context, in store.WorkspaceInput) (int64, error)
	DeleteWorkspace(ctx context.Context, slug string) error
	CreateUser(ctx context.Context, name, passwordHash string) (int64, error)
	AuthenticateUser(ctx context.Context, name, password string) (*store.User, error)
	CreateSession(ctx context.Context, userID int64, ttl time.Duration) (string, error)
	SessionUser(ctx context.Context, token string, ttl time.Duration) (*store.User, error)
	DeleteSession(ctx context.Context, token string) error
	APITokenUser(ctx context.Context, token string) (*store.User, error)
	// Pull requests (docs/sketch/ja/pull-requests.md) — workspace-independent;
	// used only when the repo was resolved at startup.
	ListPullRequests(ctx context.Context, f store.PRListFilter) ([]*store.PullRequest, int, error)
	GetPullRequest(ctx context.Context, id int64) (*store.PullRequest, error)
	CreatePullRequest(ctx context.Context, in store.PRInput, authorID int64, pin func(prID int64) error) (int64, error)
	UpdatePullRequest(ctx context.Context, id int64, in store.PRInput) error
	DeletePullRequest(ctx context.Context, id int64) error
	ClosePullRequest(ctx context.Context, id int64, headSHA, mergeBaseSHA string) error
	ReopenPullRequest(ctx context.Context, id int64) error
	MarkMerged(ctx context.Context, id int64, info store.PRMergeInfo) error
	ReserveMerge(ctx context.Context, id int64) (int64, error)
	ReleaseMerge(ctx context.Context, id int64, token int64) error
	MarkMergedWithReservation(ctx context.Context, id int64, info store.PRMergeInfo, token int64) error
	ListPRComments(ctx context.Context, prID int64) ([]*store.PRComment, error)
	GetPRComment(ctx context.Context, prID, commentID int64) (*store.PRComment, error)
	CreatePRComment(ctx context.Context, prID int64, in store.PRCommentInput, authorID int64) (int64, error)
	UpdatePRComment(ctx context.Context, prID, commentID int64, body string) error
	UpdatePRCommentVersion(ctx context.Context, prID, commentID int64, body string, expectedVersion int64) error
	DeletePRComment(ctx context.Context, prID, commentID int64) error
	SetPRCommentHidden(ctx context.Context, prID, commentID int64, reason string) error
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
	repo       *gitrepo.Repo
	clock      func() time.Time
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
// SSE endpoints broadcast from. It must be the same hub the store's
// change watcher publishes to — main wires both sides via
// (*store.Store).WatchChanges(ctx, watermark, hub.Publish). Callers that skip the
// option (tests, the storybook) get a private quiet hub: the endpoints
// exist but never emit.
func WithHub(hub *events.Hub) Option {
	return func(c *config) { c.hub = hub }
}

// WithClock overrides the wall clock used for response-only entities.
// Tests and storybook replays use it to keep rendered fragments deterministic.
func WithClock(clock func() time.Time) Option {
	return func(c *config) { c.clock = clock }
}

// WithRepo enables the pull-requests feature (docs/sketch/ja/pull-requests.md):
// a nil repo mounts no /pulls routes and hides the nav link — the feature
// simply does not exist without a repository.
func WithRepo(repo *gitrepo.Repo) Option {
	return func(c *config) { c.repo = repo }
}

// shortRef renders a full ref name for display: refs/heads/main → main.
func shortRef(ref string) string {
	return strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/")
}

// renderPathWriter carries the request path+query to the shared layout
// without changing every typed render method. Unwrap keeps
// http.ResponseController (used by SSE flush) working through the wrapper.
type renderPathWriter struct {
	http.ResponseWriter
	req *http.Request
}

func (w renderPathWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// renderPath is lazy: computing it while dispatching would parse the POST
// body before parseForm installs its size cap.
func (w renderPathWriter) renderPath() string { return renderCurrentPath(w.req) }

// renderCurrentPath avoids nesting one auth redirect inside another when
// the header is rendered on the login/register page itself.
func renderCurrentPath(r *http.Request) string {
	if (r.URL.Path == "/login" || r.URL.Path == "/register") && r.URL.Query().Get("next") != "" {
		return safeNext(r.URL.Query().Get("next"))
	}
	if r.Method == http.MethodPost {
		// Never trigger parsing here: withUser hands handlers a shallow request
		// copy, so this original request can still have an uncapped unread body.
		// Auth forms put next in their action query; PostForm is only a no-read
		// convenience when the handler already parsed the same request object.
		if r.PostForm != nil {
			if next := r.PostForm.Get("next"); next != "" {
				return safeNext(next)
			}
		}
		// Validation errors are rendered from POST-only mutation URLs. Use the
		// submitting page as the return destination; without it there is no
		// meaningful page to go back to.
		if ref := r.Referer(); ref != "" {
			if u, err := url.Parse(ref); err == nil && u.Host == r.Host && (u.Scheme == "http" || u.Scheme == "https") {
				return safeNext(u.RequestURI())
			}
		}
		return "/issues"
	}
	return r.URL.RequestURI()
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
	if cfg.clock == nil {
		cfg.clock = time.Now
	}
	h := &handler{store: st, r: NewRendererWithPulls(cfg.repo != nil), hub: cfg.hub, repo: cfg.repo, clock: cfg.clock}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.home)
	mux.HandleFunc("GET /register", h.registerForm)
	mux.HandleFunc("POST /register", h.register)
	mux.HandleFunc("GET /login", h.loginForm)
	mux.HandleFunc("POST /login", h.login)
	mux.HandleFunc("POST /logout", h.logout)
	mux.HandleFunc("POST /preview", h.scopedMarkdownPreview)
	// Workspace management (docs/sketch/ja/workspaces.md).
	mux.HandleFunc("GET /workspaces", h.listWorkspaces)
	mux.HandleFunc("GET /workspaces/new", h.newWorkspace)
	mux.HandleFunc("POST /workspaces", h.createWorkspace)
	mux.HandleFunc("GET /w/{ws}/delete", h.scopedWithCounts(h.confirmDeleteWorkspace))
	mux.HandleFunc("POST /w/{ws}/delete", h.scoped(h.deleteWorkspace))
	// Canonical resource URLs live under /w/{slug}; the workspace-less
	// /issues and /discussions paths stay as redirects at the bottom.
	mux.HandleFunc("POST /w/{ws}/preview", h.scoped(h.markdownPreview))
	mux.HandleFunc("GET /w/{ws}/issues", h.scoped(h.listIssues))
	mux.HandleFunc("GET /w/{ws}/issues/new", h.scoped(h.newIssue))
	mux.HandleFunc("POST /w/{ws}/issues", h.scoped(h.createIssue))
	mux.HandleFunc("GET /w/{ws}/issues/{id}", h.scoped(h.showIssue))
	mux.HandleFunc("GET /w/{ws}/issues/{id}/markdown", h.scoped(h.issueMarkdown))
	mux.HandleFunc("GET /w/{ws}/issues/{id}/edit", h.scoped(h.editIssue))
	mux.HandleFunc("POST /w/{ws}/issues/{id}/edit", h.scoped(h.updateIssue))
	mux.HandleFunc("POST /w/{ws}/issues/{id}/status", h.scoped(h.setStatus))
	mux.HandleFunc("GET /w/{ws}/issues/{id}/delete", h.scoped(h.confirmDelete))
	mux.HandleFunc("POST /w/{ws}/issues/{id}/delete", h.scoped(h.deleteIssue))
	mux.HandleFunc("POST /w/{ws}/issues/{id}/comments", h.scoped(h.createComment))
	// Comment moderation (internal/web/comments.go): edit via a dedicated
	// page, delete via a GET confirmation page, hide/unhide as in-place
	// POSTs — the same shapes the issue resource itself uses.
	mux.HandleFunc("GET /w/{ws}/issues/{id}/comments/{cid}/edit", h.scoped(h.editCommentForm))
	mux.HandleFunc("POST /w/{ws}/issues/{id}/comments/{cid}/edit", h.scoped(h.updateComment))
	mux.HandleFunc("GET /w/{ws}/issues/{id}/comments/{cid}/delete", h.scoped(h.confirmDeleteComment))
	mux.HandleFunc("POST /w/{ws}/issues/{id}/comments/{cid}/delete", h.scoped(h.deleteComment))
	mux.HandleFunc("POST /w/{ws}/issues/{id}/comments/{cid}/hide", h.scoped(h.hideComment))
	mux.HandleFunc("POST /w/{ws}/issues/{id}/comments/{cid}/unhide", h.scoped(h.unhideComment))
	// Label management (docs/ja/decisions.md — labels are DB-managed, not
	// constants): /w/{ws}/labels administers both the workspace's own
	// labels and the shared master labels.
	mux.HandleFunc("GET /w/{ws}/labels", h.scoped(h.listLabels))
	mux.HandleFunc("POST /w/{ws}/labels", h.scoped(h.createLabel))
	mux.HandleFunc("GET /w/{ws}/labels/{id}/edit", h.scoped(h.editLabel))
	mux.HandleFunc("POST /w/{ws}/labels/{id}/edit", h.scoped(h.updateLabel))
	mux.HandleFunc("POST /w/{ws}/labels/{id}/archive", h.scoped(h.archiveLabel))
	mux.HandleFunc("GET /w/{ws}/discussions", h.scoped(h.listDiscussions))
	mux.HandleFunc("GET /w/{ws}/discussions/new", h.scoped(h.newDiscussion))
	mux.HandleFunc("POST /w/{ws}/discussions", h.scoped(h.createDiscussion))
	mux.HandleFunc("GET /w/{ws}/discussions/{id}", h.scoped(h.showDiscussion))
	mux.HandleFunc("GET /w/{ws}/discussions/{id}/markdown", h.scoped(h.discussionMarkdown))
	mux.HandleFunc("GET /w/{ws}/discussions/{id}/edit", h.scoped(h.editDiscussion))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/edit", h.scoped(h.updateDiscussion))
	mux.HandleFunc("GET /w/{ws}/discussions/{id}/delete", h.scoped(h.confirmDeleteDiscussion))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/delete", h.scoped(h.deleteDiscussion))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/comments", h.scoped(h.createDiscussionComment))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/comments/{cid}/answer", h.scoped(h.setCommentAnswer))
	// Thread and reply moderation — the same edit/delete/hide shapes as
	// issue comments (internal/web/comments.go). Threads own the
	// threads/{tid} tree; replies live one level under it.
	mux.HandleFunc("GET /w/{ws}/discussions/{id}/threads/{tid}/edit", h.scoped(h.editThreadForm))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/threads/{tid}/edit", h.scoped(h.updateThread))
	mux.HandleFunc("GET /w/{ws}/discussions/{id}/threads/{tid}/delete", h.scoped(h.confirmDeleteThread))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/threads/{tid}/delete", h.scoped(h.deleteThread))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/threads/{tid}/hide", h.scoped(h.hideThread))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/threads/{tid}/unhide", h.scoped(h.unhideThread))
	mux.HandleFunc("GET /w/{ws}/discussions/{id}/threads/{tid}/comments/{cid}/edit", h.scoped(h.editReplyForm))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/threads/{tid}/comments/{cid}/edit", h.scoped(h.updateReply))
	mux.HandleFunc("GET /w/{ws}/discussions/{id}/threads/{tid}/comments/{cid}/delete", h.scoped(h.confirmDeleteReply))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/threads/{tid}/comments/{cid}/delete", h.scoped(h.deleteReply))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/threads/{tid}/comments/{cid}/hide", h.scoped(h.hideReply))
	mux.HandleFunc("POST /w/{ws}/discussions/{id}/threads/{tid}/comments/{cid}/unhide", h.scoped(h.unhideReply))
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
	if h.repo != nil {
		// Pull requests — workspace-independent, mounted only with a repo.
		mux.HandleFunc("GET /pulls", h.listPulls)
		mux.HandleFunc("GET /pulls/new", h.newPullForm)
		mux.HandleFunc("POST /pulls", h.createPull)
		mux.HandleFunc("GET /pulls/{id}", h.pullDetail)
		mux.HandleFunc("GET /pulls/{id}/edit", h.editPullForm)
		mux.HandleFunc("POST /pulls/{id}/edit", h.updatePull)
		mux.HandleFunc("GET /pulls/{id}/files", h.pullFiles)
		mux.HandleFunc("POST /pulls/{id}/comments", h.createPRComment)
		mux.HandleFunc("GET /pulls/{id}/comments/{cid}/edit", h.editPRCommentForm)
		mux.HandleFunc("POST /pulls/{id}/comments/{cid}/edit", h.updatePRComment)
		mux.HandleFunc("GET /pulls/{id}/comments/{cid}/delete", h.confirmDeletePRComment)
		mux.HandleFunc("POST /pulls/{id}/comments/{cid}/delete", h.deletePRComment)
		mux.HandleFunc("POST /pulls/{id}/comments/{cid}/hide", h.hidePRComment)
		mux.HandleFunc("POST /pulls/{id}/comments/{cid}/unhide", h.unhidePRComment)
		mux.HandleFunc("POST /pulls/{id}/status", h.updatePullStatus)
		mux.HandleFunc("POST /pulls/{id}/repair-pin", h.repairPullPin)
		mux.HandleFunc("GET /pulls/{id}/merge", h.mergePullForm)
		mux.HandleFunc("POST /pulls/{id}/merge", h.mergePull)
		mux.HandleFunc("GET /pulls/{id}/events", h.pullEvents)
		mux.HandleFunc("GET /pulls/{id}/fragments/comments", h.pullCommentsFragment)
		mux.HandleFunc("GET /pulls/{id}/fragments/article", h.pullArticleFragment)
	}
	if cfg.apiEnabled {
		// Method-scoped subtree patterns: a method-less "/api/v1/" would
		// conflict with the catch-all "GET /" (more general path, fewer
		// methods). Other verbs (PUT, OPTIONS, ...) keep the mux's 405.
		apiHandler := api.New(st, cfg.apiToken, cfg.repo)
		for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
			mux.Handle(method+" /api/v1/", apiHandler)
		}
	}
	// Static files: explicit per-file routes — a bare "GET /static/" file
	// server would expose a directory listing and unstyled plaintext 404s.
	// Rendered pages reference them with ?v=<content hash> and those URLs
	// are immutable. Unversioned URLs remain as compatibility fallbacks
	// with the older one-hour cache lifetime.
	staticMux := http.NewServeMux()
	staticPaths := map[string]bool{}
	serveStatic := func(name, contentType string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			version := r.URL.Query().Get("v")
			if version != "" && version != staticBuildVersion() {
				http.NotFound(w, r)
				return
			}
			versioned := version == staticBuildVersion()
			asset, err := loadStaticAsset(name, versioned)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			body := asset.body
			etag := asset.etag
			useGzip := acceptsGzip(r.Header.Get("Accept-Encoding"))
			if useGzip {
				body = asset.gzipBody
				etag = asset.gzipETag
			}
			cacheControl := "public, max-age=3600"
			if versioned {
				cacheControl = "public, max-age=31536000, immutable"
			}
			h := w.Header()
			h.Set("Cache-Control", cacheControl)
			h.Set("ETag", etag)
			h.Add("Vary", "Accept-Encoding")
			ct := contentType
			if ct == "" {
				ct = asset.contentType
			}
			if ct != "" {
				h.Set("Content-Type", ct)
			}
			if ifNoneMatch(r.Header.Get("If-None-Match"), etag) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			if useGzip {
				h.Set("Content-Encoding", "gzip")
			}
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
		}
	}
	registerStatic := func(route, name string, contentTypes ...string) {
		contentType := ""
		if len(contentTypes) > 0 {
			contentType = contentTypes[0]
		}
		staticPaths[route] = true
		staticMux.HandleFunc("GET "+route, serveStatic(name, contentType))
	}
	registerStatic("/static/app.css", "app.css")
	registerStatic("/static/i18n.js", "i18n.js")
	// External speculation rules for the issue list (Speculation-Rules
	// response header). The spec requires this exact media type; .json's
	// application/json default would make the browser ignore the file.
	registerStatic("/static/speculation-rules.json", "speculation-rules.json", "application/speculationrules+json")
	// Search-term highlighting on the list page (CSS Custom Highlight API).
	registerStatic("/static/search-highlight.js", "search-highlight.js")
	// Comment permalink copy buttons on the detail page.
	registerStatic("/static/copy-permalink.js", "copy-permalink.js")
	// "Copy as markdown" action on the article rows (imported by
	// page-detail.js).
	registerStatic("/static/copy-markdown.js", "copy-markdown.js")
	// Shared clipboard write + tip feedback used by the copy-* modules
	// (imported, not loaded as a script tag).
	registerStatic("/static/clipboard.js", "clipboard.js")
	// Ctrl/Cmd+Enter form submission from textareas.
	registerStatic("/static/enter-submit.js", "enter-submit.js")
	// Direction-tagging for cross-document view transitions between
	// paginated list pages.
	registerStatic("/static/pagination-transition.js", "pagination-transition.js")
	registerStatic("/static/nav-menu.js", "nav-menu.js")
	// aria-invalid ↔ :user-invalid sync + polite error announcements on
	// pages with forms (detail comment form, issue create/edit form).
	registerStatic("/static/form-errors.js", "form-errors.js")
	// Shared polite live region used by the form-error and partial-submit
	// modules (imported, not loaded as a script tag).
	registerStatic("/static/announcer.js", "announcer.js")
	// Comment forms' partial-update submission (fetch → fragment → DOM
	// swap) on the issue/discussion detail pages.
	registerStatic("/static/partial-submit.js", "partial-submit.js")
	// Visual toast notifications shown on partial-post success (imported
	// by partial-submit.js, not loaded as a script tag).
	registerStatic("/static/toast.js", "toast.js")
	// Disable comment submit buttons while the textarea is empty (module;
	// also imported by partial-submit.js to re-sync after a partial post).
	registerStatic("/static/comment-submit-disabled.js", "comment-submit-disabled.js")
	// beforeunload guard against losing unsaved form input.
	registerStatic("/static/leave-guard.js", "leave-guard.js")
	registerStatic("/static/markdown-editor.js", "markdown-editor.js")
	// Shared element-lifecycle registry (attach/detach on DOM mutations) —
	// a module, fetched only as an import dependency.
	registerStatic("/static/dom-hooks.js", "dom-hooks.js")
	// Fragment parsing + scoped-transition helpers shared by
	// partial-submit.js and live-updates.js (imported, not a script tag).
	registerStatic("/static/fragments.js", "fragments.js")
	// EventSource client that refetches fragments on pushed change hints
	// (imported by page-detail.js).
	registerStatic("/static/live-updates.js", "live-updates.js")
	// Per-page-type module entries: the single <script type="module"> tag
	// on the detail pages and the create/edit form pages.
	registerStatic("/static/page-detail.js", "page-detail.js")
	registerStatic("/static/page-form.js", "page-form.js")
	// Catch-alls: unknown GET/POST paths render the styled 404 page instead
	// of the mux's plaintext default. They are method-scoped on purpose — a
	// method-less "/" pattern would match PUT/DELETE too and swallow the
	// mux's automatic 405 Method Not Allowed.
	mux.HandleFunc("GET /locale", setLocale)
	mux.HandleFunc("GET /", h.notFound)
	mux.HandleFunc("POST /", h.notFound)

	// Static files bypass withUser: embedded public assets must not pay a
	// session lookup for every stylesheet/module. Dynamic requests still run
	// under withUser, then the stdlib CrossOriginProtection middleware
	// rejects cross-site state-changing browser requests (CSRF) via
	// Sec-Fetch-Site / Origin headers. withLocale sits outside them so the
	// ?lang= switcher redirect needs neither a session nor CSRF checks (it's
	// a plain GET).
	authedMux := withUser(h, mux)
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if staticPaths[r.URL.Path] {
			staticMux.ServeHTTP(w, r)
			return
		}
		authedMux.ServeHTTP(renderPathWriter{ResponseWriter: w, req: r}, r)
	})
	return securityHeaders(withLocale(http.NewCrossOriginProtection().Handler(root)))
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
		// First-visit locale resolution reads Accept-Language, so shared caches
		// must not serve a ja-negotiated page to an en client (or vice versa).
		h.Add("Vary", "Accept-Language")
		next.ServeHTTP(w, r)
	})
}

// handler bundles the dependencies shared by all route handlers.
type handler struct {
	store Store
	r     *Renderer
	hub   *events.Hub
	repo  *gitrepo.Repo
	clock func() time.Time
}

var funcMap = template.FuncMap{
	"fmtTime":        fmtTime,
	"markdown":       renderMarkdown,
	"markdownWS":     renderMarkdownForWorkspace,
	"staticURL":      staticURL,
	"modulePreloads": modulePreloads,
	"add":            func(a, b int) int { return a + b },
	"sub":            func(a, b int) int { return a - b },
	"isChecked":      isChecked,
	"authorName":     authorName,
	"listURL":        listURL,
	"dListURL":       dListURL,
	"statusURL":      statusURL,
	"labelURL":       labelURL,
	"maxTitleLen":    func() int { return store.MaxTitleRunes },
	"hideReasons":    func() []string { return store.HideReasons },
	"dict":           dict,
	"shortRef":       shortRef,
	"urlquery":       url.QueryEscape,
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
	h.r.renderErrorFor(w, status, heading, message, userFromContext(r.Context()), localeFromContext(r.Context()))
}

// notFound serves the styled 404 page. It is registered as a route handler for
// the catch-all patterns, so it keeps the http.HandlerFunc signature; the
// request itself is only used to resolve the current user.
func (h *handler) notFound(w http.ResponseWriter, r *http.Request) {
	msg := msgFor(r)
	h.renderError(w, r, http.StatusNotFound, msg.ErrNotFoundH, msg.ErrNotFoundM)
}

// internalError logs the underlying error but renders a generic page so no
// internals leak to the client.
func (h *handler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("web: %s %s: %v", r.Method, r.URL.Path, err)
	msg := msgFor(r)
	h.renderError(w, r, http.StatusInternalServerError, msg.ErrInternalH, msg.ErrInternalM)
}

// formVersion reads the hidden optimistic-concurrency token. Empty means
// an unconditional update, matching the API's absent If-Match; malformed
// values are treated as absent because they can only come from a crafted
// form post, while normal browsers submit the server-rendered value.
func formVersion(r *http.Request) int64 {
	v, _ := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if v < 1 {
		return 0
	}
	return v
}

func (h *handler) versionConflict(w http.ResponseWriter, r *http.Request) {
	msg := msgFor(r)
	h.renderError(w, r, http.StatusConflict, msg.ErrVersionConflictH, msg.ErrVersionConflictM)
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
			msg := msgFor(r)
			h.renderError(w, r, http.StatusRequestEntityTooLarge, msg.ErrFormTooLargeH, msg.ErrFormTooLargeM)
			return false
		}
		h.renderError(w, r, http.StatusBadRequest, msgFor(r).ErrBadRequestH, msgFor(r).ErrFormReadM)
		return false
	}
	return true
}

// redirectSeeOther completes a successful classic form POST. When the form
// carried the client-generated draft key, echo it in a short-lived cookie so
// markdown-editor.js can delete exactly that local draft after the PRG
// redirect; validation failures render in place and therefore never set it.
func redirectSeeOther(w http.ResponseWriter, r *http.Request, target string) {
	if key := r.PostFormValue("draft_key"); key != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     "zatudan_draft_posted",
			Value:    url.QueryEscape(key),
			Path:     "/",
			MaxAge:   30,
			SameSite: http.SameSiteLaxMode,
		})
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// writeMarkdown serves an exported markdown document. The copy-as-markdown
// button fetches these endpoints, so Content-Disposition stays inline — the
// body is clipboard input, not a download.
func writeMarkdown(w http.ResponseWriter, filename, doc string) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filename))
	io.WriteString(w, doc)
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
