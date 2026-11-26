package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

func TestClientPullViewAndStatus(t *testing.T) {
	var mu sync.Mutex
	var statusBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/pulls/7":
			io.WriteString(w, `{
				"pull_request":{"id":7,"title":"Improve cache","body":"body","status":"closed","base":"refs/heads/main","head":"refs/heads/topic","head_sha":"headsha","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-03T03:04:05Z","closed_at":"2026-01-04T03:04:05Z"},
				"comments":[{"id":9,"body":"review","path":"a.go","line":12,"side":"right","anchor_sha":"anchor","diff_snippet":"@@","created_at":"2026-01-02T04:04:05Z"}],
				"commits":[{"sha":"abcdef","subject":"topic","author":"demo","date":"2026-01-02T03:04:05Z"}],
				"mergeable":{"status":"conflicting","files":["a.go"]}
			}`)
		case "/api/v1/pulls/7/status":
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			statusBody = string(raw)
			mu.Unlock()
			io.WriteString(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")

	v, err := c.PullView(context.Background(), 7)
	if err != nil {
		t.Fatalf("PullView: %v", err)
	}
	if v.PR.ID != 7 || v.PR.Status != store.PRStatusClosed || v.PR.ClosedAt == nil {
		t.Fatalf("PR mapping = %+v", v.PR)
	}
	if len(v.Comments) != 1 || !v.Comments[0].Anchored() || v.Comments[0].Line != 12 {
		t.Fatalf("comments = %+v", v.Comments)
	}
	if len(v.Commits) != 1 || v.Mergeable == nil || v.Mergeable.Status != "conflicting" || v.Mergeable.Files[0] != "a.go" {
		t.Fatalf("commits/mergeable = %+v %+v", v.Commits, v.Mergeable)
	}

	if err := c.ClosePullRequest(context.Background(), 7, "ignored", "ignored"); err != nil {
		t.Fatalf("ClosePullRequest: %v", err)
	}
	mu.Lock()
	body := statusBody
	mu.Unlock()
	if !strings.Contains(body, `"status":"closed"`) {
		t.Fatalf("status body = %q", body)
	}
}

func TestClientPullTextEndpointsAndErrorMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/pulls/7/diff":
			w.Header().Set("X-Truncated", "true")
			io.WriteString(w, "diff --git a/a b/a\n")
		case "/api/v1/pulls/7/markdown":
			io.WriteString(w, "# markdown\n")
		case "/api/v1/pulls/8/diff":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":{"message":"Not found."}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")

	diff, truncated, err := c.PullDiffWithTruncation(context.Background(), 7)
	if err != nil || diff != "diff --git a/a b/a\n" || !truncated {
		t.Fatalf("diff = %q truncated=%v err=%v", diff, truncated, err)
	}
	markdown, err := c.PullMarkdown(context.Background(), 7)
	if err != nil || markdown != "# markdown\n" {
		t.Fatalf("markdown = %q err=%v", markdown, err)
	}
	_, err = c.PullDiff(context.Background(), 8)
	ae, ok := err.(*Error)
	if !ok || ae.Status != http.StatusNotFound || ae.Message != "Not found." {
		t.Fatalf("error = %#v", err)
	}
}
