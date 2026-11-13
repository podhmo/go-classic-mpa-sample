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
	if loc2 := resp.Header.Get("Location"); !strings.HasPrefix(loc2, loc) {
		t.Fatalf("comment redirect = %q, want %s…", loc2, loc)
	}
	_, _, body := e2eGet(t, srv, loc)
	if !strings.Contains(body, "Comments (1)") {
		t.Error("detail should show a comment count of 1")
	}
	if !strings.Contains(body, `comment-body">hello there<`) {
		t.Error("comment should be rendered in the comments section")
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
