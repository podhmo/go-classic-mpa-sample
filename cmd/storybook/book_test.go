package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/podhmo/minigo/storybook"
)

const webDir = "../../internal/web"

func newBook() *Book {
	return &Book{Book: &storybook.Book{Dir: webDir, Tag: "story", Setup: bindApp}}
}

// The tests below assert properties of the whole discovered corpus, not
// a hardcoded story list: adding a story or a handler never requires
// updating this file — the corpus is the spec.
func TestStories(t *testing.T) {
	stories, err := newBook().Stories()
	if err != nil {
		t.Fatal(err)
	}
	var nPrint, nHTTP int
	groups := map[string]int{}
	for _, s := range stories {
		if s.Doc == "" || s.Pos == "" {
			t.Errorf("missing doc or position: %+v", s)
		}
		if s.IsHTTP() {
			nHTTP++
			if s.Group != "" {
				t.Errorf("%s: handler story must not group, got %q", s.Name, s.Group)
			}
		} else {
			nPrint++
			if s.Group == "" {
				t.Errorf("%s: print story has no group", s.Name)
			}
			groups[s.Group]++
		}
	}
	if nPrint == 0 || nHTTP == 0 {
		t.Errorf("want both story kinds, got %d print + %d http", nPrint, nHTTP)
	}
	// Grouping must discriminate: a single group holding every print
	// story is the degenerate "all under the last helper" outcome.
	if len(groups) < 2 && nPrint > 1 {
		t.Errorf("print stories collapse into groups %v", groups)
	}
}

func TestRender(t *testing.T) {
	b := newBook()
	stories, err := b.Stories()
	if err != nil {
		t.Fatal(err)
	}
	sawNon200 := false
	for _, s := range stories {
		t.Run(s.Name, func(t *testing.T) {
			page, err := b.Render(context.Background(), s.Name)
			if err != nil {
				t.Fatal(err)
			}
			// Print stories are pages — except the *Fragment ones, which
			// print a partial-update fragment by design. HTTP stories
			// legitimately answer any shape (303, fragment, page).
			if !s.IsHTTP() && !strings.HasSuffix(s.Name, "Fragment") && !strings.Contains(string(page.Body), "<html") {
				t.Errorf("%s: body misses the page markup", s.Name)
			}
			if page.Status >= 500 {
				t.Errorf("%s: status %d", s.Name, page.Status)
			}
			if s.IsHTTP() {
				if page.Status != http.StatusOK {
					sawNon200 = true
				}
				if got := page.Header["Content-Security-Policy"]; len(got) == 0 || !strings.Contains(got[0], "script-src 'self'") {
					t.Errorf("%s: CSP misses script-src 'self': %v", s.Name, got)
				}
			}
		})
	}
	// Status replay is only proven when the corpus has a non-200 case.
	if !sawNon200 {
		t.Error("no http story replays a non-200 status")
	}
}

func TestRenderRejectsNonStory(t *testing.T) {
	if _, err := newBook().Render(context.Background(), "StoryMissing"); err == nil || !strings.Contains(err.Error(), "not a story") {
		t.Fatalf("want non-story error, got %v", err)
	}
}

