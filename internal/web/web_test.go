package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// fakeStore is an in-memory implementation of the Store contract used by the
// web layer. It honors the documented semantics: ErrNotFound, status/query/
// label filtering, newest/oldest sorting and pre-pagination totals.
type fakeStore struct {
	mu          sync.Mutex
	nextIssue   int64
	nextComment int64
	issues      map[int64]*store.Issue
	comments    map[int64][]*store.Comment
	labels      []*store.Label
}

func newFakeStore() *fakeStore {
	f := &fakeStore{
		nextIssue:   1,
		nextComment: 1,
		issues:      map[int64]*store.Issue{},
		comments:    map[int64][]*store.Comment{},
	}
	f.labels = []*store.Label{
		{ID: 1, Name: "Bug", Color: "#d73a4a"},
		{ID: 2, Name: "Documentation", Color: "#0075ca"},
		{ID: 3, Name: "Feature", Color: "#a2eeef"},
	}
	return f
}

// addIssue is a test helper that inserts an issue directly.
func (f *fakeStore) addIssue(title, body, status string, created time.Time, labelIDs ...int64) *store.Issue {
	f.mu.Lock()
	defer f.mu.Unlock()
	is := &store.Issue{
		ID:        f.nextIssue,
		Title:     title,
		Body:      body,
		Status:    status,
		CreatedAt: created,
		UpdatedAt: created,
	}
	f.nextIssue++
	for _, id := range labelIDs {
		if l := f.labelByID(id); l != nil {
			is.Labels = append(is.Labels, *l)
		}
	}
	f.issues[is.ID] = is
	return is
}

func (f *fakeStore) labelByID(id int64) *store.Label {
	for _, l := range f.labels {
		if l.ID == id {
			return l
		}
	}
	return nil
}

func cloneIssue(is *store.Issue) *store.Issue {
	c := *is
	c.Labels = append([]store.Label(nil), is.Labels...)
	return &c
}

