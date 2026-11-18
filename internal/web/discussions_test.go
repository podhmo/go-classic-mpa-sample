package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func seedDiscussions(t *testing.T, f *fakeStore) {
	t.Helper()
	now := time.Now()
	f.addDiscussion("Welcome to the forum", "announcement body", 1, now.Add(-72*time.Hour))
	f.addDiscussion("How does triage work?", "qa body", 2, now.Add(-24*time.Hour))
}

func TestDiscussionListRenders(t *testing.T) {
	srv, f := newTestServer(t)
	seedDiscussions(t, f)

	resp, err := srv.Client().Get(srv.URL + "/w/default/discussions")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for _, want := range []string{"Welcome to the forum", "How does triage work?", "discussion-list", "Q&amp;A", "General"} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing %q", want)
		}
	}
	if !strings.Contains(body, `href="/w/default/discussions" aria-current="page"`) {
		t.Error("discussions nav tab should be aria-current")
	}
	if strings.Contains(body, `href="/w/default/issues" aria-current="page"`) {
		t.Error("issues nav tab must not be aria-current on /discussions")
	}
}

func TestDiscussionListCategoryFilter(t *testing.T) {
	srv, f := newTestServer(t)
	seedDiscussions(t, f)

	resp, err := srv.Client().Get(srv.URL + "/w/default/discussions?category=qa")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if !strings.Contains(body, "How does triage work?") {
		t.Error("qa discussion missing")
	}
	if strings.Contains(body, "Welcome to the forum") {
		t.Error("general discussion leaked into category=qa")
	}
}

func TestDiscussionListAnsweredFilter(t *testing.T) {
	srv, f := newTestServer(t)
	d := f.addDiscussion("Answered question", "b", 2, time.Now())
	f.addDiscussion("Open question", "b", 2, time.Now())

	cid, err := f.CreateDiscussionThread(t.Context(), d.ID, "the fix", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.SetThreadAnswer(t.Context(), d.ID, cid, true); err != nil {
		t.Fatal(err)
	}

	resp, err := srv.Client().Get(srv.URL + "/w/default/discussions?answered=yes")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if !strings.Contains(body, "Answered question") || strings.Contains(body, "Open question") {
		t.Error("answered=yes filter wrong")
	}
	if !strings.Contains(body, `badge-answered`) {
		t.Error("expected Answered badge")
	}

	resp, err = srv.Client().Get(srv.URL + "/w/default/discussions?answered=no")
	if err != nil {
		t.Fatal(err)
	}
	body = getBody(t, resp)
	if strings.Contains(body, "Answered question") || !strings.Contains(body, "Open question") {
		t.Error("answered=no filter wrong")
	}
}

func TestDiscussionDetailRendersThreadAndAnswer(t *testing.T) {
	srv, f := newTestServer(t)
	d := f.addDiscussion("Detail", "body", 2, time.Now())
	cid, _ := f.CreateDiscussionThread(t.Context(), d.ID, "top comment", 0)
	if _, err := f.CreateDiscussionComment(t.Context(), d.ID, cid, "nested reply", 0); err != nil {
		t.Fatal(err)
	}
	if err := f.SetThreadAnswer(t.Context(), d.ID, cid, true); err != nil {
		t.Fatal(err)
	}

	resp, err := srv.Client().Get(srv.URL + "/w/default/discussions/" + itoa(d.ID))
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	for _, want := range []string{"top comment", "nested reply", "comment-answer", "badge-answer", "Unmark answer", `name="thread_id"`, "reply-form"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail missing %q", want)
		}
	}
	// Per-comment controls repeat identically on every row, so each carries
	// the comment id in its accessible name (accessibility guide: same look,
	// different action → distinct accessible name).
	for _, want := range []string{
		`aria-label="Unmark answer on thread ` + itoa(cid) + `"`,
		`aria-label="Add a comment on thread ` + itoa(cid) + `"`,
		`aria-label="Post comment on thread ` + itoa(cid) + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("per-comment control lacks unique name %q", want)
		}
	}
}

