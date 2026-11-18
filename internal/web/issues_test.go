package web

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

func TestRootRedirectsToIssues(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := noRedirectClient().Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/w/default/issues" {
		t.Fatalf("Location = %q, want /issues", loc)
	}
}

func TestListRendersIssues(t *testing.T) {
	srv, f := newTestServer(t)
	seedIssues(t, f)

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{"Fix flaky test", "Add dark mode"} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing %q", want)
		}
	}
	if strings.Contains(body, "Write docs") {
		t.Error("closed issue should not appear in default list")
	}
	if !strings.Contains(body, "of 2 issues") {
		t.Errorf("expected total of 2 open issues, body:\n%s", body)
	}
}

func TestListStatusFilter(t *testing.T) {
	srv, f := newTestServer(t)
	seedIssues(t, f)

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues?status=closed")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if !strings.Contains(body, "Write docs") {
		t.Error("closed tab should show closed issue")
	}
	if strings.Contains(body, "Fix flaky test") {
		t.Error("open issue should not appear in closed tab")
	}
}

func TestListQueryFilter(t *testing.T) {
	srv, f := newTestServer(t)
	seedIssues(t, f)

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues?status=all&q=flaky")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if !strings.Contains(body, "Fix flaky test") {
		t.Error("q=flaky should match the issue title")
	}
	if strings.Contains(body, "Write docs") || strings.Contains(body, "Add dark mode") {
		t.Error("non-matching issues should be filtered out")
	}
}

func TestListLabelFilter(t *testing.T) {
	srv, f := newTestServer(t)
	seedIssues(t, f)

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues?status=all&label=2")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if !strings.Contains(body, "Write docs") {
		t.Error("label=2 should show the Documentation-labeled issue")
	}
	if strings.Contains(body, "Fix flaky test") {
		t.Error("label=2 should not show Bug-labeled issue")
	}
}

func TestListPagination(t *testing.T) {
	srv, f := newTestServer(t)
	now := time.Now()
	// 25 open issues → page size 20 → 2 pages.
	for i := range 25 {
		f.addIssue("Issue number "+strconv.Itoa(i), "", store.StatusOpen, now.Add(-time.Duration(i)*time.Hour))
	}

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if !strings.Contains(body, "Showing 1–20 of 25 issues") {
		t.Errorf("expected range header, got:\n%s", body)
	}
	if !strings.Contains(body, `href="/w/default/issues?page=2&amp;status=open"`) &&
		!strings.Contains(body, "page=2") {
		t.Error("expected a next-page link preserving filters")
	}
	if strings.Contains(body, "page=0") {
		t.Error("should not emit a previous link on page 1")
	}

	resp2, err := srv.Client().Get(srv.URL + "/w/default/issues?page=2")
	if err != nil {
		t.Fatal(err)
	}
	body2 := getBody(t, resp2)
	if !strings.Contains(body2, "Showing 21–25 of 25 issues") {
		t.Errorf("expected second page range, got:\n%s", body2)
	}
}

func TestShowIssueNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/999")
	if err != nil {
		t.Fatal(err)
	}
	getBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestShowIssueNonNumericID(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/abc")
	if err != nil {
		t.Fatal(err)
	}
	getBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestShowIssueRendersDetail(t *testing.T) {
	srv, f := newTestServer(t)
	is := f.addIssue("Detail title", "line one\nline two", store.StatusOpen, time.Now(), 1)
	f.comments[is.ID] = append(f.comments[is.ID], &store.Comment{ID: 1, IssueID: is.ID, Body: "first!", CreatedAt: time.Now()})

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/1")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	for _, want := range []string{"Detail title", "#1", "line one", "first!", "Open"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail missing %q", want)
		}
	}
}

