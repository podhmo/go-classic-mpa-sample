package web

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
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
// ParseFS works the same when this package is interpreted (e.g. by minigo
// for the storybook preview): the bound template's ParseFS proxies a
// script-side embed.FS through fs.FS.
func parsePages() map[string]*template.Template {
	names := []string{"issues", "issue_detail", "issue_form", "issue_delete", "error", "auth_form",
		"discussions", "discussion_detail", "discussion_form", "discussion_delete",
		"workspaces", "workspace_form", "workspace_delete"}
	// The detail pages also parse fragments.html: its defines power both
	// their inline markup and the standalone partial-update responses.
	fragments := map[string]bool{"issue_detail": true, "discussion_detail": true}
	pages := make(map[string]*template.Template, len(names))
	for _, name := range names {
		files := []string{"templates/layout.html", "templates/" + name + ".html"}
		if fragments[name] {
			files = append(files, "templates/fragments.html")
		}
		pages[name] = template.Must(template.New(name).Funcs(funcMap).ParseFS(templateFS, files...))
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

// renderFragment renders one named define from a page's template set and
// writes it as a standalone response — the partial-update fragment contract
// (X-Partial request header; see docs/sketch/ja/partial-updates.md). Like
// execute it buffers first so a template failure can't half-write a reply.
func (r *Renderer) renderFragment(w http.ResponseWriter, page, name string, status int, data any) {
	var buf bytes.Buffer
	if err := r.pages[page].ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("web: render fragment %q in %q: %v", name, page, err)
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

// renderAuthForm renders the register and login forms.
func (r *Renderer) renderAuthForm(w http.ResponseWriter, status int, data authFormPage) {
	r.execute(w, "auth_form", status, data)
}

// renderDiscussionList renders GET /discussions.
func (r *Renderer) renderDiscussionList(w http.ResponseWriter, status int, data discussionListPage) {
	r.execute(w, "discussions", status, data)
}

// renderDiscussionDetail renders GET /discussions/{id}.
func (r *Renderer) renderDiscussionDetail(w http.ResponseWriter, status int, data discussionDetailPage) {
	r.execute(w, "discussion_detail", status, data)
}

// renderDiscussionForm renders the new-discussion and edit-discussion forms.
func (r *Renderer) renderDiscussionForm(w http.ResponseWriter, status int, data discussionFormPage) {
	r.execute(w, "discussion_form", status, data)
}

// renderDiscussionDelete renders the delete confirmation page.
func (r *Renderer) renderDiscussionDelete(w http.ResponseWriter, status int, data discussionDeletePage) {
	r.execute(w, "discussion_delete", status, data)
}

// renderWorkspaceList renders GET /workspaces.
func (r *Renderer) renderWorkspaceList(w http.ResponseWriter, status int, data workspaceListPage) {
	r.execute(w, "workspaces", status, data)
}

// renderWorkspaceForm renders the new-workspace form.
func (r *Renderer) renderWorkspaceForm(w http.ResponseWriter, status int, data workspaceFormPage) {
	r.execute(w, "workspace_form", status, data)
}

// renderWorkspaceDelete renders the workspace delete confirmation page.
func (r *Renderer) renderWorkspaceDelete(w http.ResponseWriter, status int, data workspaceDeletePage) {
	r.execute(w, "workspace_delete", status, data)
}

type errorPageData struct {
	Title     string
	Code      int
	Heading   string
	Message   string
	Workspace *store.Workspace
	Section   string
	User      *store.User
}

// renderError renders the error page with an anonymous header; callers
// holding the request should use renderErrorFor so a logged-in user keeps
// their logged-in header on error pages too.
func (r *Renderer) renderError(w http.ResponseWriter, status int, heading, message string) {
	r.renderErrorFor(w, status, heading, message, nil)
}

func (r *Renderer) renderErrorFor(w http.ResponseWriter, status int, heading, message string, user *store.User) {
	r.execute(w, "error", status, errorPageData{
		Title:   fmt.Sprintf("%d %s", status, heading),
		Code:    status,
		Heading: heading,
		Message: message,
		User:    user,
	})
}

func (r *Renderer) notFound(w http.ResponseWriter) {
	r.renderError(w, http.StatusNotFound, "Not found",
		"The page or issue you are looking for does not exist or may have been deleted.")
}