func (f *fakeStore) ListIssues(_ context.Context, fl store.ListFilter) ([]*store.Issue, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	status := fl.Status
	if status == "" {
		status = store.StatusOpen
	}
	var matched []*store.Issue
	for _, is := range f.issues {
		if status != "all" && is.Status != status {
			continue
		}
		if fl.Query != "" {
			q := strings.ToLower(fl.Query)
			if !strings.Contains(strings.ToLower(is.Title), q) &&
				!strings.Contains(strings.ToLower(is.Body), q) {
				continue
			}
		}
		if fl.LabelID != 0 {
			found := false
			for _, l := range is.Labels {
				if l.ID == fl.LabelID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		matched = append(matched, is)
	}
	sort.Slice(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		if fl.Sort == "oldest" {
			if a.CreatedAt.Equal(b.CreatedAt) {
				return a.ID < b.ID
			}
			return a.CreatedAt.Before(b.CreatedAt)
		}
		if a.CreatedAt.Equal(b.CreatedAt) {
			return a.ID > b.ID
		}
		return a.CreatedAt.After(b.CreatedAt)
	})

	total := len(matched)
	page := max(fl.Page, 1)
	per := fl.PerPage
	if per <= 0 {
		per = 20
	}
	start := (page - 1) * per
	if start >= total {
		return nil, total, nil
	}
	end := min(start+per, total)
	out := make([]*store.Issue, 0, end-start)
	for _, is := range matched[start:end] {
		out = append(out, cloneIssue(is))
	}
	return out, total, nil
}

func (f *fakeStore) GetIssue(_ context.Context, id int64) (*store.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	is, ok := f.issues[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneIssue(is), nil
}

func (f *fakeStore) CreateIssue(_ context.Context, in store.IssueInput) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	is := &store.Issue{
		ID:        f.nextIssue,
		Title:     in.Title,
		Body:      in.Body,
		Status:    store.StatusOpen,
		CreatedAt: now,
		UpdatedAt: now,
	}
	f.nextIssue++
	for _, id := range in.LabelIDs {
		if l := f.labelByID(id); l != nil {
			is.Labels = append(is.Labels, *l)
		}
	}
	f.issues[is.ID] = is
	return is.ID, nil
}

func (f *fakeStore) UpdateIssue(_ context.Context, id int64, in store.IssueInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	is, ok := f.issues[id]
	if !ok {
		return store.ErrNotFound
	}
	is.Title = in.Title
	is.Body = in.Body
	is.UpdatedAt = time.Now()
	is.Labels = nil
	for _, lid := range in.LabelIDs {
		if l := f.labelByID(lid); l != nil {
			is.Labels = append(is.Labels, *l)
		}
	}
	return nil
}

func (f *fakeStore) SetIssueStatus(_ context.Context, id int64, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	is, ok := f.issues[id]
	if !ok {
		return store.ErrNotFound
	}
	is.Status = status
	is.UpdatedAt = time.Now()
	return nil
}

func (f *fakeStore) DeleteIssue(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.issues[id]; !ok {
		return store.ErrNotFound
	}
	delete(f.issues, id)
	delete(f.comments, id)
	return nil
}

func (f *fakeStore) ListComments(_ context.Context, issueID int64) ([]*store.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	src := f.comments[issueID]
	out := make([]*store.Comment, len(src))
	copy(out, src)
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (f *fakeStore) CreateComment(_ context.Context, issueID int64, body string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.issues[issueID]; !ok {
		return 0, store.ErrNotFound
	}
	c := &store.Comment{
		ID:        f.nextComment,
		IssueID:   issueID,
		Body:      body,
		CreatedAt: time.Now(),
	}
	f.nextComment++
	f.comments[issueID] = append(f.comments[issueID], c)
	return c.ID, nil
}

func (f *fakeStore) ListLabels(_ context.Context) ([]*store.Label, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*store.Label, len(f.labels))
	for i, l := range f.labels {
		c := *l
		out[i] = &c
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// compile-time check that the fake satisfies the consumer-side interface.
var _ Store = (*fakeStore)(nil)

/* ---------- test helpers ---------- */

func newTestServer(t *testing.T) (*httptest.Server, *fakeStore) {
	t.Helper()
	f := newFakeStore()
	srv := httptest.NewServer(New(f))
	t.Cleanup(srv.Close)
	return srv, f
}

// noRedirectClient follows no redirects so we can assert on 303 + Location.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func postForm(t *testing.T, client *http.Client, rawurl string, form url.Values) *http.Response {
	t.Helper()
	resp, err := client.PostForm(rawurl, form)
	if err != nil {
		t.Fatalf("POST %s: %v", rawurl, err)
	}
	return resp
}

func getBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func seedIssues(t *testing.T, f *fakeStore) {
	t.Helper()
	now := time.Now()
	f.addIssue("Fix flaky test", "the CI job fails sometimes", store.StatusOpen, now.Add(-72*time.Hour), 1)
	f.addIssue("Write docs", "document the API surface", store.StatusClosed, now.Add(-48*time.Hour), 2)
	f.addIssue("Add dark mode", "nice to have", store.StatusOpen, now.Add(-24*time.Hour), 3)
}

/* ---------- tests ---------- */

func TestRootRedirectsToIssues(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := noRedirectClient().Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/issues" {
		t.Fatalf("Location = %q, want /issues", loc)
	}
}

func TestListRendersIssues(t *testing.T) {
	srv, f := newTestServer(t)
	seedIssues(t, f)

	resp, err := srv.Client().Get(srv.URL + "/issues")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	// Open issues shown by default.
	for _, want := range []string{"Fix flaky test", "Add dark mode"} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing %q", want)
		}
	}
	// Closed issue hidden under the default open filter.
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

	resp, err := srv.Client().Get(srv.URL + "/issues?status=closed")
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

	resp, err := srv.Client().Get(srv.URL + "/issues?status=all&q=flaky")
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

	resp, err := srv.Client().Get(srv.URL + "/issues?status=all&label=2")
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

	resp, err := srv.Client().Get(srv.URL + "/issues")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if !strings.Contains(body, "Showing 1–20 of 25 issues") {
		t.Errorf("expected range header, got:\n%s", body)
	}
	if !strings.Contains(body, `href="/issues?page=2&amp;status=open"`) &&
		!strings.Contains(body, "page=2") {
		t.Error("expected a next-page link preserving filters")
	}
	if strings.Contains(body, "page=0") {
		t.Error("should not emit a previous link on page 1")
	}

	resp2, err := srv.Client().Get(srv.URL + "/issues?page=2")
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
	resp, err := srv.Client().Get(srv.URL + "/issues/999")
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
	resp, err := srv.Client().Get(srv.URL + "/issues/abc")
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

	resp, err := srv.Client().Get(srv.URL + "/issues/1")
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

	resp, err := srv.Client().Get(srv.URL + "/issues/1")
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

	// The edit form must keep the raw markdown source, not rendered HTML.
	resp2, err := srv.Client().Get(srv.URL + "/issues/1/edit")
	if err != nil {
		t.Fatal(err)
	}
	body2 := getBody(t, resp2)
	if !strings.Contains(body2, "```") || !strings.Contains(body2, "&lt;script&gt;") {
		t.Error("edit form should preserve the raw markdown source in the textarea")
	}
}

