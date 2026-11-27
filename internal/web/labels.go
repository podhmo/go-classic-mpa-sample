// Label management routes under /w/{ws}/labels. Labels live in two
// scopes — shared master rows (workspace_id NULL) and workspace rows —
// and two kinds (issue/discussion); this page administers both. Deleting
// archives the row: it disappears from pickers and filters but stays
// attached to the issues/discussions that already carry it.
package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// labelsPage is the view model for GET /w/{ws}/labels. Input and Errors
// carry a failed create submission back into the form.
type labelsPage struct {
	Title            string
	IssueLabels      []*store.Label
	DiscussionLabels []*store.Label
	ArchivedLabels   []*store.Label
	Input            store.LabelInput
	Kind             string
	Scope            string
	Errors           labelFormErrors
	Section          string
	Workspace        *store.Workspace
	User             *store.User
}

// labelFormPage is the view model for the label edit form. Kind and scope
// are immutable — the label carries them for display only.
type labelFormPage struct {
	Title     string
	Heading   string
	Action    string
	Label     *store.Label
	Input     store.LabelInput
	Errors    labelFormErrors
	Section   string
	Workspace *store.Workspace
	User      *store.User
}

// labelFormErrors carries per-field validation messages for label forms.
type labelFormErrors struct {
	Name  string
	Color string
	Other string // cross-field problems, e.g. a duplicate name
}

func (e labelFormErrors) any() bool { return e.Name != "" || e.Color != "" || e.Other != "" }

// listLabels renders the management page: active labels grouped by kind
// plus the archived ones.
func (h *handler) listLabels(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	h.renderLabels(w, r, ws, http.StatusOK, labelsPage{Title: msgFor(r).LabelsTitle, Section: "labels"})
}

// renderLabels loads the three label groups and renders the page with
// the given status; data supplies the create form's draft on error
// re-renders.
func (h *handler) renderLabels(w http.ResponseWriter, r *http.Request, ws *store.Workspace, status int, data labelsPage) {
	var err error
	data.IssueLabels, err = h.store.ListLabels(r.Context(), ws.ID, store.KindLabelIssue)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	data.DiscussionLabels, err = h.store.ListLabels(r.Context(), ws.ID, store.KindLabelDiscussion)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	archived, err := h.store.ListArchivedLabels(r.Context(), ws.ID, store.KindLabelIssue)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	archivedD, err := h.store.ListArchivedLabels(r.Context(), ws.ID, store.KindLabelDiscussion)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	data.ArchivedLabels = append(archived, archivedD...)
	if data.Title == "" {
		data.Title = msgFor(r).LabelsTitle
	}
	data.Section = "labels"
	data.Workspace = ws
	data.User = userFromContext(r.Context())
	h.r.renderLabels(w, status, data, localeFromContext(r.Context()))
}

// createLabel validates POST /w/{ws}/labels and redirects back to the
// list. Fields: name, color, kind (issue|discussion), scope
// (workspace|shared).
func (h *handler) createLabel(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	if !h.parseForm(w, r) {
		return
	}
	in := store.LabelInput{
		Name:  strings.TrimSpace(r.PostFormValue("name")),
		Color: strings.TrimSpace(r.PostFormValue("color")),
	}
	kind := r.PostFormValue("kind")
	scope := r.PostFormValue("scope")

	errs := labelFormErrors{}
	if kind != store.KindLabelIssue && kind != store.KindLabelDiscussion {
		errs.Other = msgFor(r).ErrLabelKind
	}
	if scope != "workspace" && scope != "shared" {
		scope = "workspace"
	}
	fe := store.ValidateLabelInput(in)
	lang := localeFromContext(r.Context())
	errs.Name = fieldErrText(lang, "name", fe["name"])
	errs.Color = fieldErrText(lang, "color", fe["color"])

	if !errs.any() {
		wsID := ws.ID
		if scope == "shared" {
			wsID = 0
		}
		_, err := h.store.CreateLabel(r.Context(), wsID, kind, in)
		switch {
		case err == nil:
			http.Redirect(w, r, "/w/"+ws.Slug+"/labels", http.StatusSeeOther)
			return
		case errors.Is(err, store.ErrDuplicateLabel):
			errs.Other = msgFor(r).ErrLabelDup
		default:
			h.internalError(w, r, err)
			return
		}
	}
	h.renderLabels(w, r, ws, http.StatusUnprocessableEntity, labelsPage{
		Input:  in,
		Kind:   kind,
		Scope:  scope,
		Errors: errs,
	})
}

