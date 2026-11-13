// Package web implements the HTTP layer of the issue tracker: a classic
// multi-page application rendered server-side with html/template and no
// JavaScript. All state changes happen through HTML forms following the
// POST → Redirect → GET (PRG) pattern.
package web

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
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
	h := &handler{store: st, pages: parsePages()}

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
	mux.Handle("GET /static/", http.FileServerFS(staticFS))

	return http.NewCrossOriginProtection().Handler(mux)
}

// handler bundles the dependencies shared by all route handlers.
type handler struct {
	store Store
	pages map[string]*template.Template
}

// parsePages builds one template set per page. Each set contains the shared
// layout plus a single page template that defines the "content" block.
func parsePages() map[string]*template.Template {
	names := []string{"issues", "issue_detail", "issue_form", "issue_delete", "error"}
	pages := make(map[string]*template.Template, len(names))
	for _, name := range names {
		pages[name] = template.Must(template.New(name).Funcs(funcMap).ParseFS(
			templateFS,
			"templates/layout.html",
			"templates/"+name+".html",
		))
	}
	return pages
}

var funcMap = template.FuncMap{
	"fmtTime":     fmtTime,
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
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// issuesURL builds /issues URLs carrying the current filter state, so
// pagination, tabs and label links preserve active filters.
func issuesURL(f store.ListFilter) string {
	v := url.Values{}
	if f.Status != "" {
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

// render executes a page template through the shared layout. The body is
// rendered into a buffer first so template failures cannot produce a
// half-written response.
func (h *handler) render(w http.ResponseWriter, name string, status int, data any) {
	var buf bytes.Buffer
	if err := h.pages[name].ExecuteTemplate(&buf, "layout", data); err != nil {
		log.Printf("web: render %q: %v", name, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

type errorPageData struct {
	Title   string
	Code    int
	Heading string
	Message string
}

func (h *handler) renderError(w http.ResponseWriter, status int, heading, message string) {
	h.render(w, "error", status, errorPageData{
		Title:   fmt.Sprintf("%d %s", status, heading),
		Code:    status,
		Heading: heading,
		Message: message,
	})
}

func (h *handler) notFound(w http.ResponseWriter) {
	h.renderError(w, http.StatusNotFound, "Not found",
		"The page or issue you are looking for does not exist or may have been deleted.")
}

// internalError logs the underlying error but renders a generic page so no
// internals leak to the client.
func (h *handler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("web: %s %s: %v", r.Method, r.URL.Path, err)
	h.renderError(w, http.StatusInternalServerError, "Internal server error",
		"Something went wrong on our side. Please try again.")
}

// parseForm installs a body size cap and parses the submitted form. On
// failure it writes the error response itself and reports false.
func (h *handler) parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		var mbErr *http.MaxBytesError
		if errors.As(err, &mbErr) {
			h.renderError(w, http.StatusRequestEntityTooLarge, "Form too large",
				"The submitted form exceeded the size limit.")
			return false
		}
		h.renderError(w, http.StatusBadRequest, "Bad request",
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
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 0 {
		f.Page = n
	}
	if v := q.Get("label"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			f.LabelID = id
		}
	}
	return f
}
