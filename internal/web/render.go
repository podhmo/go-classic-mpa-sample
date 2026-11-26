package web

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"maps"
	"net/http"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// Renderer renders each page template through the shared layout. Handlers call
// the typed renderXxx methods so page templates and their view models stay in
// sync at compile time.
type Renderer struct {
	// pagesByLang holds one parsed set per supported locale: each parse
	// binds the message functions and {{lang}} to that locale so templates
	// and the shared layout localize without the view model carrying it.
	pagesByLang map[string]map[string]*template.Template
}

// NewRenderer parses the embedded page templates and returns a Renderer for
// a server without the pull-requests feature.
func NewRenderer() *Renderer {
	return NewRendererWithPulls(false)
}

// NewRendererWithPulls parses the embedded page templates with the nav's
// pull-request link state bound to this Renderer. Keeping that state here
// instead of in package state lets one process host repo-backed and repo-less
// handlers at the same time.
func NewRendererWithPulls(pullsOn bool) *Renderer {
	pages := make(map[string]map[string]*template.Template, len(supportedLangs))
	for _, lang := range supportedLangs {
		pages[lang] = parsePages(lang, pullsOn)
	}
	return &Renderer{pagesByLang: pages}
}

// parsePages builds one template set per page for the given locale. Each set
// contains the shared layout plus a single page template that defines the
// "content" block. ParseFS works the same when this package is interpreted
// (e.g. by minigo for the storybook preview): the bound template's ParseFS
// proxies a script-side embed.FS through fs.FS.
func parsePages(lang string, pullsOn bool) map[string]*template.Template {
	names := []string{"issues", "issue_detail", "issue_form", "issue_delete", "error", "auth_form",
		"discussions", "discussion_detail", "discussion_form", "discussion_delete",
		"workspaces", "workspace_form", "workspace_delete", "labels", "label_form",
		"comment_edit", "comment_delete",
		"pulls", "pull_detail", "pull_form", "pull_files", "pull_merge"}
	// Per-locale bindings: every messages field becomes a same-named
	// function ({{IssuesTitle}}, {{IssuesCount args...}}), so a typo'd call
	// is a parse error. {{lang}} prints the locale (<html lang>). jsDict is
	// marshaled once per locale rather than once per page render.
	funcs := maps.Clone(funcMap)
	messageFuncs(funcs, lang)
	funcs["lang"] = func() string { return lang }
	jsDict := jsDictFor(lang)
	funcs["jsDict"] = func() template.HTMLAttr { return jsDict }
	funcs["pullsOn"] = func() bool { return pullsOn }

	// Parse the shared layout once per locale, then clone it for each page.
	// Keep fragment files selective: html/template.Clone copies parse trees
	// (unlike text/template's shallow copy), so putting every fragment in
	// the shared set would duplicate unused trees into every page.
	fragments := map[string][]string{
		"issue_detail":      {"templates/fragments.html"},
		"discussion_detail": {"templates/fragments.html"},
		// pull_detail needs both: its own defines plus the shared
		// comment_actions it borrows for moderation controls.
		"pull_detail": {"templates/pull_fragments.html", "templates/fragments.html"},
	}
	partials := map[string][]string{
		"issues":            {"templates/partials.html"},
		"discussions":       {"templates/partials.html"},
		"pulls":             {"templates/partials.html"},
		"issue_delete":      {"templates/partials.html"},
		"discussion_delete": {"templates/partials.html"},
	}
	shared := template.Must(template.New("shared").Funcs(funcs).ParseFS(templateFS, "templates/layout.html"))
	pages := make(map[string]*template.Template, len(names))
	for _, name := range names {
		page := template.Must(shared.Clone())
		files := []string{"templates/" + name + ".html"}
		files = append(files, fragments[name]...)
		files = append(files, partials[name]...)
		pages[name] = template.Must(page.ParseFS(templateFS, files...))
	}
	return pages
}

// layoutData separates layout-only request state from the page view model.
// The content/head templates still receive Data unchanged, while the shared
// layout can build auth links that return to the current path+query.
type layoutData struct {
	Data        any
	CurrentPath string
}

type renderPathProvider interface {
	renderPath() string
}