func TestHandler(t *testing.T) {
	b := newBook()
	stories, err := b.Stories()
	if err != nil {
		t.Fatal(err)
	}
	members := map[string][]string{}
	var aPrint string
	var httpStories []Story
	for _, s := range stories {
		if s.IsHTTP() {
			httpStories = append(httpStories, s)
			continue
		}
		if aPrint == "" {
			aPrint = s.Name
		}
		members[s.Group] = append(members[s.Group], s.Name)
	}
	var grid string
	for g := range members {
		if len(members[g]) > len(members[grid]) {
			grid = g
		}
	}

	h := b.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`href="?s=` + aPrint + `"`,
		`target="_blank"`,               // http stories open in a new tab
		`?w=360`,                        // viewport widths
		`href="?g=` + grid + `"`,        // a grid pill per render function
		`data-src="/grid/` + grid + `"`, // ...carrying the grid document
		`data-src="/story/` + aPrint + `"`,
		`type="search"`, // the story filter
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index misses %s", want)
		}
	}

	// ?g previews the grid inside the index frame; ?q filters the nav
	// down to matching stories.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?g="+grid, nil))
	if !strings.Contains(rec.Body.String(), `src="/grid/`+grid+`"`) {
		t.Error("?g should preview the grid in the index frame")
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?q="+url.QueryEscape(aPrint+" nomatch"), nil))
	if !strings.Contains(rec.Body.String(), "no stories match") {
		t.Error("a filter matching nothing should say so")
	}
	other := ""
	for _, s := range stories {
		if !s.IsHTTP() && s.Name != aPrint && !matchStory(s, aPrint) {
			other = s.Name
			break
		}
	}
	if other != "" {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?q="+aPrint, nil))
		body = rec.Body.String()
		if !strings.Contains(body, `data-name="`+aPrint+`"`) {
			t.Errorf("filtered index drops %s", aPrint)
		}
		// Non-matching rows stay in the DOM (hidden) so the client-side
		// filter can restore them when the query changes.
		i := strings.Index(body, `data-name="`+other+`"`)
		if i < 0 {
			t.Errorf("filtered index omits non-matching %s", other)
		} else {
			end := strings.Index(body[i:], ">")
			if end < 0 || !strings.HasSuffix(body[i:i+end], " hidden") {
				t.Errorf("non-matching %s is not marked hidden", other)
			}
		}
	}

	// The grid lays one render function's variants side by side and
	// leaks no member of another group.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/grid/"+grid, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("grid status %d, want 200", rec.Code)
	}
	for _, name := range members[grid] {
		if !strings.Contains(rec.Body.String(), `src="/story/`+name+`"`) {
			t.Errorf("grid misses %s", name)
		}
	}
	for g, names := range members {
		if g == grid {
			continue
		}
		for _, name := range names {
			if strings.Contains(rec.Body.String(), `/story/`+name) {
				t.Errorf("grid leaks %s of group %s", name, g)
			}
		}
	}

	// Every handler story replays the status its recorder captured.
	for _, s := range httpStories {
		want, err := b.Render(context.Background(), s.Name)
		if err != nil {
			t.Fatal(err)
		}
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/story/"+s.Name, nil))
		if rec.Code != want.Status {
			t.Errorf("preview status for %s %d, want %d", s.Name, rec.Code, want.Status)
		}
	}

	// The app's real assets are served at their original path.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/app.css", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("static status %d, want 200", rec.Code)
	}
}

func TestExport(t *testing.T) {
	out := t.TempDir()
	b := newBook()
	stories, err := b.Stories()
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Export(context.Background(), out, stories); err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stories {
		if !strings.Contains(string(index), `href="`+s.Name+`.html"`) {
			t.Errorf("index misses %s", s.Name)
		}
		if _, err := os.Stat(filepath.Join(out, s.Name+".html")); err != nil {
			t.Errorf("export misses %s: %v", s.Name, err)
		}
	}
	// The snapshot ships the app's assets and one grid per render
	// function, so serving the export root mirrors the live preview.
	if _, err := os.Stat(filepath.Join(out, "static", "app.css")); err != nil {
		t.Errorf("export misses static assets: %v", err)
	}
	grids := map[string]bool{}
	for _, s := range stories {
		if s.Group != "" {
			grids[s.Group] = true
		}
	}
	for g := range grids {
		if _, err := os.Stat(filepath.Join(out, "grid_"+g+".html")); err != nil {
			t.Errorf("export misses the variants grid for %s: %v", g, err)
		}
	}
}

// staticStories builds a minimal app with one story and one static asset.
func staticStories(t *testing.T) (string, []Story) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/staticx\n\ngo 1.26.0\n")
	writeFile(t, filepath.Join(dir, "app.go"), "package app\n")
	writeFile(t, filepath.Join(dir, "story.go"), "//go:build story\n\npackage app\n\nfunc StoryX() {}\n")
	if err := os.MkdirAll(filepath.Join(dir, "static"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "static", "app.css"), "a{}\n")
	stories, err := (&Book{Book: &storybook.Book{Dir: dir, Tag: "story"}}).Stories()
	if err != nil {
		t.Fatal(err)
	}
	return dir, stories
}

// Exporting into the package directory itself must refuse, not delete
// the application's real assets — a relative -o used to slip the
// dst == src containment check because src is absolute.
func TestExportRejectsOutputCoveringStaticSource(t *testing.T) {
	dir, stories := staticStories(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wd, dir)
	if err != nil {
		t.Fatal(err)
	}
	b := &Book{Book: &storybook.Book{Dir: dir, Tag: "story"}}
	if err := b.Export(context.Background(), rel, stories); err == nil {
		t.Fatal("export covering the static source must fail")
	}
	if _, err := os.Stat(filepath.Join(dir, "static", "app.css")); err != nil {
		t.Errorf("export deleted the real assets: %v", err)
	}
}

// A `static` that is a regular file is not a mirror source, but the
// unconditional cleanup must still not delete it when -o covers it.
func TestExportRejectsFileStaticSource(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/filesrc\n\ngo 1.26.0\n")
	writeFile(t, filepath.Join(dir, "app.go"), "package app\n")
	writeFile(t, filepath.Join(dir, "story.go"), "//go:build story\n\npackage app\n\nfunc StoryX() {}\n")
	writeFile(t, filepath.Join(dir, "static"), "not a directory\n")
	b := &Book{Book: &storybook.Book{Dir: dir, Tag: "story"}}
	stories, err := b.Stories()
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Export(context.Background(), dir, stories); err == nil {
		t.Error("export covering a file static source must fail")
	}
	got, err := os.ReadFile(filepath.Join(dir, "static"))
	if err != nil {
		t.Fatalf("export deleted the file: %v", err)
	}
	if string(got) != "not a directory\n" {
		t.Errorf("file contents changed: %q", got)
	}
}