func TestShowIssueRendersMarkdown(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("MD issue", "# Heading\n\n```\ncode <x>\n```\n\n<script>alert(1)</script>",
		store.StatusOpen, time.Now())
	f.comments[1] = append(f.comments[1], &store.Comment{
		ID: 1, IssueID: 1, Body: "**bold** comment", CreatedAt: time.Now()})

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/1")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	for _, want := range []string{"<h1>Heading</h1>", "<pre><code", "<strong>bold</strong> comment"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail missing rendered markdown %q", want)
		}
	}
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Error("raw HTML in the markdown source must not render")
	}
	if !strings.Contains(body, "code &lt;x&gt;") {
		t.Error("fenced code content should appear escaped inside the block")
	}

	resp2, err := srv.Client().Get(srv.URL + "/w/default/issues/1/edit")
	if err != nil {
		t.Fatal(err)
	}
	body2 := getBody(t, resp2)
	if !strings.Contains(body2, "```") || !strings.Contains(body2, "&lt;script&gt;") {
		t.Error("edit form should preserve the raw markdown source in the textarea")
	}
}

// The /markdown route answers text/markdown — the fetch target of the
// "Copy as markdown" action — while errors keep the styled HTML pages.
func TestIssueMarkdownEndpoint(t *testing.T) {
	srv, f := newTestServer(t)
	is := f.addIssue("Detail title", "line one\nline two", store.StatusOpen, time.Now())
	f.comments[is.ID] = append(f.comments[is.ID], &store.Comment{ID: 1, IssueID: is.ID, Body: "first!", CreatedAt: time.Now()})

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/1/markdown")
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
	if cd := resp.Header.Get("Content-Disposition"); cd != `inline; filename="issue-1.md"` {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	for _, want := range []string{"# Detail title\n", "- **Status**: open\n", "line one\nline two", "## Comments", "first!"} {
		if !strings.Contains(body, want) {
			t.Errorf("markdown missing %q:\n%s", want, body)
		}
	}

	for _, path := range []string{"/w/default/issues/999/markdown", "/w/default/issues/abc/markdown"} {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		getBody(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, resp.StatusCode)
		}
	}

	// A GET under the wrong workspace redirects to the canonical URL —
	// the same rule the detail page follows.
	if _, err := f.CreateWorkspace(t.Context(), store.WorkspaceInput{Slug: "other"}); err != nil {
		t.Fatal(err)
	}
	resp3, err := noRedirectClient().Get(srv.URL + "/w/other/issues/1/markdown")
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusFound {
		t.Fatalf("cross-workspace status = %d, want 302", resp3.StatusCode)
	}
	if loc := resp3.Header.Get("Location"); loc != "/w/default/issues/1/markdown" {
		t.Fatalf("Location = %q, want /w/default/issues/1/markdown", loc)
	}
}

func TestCreateIssueValidationError(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := postForm(t, srv.Client(), srv.URL+"/w/default/issues", url.Values{
		"title": {"   "},
		"body":  {"keep this body"},
	})
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 422 or 400", resp.StatusCode)
	}
	if !strings.Contains(body, "Title is required") {
		t.Error("expected title validation error")
	}
	if !strings.Contains(body, "keep this body") {
		t.Error("expected submitted body to be preserved")
	}
	if !strings.Contains(body, `aria-invalid="true" aria-describedby="f-title-error" autofocus`) {
		t.Error("expected aria-invalid, aria-describedby and autofocus on the invalid title")
	}
	if !strings.Contains(body, `<p class="field-error" id="f-title-error">Title is required.</p>`) {
		t.Error("expected the server-rendered title error, not the pending one")
	}
	if strings.Contains(body, `role="alert"`) {
		t.Error("errors are announced through focus on reload, not role=alert")
	}
	if !strings.Contains(body, `aria-invalid="false" aria-describedby="f-body-hint"`) {
		t.Error("the valid body should stay aria-invalid=false without the error id")
	}
}