// executeLang renders the named page template through the shared layout.
// Unknown locales fall back to the default set so a half-localized page stays
// renderable. The body is rendered into a buffer first so template failures
// cannot produce a half-written response.
func (r *Renderer) executeLang(w http.ResponseWriter, page, lang string, status int, data any) {
	currentPath := "/issues"
	if provider, ok := w.(renderPathProvider); ok {
		currentPath = safeNext(provider.renderPath())
	}
	data = layoutData{Data: data, CurrentPath: currentPath}
	pages := r.pagesByLang[lang]
	if pages == nil {
		pages = r.pagesByLang[supportedLangs[0]]
	}
	var buf bytes.Buffer
	if err := pages[page].ExecuteTemplate(&buf, "layout", data); err != nil {
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
func (r *Renderer) renderFragment(w http.ResponseWriter, page, name string, status int, data any, lang string) {
	pages := r.pagesByLang[lang]
	if pages == nil {
		pages = r.pagesByLang[supportedLangs[0]]
	}
	var buf bytes.Buffer
	if err := pages[page].ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("web: render fragment %q in %q: %v", name, page, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// renderIssueList renders GET /issues. The one prototype-localized page
// takes the request locale; every other page renders with the default set.
func (r *Renderer) renderIssueList(w http.ResponseWriter, status int, data issueListPage, lang string) {
	r.executeLang(w, "issues", lang, status, data)
}

// renderIssueDetail renders GET /issues/{id}.
func (r *Renderer) renderIssueDetail(w http.ResponseWriter, status int, data issueDetailPage, lang string) {
	r.executeLang(w, "issue_detail", lang, status, data)
}

// renderIssueForm renders the new-issue and edit-issue forms.
func (r *Renderer) renderIssueForm(w http.ResponseWriter, status int, data issueFormPage, lang string) {
	r.executeLang(w, "issue_form", lang, status, data)
}

// renderIssueDelete renders the delete confirmation page.
func (r *Renderer) renderIssueDelete(w http.ResponseWriter, status int, data issueDeletePage, lang string) {
	r.executeLang(w, "issue_delete", lang, status, data)
}

// renderAuthForm renders the register and login forms.
func (r *Renderer) renderAuthForm(w http.ResponseWriter, status int, data authFormPage, lang string) {
	r.executeLang(w, "auth_form", lang, status, data)
}

// renderDiscussionList renders GET /discussions.
func (r *Renderer) renderDiscussionList(w http.ResponseWriter, status int, data discussionListPage, lang string) {
	r.executeLang(w, "discussions", lang, status, data)
}

// renderDiscussionDetail renders GET /discussions/{id}.
func (r *Renderer) renderDiscussionDetail(w http.ResponseWriter, status int, data discussionDetailPage, lang string) {
	r.executeLang(w, "discussion_detail", lang, status, data)
}

// renderDiscussionForm renders the new-discussion and edit-discussion forms.
func (r *Renderer) renderDiscussionForm(w http.ResponseWriter, status int, data discussionFormPage, lang string) {
	r.executeLang(w, "discussion_form", lang, status, data)
}

// renderDiscussionDelete renders the delete confirmation page.
func (r *Renderer) renderDiscussionDelete(w http.ResponseWriter, status int, data discussionDeletePage, lang string) {
	r.executeLang(w, "discussion_delete", lang, status, data)
}

// renderWorkspaceList renders GET /workspaces.
func (r *Renderer) renderWorkspaceList(w http.ResponseWriter, status int, data workspaceListPage, lang string) {
	r.executeLang(w, "workspaces", lang, status, data)
}

// renderWorkspaceForm renders the new-workspace form.
func (r *Renderer) renderWorkspaceForm(w http.ResponseWriter, status int, data workspaceFormPage, lang string) {
	r.executeLang(w, "workspace_form", lang, status, data)
}

// renderLabels renders GET /w/{ws}/labels.
func (r *Renderer) renderLabels(w http.ResponseWriter, status int, data labelsPage, lang string) {
	r.executeLang(w, "labels", lang, status, data)
}

// renderLabelForm renders the label edit form.
func (r *Renderer) renderLabelForm(w http.ResponseWriter, status int, data labelFormPage, lang string) {
	r.executeLang(w, "label_form", lang, status, data)
}

// renderWorkspaceDelete renders the workspace delete confirmation page.
func (r *Renderer) renderWorkspaceDelete(w http.ResponseWriter, status int, data workspaceDeletePage, lang string) {
	r.executeLang(w, "workspace_delete", lang, status, data)
}

// renderCommentEdit renders the shared comment edit page (issue
// comments, threads and replies all use it).
func (r *Renderer) renderCommentEdit(w http.ResponseWriter, status int, data commentEditPage, lang string) {
	r.executeLang(w, "comment_edit", lang, status, data)
}

// renderCommentDelete renders the shared comment delete confirmation.
func (r *Renderer) renderCommentDelete(w http.ResponseWriter, status int, data commentDeletePage, lang string) {
	r.executeLang(w, "comment_delete", lang, status, data)
}

// renderPullList renders GET /pulls.
func (r *Renderer) renderPullList(w http.ResponseWriter, status int, data pullListPage, lang string) {
	r.executeLang(w, "pulls", lang, status, data)
}

// renderPullDetail renders GET /pulls/{id}.
func (r *Renderer) renderPullDetail(w http.ResponseWriter, status int, data pullDetailPage, lang string) {
	r.executeLang(w, "pull_detail", lang, status, data)
}

// renderPullForm renders the new/edit pull request form.
func (r *Renderer) renderPullForm(w http.ResponseWriter, status int, data pullFormPage, lang string) {
	r.executeLang(w, "pull_form", lang, status, data)
}

// renderPullFiles renders GET /pulls/{id}/files.
func (r *Renderer) renderPullFiles(w http.ResponseWriter, status int, data pullFilesPage, lang string) {
	r.executeLang(w, "pull_files", lang, status, data)
}

// renderPullMerge renders GET /pulls/{id}/merge.
func (r *Renderer) renderPullMerge(w http.ResponseWriter, status int, data pullMergePage, lang string) {
	r.executeLang(w, "pull_merge", lang, status, data)
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
	r.renderErrorFor(w, status, heading, message, nil, supportedLangs[0])
}

func (r *Renderer) renderErrorFor(w http.ResponseWriter, status int, heading, message string, user *store.User, lang string) {
	r.executeLang(w, "error", lang, status, errorPageData{
		Title:   fmt.Sprintf("%d %s", status, heading),
		Code:    status,
		Heading: heading,
		Message: message,
		User:    user,
	})
}
