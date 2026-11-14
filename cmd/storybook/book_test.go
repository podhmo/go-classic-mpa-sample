package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
			// Handler stories stay ungrouped.
			if s.Group != "" {
				t.Errorf("%s: handler story must not group, got %q", s.Name, s.Group)
			}
		} else {
			nPrint++
			// Print stories group by the render function they drive.
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
			if !strings.Contains(string(page.Body), "<html") {
				t.Errorf("%s: body misses the page markup", s.Name)
			}
			if page.Status >= 500 {
				t.Errorf("%s: status %d", s.Name, page.Status)
			}
			if s.IsHTTP() {
				if page.Status != http.StatusOK {
					sawNon200 = true
				}
				// A handler story replays the app's real headers.
				if got := page.Header["Content-Security-Policy"]; len(got) == 0 || !strings.Contains(got[0], "script-src 'none'") {
					t.Errorf("%s: CSP misses script-src 'none': %v", s.Name, got)
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
	// The largest group fills the grid the most.
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
		`target="_blank"`,           // http stories open in a new tab
		`?w=360`,                    // viewport widths
		`href="/grid/` + grid + `"`, // a variants grid per render function
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index misses %s", want)
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

func writeFile(t *testing.T, path, src string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}