// Before any submit, every field is aria-invalid="false" and no error id is
// referenced from aria-describedby; the browser-side "required" message is in
// the page but marked pending (shown by CSS only under :user-invalid).
func TestFormFeedbackBeforeSubmit(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("Feedback", "", store.StatusOpen, time.Now())

	for _, tc := range []struct {
		path    string
		pending string
	}{
		{"/w/default/issues/new", `<p class="field-error field-error-pending" id="f-title-error">Title is required.</p>`},
		{"/w/default/issues/1/edit", `<p class="field-error field-error-pending" id="f-title-error">Title is required.</p>`},
		{"/w/default/issues/1", `<p class="field-error field-error-pending" id="comment-error">Body is required.</p>`},
	} {
		resp, err := srv.Client().Get(srv.URL + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		body := getBody(t, resp)
		if !strings.Contains(body, tc.pending) {
			t.Errorf("%s: missing pending error %q", tc.path, tc.pending)
		}
		for _, bad := range []string{`aria-describedby="f-title-error`, `aria-describedby="comment-error`, `-hint" autofocus`, `aria-invalid="false" autofocus`} {
			if strings.Contains(body, bad) {
				t.Errorf("%s: unexpected %q before submit", tc.path, bad)
			}
		}
		if strings.Contains(body, `aria-invalid="true"`) {
			t.Errorf("%s: no field should be invalid before submit", tc.path)
		}
		if !strings.Contains(body, `aria-invalid="false"`) {
			t.Errorf("%s: fields should start at aria-invalid=false", tc.path)
		}
	}
}

func TestCreateIssueSuccess(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := postForm(t, noRedirectClient(), srv.URL+"/w/default/issues", url.Values{
		"title":    {"Brand new issue"},
		"body":     {"details here"},
		"label_id": {"1", "3"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/w/default/issues/") || loc == "/w/default/issues/new" {
		t.Fatalf("Location = %q, want /issues/{id}", loc)
	}

	resp2, err := srv.Client().Get(srv.URL + loc)
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp2)
	for _, want := range []string{"Brand new issue", "details here", "Bug", "Feature"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail missing %q", want)
		}
	}
}

func TestCreateComment(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("Comment target", "", store.StatusOpen, time.Now())

	resp := postForm(t, noRedirectClient(), srv.URL+"/w/default/issues/1/comments", url.Values{
		"body": {"hello there"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/w/default/issues/1#comment-") {
		t.Fatalf("Location = %q, want /issues/1#comment-…", loc)
	}

	resp2, err := srv.Client().Get(srv.URL + "/w/default/issues/1")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp2)
	if !strings.Contains(body, "hello there") {
		t.Error("comment should appear on the detail page")
	}
	if !strings.Contains(body, `id="comment-1"`) || !strings.Contains(body, `href="#comment-1"`) {
		t.Error("comment should render with a permalink anchor and link")
	}
}

func TestCreateCommentEmpty(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("Comment target", "", store.StatusOpen, time.Now())

	resp := postForm(t, srv.Client(), srv.URL+"/w/default/issues/1/comments", url.Values{
		"body": {""},
	})
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 422 or 400", resp.StatusCode)
	}
	if !strings.Contains(body, "Body is required") {
		t.Error("expected comment validation error")
	}
	if !strings.Contains(body, `aria-invalid="true" aria-describedby="comment-error comment-hint" autofocus`) {
		t.Error("expected the invalid comment textarea to be described by its error and focused")
	}
	if strings.Contains(body, "field-error-pending") {
		t.Error("the server error should replace the pending message")
	}
}

func TestToggleStatus(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("Toggle me", "", store.StatusOpen, time.Now())

	resp := postForm(t, noRedirectClient(), srv.URL+"/w/default/issues/1/status", url.Values{
		"status": {"closed"},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	got, _ := f.GetIssue(context.Background(), 1)
	if got.Status != store.StatusClosed {
		t.Fatalf("status = %q, want closed", got.Status)
	}

	resp2 := postForm(t, noRedirectClient(), srv.URL+"/w/default/issues/1/status", url.Values{
		"status": {"open"},
	})
	resp2.Body.Close()
	got2, _ := f.GetIssue(context.Background(), 1)
	if got2.Status != store.StatusOpen {
		t.Fatalf("status = %q, want open", got2.Status)
	}
}

// Phase 2 of the partial-update contract: a partial status POST answers
// the re-rendered issue article — badge, meta and the flipped toggle —
// which the client swaps over .issue-detail (replace mode).
func TestPartialSetStatusContract(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("Toggle me", "", store.StatusOpen, time.Now())
	partial := map[string]string{"X-Partial": "1"}

	resp := postFormWithHeaders(t, srv.URL+"/w/default/issues/1/status",
		url.Values{"status": {"closed"}}, partial)
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("partial status POST status = %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{
		`<article class="card issue-detail" id="issue-article">`,
		`badge-closed`,
		`name="status" value="open"`,
		"Reopen issue",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("status swap fragment missing %q", want)
		}
	}
	if strings.Contains(body, "<!DOCTYPE") {
		t.Error("partial response must be a fragment, not a full page")
	}
	got, _ := f.GetIssue(context.Background(), 1)
	if got.Status != store.StatusClosed {
		t.Fatalf("status = %q, want closed", got.Status)
	}

	// A no-op toggle (closed on already-closed) must NOT show a fresh
	// timestamp: the store leaves updated_at alone, so the fragment
	// echoes the stored value — not time.Now().
	resp2 := postFormWithHeaders(t, srv.URL+"/w/default/issues/1/status",
		url.Values{"status": {"closed"}}, partial)
	body2 := getBody(t, resp2)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("no-op partial status POST status = %d, want 200", resp2.StatusCode)
	}
	// Format the stored value the way the template does — in the time's
	// own zone, not forced to UTC. A non-UTC zone renders "+09:00", which
	// html/template escapes to &#43; in the attribute, so normalize the
	// body before comparing (works in any local zone).
	stamp := got.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
	if !strings.Contains(strings.ReplaceAll(body2, "&#43;", "+"), `datetime="`+stamp+`"`) {
		t.Errorf("no-op toggle fragment must keep stored updated_at %q", stamp)
	}
}

func TestSetStatusInvalid(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("Bad status", "", store.StatusOpen, time.Now())
	resp := postForm(t, srv.Client(), srv.URL+"/w/default/issues/1/status", url.Values{
		"status": {"bogus"},
	})
	getBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestDeleteFlow(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("Delete me", "", store.StatusOpen, time.Now())

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/1/delete")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "Delete me") {
		t.Error("confirm page should name the issue")
	}
	if _, err := f.GetIssue(context.Background(), 1); err != nil {
		t.Fatal("GET delete confirm must not delete the issue")
	}

	resp2 := postForm(t, noRedirectClient(), srv.URL+"/w/default/issues/1/delete", url.Values{})
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp2.StatusCode)
	}
	if loc := resp2.Header.Get("Location"); loc != "/w/default/issues" {
		t.Fatalf("Location = %q, want /issues", loc)
	}
	if _, err := f.GetIssue(context.Background(), 1); err != store.ErrNotFound {
		t.Fatalf("issue should be gone, got err=%v", err)
	}
}

