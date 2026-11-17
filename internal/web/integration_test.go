// End-to-end tests exercising the real application stack: a SQLite file
// store, the Go 1.22 method+path mux wrapped in CrossOriginProtection and
// security-header middleware, and the embedded html/template pages — driven
// over a live HTTP server. The fakeStore tests in web_test.go cover
// handler-level logic; these prove the whole pipeline works together.
package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

var _ Store = (*store.Store)(nil)

// newRealServer boots the full application against a fresh, empty SQLite
// database (no SeedDemoData) and returns the server plus the store handle
// for fixtures that have no HTTP endpoint.
func newRealServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := httptest.NewServer(New(st))
	t.Cleanup(srv.Close)
	return srv, st
}

// e2eCreateIssue POSTs the new-issue form, asserts the PRG contract
// (303 → /issues/{id}) and returns the new issue's detail path.
func e2eCreateIssue(t *testing.T, srv *httptest.Server, form url.Values) string {
	t.Helper()
	resp := postForm(t, noRedirectClient(), srv.URL+"/w/default/issues", form)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /issues status = %d, want 303", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/w/default/issues/") || loc == "/w/default/issues/new" {
		t.Fatalf("POST /issues Location = %q, want /issues/{id}", loc)
	}
	return loc
}

// e2eGet GETs path (following redirects) and returns the status, headers and
// decoded body.
func e2eGet(t *testing.T, srv *httptest.Server, path string) (int, http.Header, string) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp.StatusCode, resp.Header, getBody(t, resp)
}

