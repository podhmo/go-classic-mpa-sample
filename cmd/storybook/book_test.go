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

var printNames = []string{
	"StoryError404",
	"StoryIssueDelete",
	"StoryIssueDetail",
	"StoryIssueDetailCommentError",
	"StoryIssueFormEdit",
	"StoryIssueFormErrors",
	"StoryIssueFormNew",
	"StoryIssueList",
	"StoryIssueListEmpty",
	"StoryIssueListPaged",
}

var httpNames = []string{
	"StoryHttpIssueList",
	"StoryHttpNotFound",
}

func newBook() *Book {
	return &Book{Book: &storybook.Book{Dir: webDir, Tag: "story", Setup: bindApp}}
}

func TestStories(t *testing.T) {
	stories, err := newBook().Stories()
	if err != nil {
		t.Fatal(err)
	}
	wantGroup := map[string]string{
		"StoryIssueList":               "renderIssueList",
		"StoryIssueListEmpty":          "renderIssueList",
		"StoryIssueListPaged":          "renderIssueList",
		"StoryIssueDetail":             "renderIssueDetail",
		"StoryIssueDetailCommentError": "renderIssueDetail",
		"StoryIssueFormNew":            "renderIssueForm",
		"StoryIssueFormErrors":         "renderIssueForm",
		"StoryIssueFormEdit":           "renderIssueForm",
		"StoryIssueDelete":             "renderIssueDelete",
		"StoryError404":                "renderError",
	}
	var names []string
	for _, s := range stories {
		names = append(names, s.Name)
		if s.Doc == "" || s.Pos == "" {
			t.Errorf("missing doc or position: %+v", s)
		}
		if want := strings.HasPrefix(s.Name, "StoryHttp"); s.IsHTTP() != want {
			t.Errorf("%s: IsHTTP %v, want %v", s.Name, s.IsHTTP(), want)
		}
		// Print stories group by the render function they drive;
		// handler stories stay ungrouped.
		if s.IsHTTP() {
			if s.Group != "" {
				t.Errorf("%s: handler story must not group, got %q", s.Name, s.Group)
			}
		} else if got, want := s.Group, wantGroup[s.Name]; got != want {
			t.Errorf("%s: group %q, want %q", s.Name, got, want)
		}
	}
	if len(names) != len(printNames)+len(httpNames) {
		t.Errorf("want %d stories, got %d: %v", len(printNames)+len(httpNames), len(names), names)
	}
}

func TestRender(t *testing.T) {
	b := newBook()
	for _, name := range append(append([]string{}, printNames...), httpNames...) {
		t.Run(name, func(t *testing.T) {
			page, err := b.Render(context.Background(), name)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(page.Body), "<html") {
				t.Errorf("%s: body misses the page markup", name)
			}
		})
	}
	// A handler story replays the status its recorder captured.
	page, err := b.Render(context.Background(), "StoryHttpNotFound")
	if err != nil {
		t.Fatal(err)
	}
	if page.Status != http.StatusNotFound {
		t.Errorf("StoryHttpNotFound: status %d, want 404", page.Status)
	}
	// And the app's real security headers.
	if got := page.Header["Content-Security-Policy"]; len(got) == 0 || !strings.Contains(got[0], "script-src 'none'") {
		t.Errorf("CSP misses script-src 'none': %v", got)
	}
}

func TestRenderRejectsNonStory(t *testing.T) {
	if _, err := newBook().Render(context.Background(), "StoryMissing"); err == nil || !strings.Contains(err.Error(), "not a story") {
		t.Fatalf("want non-story error, got %v", err)
	}
}

func TestHandler(t *testing.T) {
	h := newBook().Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`href="?s=StoryIssueList"`,
		`target="_blank"`,              // http stories open in a new tab
		`?w=360`,                       // viewport widths
		`href="/grid/renderIssueList"`, // a variants grid per render function
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index misses %s", want)
		}
	}

	// The grid lays one render function's variants side by side.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/grid/renderIssueList", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("grid status %d, want 200", rec.Code)
	}
	for _, name := range []string{"StoryIssueList", "StoryIssueListEmpty", "StoryIssueListPaged"} {
		if !strings.Contains(rec.Body.String(), `src="/story/`+name+`"`) {
			t.Errorf("grid misses %s", name)
		}
	}
	if strings.Contains(rec.Body.String(), "StoryIssueDetail") {
		t.Error("grid leaks another render function's variants")
	}

	// A handler story replays its recorded status and headers.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/story/StoryHttpNotFound", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("preview status for StoryHttpNotFound %d, want 404", rec.Code)
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
	if _, err := os.Stat(filepath.Join(out, "grid_renderIssueList.html")); err != nil {
		t.Errorf("export misses the variants grid: %v", err)
	}
}