// The detail page opens a declarative modal <dialog> (Invoker Commands, no
// JS) whose form POSTs the delete, and keeps the link to the GET
// confirmation page for browsers without Invoker Commands.
func TestDetailDeleteDialog(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("Dialog me", "", store.StatusOpen, time.Now())

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/1")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	for _, want := range []string{
		`commandfor="delete-dialog" command="show-modal"`,
		`<dialog id="delete-dialog"`,
		`closedby="any"`,
		`aria-labelledby="delete-dialog-title"`,
		`<form method="post" action="/w/default/issues/1/delete"`,
		`commandfor="delete-dialog" command="close" autofocus`,
		`href="/w/default/issues/1/delete"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail missing %q", want)
		}
	}
	stripped := regexp.MustCompile(`<script[^>]*src="/static/[a-zA-Z0-9._/-]+"[^>]*>\s*</script>`).ReplaceAllString(body, "")
	if strings.Contains(stripped, "<script") {
		t.Error("detail page must only load same-origin /static scripts, never inline")
	}
}

func TestEditFlow(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("Old title", "old body", store.StatusOpen, time.Now(), 1)

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/1/edit")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if !strings.Contains(body, `value="Old title"`) {
		t.Error("edit form should prefill the title")
	}
	if !strings.Contains(body, "old body") {
		t.Error("edit form should prefill the body")
	}

	resp2 := postForm(t, noRedirectClient(), srv.URL+"/w/default/issues/1/edit", url.Values{
		"title":    {"New title"},
		"body":     {"new body"},
		"label_id": {"2"},
	})
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp2.StatusCode)
	}
	if loc := resp2.Header.Get("Location"); loc != "/w/default/issues/1" {
		t.Fatalf("Location = %q, want /issues/1", loc)
	}
	got, _ := f.GetIssue(context.Background(), 1)
	if got.Title != "New title" || got.Body != "new body" {
		t.Fatalf("issue not updated: %+v", got)
	}
	if len(got.Labels) != 1 || got.Labels[0].Name != "Documentation" {
		t.Fatalf("labels not updated: %+v", got.Labels)
	}
}

func TestHTMLEscaping(t *testing.T) {
	srv, f := newTestServer(t)
	payload := "<script>alert(1)</script>"
	f.addIssue(payload, payload, store.StatusOpen, time.Now())
	f.comments[1] = append(f.comments[1], &store.Comment{ID: 1, IssueID: 1, Body: payload, CreatedAt: time.Now()})

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Error("list page must escape the raw payload")
	}
	if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("list page should contain the escaped payload")
	}

	resp2, err := srv.Client().Get(srv.URL + "/w/default/issues/1")
	if err != nil {
		t.Fatal(err)
	}
	body2 := getBody(t, resp2)
	if strings.Count(body2, "<script>alert(1)</script>") != 0 {
		t.Error("detail page must escape the raw payload")
	}
	if !strings.Contains(body2, "&lt;script&gt;") {
		t.Error("detail page should contain the escaped payload")
	}
}

func TestNewIssueFormRenders(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/new")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{"New issue", `name="title"`, `name="body"`, `name="label_id"`} {
		if !strings.Contains(body, want) {
			t.Errorf("form missing %q", want)
		}
	}
}

func TestListOutOfRangePageShowsLastPage(t *testing.T) {
	srv, f := newTestServer(t)
	now := time.Now()
	for i := range 25 {
		f.addIssue("Paged issue "+strconv.Itoa(i), "", store.StatusOpen, now.Add(-time.Duration(i)*time.Hour))
	}

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues?page=99999")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "Showing 21–25 of 25 issues") {
		t.Errorf("expected last-page range header, got:\n%s", body)
	}
	if !strings.Contains(body, "Page 2 of 2") {
		t.Error("expected pagination nav reflecting the clamped page")
	}
	if !strings.Contains(body, ">Previous</a>") {
		t.Error("expected a working Previous link on the clamped last page")
	}
	if strings.Contains(body, "empty-state") {
		t.Error("out-of-range page must not render the empty state")
	}
}

func TestListAbsurdPageFallsBackToFirst(t *testing.T) {
	srv, f := newTestServer(t)
	now := time.Now()
	for i := range 25 {
		f.addIssue("Paged issue "+strconv.Itoa(i), "", store.StatusOpen, now.Add(-time.Duration(i)*time.Hour))
	}

	// 2^62 would overflow the store's (page-1)*perPage offset arithmetic.
	resp, err := srv.Client().Get(srv.URL + "/w/default/issues?page=4611686018427387904")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "Showing 1–20 of 25 issues") {
		t.Errorf("expected first-page range header, got:\n%s", body)
	}
	if strings.Contains(body, "Showing -") {
		t.Error("absurd page must not produce a negative range")
	}
}

func TestUpdateIssueMissingIssue404(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := postForm(t, srv.Client(), srv.URL+"/w/default/issues/999/edit", url.Values{
		"title": {""},
	})
	getBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDetailLabelLinkUsesStatusAll(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("Labeled issue", "", store.StatusClosed, time.Now(), 1)

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/1")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	// The link must reach the closed issue too, so it carries status=all.
	if !strings.Contains(body, `href="/w/default/issues?status=all&amp;label=1"`) {
		t.Errorf("expected label link with status=all, got:\n%s", body)
	}
}

func TestListLinksOmitDefaultStatus(t *testing.T) {
	srv, f := newTestServer(t)
	seedIssues(t, f)

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	// "open" is the default filter; generated links keep URLs clean.
	if strings.Contains(body, "status=open") {
		t.Error("generated links must not carry the redundant status=open")
	}
	if !strings.Contains(body, `href="/w/default/issues"`) {
		t.Error("expected the Open tab link to be bare /issues")
	}

	resp2, err := srv.Client().Get(srv.URL + "/w/default/issues?status=closed")
	if err != nil {
		t.Fatal(err)
	}
	body2 := getBody(t, resp2)
	if !strings.Contains(body2, "status=closed") {
		t.Error("closed-filter links must keep status=closed")
	}
}

func TestListA11ySemantics(t *testing.T) {
	srv, f := newTestServer(t)
	seedIssues(t, f)

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if !strings.Contains(body, `class="filter-bar" role="search" aria-label="Filter issues"`) {
		t.Error("filter form lacks role=search")
	}
	// list-style: none strips list semantics in Safari; role="list" restores it.
	if !strings.Contains(body, `<ul class="issue-list" role="list">`) {
		t.Error("issue-list lacks role=list")
	}
}

func TestDetailA11ySemantics(t *testing.T) {
	srv, f := newTestServer(t)
	is := f.addIssue("Detail target", "body", store.StatusOpen, time.Now(), 1)
	if _, err := f.CreateComment(context.Background(), is.ID, "first comment", 0); err != nil {
		t.Fatal(err)
	}

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/1")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if !strings.Contains(body, `<nav class="breadcrumb" aria-label="Breadcrumb"><ol>`) &&
		!strings.Contains(body, `class="breadcrumb" aria-label="Breadcrumb"`) {
		t.Error("breadcrumb nav missing")
	}
	if !strings.Contains(body, `<a href="#" aria-current="page">#1</a>`) {
		t.Error("breadcrumb current item lacks aria-current link")
	}
	if strings.Contains(body, `aria-hidden="true">/</span>`) {
		t.Error("breadcrumb separator should be CSS-generated, not markup")
	}
	if !strings.Contains(body, `<ol class="comment-list" role="list">`) {
		t.Error("comment-list lacks role=list")
	}
	// Permalinks share the same visible text; the hidden comment id keeps
	// each link's accessible name distinct.
	if !strings.Contains(body, `class="visually-hidden">(comment 1)</span>`) {
		t.Error("permalink lacks the hidden comment id")
	}
}

