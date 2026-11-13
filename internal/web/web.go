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
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

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
	ListIssues(ctx context.Context, f store.ListFilter) ([]*store.Issue, int, error)
	GetIssue(ctx context.Context, id int64) (*store.Issue, error)
	CreateIssue(ctx context.Context, in store.IssueInput) (int64, error)
	UpdateIssue(ctx context.Context, id int64, in store.IssueInput) error
	SetIssueStatus(ctx context.Context, id int64, status string) error
	DeleteIssue(ctx context.Context, id int64) error
	ListComments(ctx context.Context, issueID int64) ([]*store.Comment, error)
	CreateComment(ctx context.Context, issueID int64, body string) (int64, error)
	ListLabels(ctx context.Context) ([]*store.Label, error)
}

const (
	// maxFormBytes caps POST request bodies (forms only send a few fields).
	maxFormBytes = 1 << 20 // 1 MiB
	// maxTitleRunes is the issue title length limit.
	maxTitleRunes = 200
	// maxBodyRunes is the length limit for issue bodies and comments.
	maxBodyRunes = 10000
	// perPage is the fixed page size of the issue list.
	perPage = 20
)

// New returns the root http.Handler for the application. The ServeMux uses
// Go 1.22+ method+path patterns and is wrapped in the stdlib
// CrossOriginProtection middleware, which rejects cross-site state-changing
// browser requests (CSRF) via Sec-Fetch-Site / Origin headers.
func New(st Store) http.Handler {
	h := &handler{store: st, r: NewRenderer()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.home)
	mux.HandleFunc("GET /issues", h.listIssues)
	mux.HandleFunc("GET /issues/new", h.newIssue)
	mux.HandleFunc("POST /issues", h.createIssue)
	mux.HandleFunc("GET /issues/{id}", h.showIssue)
	mux.HandleFunc("GET /issues/{id}/edit", h.editIssue)
	mux.HandleFunc("POST /issues/{id}/edit", h.updateIssue)
	mux.HandleFunc("POST /issues/{id}/status", h.setStatus)
	mux.HandleFunc("GET /issues/{id}/delete", h.confirmDelete)
	mux.HandleFunc("POST /issues/{id}/delete", h.deleteIssue)
	mux.HandleFunc("POST /issues/{id}/comments", h.createComment)
	// Serve the single stylesheet explicitly: a bare "GET /static/" file
	// server would expose a directory listing and unstyled plaintext 404s.
	mux.HandleFunc("GET /static/app.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/app.css")
	})
	// Catch-alls: unknown GET/POST paths render the styled 404 page instead
	// of the mux's plaintext default. They are method-scoped on purpose — a
	// method-less "/" pattern would match PUT/DELETE too and swallow the
	// mux's automatic 405 Method Not Allowed.
	mux.HandleFunc("GET /", h.notFound)
	mux.HandleFunc("POST /", h.notFound)

	return securityHeaders(http.NewCrossOriginProtection().Handler(mux))
}

// securityHeaders adds a minimal set of defense-in-depth response headers.
// No CSP: label badges use inline styles, which would require unsafe-inline.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// handler bundles the dependencies shared by all route handlers.
type handler struct {
	store Store
	r     *Renderer
}

var funcMap = template.FuncMap{
	"fmtTime":     fmtTime,
	"markdown":    renderMarkdown,
	"add":         func(a, b int) int { return a + b },
	"sub":         func(a, b int) int { return a - b },
	"isChecked":   isChecked,
	"listURL":     listURL,
	"statusURL":   statusURL,
	"labelURL":    labelURL,
	"maxTitleLen": func() int { return maxTitleRunes },
}

// fmtTime renders a timestamp in a compact human-readable form.
func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("Jan 2, 2006 15:04")
}

// isChecked reports whether id is among the selected label IDs.
func isChecked(id int64, ids []int64) bool {
	return slices.Contains(ids, id)
}

// issuesURL builds /issues URLs carrying the current filter state, so
// pagination, tabs and label links preserve active filters.
func issuesURL(f store.ListFilter) string {
	v := url.Values{}
	// "open" is the default filter, so keep URLs clean by omitting it.
	if f.Status != "" && f.Status != store.StatusOpen {
		v.Set("status", f.Status)
	}
	if f.Query != "" {
		v.Set("q", f.Query)
	}
	if f.LabelID != 0 {
		v.Set("label", strconv.FormatInt(f.LabelID, 10))
	}
	if f.Sort != "" {
		v.Set("sort", f.Sort)
	}
	if f.Page > 1 {
		v.Set("page", strconv.Itoa(f.Page))
	}
	if enc := v.Encode(); enc != "" {
		return "/issues?" + enc
	}
	return "/issues"
}

// listURL keeps the filter but moves to another page.
func listURL(f store.ListFilter, page int) string {
	f.Page = page
	return issuesURL(f)
}

// statusURL keeps the filter but switches the status tab and resets paging.
func statusURL(f store.ListFilter, status string) string {
	f.Status = status
	f.Page = 1
	return issuesURL(f)
}

// labelURL keeps the filter but selects a label and resets paging.
func labelURL(f store.ListFilter, labelID int64) string {
	f.LabelID = labelID
	f.Page = 1
	return issuesURL(f)
}

// notFound serves the styled 404 page. It is registered as a route handler for
// the catch-all patterns, so it keeps the http.HandlerFunc signature; the
// request itself is unused.
func (h *handler) notFound(w http.ResponseWriter, _ *http.Request) {
	h.r.notFound(w)
}

// internalError logs the underlying error but renders a generic page so no
// internals leak to the client.
func (h *handler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("web: %s %s: %v", r.Method, r.URL.Path, err)
	h.r.renderError(w, http.StatusInternalServerError, "Internal server error",
		"Something went wrong on our side. Please try again.")
}

// parseForm installs a body size cap and parses the submitted form. On
// failure it writes the error response itself and reports false.
func (h *handler) parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			h.r.renderError(w, http.StatusRequestEntityTooLarge, "Form too large",
				"The submitted form exceeded the size limit.")
			return false
		}
		h.r.renderError(w, http.StatusBadRequest, "Bad request",
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

// parseListFilter reads and normalizes the list query parameters.
func parseListFilter(r *http.Request) store.ListFilter {
	q := r.URL.Query()
	f := store.ListFilter{
		Status:  q.Get("status"),
		Query:   strings.TrimSpace(q.Get("q")),
		Sort:    q.Get("sort"),
		Page:    1,
		PerPage: perPage,
	}
	switch f.Status {
	case store.StatusOpen, store.StatusClosed, "all":
	default:
		f.Status = store.StatusOpen
	}
	if f.Sort != "oldest" {
		f.Sort = ""
	}
	// Reject absurd page numbers: huge values can overflow the store's
	// (page-1)*perPage offset arithmetic and are never meaningful anyway.
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 0 && n <= 1_000_000 {
		f.Page = n
	}
	if v := q.Get("label"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			f.LabelID = id
		}
	}
	return f
}