// TestE2EIssueLifecycle walks one issue through its whole life against a
// real database: create → list → detail → edit → close → delete, asserting
// each transition through the rendered pages a browser would see.
func TestE2EIssueLifecycle(t *testing.T) {
	srv, st := newRealServer(t)
	ctx := context.Background()

	// This scenario needs a label to attach. Labels have no creation
	// endpoint, so this is the "test needs seeds" case: SeedDemoData
	// inserts the label set (plus demo issues, which only add realistic
	// noise to the list assertions).
	if err := st.SeedDemoData(ctx); err != nil {
		t.Fatalf("SeedDemoData: %v", err)
	}
	labels, err := st.ListLabels(ctx)
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) == 0 {
		t.Fatal("ListLabels returned nothing after seeding")
	}
	label := labels[0]

	// Create through the form: 303 → /issues/{id}.
	loc := e2eCreateIssue(t, srv, url.Values{
		"title":    {"E2E issue"},
		"body":     {"first body"},
		"label_id": {strconv.FormatInt(label.ID, 10)},
	})
	num := "#" + strings.TrimPrefix(loc, "/w/default/issues/")

	// The default (open) list shows the new issue by title and number.
	status, _, body := e2eGet(t, srv, "/w/default/issues")
	if status != http.StatusOK {
		t.Fatalf("GET /issues status = %d, want 200", status)
	}
	for _, want := range []string{"E2E issue", num} {
		if !strings.Contains(body, want) {
			t.Errorf("issue list missing %q", want)
		}
	}

	// The detail page shows the body, the attached label and the open badge.
	_, _, body = e2eGet(t, srv, loc)
	for _, want := range []string{"first body", label.Name, "badge badge-open"} {
		if !strings.Contains(body, want) {
			t.Errorf("issue detail missing %q", want)
		}
	}

	// Edit: rename, replace the body and drop all labels.
	resp := postForm(t, noRedirectClient(), srv.URL+loc+"/edit", url.Values{
		"title": {"Renamed"},
		"body":  {"second body"},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST %s/edit status = %d, want 303", loc, resp.StatusCode)
	}
	_, _, body = e2eGet(t, srv, loc)
	for _, want := range []string{"Renamed", "second body"} {
		if !strings.Contains(body, want) {
			t.Errorf("edited detail missing %q", want)
		}
	}
	for _, gone := range []string{"E2E issue", "first body", `class="label-badge"`} {
		if strings.Contains(body, gone) {
			t.Errorf("edited detail should not contain %q", gone)
		}
	}

	resp = postForm(t, noRedirectClient(), srv.URL+loc+"/status", url.Values{
		"status": {"closed"},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST %s/status status = %d, want 303", loc, resp.StatusCode)
	}
	if loc2 := resp.Header.Get("Location"); loc2 != loc {
		t.Fatalf("status redirect = %q, want %q", loc2, loc)
	}

	_, _, body = e2eGet(t, srv, loc)
	if !strings.Contains(body, "badge badge-closed") || !strings.Contains(body, ">Closed<") {
		t.Error("detail should show the Closed badge")
	}

	_, _, body = e2eGet(t, srv, "/w/default/issues")
	if strings.Contains(body, `href="`+loc+`"`) {
		t.Error("closed issue should not appear in the default (open) list")
	}
	_, _, body = e2eGet(t, srv, "/w/default/issues?status=closed")
	if !strings.Contains(body, `href="`+loc+`"`) || !strings.Contains(body, "Renamed") {
		t.Error("closed issue should appear on the closed tab")
	}

	status, _, body = e2eGet(t, srv, loc+"/delete")
	if status != http.StatusOK {
		t.Fatalf("GET %s/delete status = %d, want 200", loc, status)
	}
	if !strings.Contains(body, "Are you sure") || !strings.Contains(body, "Renamed") {
		t.Error("delete confirm page should name the issue")
	}
	if status, _, _ := e2eGet(t, srv, loc); status != http.StatusOK {
		t.Fatalf("GET delete confirm must not delete; detail status = %d", status)
	}

	resp = postForm(t, noRedirectClient(), srv.URL+loc+"/delete", url.Values{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST %s/delete status = %d, want 303", loc, resp.StatusCode)
	}
	if loc2 := resp.Header.Get("Location"); loc2 != "/w/default/issues" {
		t.Fatalf("delete redirect = %q, want /issues", loc2)
	}
	status, hdr, body := e2eGet(t, srv, loc)
	if status != http.StatusNotFound {
		t.Fatalf("deleted issue status = %d, want 404", status)
	}
	if ct := hdr.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("404 Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(body, "Not found") {
		t.Error("expected the styled Not found page")
	}
}

// TestE2ECommentsAndValidation exercises the comment endpoints against a
// fresh, empty database: happy path, validation re-rendering with draft
// state, a 404 on a missing issue, and end-to-end HTML escaping.
func TestE2ECommentsAndValidation(t *testing.T) {
	srv, _ := newRealServer(t)

	loc := e2eCreateIssue(t, srv, url.Values{
		"title": {"Comment target"},
		"body":  {"issue body"},
	})

	resp := postForm(t, noRedirectClient(), srv.URL+loc+"/comments", url.Values{
		"body": {"hello there"},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST %s/comments status = %d, want 303", loc, resp.StatusCode)
	}
	if loc2 := resp.Header.Get("Location"); !strings.HasPrefix(loc2, loc+"#comment-") {
		t.Fatalf("comment redirect = %q, want %s#comment-…", loc2, loc)
	}
	_, _, body := e2eGet(t, srv, loc)
	if !strings.Contains(body, `data-comment-count>1<`) {
		t.Error("detail should show a comment count of 1")
	}
	if !strings.Contains(body, `markdown-body"><p>hello there</p>`) {
		t.Error("comment should be rendered in the comments section")
	}
	if !strings.Contains(body, `id="comment-`) || !strings.Contains(body, `href="#comment-`) {
		t.Error("comment should render with a permalink anchor and link")
	}

	for _, tc := range []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := postForm(t, srv.Client(), srv.URL+loc+"/comments", url.Values{
				"body": {tc.body},
			})
			page := getBody(t, resp)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", resp.StatusCode)
			}
			if !strings.Contains(page, "Body is required") {
				t.Error("expected the comment validation error")
			}
			if !strings.Contains(page, `aria-invalid="true"`) {
				t.Error("expected the textarea to be marked invalid")
			}
			// The handler trims the submitted body before storing it as
			// the draft (strings.TrimSpace in createComment), so a
			// whitespace-only submission also re-renders an empty textarea
			// rather than preserving the raw whitespace.
			if !strings.Contains(page, "></textarea>") {
				t.Error("expected an empty comment textarea draft")
			}
		})
	}
	_, _, body = e2eGet(t, srv, loc)
	if !strings.Contains(body, `data-comment-count>1<`) || strings.Contains(body, `data-comment-count>2<`) {
		t.Error("invalid comments must not be persisted")
	}

	resp = postForm(t, srv.Client(), srv.URL+"/w/default/issues/999/comments", url.Values{
		"body": {"valid comment"},
	})
	getBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	xssLoc := e2eCreateIssue(t, srv, url.Values{
		"title": {"<script>alert(1)</script>"},
	})
	status, _, body := e2eGet(t, srv, xssLoc)
	if status != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", xssLoc, status)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("expected the escaped payload in the detail page")
	}
	if strings.Contains(body, "<script>alert") {
		t.Error("detail page must not render a literal <script> tag")
	}
}