func TestAuthorRecordedAndDisplayed(t *testing.T) {
	srv, f := newTestServer(t)
	f.addUser(t, "alice", "password123")

	resp := postForm(t, noRedirectClient(), srv.URL+"/login", url.Values{
		"name": {"alice"}, "password": {"password123"},
	})
	resp.Body.Close()
	c := sessionCookie(t, resp)

	form := url.Values{"title": {"authored issue"}, "body": {"with a name"}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/w/default/issues", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(c)
	resp, err = noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("POST /issues: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if f.issues[1].Author == nil || f.issues[1].Author.Name != "alice" {
		t.Fatal("issue author not recorded")
	}

	f.addIssue("anon issue", "", store.StatusOpen, time.Now(), 2)
	code, body := getWithCookie(t, srv, "/w/default/issues/2", nil)
	if code != http.StatusOK || !strings.Contains(body, "Opened by anonymous") {
		t.Error("anonymous post not rendered as anonymous")
	}
	code, body = getWithCookie(t, srv, "/w/default/issues/1", nil)
	if code != http.StatusOK || !strings.Contains(body, "Opened by alice") {
		t.Error("authored issue missing author name")
	}

	form = url.Values{"body": {"signed comment"}}
	req, err = http.NewRequest(http.MethodPost, srv.URL+"/w/default/issues/1/comments", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(c)
	resp, err = noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("POST comment: %v", err)
	}
	resp.Body.Close()
	_, body = getWithCookie(t, srv, "/w/default/issues/1", nil)
	if !strings.Contains(body, "alice commented") {
		t.Error("comment missing author name")
	}
}