// labelInScope reports whether the label is manageable from ws: master
// labels anywhere, workspace labels only in their own workspace.
func labelInScope(l *store.Label, ws *store.Workspace) bool {
	return l.WorkspaceID == 0 || l.WorkspaceID == ws.ID
}

// editLabel renders the edit form for one label (master or this
// workspace's); another workspace's label is a 404.
func (h *handler) editLabel(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	l, err := h.store.GetLabel(r.Context(), id)
	if err != nil {
		h.issueError(w, r, err)
		return
	}
	if !labelInScope(l, ws) {
		h.notFound(w, r)
		return
	}
	h.r.renderLabelForm(w, http.StatusOK, labelFormPage{
		Title:     fmt.Sprintf(msgFor(r).EditLabel, l.Name),
		Heading:   fmt.Sprintf(msgFor(r).EditLabel, l.Name),
		Action:    fmt.Sprintf("/w/%s/labels/%d/edit", ws.Slug, id),
		Label:     l,
		Input:     store.LabelInput{Name: l.Name, Color: l.Color, ExpectedVersion: l.Version},
		Section:   "labels",
		Workspace: ws,
		User:      userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}

// updateLabel validates POST /w/{ws}/labels/{id}/edit and redirects to the
// list. Only name and color change.
func (h *handler) updateLabel(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	l, err := h.store.GetLabel(r.Context(), id)
	if err != nil {
		h.issueError(w, r, err)
		return
	}
	if !labelInScope(l, ws) {
		h.notFound(w, r)
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	in := store.LabelInput{
		Name:            strings.TrimSpace(r.PostFormValue("name")),
		Color:           strings.TrimSpace(r.PostFormValue("color")),
		ExpectedVersion: formVersion(r),
	}
	fe := store.ValidateLabelInput(in)
	lang := localeFromContext(r.Context())
	errs := labelFormErrors{Name: fieldErrText(lang, "name", fe["name"]), Color: fieldErrText(lang, "color", fe["color"])}
	if !errs.any() {
		err := h.store.UpdateLabel(r.Context(), id, in)
		switch {
		case err == nil:
			http.Redirect(w, r, "/w/"+ws.Slug+"/labels", http.StatusSeeOther)
			return
		case errors.Is(err, store.ErrDuplicateLabel):
			errs.Other = msgFor(r).ErrLabelDup
		default:
			h.issueError(w, r, err)
			return
		}
	}
	h.r.renderLabelForm(w, http.StatusUnprocessableEntity, labelFormPage{
		Title:     fmt.Sprintf(msgFor(r).EditLabel, l.Name),
		Heading:   fmt.Sprintf(msgFor(r).EditLabel, l.Name),
		Action:    fmt.Sprintf("/w/%s/labels/%d/edit", ws.Slug, id),
		Label:     l,
		Input:     in,
		Errors:    errs,
		Section:   "labels",
		Workspace: ws,
		User:      userFromContext(r.Context()),
	}, localeFromContext(r.Context()))

}

// archiveLabel handles POST /w/{ws}/labels/{id}/archive — the "delete"
// button submits archived=1, the archived section's restore button
// archived=0.
func (h *handler) archiveLabel(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	l, err := h.store.GetLabel(r.Context(), id)
	if err != nil {
		h.issueError(w, r, err)
		return
	}
	if !labelInScope(l, ws) {
		h.notFound(w, r)
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	var archived bool
	switch r.PostFormValue("archived") {
	case "1":
		archived = true
	case "0":
	default:
		h.renderError(w, r, http.StatusBadRequest, msgFor(r).ErrBadRequestH, msgFor(r).ErrArchivedValue)
		return
	}
	if err := h.store.ArchiveLabel(r.Context(), id, archived); err != nil {
		h.issueError(w, r, err)
		return
	}
	http.Redirect(w, r, "/w/"+ws.Slug+"/labels", http.StatusSeeOther)
}