// newSeededServer is newRealServer plus the demo fixture (SeedDemoData):
// labels Bug, Documentation, Enhancement and Feature, and seven backdated
// issues — five open and two closed, several with comments. The concrete
// titles quoted below come straight from internal/store/seed.go.
func newSeededServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	srv, st := newRealServer(t)
	if err := st.SeedDemoData(context.Background()); err != nil {
		t.Fatalf("SeedDemoData: %v", err)
	}
	return srv, st
}

// postFormWithHeaders is postForm plus explicit request headers — needed to
// drive the CrossOriginProtection middleware (Sec-Fetch-Site, Origin),
// which client.PostForm cannot set.
func postFormWithHeaders(t *testing.T, rawurl string, form url.Values, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, rawurl, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("POST %s: %v", rawurl, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", rawurl, err)
	}
	return resp
}

// TestE2EListInteractions drives every control on the issue list — status
// tabs, keyword search, the label filter, sort order and pagination —
// against the seeded database, asserting on the rendered rows.
func TestE2EListInteractions(t *testing.T) {
	srv, st := newSeededServer(t)
	ctx := context.Background()

	openSeedTitles := []string{
		"Crash when opening an empty project",
		"Add dark mode to the settings page",
		// html/template escapes "+" as "&#43;", so match the title up
		// to the "+" rather than the full "10,000+ records" text.
		"Search slows down with 10,000",
		"Improve onboarding for first-time users",
		"Export issues as CSV",
	}
	closedSeedTitles := []string{
		"Typo in the getting-started guide",
		"Update screenshots in the user manual",
	}
	// Rows are counted via the issue-row <li>; the quoted class cannot
	// false-match the issue-row-main / issue-row-meta children.
	countRows := func(body string) int { return strings.Count(body, `class="issue-row"`) }

	status, _, body := e2eGet(t, srv, "/w/default/issues")
	if status != http.StatusOK {
		t.Fatalf("GET /issues status = %d, want 200", status)
	}
	if n := countRows(body); n != len(openSeedTitles) {
		t.Errorf("default list rendered %d rows, want %d", n, len(openSeedTitles))
	}
	for _, title := range openSeedTitles {
		if !strings.Contains(body, title) {
			t.Errorf("default list missing seeded title %q", title)
		}
	}
	for _, title := range closedSeedTitles {
		if strings.Contains(body, title) {
			t.Errorf("default list should not show closed issue %q", title)
		}
	}

	_, _, body = e2eGet(t, srv, "/w/default/issues?status=closed")
	if n := countRows(body); n != len(closedSeedTitles) {
		t.Errorf("closed list rendered %d rows, want %d", n, len(closedSeedTitles))
	}
	for _, title := range closedSeedTitles {
		if !strings.Contains(body, title) {
			t.Errorf("closed list missing seeded title %q", title)
		}
	}
	for _, title := range openSeedTitles {
		if strings.Contains(body, title) {
			t.Errorf("closed list should not show open issue %q", title)
		}
	}

	// q searches bodies, not just titles: "trigram" appears only in the
	// body of "Search slows down with 10,000+ records" (seed.go).
	_, _, body = e2eGet(t, srv, "/w/default/issues?q=trigram")
	if n := countRows(body); n != 1 {
		t.Errorf("q=trigram rendered %d rows, want 1", n)
	}
	if !strings.Contains(body, "Search slows down with 10,000") {
		t.Error("q=trigram should match via the issue body")
	}
	if strings.Contains(body, "Crash when opening an empty project") {
		t.Error("q=trigram must not match unrelated issues")
	}

	// Sort flips the row order. Seed ages are fixed relative to now:
	// "Update screenshots…" (8h) is the newest seed and "Crash…" (6.5d) the
	// oldest — assert their relative positions, not absolute offsets.
	_, _, newestFirst := e2eGet(t, srv, "/w/default/issues?status=all")
	_, _, oldestFirst := e2eGet(t, srv, "/w/default/issues?status=all&sort=oldest")
	shotsIdx := strings.Index(newestFirst, "Update screenshots in the user manual")
	crashIdx := strings.Index(newestFirst, "Crash when opening an empty project")
	if shotsIdx < 0 || crashIdx < 0 {
		t.Error("status=all should list both boundary seeds")
	} else if shotsIdx > crashIdx {
		t.Error("newest-first should list the newest seed before the oldest")
	}
	shotsIdx = strings.Index(oldestFirst, "Update screenshots in the user manual")
	crashIdx = strings.Index(oldestFirst, "Crash when opening an empty project")
	if shotsIdx < 0 || crashIdx < 0 {
		t.Error("sort=oldest should list both boundary seeds")
	} else if crashIdx > shotsIdx {
		t.Error("oldest-first should list the oldest seed before the newest")
	}

	// The label filter keeps the default open status: the Bug-labeled open
	// seeds are "Crash…" and "Search slows…" only.
	labels, err := st.ListLabels(ctx)
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	var bugID int64
	for _, l := range labels {
		if l.Name == "Bug" {
			bugID = l.ID
		}
	}
	if bugID == 0 {
		t.Fatal("seeded Bug label not found")
	}
	_, _, body = e2eGet(t, srv, "/w/default/issues?label="+strconv.FormatInt(bugID, 10))
	if n := countRows(body); n != 2 {
		t.Errorf("label=Bug rendered %d rows, want 2", n)
	}
	for _, title := range []string{
		"Crash when opening an empty project",
		"Search slows down with 10,000",
	} {
		if !strings.Contains(body, title) {
			t.Errorf("Bug filter missing %q", title)
		}
	}
	for _, title := range []string{
		"Add dark mode to the settings page",    // open, but not Bug-labeled
		"Typo in the getting-started guide",     // closed, not Bug-labeled
		"Update screenshots in the user manual", // closed, not Bug-labeled
	} {
		if strings.Contains(body, title) {
			t.Errorf("Bug filter should not list %q", title)
		}
	}

	// Pagination: seven seeds fit on a single page, so add twenty more
	// issues to create a real second page (perPage = 20 → 27 total).
	for i := 1; i <= 20; i++ {
		e2eCreateIssue(t, srv, url.Values{"title": {"Filler issue " + strconv.Itoa(i)}})
	}

	status, _, body = e2eGet(t, srv, "/w/default/issues?status=all&page=999")
	if status != http.StatusOK {
		t.Fatalf("GET page=999 status = %d, want 200", status)
	}
	if !strings.Contains(body, "Showing 21–27 of 27 issues") || !strings.Contains(body, "Page 2 of 2") {
		t.Error("page=999 should clamp to the last page")
	}
	if !strings.Contains(body, "Crash when opening an empty project") {
		t.Error("clamped last page should contain the oldest seeded issue")
	}
	if strings.Contains(body, "Filler issue") || strings.Contains(body, "empty-state") {
		t.Error("clamped last page should hold only the oldest rows")
	}

	status, _, body = e2eGet(t, srv, "/w/default/issues?status=all&page=abc")
	if status != http.StatusOK {
		t.Fatalf("GET page=abc status = %d, want 200", status)
	}
	if !strings.Contains(body, "Showing 1–20 of 27 issues") {
		t.Error("page=abc should fall back to page 1")
	}
	if !strings.Contains(body, "Filler issue") || strings.Contains(body, "Crash when opening an empty project") {
		t.Error("page 1 should hold the twenty newest issues")
	}
}

