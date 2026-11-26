package web

import (
	"bufio"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/events"
)

// nextLine reads one SSE line off the stream with a deadline so a silent
// server fails the test instead of hanging it.
func nextLine(t *testing.T, lines <-chan string, errs <-chan error) string {
	t.Helper()
	select {
	case l := <-lines:
		return l
	case err := <-errs:
		t.Fatalf("stream error: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for an SSE line")
	}
	return ""
}

func TestIssueEventsStreamsPublishedChanges(t *testing.T) {
	st := newFakeStore()
	issue := st.addIssue("live", "body", "open", time.Now())
	hub := events.NewHub()
	srv := httptest.NewServer(New(st, WithHub(hub)))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/" + itoa(issue.ID) + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}

	lines := make(chan string)
	errs := make(chan error, 1)
	go func() {
		r := bufio.NewReader(resp.Body)
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				errs <- err
				return
			}
			lines <- strings.TrimRight(l, "\n")
		}
	}()

	if got := nextLine(t, lines, errs); got != "retry: 3000" {
		t.Fatalf("first frame = %q, want retry directive", got)
	}
	nextLine(t, lines, errs) // blank line ends the frame

	hub.Publish(events.Change{Topic: events.IssueTopic(issue.ID), Kind: events.KindComment, Target: 42})
	if got := nextLine(t, lines, errs); got != "id: 1" {
		t.Fatalf("id line = %q", got)
	}
	if got := nextLine(t, lines, errs); got != "event: comment" {
		t.Fatalf("event line = %q", got)
	}
	data := nextLine(t, lines, errs)
	if !strings.HasPrefix(data, "data: ") || !strings.Contains(data, `"target":42`) {
		t.Fatalf("data line = %q", data)
	}
}

func TestIssueEventsNotFound(t *testing.T) {
	srv := httptest.NewServer(New(newFakeStore()))
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/999/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestIssueCommentsFragment(t *testing.T) {
	st := newFakeStore()
	issue := st.addIssue("frag", "body", "open", time.Now())
	if _, err := st.CreateComment(t.Context(), issue.ID, "first", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateComment(t.Context(), issue.ID, "second", 0); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(st))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/" + itoa(issue.ID) + "/fragments/comments?ids=2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `id="issue-meta"`) {
		t.Fatal("meta swap node missing")
	}
	if !strings.Contains(string(body), `id="comment-2"`) {
		t.Fatal("comment-2 missing")
	}
	if strings.Contains(string(body), `id="comment-1"`) {
		t.Fatal("comment-1 should be filtered out by ?ids=2")
	}

	// No ids = the resync path: the whole list must come back.
	resp2, err := srv.Client().Get(srv.URL + "/w/default/issues/" + itoa(issue.ID) + "/fragments/comments")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	for _, want := range []string{`id="comment-1"`, `id="comment-2"`} {
		if !strings.Contains(string(body2), want) {
			t.Fatalf("resync fragment missing %s", want)
		}
	}

	// A present but empty ids parameter is an explicit empty selection.
	resp3, err := srv.Client().Get(srv.URL + "/w/default/issues/" + itoa(issue.ID) + "/fragments/comments?ids=")
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	body3, _ := io.ReadAll(resp3.Body)
	if strings.Contains(string(body3), `id="comment-`) {
		t.Fatal("empty ids should filter every comment out")
	}
}