// The /markdown route answers text/markdown — the fetch target of the
// "Copy as markdown" action — with threads and replies flattened into
// headings; errors keep the styled HTML pages.
func TestDiscussionMarkdownEndpoint(t *testing.T) {
	srv, f := newTestServer(t)
	d := f.addDiscussion("How does triage work?", "qa body", 2, time.Now())
	tid, err := f.CreateDiscussionThread(t.Context(), d.ID, "the fix", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.CreateDiscussionComment(t.Context(), d.ID, tid, "it worked", 0); err != nil {
		t.Fatal(err)
	}
	if err := f.SetThreadAnswer(t.Context(), d.ID, tid, true); err != nil {
		t.Fatal(err)
	}

	resp, err := srv.Client().Get(srv.URL + "/w/default/discussions/" + itoa(d.ID) + "/markdown")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/markdown; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/markdown", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `inline; filename="discussion-`+itoa(d.ID)+`.md"` {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	for _, want := range []string{
		"# How does triage work?\n", "- **Category**: Q&A\n", "- **Answered**: yes\n",
		"qa body", "## Threads", "· answer\n", "the fix", "#### Reply by", "it worked",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("markdown missing %q:\n%s", want, body)
		}
	}

	resp2, err := srv.Client().Get(srv.URL + "/w/default/discussions/999/markdown")
	if err != nil {
		t.Fatal(err)
	}
	getBody(t, resp2)
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("missing discussion status = %d, want 404", resp2.StatusCode)
	}
}