func TestCreateIssueValidationError(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := postForm(t, srv.Client(), srv.URL+"/issues", url.Values{
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
		{"/issues/new", `<p class="field-error field-error-pending" id="f-title-error">Title is required.</p>`},
		{"/issues/1/edit", `<p class="field-error field-error-pending" id="f-title-error">Title is required.</p>`},
		{"/issues/1", `<p class="field-error field-error-pending" id="comment-error">Comment is required.</p>`},
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
	resp := postForm(t, noRedirectClient(), srv.URL+"/issues", url.Values{
		"title":    {"Brand new issue"},
		"body":     {"details here"},
		"label_id": {"1", "3"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/issues/") || loc == "/issues/new" {
		t.Fatalf("Location = %q, want /issues/{id}", loc)
	}

	// The created issue must be fetchable with its labels.
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

	resp := postForm(t, noRedirectClient(), srv.URL+"/issues/1/comments", url.Values{
		"body": {"hello there"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/issues/1#comment-") {
		t.Fatalf("Location = %q, want /issues/1#comment-…", loc)
	}

	resp2, err := srv.Client().Get(srv.URL + "/issues/1")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp2)
	if !strings.Contains(body, "hello there") {
		t.Error("comment should appear on the detail page")
	}
	// Each comment carries a permalink: an id anchor on the <li> and the
	// timestamp linking back to it.
	if !strings.Contains(body, `id="comment-1"`) || !strings.Contains(body, `href="#comment-1"`) {
		t.Error("comment should render with a permalink anchor and link")
	}
}

func TestCreateCommentEmpty(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("Comment target", "", store.StatusOpen, time.Now())

	resp := postForm(t, srv.Client(), srv.URL+"/issues/1/comments", url.Values{
		"body": {""},
	})
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 422 or 400", resp.StatusCode)
	}
	if !strings.Contains(body, "Comment is required") {
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

	// open → closed
	resp := postForm(t, noRedirectClient(), srv.URL+"/issues/1/status", url.Values{
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

	// closed → open
	resp2 := postForm(t, noRedirectClient(), srv.URL+"/issues/1/status", url.Values{
		"status": {"open"},
	})
	resp2.Body.Close()
	got2, _ := f.GetIssue(context.Background(), 1)
	if got2.Status != store.StatusOpen {
		t.Fatalf("status = %q, want open", got2.Status)
	}
}

func TestSetStatusInvalid(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("Bad status", "", store.StatusOpen, time.Now())
	resp := postForm(t, srv.Client(), srv.URL+"/issues/1/status", url.Values{
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

	// GET confirmation page renders WITHOUT deleting.
	resp, err := srv.Client().Get(srv.URL + "/issues/1/delete")
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

	// POST actually deletes and redirects to the list.
	resp2 := postForm(t, noRedirectClient(), srv.URL+"/issues/1/delete", url.Values{})
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp2.StatusCode)
	}
	if loc := resp2.Header.Get("Location"); loc != "/issues" {
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

	resp, err := srv.Client().Get(srv.URL + "/issues/1")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	for _, want := range []string{
		`commandfor="delete-dialog" command="show-modal"`,
		`<dialog id="delete-dialog"`,
		`closedby="any"`,
		`aria-labelledby="delete-dialog-title"`,
		`<form method="post" action="/issues/1/delete"`,
		`commandfor="delete-dialog" command="close" autofocus`,
		`href="/issues/1/delete"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail missing %q", want)
		}
	}
	if strings.Contains(body, "<script") {
		t.Error("detail page must not contain <script>")
	}
}

func TestEditFlow(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("Old title", "old body", store.StatusOpen, time.Now(), 1)

	// Edit form is populated with current values.
	resp, err := srv.Client().Get(srv.URL + "/issues/1/edit")
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

	// POST updates and redirects to the detail page.
	resp2 := postForm(t, noRedirectClient(), srv.URL+"/issues/1/edit", url.Values{
		"title":    {"New title"},
		"body":     {"new body"},
		"label_id": {"2"},
	})
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp2.StatusCode)
	}
	if loc := resp2.Header.Get("Location"); loc != "/issues/1" {
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

	// List page.
	resp, err := srv.Client().Get(srv.URL + "/issues")
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

	// Detail page (title, body and comment).
	resp2, err := srv.Client().Get(srv.URL + "/issues/1")
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

func TestCSRFRejectsCrossSitePost(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("CSRF target", "", store.StatusOpen, time.Now())

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/issues",
		strings.NewReader("title=hi"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}

	// Same-site requests keep working.
	req2, err := http.NewRequest(http.MethodPost, srv.URL+"/issues",
		strings.NewReader("title=ok"))
	if err != nil {
		t.Fatal(err)
	}
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.Header.Set("Sec-Fetch-Site", "same-origin")
	resp2, err := noRedirectClient().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("same-site POST status = %d, want 303", resp2.StatusCode)
	}
}

func TestUnsupportedMethodRenders405(t *testing.T) {
	srv, _ := newTestServer(t)
	// The catch-alls are method-scoped (GET/POST only), so a disallowed
	// method on a known path falls through to the mux's automatic 405.
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/issues", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

func TestUnknownPath404(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/definitely-not-here")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestStaticCSS(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/css") {
		t.Fatalf("Content-Type = %q, want text/css", ct)
	}
	body := getBody(t, resp)
	if !strings.Contains(body, "@view-transition") {
		t.Error("app.css does not opt in to cross-document view transitions")
	}
	// Text must follow the browser/OS text size: no px font sizes, and no
	// root font-size override that would defeat <meta name="text-scale">.
	if m := regexp.MustCompile(`font-size:\s*[0-9.]+px`).FindString(body); m != "" {
		t.Errorf("app.css uses a px font size: %q", m)
	}
	if regexp.MustCompile(`(?s)(?:^|[}\s])(?::root|html)\s*\{[^}]*font-size`).MatchString(body) {
		t.Error("app.css overrides the root font size")
	}
	// content-visibility: auto without a placeholder size collapses skipped
	// elements to 0px and makes the scrollbar jump.
	for _, m := range regexp.MustCompile(`\{[^}]*content-visibility:\s*auto[^}]*\}`).FindAllString(body, -1) {
		if !strings.Contains(m, "contain-intrinsic-") {
			t.Errorf("content-visibility: auto without contain-intrinsic-size: %q", m)
		}
	}
}

func TestLayoutOptsIntoOSTextScale(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/issues")
	if err != nil {
		t.Fatal(err)
	}
	if body := getBody(t, resp); !strings.Contains(body, `<meta name="text-scale" content="scale">`) {
		t.Error(`layout lacks <meta name="text-scale" content="scale">`)
	}
}

func TestNewIssueFormRenders(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/issues/new")
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

	resp, err := srv.Client().Get(srv.URL + "/issues?page=99999")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	// Clamped to the last page: real items plus a correct range header and
	// working pagination — not the misleading empty state.
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
	resp, err := srv.Client().Get(srv.URL + "/issues?page=4611686018427387904")
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

func TestUnknownPathRendersStyled404(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/nonexistent-path")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(body, "Not found") {
		t.Error("expected the styled Not found page")
	}
	if strings.Contains(body, "404 page not found") {
		t.Error("must not render the mux's plaintext 404")
	}
}

func TestIssuesTrailingSlashRendersStyled404(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/issues/")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(body, "Not found") {
		t.Error("expected the styled Not found page")
	}
}

func TestStaticDirectoryListing404(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/static/")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if !strings.Contains(body, "Not found") {
		t.Error("expected the styled Not found page, not a directory listing")
	}
}

func TestUpdateIssueMissingIssue404(t *testing.T) {
	srv, _ := newTestServer(t)
	// A validation-failing POST to a nonexistent issue must 404, not 422.
	resp := postForm(t, srv.Client(), srv.URL+"/issues/999/edit", url.Values{
		"title": {""},
	})
	getBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestSecurityHeaders(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/issues")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if v := resp.Header.Get("X-Content-Type-Options"); v != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", v)
	}
	if v := resp.Header.Get("X-Frame-Options"); v != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", v)
	}
	if v := resp.Header.Get("Referrer-Policy"); v != "same-origin" {
		t.Errorf("Referrer-Policy = %q, want same-origin", v)
	}

	csp := resp.Header.Get("Content-Security-Policy")
	for _, directive := range []string{
		"default-src 'none'",
		"script-src 'none'",
		"style-src 'self'",
		"style-src-attr 'unsafe-inline'", // label badges' style="--label-color"
		"img-src 'self' data:",           // data: is for the favicon
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(csp, directive) {
			t.Errorf("Content-Security-Policy missing %q, got %q", directive, csp)
		}
	}
	if v := resp.Header.Get("Cross-Origin-Opener-Policy"); v != "same-origin-allow-popups" {
		t.Errorf("Cross-Origin-Opener-Policy = %q, want same-origin-allow-popups", v)
	}
	if v := resp.Header.Get("Cross-Origin-Resource-Policy"); v != "same-origin" {
		t.Errorf("Cross-Origin-Resource-Policy = %q, want same-origin", v)
	}
	if v := resp.Header.Get("Permissions-Policy"); v == "" {
		t.Error("Permissions-Policy is not set")
	}
	// Vary must cover the request headers CrossOriginProtection inspects.
	if v := resp.Header.Values("Vary"); !slices.Contains(v, "Sec-Fetch-Site") ||
		!slices.Contains(v, "Origin") {
		t.Errorf("Vary = %v, want Sec-Fetch-Site and Origin", v)
	}
}

func TestDetailLabelLinkUsesStatusAll(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("Labeled issue", "", store.StatusClosed, time.Now(), 1)

	resp, err := srv.Client().Get(srv.URL + "/issues/1")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	// The link must reach the closed issue too, so it carries status=all.
	if !strings.Contains(body, `href="/issues?status=all&amp;label=1"`) {
		t.Errorf("expected label link with status=all, got:\n%s", body)
	}
}

func TestListLinksOmitDefaultStatus(t *testing.T) {
	srv, f := newTestServer(t)
	seedIssues(t, f)

	resp, err := srv.Client().Get(srv.URL + "/issues")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	// "open" is the default filter; generated links keep URLs clean.
	if strings.Contains(body, "status=open") {
		t.Error("generated links must not carry the redundant status=open")
	}
	if !strings.Contains(body, `href="/issues"`) {
		t.Error("expected the Open tab link to be bare /issues")
	}

	// Non-default statuses are still preserved in generated links.
	resp2, err := srv.Client().Get(srv.URL + "/issues?status=closed")
	if err != nil {
		t.Fatal(err)
	}
	body2 := getBody(t, resp2)
	if !strings.Contains(body2, "status=closed") {
		t.Error("closed-filter links must keep status=closed")
	}
}
