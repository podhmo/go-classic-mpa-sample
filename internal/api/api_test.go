// Handler tests for the /api/v1 JSON API. They run against a real SQLite
// store (fresh file DB per test) — the API is a thin JSON layer over the
// store, so the meaningful coverage is the wire contract: auth, status
// codes, envelope shapes, and per-field validation errors. The mount-time
// fail-closed behavior (no token → no /api/v1 at all) is covered by the
// web package's integration tests.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

const testToken = "test-token-1234"

// newServer boots the API handler over a fresh store and returns the
// server plus the store handle (needed to seed labels/categories that
// have no API endpoint).
func newServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := httptest.NewServer(New(st, testToken))
	t.Cleanup(srv.Close)
	return srv, st
}

// do sends an authorized JSON request and returns status + decoded body.
func do(t *testing.T, srv *httptest.Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != "" {
		rd = bytes.NewReader([]byte(body))
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if resp.ContentLength != 0 {
		json.NewDecoder(resp.Body).Decode(&out)
	}
	return resp.StatusCode, out
}

// createIssue POSTs an issue and returns its id.
func createIssue(t *testing.T, srv *httptest.Server, body string) int64 {
	t.Helper()
	code, out := do(t, srv, "POST", "/api/v1/issues", body)
	if code != http.StatusCreated {
		t.Fatalf("POST /api/v1/issues status = %d, want 201 (body %v)", code, out)
	}
	return int64(out["issue"].(map[string]any)["id"].(float64))
}

func TestAuthRequired(t *testing.T) {
	srv, _ := newServer(t)

	for _, auth := range []string{"", "Bearer wrong", "bearer " + testToken, "Basic " + testToken} {
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/issues", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("Authorization %q → status %d, want 401", auth, resp.StatusCode)
		}
	}

	code, _ := do(t, srv, "GET", "/api/v1/issues", "")
	if code != http.StatusOK {
		t.Fatalf("authorized GET → %d, want 200", code)
	}
	// Unauthorized responses are JSON, not HTML.
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/issues", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("401 Content-Type = %q, want application/json", ct)
	}
}

func TestIssueLifecycle(t *testing.T) {
	srv, _ := newServer(t)

	// create
	code, out := do(t, srv, "POST", "/api/v1/issues", `{"title":"first","body":"hello"}`)
	if code != http.StatusCreated {
		t.Fatalf("create status = %d (%v)", code, out)
	}
	issue := out["issue"].(map[string]any)
	if issue["status"] != "open" || issue["title"] != "first" {
		t.Fatalf("unexpected issue DTO: %v", issue)
	}
	id := int64(issue["id"].(float64))

	// detail carries comments in one call
	code, out = do(t, srv, "GET", fmt.Sprintf("/api/v1/issues/%d", id), "")
	if code != http.StatusOK {
		t.Fatalf("get status = %d", code)
	}
	if cs, ok := out["comments"].([]any); !ok || len(cs) != 0 {
		t.Fatalf("comments = %v, want empty array", out["comments"])
	}

	// list filter
	code, out = do(t, srv, "GET", "/api/v1/issues?status=open&q=first", "")
	if code != http.StatusOK || out["total"].(float64) != 1 {
		t.Fatalf("list = %d %v", code, out)
	}

	// patch only title; body must be preserved
	code, out = do(t, srv, "PATCH", fmt.Sprintf("/api/v1/issues/%d", id), `{"title":"renamed"}`)
	if code != http.StatusOK {
		t.Fatalf("patch status = %d (%v)", code, out)
	}
	if got := out["issue"].(map[string]any); got["title"] != "renamed" || got["body"] != "hello" {
		t.Fatalf("patch lost body: %v", got)
	}

	// status toggle
	code, out = do(t, srv, "POST", fmt.Sprintf("/api/v1/issues/%d/status", id), `{"status":"closed"}`)
	if code != http.StatusOK || out["issue"].(map[string]any)["status"] != "closed" {
		t.Fatalf("status = %d %v", code, out)
	}
	code, out = do(t, srv, "GET", "/api/v1/issues?status=open", "")
	if out["total"].(float64) != 0 {
		t.Fatalf("open list should be empty: %v", out)
	}

	// comment
	code, out = do(t, srv, "POST", fmt.Sprintf("/api/v1/issues/%d/comments", id), `{"body":"me too"}`)
	if code != http.StatusCreated || out["comment"].(map[string]any)["body"] != "me too" {
		t.Fatalf("comment = %d %v", code, out)
	}
	code, out = do(t, srv, "GET", fmt.Sprintf("/api/v1/issues/%d", id), "")
	if n := len(out["comments"].([]any)); n != 1 {
		t.Fatalf("comments = %d, want 1", n)
	}

	// delete
	code, _ = do(t, srv, "DELETE", fmt.Sprintf("/api/v1/issues/%d", id), "")
	if code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", code)
	}
	code, _ = do(t, srv, "GET", fmt.Sprintf("/api/v1/issues/%d", id), "")
	if code != http.StatusNotFound {
		t.Fatalf("deleted issue = %d, want 404", code)
	}
}

