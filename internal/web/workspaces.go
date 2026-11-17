// Workspace routing: every issue/discussion URL lives under
// /w/{slug}/..., plus the workspace management pages (/workspaces, new,
// delete) and the redirects that keep pre-workspace /issues and
// /discussions URLs working. Design: docs/sketch/ja/workspaces.md.
package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// scoped resolves the {ws} path segment into its workspace and calls fn.
// An unknown slug is a plain 404 — the segment is part of the canonical
// address, not a redirect hint.
func (h *handler) scoped(fn func(http.ResponseWriter, *http.Request, *store.Workspace)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws, err := h.store.WorkspaceBySlug(r.Context(), r.PathValue("ws"))
		if err != nil {
			h.workspaceError(w, r, err)
			return
		}
		fn(w, r, ws)
	}
}

// requireScope checks that a fetched resource belongs to the request's
// workspace. On a mismatch GET/HEAD redirect (302) to the same path under
// the resource's real workspace — the canonical URL rule of
// docs/sketch/ja/workspaces.md — while non-GET methods get a 404: a POST
// silently replayed in another workspace could mutate the wrong place.
func (h *handler) requireScope(w http.ResponseWriter, r *http.Request, ws *store.Workspace, realWSID int64) bool {
	if realWSID == ws.ID {
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.notFound(w, r)
		return false
	}
	real, err := h.store.WorkspaceByID(r.Context(), realWSID)
	if err != nil {
		h.internalError(w, r, err)
		return false
	}
	http.Redirect(w, r, rebasePath(r, "/w/"+real.Slug), http.StatusFound)
	return false
}

// rebasePath returns the request path with its /w/{old}/ prefix replaced,
// preserving the query string.
func rebasePath(r *http.Request, prefix string) string {
	// PathValue("ws") is the segment as routed; the raw path may escape
	// differently, so rebuild from segments rather than string surgery.
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 3)
	target := prefix
	if len(parts) > 2 {
		target += "/" + parts[2]
	}
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	return target
}

// home redirects the bare root to the default workspace's issue list —
// or, when 'default' was deleted, the first workspace's; an entirely
// workspace-less tracker goes to the management page instead.
func (h *handler) home(w http.ResponseWriter, r *http.Request) {
	ws, err := h.defaultWorkspace(r)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if ws == nil {
		http.Redirect(w, r, "/workspaces", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/w/"+ws.Slug+"/issues", http.StatusFound)
}

// defaultWorkspace returns 'default' when present, else the first
// workspace by slug, else nil.
func (h *handler) defaultWorkspace(r *http.Request) (*store.Workspace, error) {
	ws, err := h.store.WorkspaceBySlug(r.Context(), store.DefaultWorkspaceSlug)
	if err == nil {
		return ws, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	list, err := h.store.ListWorkspaces(r.Context())
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return list[0], nil
}

// legacyCollection redirects /issues and /discussions (the workspace-less
// collection URLs) to the same collection in the default workspace.
// GET/HEAD get a 302; other methods a 307 so a POST keeps its method and
// body and lands on the canonical handler, which still PRGs afterwards.
func (h *handler) legacyCollection(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws, err := h.defaultWorkspace(r)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		if ws == nil {
			h.notFound(w, r)
			return
		}
		h.legacyRedirect(w, r, "/w/"+ws.Slug+"/"+kind)
	}
}

// legacySub redirects /issues/{rest...} and /discussions/{rest...}: the
// "new" collection member goes to the default workspace, everything else
// carries {id} and goes to that resource's real workspace.
func (h *handler) legacySub(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rest := r.PathValue("rest")
		first, _, _ := strings.Cut(rest, "/")
		var slug string
		switch {
		case first == "" || first == "new":
			ws, err := h.defaultWorkspace(r)
			if err != nil {
				h.internalError(w, r, err)
				return
			}
			if ws == nil {
				h.notFound(w, r)
				return
			}
			slug = ws.Slug
		default:
			id, err := strconv.ParseInt(first, 10, 64)
			if err != nil || id < 1 {
				h.notFound(w, r)
				return
			}
			ws, err := h.resourceWorkspace(r, kind, id)
			if err != nil {
				h.workspaceError(w, r, err)
				return
			}
			slug = ws.Slug
		}
		h.legacyRedirect(w, r, "/w/"+slug+"/"+kind+"/"+rest)
	}
}

// resourceWorkspace resolves the workspace of issue/discussion id.
func (h *handler) resourceWorkspace(r *http.Request, kind string, id int64) (*store.Workspace, error) {
	var wsID int64
	if kind == "issues" {
		i, err := h.store.GetIssue(r.Context(), id)
		if err != nil {
			return nil, err
		}
		wsID = i.WorkspaceID
	} else {
		d, err := h.store.GetDiscussion(r.Context(), id)
		if err != nil {
			return nil, err
		}
		wsID = d.WorkspaceID
	}
	return h.store.WorkspaceByID(r.Context(), wsID)
}

func (h *handler) legacyRedirect(w http.ResponseWriter, r *http.Request, target string) {
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	code := http.StatusFound
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		code = http.StatusTemporaryRedirect
	}
	http.Redirect(w, r, target, code)
}

