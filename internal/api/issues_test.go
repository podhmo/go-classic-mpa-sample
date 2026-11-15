package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createIssue POSTs an issue and returns its id.
func createIssue(t *testing.T, srv *httptest.Server, body string) int64 {
	t.Helper()
	code, out := do(t, srv, "POST", "/api/v1/issues", body)
	if code != http.StatusCreated {
		t.Fatalf("POST /api/v1/issues status = %d, want 201 (body %v)", code, out)
	}
	return int64(out["issue"].(map[string]any)["id"].(float64))
}

func TestIssueLifecycle(t *testing.T) {
	srv, _ := newServer(t)

	code, out := do(t, srv, "POST", "/api/v1/issues", `{"title":"first","body":"hello"}`)
	if code != http.StatusCreated {
		t.Fatalf("create status = %d (%v)", code, out)
	}
	issue := out["issue"].(map[string]any)
	if issue["status"] != "open" || issue["title"] != "first" {
		t.Fatalf("unexpected issue DTO: %v", issue)
	}
	id := int64(issue["id"].(float64))

	code, out = do(t, srv, "GET", fmt.Sprintf("/api/v1/issues/%d", id), "")
	if code != http.StatusOK {
		t.Fatalf("get status = %d", code)
	}
	if cs, ok := out["comments"].([]any); !ok || len(cs) != 0 {
		t.Fatalf("comments = %v, want empty array", out["comments"])
	}

	code, out = do(t, srv, "GET", "/api/v1/issues?status=open&q=first", "")
	if code != http.StatusOK || out["total"].(float64) != 1 {
		t.Fatalf("list = %d %v", code, out)
	}

	code, out = do(t, srv, "PATCH", fmt.Sprintf("/api/v1/issues/%d", id), `{"title":"renamed"}`)
	if code != http.StatusOK {
		t.Fatalf("patch status = %d (%v)", code, out)
	}
	if got := out["issue"].(map[string]any); got["title"] != "renamed" || got["body"] != "hello" {
		t.Fatalf("patch lost body: %v", got)
	}

	code, out = do(t, srv, "POST", fmt.Sprintf("/api/v1/issues/%d/status", id), `{"status":"closed"}`)
	if code != http.StatusOK || out["issue"].(map[string]any)["status"] != "closed" {
		t.Fatalf("status = %d %v", code, out)
	}
	code, out = do(t, srv, "GET", "/api/v1/issues?status=open", "")
	if out["total"].(float64) != 0 {
		t.Fatalf("open list should be empty: %v", out)
	}

	code, out = do(t, srv, "POST", fmt.Sprintf("/api/v1/issues/%d/comments", id), `{"body":"me too"}`)
	if code != http.StatusCreated || out["comment"].(map[string]any)["body"] != "me too" {
		t.Fatalf("comment = %d %v", code, out)
	}
	code, out = do(t, srv, "GET", fmt.Sprintf("/api/v1/issues/%d", id), "")
	if n := len(out["comments"].([]any)); n != 1 {
		t.Fatalf("comments = %d, want 1", n)
	}

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

	code, out := do(t, srv, "POST", "/api/v1/issues", `{"title":"  ","body":"x"}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d (%v)", code, out)
	}
	fields := out["error"].(map[string]any)["fields"].(map[string]any)
	if fields["title"] == nil {
		t.Fatalf("fields = %v, want title error", fields)
	}

	code, _ = do(t, srv, "POST", "/api/v1/issues", `{"title":`)
	if code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", code)
	}

	code, out = do(t, srv, "POST", "/api/v1/issues", `{"title":"t","labels":["nope"]}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown label = %d", code)
	}
	if out["error"].(map[string]any)["fields"].(map[string]any)["labels"] == nil {
		t.Fatalf("fields = %v, want labels error", out)
	}

	code, out = do(t, srv, "GET", "/api/v1/labels", "")
	if code != http.StatusOK {
		t.Fatalf("labels = %d", code)
	}
	if _, ok := out["labels"].([]any); !ok {
		t.Fatalf("labels payload = %v", out)
	}

	id := createIssue(t, srv, `{"title":"t"}`)
	code, out = do(t, srv, "POST", fmt.Sprintf("/api/v1/issues/%d/status", id), `{"status":"wip"}`)
	if code != http.StatusUnprocessableEntity || out["error"].(map[string]any)["fields"].(map[string]any)["status"] == nil {
		t.Fatalf("status = %d %v", code, out)
	}
}
