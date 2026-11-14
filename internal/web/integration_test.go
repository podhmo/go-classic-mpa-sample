// End-to-end tests exercising the real application stack: a SQLite file
// store, the Go 1.22 method+path mux wrapped in CrossOriginProtection and
// security-header middleware, and the embedded html/template pages — driven
// over a live HTTP server. The fakeStore tests in web_test.go cover
// handler-level logic; these prove the whole pipeline works together.
package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// The real *store.Store must satisfy the consumer-side Store interface,
// same as the fake does.
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
	resp := postForm(t, noRedirectClient(), srv.URL+"/issues", form)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /issues status = %d, want 303", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/issues/") || loc == "/issues/new" {
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
	num := "#" + strings.TrimPrefix(loc, "/issues/")

	// The default (open) list shows the new issue by title and number.
	status, _, body := e2eGet(t, srv, "/issues")
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

	// Close via the status form: 303, the badge flips to Closed, and the
	// issue moves off the default list onto the closed tab.
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

	_, _, body = e2eGet(t, srv, "/issues")
	if strings.Contains(body, `href="`+loc+`"`) {
		t.Error("closed issue should not appear in the default (open) list")
	}
	_, _, body = e2eGet(t, srv, "/issues?status=closed")
	if !strings.Contains(body, `href="`+loc+`"`) || !strings.Contains(body, "Renamed") {
		t.Error("closed issue should appear on the closed tab")
	}

	// The GET confirmation page renders without deleting anything.
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

	// POST deletes, redirects to the list, and the detail page becomes the
	// styled 404.
	resp = postForm(t, noRedirectClient(), srv.URL+loc+"/delete", url.Values{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST %s/delete status = %d, want 303", loc, resp.StatusCode)
	}
	if loc2 := resp.Header.Get("Location"); loc2 != "/issues" {
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

	// A valid comment is stored and rendered in the comments section.
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
	if !strings.Contains(body, "Comments (1)") {
		t.Error("detail should show a comment count of 1")
	}
	if !strings.Contains(body, `markdown-body"><p>hello there</p>`) {
		t.Error("comment should be rendered in the comments section")
	}
	if !strings.Contains(body, `id="comment-`) || !strings.Contains(body, `href="#comment-`) {
		t.Error("comment should render with a permalink anchor and link")
	}

	// Empty and whitespace-only comments are rejected with 422: the detail
	// page re-renders with the error and the draft, and nothing persists.
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
			if !strings.Contains(page, "Comment is required") {
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
	// The rejected comments never reached the database.
	_, _, body = e2eGet(t, srv, loc)
	if !strings.Contains(body, "Comments (1)") || strings.Contains(body, "Comments (2)") {
		t.Error("invalid comments must not be persisted")
	}

	// Commenting on a missing issue 404s rather than 422s or 500s.
	resp = postForm(t, srv.Client(), srv.URL+"/issues/999/comments", url.Values{
		"body": {"valid comment"},
	})
	getBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	// Markup in a submitted title travels form → store → html/template and
	// comes out escaped end to end.
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

/* ---------- second round: seeded fixtures, CSRF, pagination ---------- */

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

	// The default list is the open tab.
	status, _, body := e2eGet(t, srv, "/issues")
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

	// The closed tab shows the complement.
	_, _, body = e2eGet(t, srv, "/issues?status=closed")
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
	_, _, body = e2eGet(t, srv, "/issues?q=trigram")
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
	_, _, newestFirst := e2eGet(t, srv, "/issues?status=all")
	_, _, oldestFirst := e2eGet(t, srv, "/issues?status=all&sort=oldest")
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
	_, _, body = e2eGet(t, srv, "/issues?label="+strconv.FormatInt(bugID, 10))
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

	// An out-of-range page clamps to the last page — the oldest rows, i.e.
	// all seven seeds — instead of the misleading empty state.
	status, _, body = e2eGet(t, srv, "/issues?status=all&page=999")
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

	// A non-numeric page falls back to page 1 — the twenty newest rows,
	// which are all fillers (seeds are backdated by hours or days).
	status, _, body = e2eGet(t, srv, "/issues?status=all&page=abc")
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

	// A cross-site POST is rejected.
	resp := postFormWithHeaders(t, srv.URL+"/issues", url.Values{
		"title": {"Cross-site forged issue"},
		"body":  {"forged"},
	}, map[string]string{"Sec-Fetch-Site": "cross-site"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site POST status = %d, want 403", resp.StatusCode)
	}

	// Without Sec-Fetch-Site the middleware falls back to comparing the
	// Origin host with the request host; a foreign Origin is rejected too.
	resp = postFormWithHeaders(t, srv.URL+"/issues", url.Values{
		"title": {"Origin forged issue"},
	}, map[string]string{"Origin": "http://evil.example"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign-Origin POST status = %d, want 403", resp.StatusCode)
	}

	// Both rejections happened before the handler: the store is still empty.
	_, _, body := e2eGet(t, srv, "/issues")
	for _, forged := range []string{"Cross-site forged issue", "Origin forged issue"} {
		if strings.Contains(body, forged) {
			t.Errorf("rejected POST must not create %q", forged)
		}
	}
	if !strings.Contains(body, "empty-state") {
		t.Error("list should still render the empty state after rejected POSTs")
	}

	// Same-origin fetch metadata passes through to the handler.
	resp = postFormWithHeaders(t, srv.URL+"/issues", url.Values{
		"title": {"Same-origin issue"},
	}, map[string]string{"Sec-Fetch-Site": "same-origin"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("same-origin POST status = %d, want 303", resp.StatusCode)
	}
	if _, _, body := e2eGet(t, srv, "/issues"); !strings.Contains(body, "Same-origin issue") {
		t.Error("same-origin POST should have created the issue")
	}
}

// TestE2ESeededDataRenders checks that the seed fixture renders faithfully:
// every seeded title with its label badges on the list, and seeded labels
// plus comments on a detail page.
func TestE2ESeededDataRenders(t *testing.T) {
	srv, st := newSeededServer(t)
	ctx := context.Background()

	status, _, body := e2eGet(t, srv, "/issues?status=all")
	if status != http.StatusOK {
		t.Fatalf("GET /issues?status=all status = %d, want 200", status)
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
	issues, _, err := st.ListIssues(ctx, store.ListFilter{Status: "all", Sort: "oldest", PerPage: 1})
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
	status, _, body = e2eGet(t, srv, "/issues/"+strconv.FormatInt(seeded.ID, 10))
	if status != http.StatusOK {
		t.Fatalf("GET detail status = %d, want 200", status)
	}
	for _, want := range []string{
		"Crash when opening an empty project",
		`class="label-badge"`, ">Bug<",
		`id="comments"`, "Comments (2)",
		"Reproduced on macOS and Linux. Windows seems unaffected.",
		"Bisected to the changes in the file scanner last week.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("seeded detail missing %q", want)
		}
	}
}
