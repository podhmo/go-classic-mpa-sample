package main

import (
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"

	"github.com/podhmo/minigo"
	"github.com/podhmo/minigo/resolve"
)

// appStory is the handler-shaped story that routes a request through the
// app's real mux (internal/web/story_pages.go: StoryHttpApp → New(storyStore{})).
const appStory = "StoryHttpApp"

// appEngine lazily builds a second base engine for the app mount. It
// can't share the Book's engine (storybook.Book.base is unexported), so
// the same build options and Setup hook are applied here; the two
// engines coexist, each forking a fresh session per request.
var appState struct {
	once sync.Once
	eng  *minigo.Engine
	dir  string
	err  error
}

func (b *Book) appEngine() (*minigo.Engine, string, error) {
	appState.once.Do(func() {
		dir, err := filepath.Abs(b.Dir)
		if err != nil {
			appState.err = err
			return
		}
		opts := []minigo.Option{minigo.WithBuildConfig(resolve.BuildConfig{Tags: []string{b.Tag}})}
		if len(b.Src) > 0 {
			modes := map[string]minigo.PackageMode{}
			for _, p := range b.Src {
				modes[p] = minigo.ModeSource
			}
			opts = append(opts, minigo.WithPackageModes(modes))
		}
		appState.eng = minigo.NewEngine(dir, opts...)
		appState.dir = dir
		if b.Setup != nil {
			appState.err = b.Setup(appState.eng)
		}
	})
	return appState.eng, appState.dir, appState.err
}

// appHandler is the catch-all mounted at "/": every request not claimed
// by the storybook chrome (index, /story, /grid, /static, live reload)
// goes through the app's real mux via the StoryHttpApp story. That makes
// the storybook a fake-data instance of the whole app — enough for
// browser JavaScript like partial-submit.js's fetch → fragment → DOM
// swap to run end to end, which a single-story render cannot exercise
// (the fetch needs a live endpoint to hit).
func (b *Book) appHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		eng, dir, err := b.appEngine()
		if err != nil {
			b.errorPage(w, http.StatusInternalServerError, err)
			return
		}
		s := eng.NewSession()
		pkg, err := s.Package(r.Context(), dir)
		if err != nil {
			b.errorPage(w, http.StatusInternalServerError, err)
			return
		}
		rec := httptest.NewRecorder()
		if _, err := s.Call(r.Context(), pkg, appStory, rec, r); err != nil {
			b.errorPage(w, http.StatusInternalServerError, fmt.Errorf("%s: %w", appStory, err))
			return
		}
		res := rec.Result()
		h := w.Header()
		for k, vs := range res.Header {
			h[k] = vs
		}
		if rec.Body.Len() > 0 && h.Get("Content-Type") == "" {
			h.Set("Content-Type", "text/html; charset=utf-8")
		}
		// Live reload on app pages too: source and template edits
		// re-render on the next request, so watching pages should
		// refresh themselves like the story pages do.
		body := b.AppendLiveReload(h, rec.Body.Bytes())
		w.WriteHeader(res.StatusCode)
		w.Write(body)
	})
}

// errorPage mirrors the library's error page: an HTML page carrying the
// reload script while watching — a page that loses the script never
// rejoins the reload loop — and plain text otherwise.
func (b *Book) errorPage(w http.ResponseWriter, status int, err error) {
	if tag := b.ScriptTag(); tag != "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		fmt.Fprintf(w, "<pre>%s</pre>%s", template.HTMLEscapeString(err.Error()), tag)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprint(w, err)
}
