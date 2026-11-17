package api

import (
	"fmt"
	"net/http"
	"testing"
)

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

	code, out := do(t, srv, "GET", "/api/v1/discussion-categories", "")
	if code != http.StatusOK || len(out["categories"].([]any)) == 0 {
		t.Fatalf("categories = %d %v", code, out)
	}

	code, out = do(t, srv, "POST", "/api/v1/w/default/discussions",
		fmt.Sprintf(`{"title":"how?","body":"details","category":"%s"}`, qaSlug))
	if code != http.StatusCreated {
		t.Fatalf("create discussion = %d (%v)", code, out)
	}
	d := out["discussion"].(map[string]any)
	did := int64(d["id"].(float64))
	if d["category"].(map[string]any)["slug"] != qaSlug {
		t.Fatalf("category = %v", d["category"])
	}

	code, out = do(t, srv, "POST", fmt.Sprintf("/api/v1/w/default/discussions/%d/threads", did), `{"body":"try this"}`)
	if code != http.StatusCreated {
		t.Fatalf("thread = %d", code)
	}
	tid := int64(out["thread"].(map[string]any)["id"].(float64))
	code, out = do(t, srv, "POST", fmt.Sprintf("/api/v1/w/default/discussions/%d/threads/%d/comments", did, tid),
		`{"body":"it worked"}`)
	if code != http.StatusCreated {
		t.Fatalf("reply = %d %v", code, out)
	}
	rid := int64(out["comment"].(map[string]any)["id"].(float64))
	// A comment id is not a thread: nesting a reply under a reply is 422.
	code, _ = do(t, srv, "POST", fmt.Sprintf("/api/v1/w/default/discussions/%d/threads/%d/comments", did, rid),
		`{"body":"nested"}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("nested reply = %d, want 422", code)
	}

	code, out = do(t, srv, "POST", fmt.Sprintf("/api/v1/w/default/discussions/%d/threads/%d/answer", did, tid), `{}`)
	if code != http.StatusUnprocessableEntity || out["error"].(map[string]any)["fields"].(map[string]any)["answer"] == nil {
		t.Fatalf("empty answer body = %d %v", code, out)
	}
	code, _ = do(t, srv, "POST", fmt.Sprintf("/api/v1/w/default/discussions/%d/threads/%d/answer", did, tid), `{"answer":true}`)
	if code != http.StatusNoContent {
		t.Fatalf("answer = %d, want 204", code)
	}
	code, out = do(t, srv, "GET", fmt.Sprintf("/api/v1/w/default/discussions/%d", did), "")
	if out["discussion"].(map[string]any)["answered"] != true {
		t.Fatalf("answered = %v", out["discussion"])
	}
	t0 := out["threads"].([]any)[0].(map[string]any)
	if t0["is_answer"] != true || len(t0["comments"].([]any)) != 1 {
		t.Fatalf("thread tree = %v", t0)
	}

	code, out = do(t, srv, "GET", "/api/v1/w/default/discussions?answered=yes", "")
	found := false
	for _, d := range out["discussions"].([]any) {
		if int64(d.(map[string]any)["id"].(float64)) == did {
			found = true
		}
	}
	if !found {
		t.Fatalf("answered=yes missing discussion %d: %v", did, out)
	}

	code, out = do(t, srv, "PATCH", fmt.Sprintf("/api/v1/w/default/discussions/%d", did), `{"title":"renamed"}`)
	if code != http.StatusOK || out["discussion"].(map[string]any)["title"] != "renamed" {
		t.Fatalf("patch = %d %v", code, out)
	}
	code, _ = do(t, srv, "DELETE", fmt.Sprintf("/api/v1/w/default/discussions/%d", did), "")
	if code != http.StatusNoContent {
		t.Fatalf("delete = %d", code)
	}
}