// When the source static dir disappears between exports, the stale
// mirror must disappear too.
func TestExportDropsStaticWhenSourceGone(t *testing.T) {
	dir, stories := staticStories(t)
	b := &Book{Book: &storybook.Book{Dir: dir, Tag: "story"}}
	out := filepath.Join(t.TempDir(), "out")
	if err := b.Export(context.Background(), out, stories); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "static", "app.css")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "static")); err != nil {
		t.Fatal(err)
	}
	if err := b.Export(context.Background(), out, stories); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "static")); !os.IsNotExist(err) {
		t.Errorf("stale static mirror survives a vanished source: %v", err)
	}
}

// -watch is a preview-server option: without -http it is an error.
func TestWatchRequiresHTTP(t *testing.T) {
	err := run(context.Background(), []string{"-watch", webDir}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "-watch requires -http") {
		t.Fatalf("want -watch requires -http, got %v", err)
	}
}

// Watching stamps the reload script on the preview pages — index, grid,
// and every HTML story body — and serves its endpoints; non-HTML bodies
// stay byte-identical. Error pages keep the script too: a page that
// loses it never rejoins the reload loop once the source is fixed.
func TestHandlerWatch(t *testing.T) {
	b := newBook()
	if err := b.Watch(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	stories, err := b.Stories()
	if err != nil {
		t.Fatal(err)
	}
	h := b.Handler()
	const tag = `<script src="/_livereload.js" defer></script>`

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), tag) {
		t.Error("index misses the reload script")
	}
	for _, s := range stories {
		if s.Group == "" {
			continue
		}
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/grid/"+s.Group, nil))
		if !strings.Contains(rec.Body.String(), tag) {
			t.Errorf("grid %s misses the reload script", s.Group)
		}
		break // one grid is enough; every grid shares gridTmpl
	}
	// A story gets the script iff its served response is HTML.
	for _, s := range stories {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/story/"+s.Name, nil))
		html := strings.Contains(rec.Header().Get("Content-Type"), "html")
		if html != strings.Contains(rec.Body.String(), "/_livereload.js") {
			t.Errorf("%s: Content-Type %q vs script presence mismatch", s.Name, rec.Header().Get("Content-Type"))
		}
		// When the story replays a CSP, it must not block the script's
		// EventSource: a policy with default-src but no connect-src
		// forbids it, so AppendLiveReload must add one (minigo#1081).
		// A policy without default-src leaves connections unrestricted
		// and is intentionally untouched.
		if csp := rec.Header().Get("Content-Security-Policy"); html &&
			strings.Contains(csp, "default-src") && !strings.Contains(csp, "connect-src") {
			t.Errorf("%s: CSP %q would block the reload EventSource", s.Name, csp)
		}
	}
	for _, target := range []string{"/story/StoryMissing", "/grid/Nope"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if !strings.Contains(rec.Body.String(), tag) {
			t.Errorf("%s: error page misses the reload script", target)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "html") {
			t.Errorf("%s: error page Content-Type %q", target, ct)
		}
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_livereload.js", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "EventSource") {
		t.Error("want the live reload script")
	}
}

// The catch-all mount routes every non-storybook path through the app's
// real mux via StoryHttpApp — the storybook doubles as a fake-data
// instance of the app, so browser JS has live endpoints to hit.
func TestAppMount(t *testing.T) {
	srv := httptest.NewServer(newBook().Handler())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index status %d, want 200", resp.StatusCode)
	}

	resp, err = srv.Client().Get(srv.URL + "/issues")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("app GET status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "<!DOCTYPE") || !strings.Contains(string(body), "Improve responsive layout") {
		t.Error("app mount should render the real issue list")
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "connect-src") {
		t.Errorf("app response should carry the app's CSP, got %q", csp)
	}

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	post := func(form string, headers map[string]string) (*http.Response, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/w/default/issues/2/comments", strings.NewReader(form))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := noRedirect.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return resp, string(body)
	}
	resp, body2 := post("body=via+fetch", map[string]string{"X-Partial": "1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("partial POST status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body2, `class="card comment"`) || strings.Contains(body2, "<!DOCTYPE") {
		t.Error("partial POST should answer the comment fragment")
	}
	resp, _ = post("body=classic", nil)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("plain POST status %d, want 303 (client must not follow)", resp.StatusCode)
	}
}

func writeFile(t *testing.T, path, src string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}