// TestE2ECSRF proves the CrossOriginProtection middleware guards the real
// stack: cross-site browser POSTs are rejected with 403 before reaching the
// handler, while same-site traffic keeps working.
func TestE2ECSRF(t *testing.T) {
	srv, _ := newRealServer(t) // empty store: any forged create would be visible

	resp := postFormWithHeaders(t, srv.URL+"/w/default/issues", url.Values{
		"title": {"Cross-site forged issue"},
		"body":  {"forged"},
	}, map[string]string{"Sec-Fetch-Site": "cross-site"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site POST status = %d, want 403", resp.StatusCode)
	}

	// Without Sec-Fetch-Site the middleware falls back to comparing the
	// Origin host with the request host; a foreign Origin is rejected too.
	resp = postFormWithHeaders(t, srv.URL+"/w/default/issues", url.Values{
		"title": {"Origin forged issue"},
	}, map[string]string{"Origin": "http://evil.example"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign-Origin POST status = %d, want 403", resp.StatusCode)
	}

	_, _, body := e2eGet(t, srv, "/w/default/issues")
	for _, forged := range []string{"Cross-site forged issue", "Origin forged issue"} {
		if strings.Contains(body, forged) {
			t.Errorf("rejected POST must not create %q", forged)
		}
	}
	if !strings.Contains(body, "empty-state") {
		t.Error("list should still render the empty state after rejected POSTs")
	}

	resp = postFormWithHeaders(t, srv.URL+"/w/default/issues", url.Values{
		"title": {"Same-origin issue"},
	}, map[string]string{"Sec-Fetch-Site": "same-origin"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("same-origin POST status = %d, want 303", resp.StatusCode)
	}
	if _, _, body := e2eGet(t, srv, "/w/default/issues"); !strings.Contains(body, "Same-origin issue") {
		t.Error("same-origin POST should have created the issue")
	}
}

// TestE2ESeededDataRenders checks that the seed fixture renders faithfully:
// every seeded title with its label badges on the list, and seeded labels
// plus comments on a detail page.
func TestE2ESeededDataRenders(t *testing.T) {
	srv, st := newSeededServer(t)
	ctx := context.Background()

	status, _, body := e2eGet(t, srv, "/w/default/issues?status=all")
	if status != http.StatusOK {
		t.Fatalf("GET /w/default/issues?status=all status = %d, want 200", status)
	}
	if n := strings.Count(body, `class="issue-row"`); n < 6 {
		t.Errorf("seeded list rendered %d rows, want at least 6", n)
	}
	if !strings.Contains(body, `class="label-badge"`) {
		t.Error("seeded rows should render label badges")
	}
	if !strings.Contains(body, "--label-color: #d73a4a") {
		t.Error("expected the Bug label badge color")
	}
	if !strings.Contains(body, `<meta name="color-scheme" content="light dark">`) {
		t.Error("layout should declare light and dark color schemes")
	}
	for _, title := range []string{
		"Crash when opening an empty project",
		"Add dark mode to the settings page",
		"Typo in the getting-started guide",
		// "+" renders as "&#43;" — match the unescaped prefix.
		"Search slows down with 10,000",
		"Improve onboarding for first-time users",
		"Export issues as CSV",
		"Update screenshots in the user manual",
	} {
		if !strings.Contains(body, title) {
			t.Errorf("seeded list missing title %q", title)
		}
	}

	// Pick a seeded issue through the store instead of assuming id=1: the
	// oldest row is "Crash when opening an empty project" (age 6.5d).
	issues, _, err := st.ListIssues(ctx, 1, store.ListFilter{Status: "all", Sort: "oldest", PerPage: 1})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) == 0 {
		t.Fatal("expected seeded issues")
	}
	seeded := issues[0]
	if seeded.Title != "Crash when opening an empty project" {
		t.Fatalf("oldest seed = %q, want the Crash issue", seeded.Title)
	}
	status, _, body = e2eGet(t, srv, "/w/default/issues/"+strconv.FormatInt(seeded.ID, 10))
	if status != http.StatusOK {
		t.Fatalf("GET detail status = %d, want 200", status)
	}
	for _, want := range []string{
		"Crash when opening an empty project",
		`class="label-badge"`, ">Bug<",
		`id="comments"`, `data-comment-count>2<`,
		"Reproduced on macOS and Linux. Windows seems unaffected.",
		"Bisected to the changes in the file scanner last week.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("seeded detail missing %q", want)
		}
	}
}

// The JSON API only exists when a Bearer token is configured
// (web.WithAPIToken); without one, /api/v1/* must fall through to the
// app's ordinary 404 — there is no unauthenticated API surface at all.

func TestAPINotMountedWithoutToken(t *testing.T) {
	srv, _ := newRealServer(t) // New(st) — no WithAPIToken
	status, _, body := e2eGet(t, srv, "/api/v1/w/default/issues")
	if status != http.StatusNotFound {
		t.Fatalf("GET /api/v1/issues without token = %d, want 404", status)
	}
	// The generic 404 page is HTML — the API must not leak a JSON handler.
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Fatalf("expected HTML 404 page, got %q", body[:min(len(body), 120)])
	}
}