func TestIssueValidation(t *testing.T) {
	srv, _ := newServer(t)

	// missing title → 422 with fields.title
	code, out := do(t, srv, "POST", "/api/v1/issues", `{"title":"  ","body":"x"}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d (%v)", code, out)
	}
	fields := out["error"].(map[string]any)["fields"].(map[string]any)
	if fields["title"] == nil {
		t.Fatalf("fields = %v, want title error", fields)
	}

	// malformed JSON → 400
	code, _ = do(t, srv, "POST", "/api/v1/issues", `{"title":`)
	if code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", code)
	}

	// unknown label name → 422 fields.labels
	code, out = do(t, srv, "POST", "/api/v1/issues", `{"title":"t","labels":["nope"]}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown label = %d", code)
	}
	if out["error"].(map[string]any)["fields"].(map[string]any)["labels"] == nil {
		t.Fatalf("fields = %v, want labels error", out)
	}

	// labels endpoint returns the array envelope
	code, out = do(t, srv, "GET", "/api/v1/labels", "")
	if code != http.StatusOK {
		t.Fatalf("labels = %d", code)
	}
	if _, ok := out["labels"].([]any); !ok {
		t.Fatalf("labels payload = %v", out)
	}

	// invalid status value → 422
	id := createIssue(t, srv, `{"title":"t"}`)
	code, out = do(t, srv, "POST", fmt.Sprintf("/api/v1/issues/%d/status", id), `{"status":"wip"}`)
	if code != http.StatusUnprocessableEntity || out["error"].(map[string]any)["fields"].(map[string]any)["status"] == nil {
		t.Fatalf("status = %d %v", code, out)
	}
}

func TestWrongMethodAndTrailingJSON(t *testing.T) {
	srv, _ := newServer(t)

	// Known path + wrong method → 405 JSON with Allow header.
	req, _ := http.NewRequest("PATCH", srv.URL+"/api/v1/issues/1/status", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("PATCH status route = %d, want 405", resp.StatusCode)
	}
	if resp.Header.Get("Allow") != "POST" {
		t.Fatalf("Allow = %q, want POST", resp.Header.Get("Allow"))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("405 Content-Type = %q", ct)
	}

	// Trailing bytes after a complete JSON object → 400.
	code, _ := do(t, srv, "POST", "/api/v1/issues", `{"title":"x"} {"title":"y"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("trailing payload = %d, want 400", code)
	}
	code, _ = do(t, srv, "POST", "/api/v1/issues", `{"title":"x"}garbage`)
	if code != http.StatusBadRequest {
		t.Fatalf("trailing garbage = %d, want 400", code)
	}
}

func TestDiscussionLifecycle(t *testing.T) {
	srv, st := newServer(t)

	// Categories are seed data; SeedDemoData also inserts demo issues and
	// discussions, so list assertions below are relative, not absolute.
	if err := st.SeedDemoData(t.Context()); err != nil {
		t.Fatalf("SeedDemoData: %v", err)
	}
	cats, err := st.ListDiscussionCategories(t.Context())
	if err != nil || len(cats) == 0 {
		t.Fatalf("no categories seeded: %v", err)
	}
	var qaSlug string
	for _, c := range cats {
		if c.Format == "qa" {
			qaSlug = c.Slug
		}
	}

	// categories endpoint
	code, out := do(t, srv, "GET", "/api/v1/discussion-categories", "")
	if code != http.StatusOK || len(out["categories"].([]any)) == 0 {
		t.Fatalf("categories = %d %v", code, out)
	}

	// create by slug
	code, out = do(t, srv, "POST", "/api/v1/discussions",
		fmt.Sprintf(`{"title":"how?","body":"details","category":"%s"}`, qaSlug))
	if code != http.StatusCreated {
		t.Fatalf("create discussion = %d (%v)", code, out)
	}
	d := out["discussion"].(map[string]any)
	did := int64(d["id"].(float64))
	if d["category"].(map[string]any)["slug"] != qaSlug {
		t.Fatalf("category = %v", d["category"])
	}

	// comment + reply + answer
	code, out = do(t, srv, "POST", fmt.Sprintf("/api/v1/discussions/%d/comments", did), `{"body":"try this"}`)
	if code != http.StatusCreated {
		t.Fatalf("comment = %d", code)
	}
	cid := int64(out["comment"].(map[string]any)["id"].(float64))
	code, out = do(t, srv, "POST", fmt.Sprintf("/api/v1/discussions/%d/comments", did),
		fmt.Sprintf(`{"body":"it worked","parent_id":%d}`, cid))
	if code != http.StatusCreated || out["comment"].(map[string]any)["parent_id"].(float64) != float64(cid) {
		t.Fatalf("reply = %d %v", code, out)
	}
	// reply to a reply must be 422 (one level only)
	rid := int64(out["comment"].(map[string]any)["id"].(float64))
	code, _ = do(t, srv, "POST", fmt.Sprintf("/api/v1/discussions/%d/comments", did),
		fmt.Sprintf(`{"body":"nested","parent_id":%d}`, rid))
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("nested reply = %d, want 422", code)
	}

	// missing "answer" field → 422 (a false default must not clear the mark)
	code, out = do(t, srv, "POST", fmt.Sprintf("/api/v1/discussions/%d/comments/%d/answer", did, cid), `{}`)
	if code != http.StatusUnprocessableEntity || out["error"].(map[string]any)["fields"].(map[string]any)["answer"] == nil {
		t.Fatalf("empty answer body = %d %v", code, out)
	}
	code, _ = do(t, srv, "POST", fmt.Sprintf("/api/v1/discussions/%d/comments/%d/answer", did, cid), `{"answer":true}`)
	if code != http.StatusNoContent {
		t.Fatalf("answer = %d, want 204", code)
	}
	code, out = do(t, srv, "GET", fmt.Sprintf("/api/v1/discussions/%d", did), "")
	if out["discussion"].(map[string]any)["answered"] != true {
		t.Fatalf("answered = %v", out["discussion"])
	}
	c0 := out["comments"].([]any)[0].(map[string]any)
	if c0["is_answer"] != true || len(c0["replies"].([]any)) != 1 {
		t.Fatalf("comment tree = %v", c0)
	}

	// answered filter: the newly answered discussion must be listed
	code, out = do(t, srv, "GET", "/api/v1/discussions?answered=yes", "")
	found := false
	for _, d := range out["discussions"].([]any) {
		if int64(d.(map[string]any)["id"].(float64)) == did {
			found = true
		}
	}
	if !found {
		t.Fatalf("answered=yes missing discussion %d: %v", did, out)
	}

	// patch + delete
	code, out = do(t, srv, "PATCH", fmt.Sprintf("/api/v1/discussions/%d", did), `{"title":"renamed"}`)
	if code != http.StatusOK || out["discussion"].(map[string]any)["title"] != "renamed" {
		t.Fatalf("patch = %d %v", code, out)
	}
	code, _ = do(t, srv, "DELETE", fmt.Sprintf("/api/v1/discussions/%d", did), "")
	if code != http.StatusNoContent {
		t.Fatalf("delete = %d", code)
	}
}

func TestNotFoundAndUnknownRoutes(t *testing.T) {
	srv, _ := newServer(t)

	for path, code := range map[string]int{
		"/api/v1/issues/999":    http.StatusNotFound,
		"/api/v1/issues/abc":    http.StatusNotFound,
		"/api/v1/nope":          http.StatusNotFound,
		"/api/v1/discussions/7": http.StatusNotFound,
	} {
		got, out := do(t, srv, "GET", path, "")
		if got != code {
			t.Fatalf("GET %s = %d, want %d", path, got, code)
		}
		if out["error"].(map[string]any)["message"] == nil {
			t.Fatalf("GET %s error body = %v", path, out)
		}
	}
}
