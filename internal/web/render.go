package web

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
)

// Renderer renders each page template through the shared layout. Handlers call
// the typed renderXxx methods so page templates and their view models stay in
// sync at compile time; the any parameter exists only in execute.
type Renderer struct {
	pages map[string]*template.Template
}

// NewRenderer parses the embedded page templates and returns a Renderer.
func NewRenderer() *Renderer {
	return &Renderer{pages: parsePages()}
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

// execute renders the named page template through the shared layout. The body
// is rendered into a buffer first so template failures cannot produce a
// half-written response.
func (r *Renderer) execute(w http.ResponseWriter, page string, status int, data any) {
	var buf bytes.Buffer
	if err := r.pages[page].ExecuteTemplate(&buf, "layout", data); err != nil {
		log.Printf("web: render %q: %v", page, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// renderIssueList renders GET /issues.
func (r *Renderer) renderIssueList(w http.ResponseWriter, status int, data issueListPage) {
	r.execute(w, "issues", status, data)
}

// renderIssueDetail renders GET /issues/{id}.
func (r *Renderer) renderIssueDetail(w http.ResponseWriter, status int, data issueDetailPage) {
	r.execute(w, "issue_detail", status, data)
}

// renderIssueForm renders the new-issue and edit-issue forms.
func (r *Renderer) renderIssueForm(w http.ResponseWriter, status int, data issueFormPage) {
	r.execute(w, "issue_form", status, data)
}

// renderIssueDelete renders the delete confirmation page.
func (r *Renderer) renderIssueDelete(w http.ResponseWriter, status int, data issueDeletePage) {
	r.execute(w, "issue_delete", status, data)
}

type errorPageData struct {
	Title   string
	Code    int
	Heading string
	Message string
}

func (r *Renderer) renderError(w http.ResponseWriter, status int, heading, message string) {
	r.execute(w, "error", status, errorPageData{
		Title:   fmt.Sprintf("%d %s", status, heading),
		Code:    status,
		Heading: heading,
		Message: message,
	})
}

func (r *Renderer) notFound(w http.ResponseWriter) {
	r.renderError(w, http.StatusNotFound, "Not found",
		"The page or issue you are looking for does not exist or may have been deleted.")
}