// Phase 2 of the partial-update contract: a partial answer POST swaps
// <li>s in place instead of appending. Since marking moves the badge, the
// fragment carries the toggled comment AND the previous mark holder, each
// self-targeting by id.
func TestPartialSetCommentAnswerContract(t *testing.T) {
	srv, f := newTestServer(t)
	qa := f.addDiscussion("QA target", "b", 2, time.Now())
	c1, _ := f.CreateDiscussionThread(t.Context(), qa.ID, "first", 0)
	c2, _ := f.CreateDiscussionThread(t.Context(), qa.ID, "second", 0)
	if err := f.SetThreadAnswer(t.Context(), qa.ID, c1, true); err != nil {
		t.Fatal(err)
	}
	partial := map[string]string{"X-Partial": "1"}

	resp := postFormWithHeaders(t,
		srv.URL+"/w/default/discussions/"+itoa(qa.ID)+"/comments/"+itoa(c2)+"/answer",
		url.Values{"answer": {"1"}}, partial)
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("partial answer POST status = %d, want 200", resp.StatusCode)
	}
	// The moved mark: c2's <li> is marked, c1's is re-rendered unmarked —
	// and the header meta rides along since marking bumps updated_at.
	for _, want := range []string{
		`class="card comment" id="thread-` + itoa(c1) + `"`,
		`class="card comment comment-answer" id="thread-` + itoa(c2) + `"`,
		`aria-label="Unmark answer on thread ` + itoa(c2) + `"`,
		`class="issue-meta" id="discussion-meta"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("swap fragment missing %q", want)
		}
	}
	if strings.Contains(body, "<!DOCTYPE") {
		t.Error("partial response must be a fragment, not a full page")
	}

	// Without X-Partial the same POST keeps the classic 303 PRG.
	resp = postForm(t, noRedirectClient(),
		srv.URL+"/w/default/discussions/"+itoa(qa.ID)+"/comments/"+itoa(c2)+"/answer",
		url.Values{"answer": {"0"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("plain answer POST status = %d, want 303", resp.StatusCode)
	}
}

func TestDiscussionDetailOpenCategoryHasNoAnswerButton(t *testing.T) {
	srv, f := newTestServer(t)
	d := f.addDiscussion("Open cat", "body", 1, time.Now())
	if _, err := f.CreateDiscussionThread(t.Context(), d.ID, "c", 0); err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Get(srv.URL + "/w/default/discussions/" + itoa(d.ID))
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if strings.Contains(body, "Mark as answer") {
		t.Error("open-format category must not offer answer marking")
	}
	if !strings.Contains(body, `name="thread_id"`) {
		t.Error("reply form still expected")
	}
}

func TestCreateDiscussionPRG(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := postForm(t, noRedirectClient(), srv.URL+"/w/default/discussions", url.Values{
		"title":       {"New thread"},
		"body":        {"hello"},
		"category_id": {"1"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/w/default/discussions/") {
		t.Fatalf("Location = %q", loc)
	}
}

func TestCreateDiscussionValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := postForm(t, srv.Client(), srv.URL+"/w/default/discussions", url.Values{
		"title": {"t"}, "body": {"b"}, "category_id": {""},
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("missing category: status = %d, want 422", resp.StatusCode)
	}
	getBody(t, resp)
	resp = postForm(t, srv.Client(), srv.URL+"/w/default/discussions", url.Values{
		"title": {"t"}, "body": {"b"}, "category_id": {"999"},
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("unknown category: status = %d, want 422", resp.StatusCode)
	}
	getBody(t, resp)
	resp = postForm(t, srv.Client(), srv.URL+"/w/default/discussions", url.Values{
		"title": {"  "}, "body": {"b"}, "category_id": {"1"},
	})
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("missing title: status = %d, want 422", resp.StatusCode)
	}
	if !strings.Contains(body, "Title is required") {
		t.Error("expected title validation error")
	}
}

func TestDiscussionCommentReplyFlow(t *testing.T) {
	srv, f := newTestServer(t)
	d := f.addDiscussion("D", "b", 1, time.Now())
	cid, _ := f.CreateDiscussionThread(t.Context(), d.ID, "top", 0)

	resp := postForm(t, noRedirectClient(), srv.URL+"/w/default/discussions/"+itoa(d.ID)+"/comments", url.Values{
		"body": {"a reply"}, "thread_id": {itoa(cid)},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "#comment-") {
		t.Fatalf("Location = %q, want anchor", loc)
	}

	rid, _ := f.CreateDiscussionComment(t.Context(), d.ID, cid, "nested", 0)
	resp = postForm(t, srv.Client(), srv.URL+"/w/default/discussions/"+itoa(d.ID)+"/comments", url.Values{
		"body": {"too deep"}, "thread_id": {itoa(rid)},
	})
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	if !strings.Contains(body, "too deep") {
		t.Error("draft not preserved")
	}
}

func TestDiscussionCommentValidation(t *testing.T) {
	srv, f := newTestServer(t)
	d := f.addDiscussion("D", "b", 1, time.Now())

	resp := postForm(t, srv.Client(), srv.URL+"/w/default/discussions/"+itoa(d.ID)+"/comments", url.Values{
		"body": {"   "},
	})
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("empty comment: status = %d, want 422", resp.StatusCode)
	}
	if !strings.Contains(body, "Body is required") {
		t.Error("expected comment validation error")
	}
	if ts, _ := f.ListDiscussionThreads(t.Context(), d.ID); len(ts) != 0 {
		t.Fatalf("empty comment was saved: %+v", ts)
	}

	resp = postForm(t, srv.Client(), srv.URL+"/w/default/discussions/"+itoa(d.ID)+"/comments", url.Values{
		"body": {"orphan"}, "thread_id": {"abc"},
	})
	getBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("bad thread_id: status = %d, want 422", resp.StatusCode)
	}
	resp = postForm(t, srv.Client(), srv.URL+"/w/default/discussions/"+itoa(d.ID)+"/comments", url.Values{
		"body": {"orphan"}, "thread_id": {"-5"},
	})
	getBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("negative thread_id: status = %d, want 422", resp.StatusCode)
	}
	if ts, _ := f.ListDiscussionThreads(t.Context(), d.ID); len(ts) != 0 {
		t.Fatalf("malformed reply was saved as new thread: %+v", ts)
	}
}

// The partial-update contract (docs/sketch/ja/partial-updates.md): a POST
// carrying X-Partial: 1 gets back just the fragment — the new comment's
// <li> on success, the error-marked form on a validation failure (422) —
// while a normal POST keeps the classic 303 PRG redirect.
func TestPartialCommentContract(t *testing.T) {
	srv, f := newTestServer(t)
	issue := f.addIssue("Partial target", "b", "open", time.Now())
	qa := f.addDiscussion("QA target", "b", 2, time.Now())
	parent, err := f.CreateDiscussionThread(t.Context(), qa.ID, "parent", 0)
	if err != nil {
		t.Fatal(err)
	}
	partial := map[string]string{"X-Partial": "1"}

	// A successful partial post returns the new comment's <li> plus the
	// header meta as a data-swap node — posting bumped updated_at, so the
	// client replaces the stale "Last updated" line instead of appending.
	resp := postFormWithHeaders(t, srv.URL+"/w/default/issues/"+itoa(issue.ID)+"/comments",
		url.Values{"body": {"fragment body"}}, partial)
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("partial POST status = %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{`class="card comment"`, "fragment body", `id="issue-meta" data-swap`} {
		if !strings.Contains(body, want) {
			t.Errorf("comment fragment missing %q", want)
		}
	}
	if strings.Contains(body, "<!DOCTYPE") {
		t.Error("partial response must be a fragment, not a full page")
	}
	// The meta node must echo the post-bump updated_at, not the stale one.
	got, _ := f.GetIssue(t.Context(), issue.ID)
	stamp := got.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
	if !strings.Contains(strings.ReplaceAll(body, "&#43;", "+"), `datetime="`+stamp+`"`) {
		t.Errorf("meta swap node must carry bumped updated_at %q", stamp)
	}

	// A validation failure returns only the error-marked form at 422.
	resp = postFormWithHeaders(t, srv.URL+"/w/default/issues/"+itoa(issue.ID)+"/comments",
		url.Values{"body": {""}}, partial)
	body = getBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("partial POST status = %d, want 422", resp.StatusCode)
	}
	for _, want := range []string{`class="comment-form`, `aria-invalid="true"`, "Body is required"} {
		if !strings.Contains(body, want) {
			t.Errorf("422 form fragment missing %q", want)
		}
	}
	if strings.Contains(body, "<!DOCTYPE") {
		t.Error("partial 422 must be a fragment, not a full page")
	}

	// A reply's fragment carries comment-reply markup plus the header meta
	// swap node (posting bumped the discussion's updated_at).
	resp = postFormWithHeaders(t, srv.URL+"/w/default/discussions/"+itoa(qa.ID)+"/comments",
		url.Values{"body": {"a reply"}, "thread_id": {itoa(parent)}}, partial)
	body = getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("partial reply status = %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{"comment-reply", `id="discussion-meta" data-swap`} {
		if !strings.Contains(body, want) {
			t.Errorf("reply fragment missing %q", want)
		}
	}
	// A top-level discussion comment POST also rides the meta node.
	resp = postFormWithHeaders(t, srv.URL+"/w/default/discussions/"+itoa(qa.ID)+"/comments",
		url.Values{"body": {"top level"}}, partial)
	body = getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("partial comment status = %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{`class="card comment"`, "top level", `id="discussion-meta" data-swap`} {
		if !strings.Contains(body, want) {
			t.Errorf("comment fragment missing %q", want)
		}
	}

	// Without the header the same endpoints keep the 303 PRG redirect.
	resp = postFormWithHeaders(t, srv.URL+"/w/default/discussions/"+itoa(qa.ID)+"/comments",
		url.Values{"body": {"classic"}}, nil)
	getBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "#thread-") {
		t.Errorf("normal POST status = %d location = %q, want 303 #thread-", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestSetCommentAnswerFlow(t *testing.T) {
	srv, f := newTestServer(t)
	d := f.addDiscussion("QA", "b", 2, time.Now())
	cid, _ := f.CreateDiscussionThread(t.Context(), d.ID, "c", 0)
	base := srv.URL + "/w/default/discussions/" + itoa(d.ID) + "/comments/" + itoa(cid) + "/answer"

	resp := postForm(t, noRedirectClient(), base, url.Values{"answer": {"1"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("mark: status = %d, want 303", resp.StatusCode)
	}
	got, err := f.GetDiscussion(t.Context(), d.ID)
	if err != nil || !got.Answered {
		t.Fatalf("answered flag after mark: %v %+v", err, got)
	}

	resp = postForm(t, noRedirectClient(), base, url.Values{"answer": {"0"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unmark: status = %d, want 303", resp.StatusCode)
	}
	got, _ = f.GetDiscussion(t.Context(), d.ID)
	if got.Answered {
		t.Fatal("still answered after unmark")
	}

	// open-format category refuses marks
	dOpen := f.addDiscussion("O", "b", 1, time.Now())
	oc, _ := f.CreateDiscussionThread(t.Context(), dOpen.ID, "c", 0)
	resp = postForm(t, srv.Client(),
		srv.URL+"/w/default/discussions/"+itoa(dOpen.ID)+"/comments/"+itoa(oc)+"/answer",
		url.Values{"answer": {"1"}})
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("open category mark: status = %d, want 422", resp.StatusCode)
	}
	if !strings.Contains(body, "Cannot mark as answer") {
		t.Error("expected cannot-mark explanation")
	}
}

func TestDiscussionNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, path := range []string{"/w/default/discussions/99", "/w/default/discussions/99/edit", "/w/default/discussions/abc"} {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		getBody(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestEditDiscussionFlow(t *testing.T) {
	srv, f := newTestServer(t)
	d := f.addDiscussion("Old title", "old body", 1, time.Now())

	resp := postForm(t, noRedirectClient(), srv.URL+"/w/default/discussions/"+itoa(d.ID)+"/edit", url.Values{
		"title": {"New title"}, "body": {"new body"}, "category_id": {"2"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	got, err := f.GetDiscussion(t.Context(), d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "New title" || got.Category.Slug != "qa" {
		t.Fatalf("update not applied: %+v", got)
	}
}

func TestDeleteDiscussionFlow(t *testing.T) {
	srv, f := newTestServer(t)
	d := f.addDiscussion("Doomed", "b", 1, time.Now())

	resp, err := srv.Client().Get(srv.URL + "/w/default/discussions/" + itoa(d.ID) + "/delete")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete confirm: status = %d", resp.StatusCode)
	}
	getBody(t, resp)

	resp = postForm(t, noRedirectClient(), srv.URL+"/w/default/discussions/"+itoa(d.ID)+"/delete", url.Values{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/w/default/discussions" {
		t.Fatalf("Location = %q, want /discussions", loc)
	}
	if _, err := f.GetDiscussion(t.Context(), d.ID); err != store.ErrNotFound {
		t.Fatalf("discussion still exists: %v", err)
	}
}
