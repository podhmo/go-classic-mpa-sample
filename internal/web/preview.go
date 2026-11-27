package web

import (
	"net/http"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// markdownPreview serves the HTML fragment used by the client-side
// Write/Preview tabs. It renders through the same goldmark pipeline as the
// persisted views, so the preview includes server-side issue references.
func (h *handler) markdownPreview(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	if !h.parseForm(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<div class="markdown-body">`))
	w.Write([]byte(renderMarkdownForWorkspace(ws, r.PostFormValue("body"))))
	w.Write([]byte(`</div>`))
}

func (h *handler) scopedMarkdownPreview(w http.ResponseWriter, r *http.Request) {
	h.markdownPreview(w, r, h.navWorkspace(r))
}