// workspaceError maps store.ErrNotFound to the 404 page.
func (h *handler) workspaceError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	h.internalError(w, r, err)
}

// ---- management pages ----

// workspaceListPage is the view model for GET /workspaces.
type workspaceListPage struct {
	Title      string
	Workspaces []*store.Workspace
	Workspace  *store.Workspace // nav scope: the default workspace
	Section    string
	User       *store.User
}

// workspaceFormPage is the view model for the new-workspace form.
type workspaceFormPage struct {
	Title       string
	Heading     string
	Action      string
	SubmitLabel string
	Input       store.WorkspaceInput
	Errors      workspaceFormErrors
	Workspace   *store.Workspace
	Section     string
	User        *store.User
}

// workspaceFormErrors carries per-field validation messages.
type workspaceFormErrors struct {
	Slug        string
	Name        string
	Description string
}

func (e workspaceFormErrors) any() bool {
	return e.Slug != "" || e.Name != "" || e.Description != ""
}

// workspaceDeletePage is the view model for the delete confirmation page.
type workspaceDeletePage struct {
	Title     string
	Workspace *store.Workspace
	Section   string
	User      *store.User
}

// listWorkspaces renders GET /workspaces — the management index.
func (h *handler) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	wss, err := h.store.ListWorkspaces(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	var nav *store.Workspace
	for _, ws := range wss {
		if ws.Slug == store.DefaultWorkspaceSlug {
			nav = ws
		}
	}
	if nav == nil && len(wss) > 0 {
		nav = wss[0]
	}
	h.r.renderWorkspaceList(w, http.StatusOK, workspaceListPage{
		Title:      "Workspaces",
		Workspaces: wss,
		Workspace:  nav,
		Section:    "workspaces",
		User:       userFromContext(r.Context()),
	})
}

// newWorkspace renders the empty create form.
func (h *handler) newWorkspace(w http.ResponseWriter, r *http.Request) {
	h.r.renderWorkspaceForm(w, http.StatusOK, workspaceFormPage{
		Title:       "New workspace",
		Heading:     "New workspace",
		Action:      "/workspaces",
		SubmitLabel: "Create workspace",
		Section:     "workspaces",
		User:        userFromContext(r.Context()),
	})
}

// createWorkspace validates POST /workspaces and redirects to the new
// workspace's issue list.
func (h *handler) createWorkspace(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r) {
		return
	}
	in, errs := validateWorkspaceInput(r)
	if errs.any() {
		h.renderWorkspaceFormError(w, r, in, errs)
		return
	}
	if _, err := h.store.CreateWorkspace(r.Context(), in); err != nil {
		if errors.Is(err, store.ErrDuplicateSlug) {
			errs.Slug = "This slug is already taken."
			h.renderWorkspaceFormError(w, r, in, errs)
			return
		}
		h.internalError(w, r, err)
		return
	}
	http.Redirect(w, r, "/w/"+in.Slug+"/issues", http.StatusSeeOther)
}

// confirmDeleteWorkspace renders the delete confirmation page.
func (h *handler) confirmDeleteWorkspace(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	h.r.renderWorkspaceDelete(w, http.StatusOK, workspaceDeletePage{
		Title:     fmt.Sprintf("Delete workspace %s", ws.Slug),
		Workspace: ws,
		Section:   "workspaces",
		User:      userFromContext(r.Context()),
	})
}

// deleteWorkspace performs the deletion — contents included — and returns
// to the workspace index. The last remaining workspace refuses.
func (h *handler) deleteWorkspace(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	if !h.parseForm(w, r) {
		return
	}
	if err := h.store.DeleteWorkspace(r.Context(), ws.Slug); err != nil {
		switch {
		case errors.Is(err, store.ErrLastWorkspace):
			h.renderError(w, r, http.StatusUnprocessableEntity, "Cannot delete workspace",
				"The last remaining workspace cannot be deleted.")
		default:
			h.workspaceError(w, r, err)
		}
		return
	}
	http.Redirect(w, r, "/workspaces", http.StatusSeeOther)
}

func (h *handler) renderWorkspaceFormError(w http.ResponseWriter, r *http.Request, in store.WorkspaceInput, errs workspaceFormErrors) {
	h.r.renderWorkspaceForm(w, http.StatusUnprocessableEntity, workspaceFormPage{
		Title:       "New workspace",
		Heading:     "New workspace",
		Action:      "/workspaces",
		SubmitLabel: "Create workspace",
		Input:       in,
		Errors:      errs,
		Section:     "workspaces",
		User:        userFromContext(r.Context()),
	})
}

// validateWorkspaceInput extracts and validates the workspace form fields.
func validateWorkspaceInput(r *http.Request) (store.WorkspaceInput, workspaceFormErrors) {
	in := store.WorkspaceInput{
		Slug:        strings.TrimSpace(r.PostFormValue("slug")),
		Name:        strings.TrimSpace(r.PostFormValue("name")),
		Description: strings.TrimSpace(r.PostFormValue("description")),
	}
	fe := store.ValidateWorkspaceInput(in)
	return in, workspaceFormErrors{Slug: fe["slug"], Name: fe["name"], Description: fe["description"]}
}
