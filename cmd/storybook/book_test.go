package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/podhmo/minigo/x/storybook"
)

const webDir = "../../internal/web"

func newBook() *Book {
	b := &Book{Book: &storybook.Book{Dir: webDir, Tag: "story", Setup: bindApp}}
	b.Fallback = b.appHandler()
	return b
}

func TestWatchRequiresHTTP(t *testing.T) {
	err := run(context.Background(), []string{"-watch", webDir}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "-watch requires -http") {
		t.Fatalf("want -watch requires -http, got %v", err)
	}
}

// TestRenderAllStories exercises the whole app story corpus through the
// interpreter. Discovery alone is not enough: fixture construction,
// markdown rendering, template execution and handler replay all fail at
// Render time when minigo cannot represent a value the app uses.
func TestRenderAllStories(t *testing.T) {
	b := newBook()
	stories, err := b.Stories()
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) == 0 {
		t.Fatal("no stories discovered")
	}
	for _, story := range stories {
		t.Run(story.Name, func(t *testing.T) {
			page, err := b.Render(t.Context(), story.Name)
			if err != nil {
				t.Fatal(err)
			}
			if page.Status < 100 || page.Status >= 500 {
				t.Fatalf("unexpected status %d", page.Status)
			}
		})
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