func TestAPIMountedWithToken(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := httptest.NewServer(New(st, WithAPIToken("secret-token")))
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/api/v1/w/default/issues")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized GET = %d, want 401", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("401 Content-Type = %q, want application/json", ct)
	}

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/w/default/issues", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorized GET = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Issues []any `json:"issues"`
		Total  int   `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

// With only per-user api_tokens as credentials (no shared env token) the
// API is still reachable via WithAPIEnabled, and requests resolve to the
// token's user. Without it, /api/v1 stays unmounted even when tokens
// exist in the database.
func TestAPIMountedWithUserTokensOnly(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	uid, err := st.CreateUser(t.Context(), "erin", "irrelevant-hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, err := st.CreateAPIToken(t.Context(), uid)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	// Mount gated only on the env token would leave the API unreachable.
	srv := httptest.NewServer(New(st))
	t.Cleanup(srv.Close)
	status, _, _ := e2eGet(t, srv, "/api/v1/labels")
	if status != http.StatusNotFound {
		t.Fatalf("GET /api/v1/labels unmounted = %d, want 404", status)
	}

	srv2 := httptest.NewServer(New(st, WithAPIEnabled(true)))
	t.Cleanup(srv2.Close)
	req, _ := http.NewRequest("GET", srv2.URL+"/api/v1/labels", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv2.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("user-token GET = %d, want 200", resp.StatusCode)
	}
}
