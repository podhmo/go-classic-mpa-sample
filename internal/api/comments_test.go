package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Comment moderation on issue comments: PATCH rewrites the body and
// stamps edited_at, hide/unhide round-trip the hidden flag + reason,
// DELETE removes the row. Invalid bodies/reasons are field-level 422s.
func TestCommentModeration(t *testing.T) {
	srv, _ := newServer(t)
	id := createIssue(t, srv, `{"title":"t","body":"x"}`)
	base := fmt.Sprintf("/api/v1/w/default/issues/%d/comments", id)

	code, out := do(t, srv, "POST", base, `{"body":"first"}`)
	if code != http.StatusCreated {
		t.Fatalf("create comment = %d", code)
	}
	cid := int64(out["comment"].(map[string]any)["id"].(float64))
	path := fmt.Sprintf("%s/%d", base, cid)

	code, out = do(t, srv, "PATCH", path, `{"body":"edited"}`)
	if code != http.StatusOK {
		t.Fatalf("patch = %d %v", code, out)
	}
	c := out["comment"].(map[string]any)
	if c["body"] != "edited" || c["edited_at"] == nil {
		t.Fatalf("patched comment = %v, want body+edited_at", c)
	}

	// Empty body and bogus reason are 422s naming the field.
	code, out = do(t, srv, "PATCH", path, `{"body":"  "}`)
	if code != http.StatusUnprocessableEntity || out["error"].(map[string]any)["fields"].(map[string]any)["body"] == nil {
		t.Fatalf("empty body = %d %v, want 422 body field", code, out)
	}
	code, out = do(t, srv, "POST", path+"/hide", `{"reason":"rude"}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("bad reason = %d %v, want 422", code, out)
	}
	if fields := out["error"].(map[string]any)["fields"].(map[string]any); fields["reason"] == nil {
		t.Fatalf("fields = %v, want reason", fields)
	}

	code, out = do(t, srv, "POST", path+"/hide", `{"reason":"spam"}`)
	if code != http.StatusOK {
		t.Fatalf("hide = %d %v", code, out)
	}
	c = out["comment"].(map[string]any)
	if c["hidden"] != true || c["hidden_reason"] != "spam" {
		t.Fatalf("hidden comment = %v", c)
	}

	code, out = do(t, srv, "POST", path+"/unhide", `{}`)
	if code != http.StatusOK {
		t.Fatalf("unhide = %d", code)
	}
	// hidden_reason is omitempty: restored rows omit it entirely.
	if c := out["comment"].(map[string]any); c["hidden"] != false || c["hidden_reason"] != nil {
		t.Fatalf("unhidden comment = %v", c)
	}

	// A comment is scoped to its issue: same cid under another issue 404s.
	other := createIssue(t, srv, `{"title":"other"}`)
	code, _ = do(t, srv, "PATCH", fmt.Sprintf("/api/v1/w/default/issues/%d/comments/%d", other, cid), `{"body":"x"}`)
	if code != http.StatusNotFound {
		t.Fatalf("cross-issue patch = %d, want 404", code)
	}

	// Workspace scope: the same comment under another workspace slug 404s.
	do(t, srv, "POST", "/api/v1/workspaces", `{"slug":"other"}`)
	for _, tc := range []struct {
		method, path, body string
	}{
		{"PATCH", fmt.Sprintf("/api/v1/w/other/issues/%d/comments/%d", id, cid), `{"body":"x"}`},
		{"DELETE", fmt.Sprintf("/api/v1/w/other/issues/%d/comments/%d", id, cid), ""},
		{"POST", fmt.Sprintf("/api/v1/w/other/issues/%d/comments/%d/hide", id, cid), `{"reason":"spam"}`},
	} {
		code, _ = do(t, srv, tc.method, tc.path, tc.body)
		if code != http.StatusNotFound {
			t.Fatalf("%s %s = %d, want 404", tc.method, tc.path, code)
		}
	}

	code, _ = do(t, srv, "DELETE", path, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", code)
	}
	code, out = do(t, srv, "GET", fmt.Sprintf("/api/v1/w/default/issues/%d", id), "")
	if len(out["comments"].([]any)) != 0 {
		t.Fatalf("comments after delete = %v", out["comments"])
	}
}

// seedDiscussion creates a discussion plus one thread and one reply via
// the API and returns (discussion id, thread id, reply id). catSlug is
// the category slug the create body needs.
func seedDiscussion(t *testing.T, srv *httptest.Server, catSlug string) (int64, int64, int64) {
	t.Helper()
	code, out := do(t, srv, "POST", "/api/v1/w/default/discussions",
		fmt.Sprintf(`{"title":"d","body":"x","category":%q}`, catSlug))
	if code != http.StatusCreated {
		t.Fatalf("discussion = %d %v", code, out)
	}
	did := int64(out["discussion"].(map[string]any)["id"].(float64))
	code, out = do(t, srv, "POST", fmt.Sprintf("/api/v1/w/default/discussions/%d/threads", did), `{"body":"op"}`)
	if code != http.StatusCreated {
		t.Fatalf("thread = %d %v", code, out)
	}
	tid := int64(out["thread"].(map[string]any)["id"].(float64))
	code, out = do(t, srv, "POST", fmt.Sprintf("/api/v1/w/default/discussions/%d/threads/%d/comments", did, tid), `{"body":"reply"}`)
	if code != http.StatusCreated {
		t.Fatalf("reply = %d %v", code, out)
	}
	rid := int64(out["comment"].(map[string]any)["id"].(float64))
	return did, tid, rid
}

func TestThreadAndReplyModeration(t *testing.T) {
	srv, st := newServer(t)
	if err := st.SeedDemoData(t.Context()); err != nil {
		t.Fatalf("SeedDemoData: %v", err)
	}
	cats, err := st.ListDiscussionCategories(t.Context())
	if err != nil || len(cats) == 0 {
		t.Fatalf("no categories seeded: %v", err)
	}
	cat := cats[0].Slug
	did, tid, rid := seedDiscussion(t, srv, cat)
	tbase := fmt.Sprintf("/api/v1/w/default/discussions/%d/threads/%d", did, tid)

	// Thread edit + hide + unhide.
	code, out := do(t, srv, "PATCH", tbase, `{"body":"edited op"}`)
	if code != http.StatusOK {
		t.Fatalf("thread patch = %d %v", code, out)
	}
	if th := out["thread"].(map[string]any); th["body"] != "edited op" || th["edited_at"] == nil {
		t.Fatalf("patched thread = %v", th)
	}
	code, out = do(t, srv, "POST", tbase+"/hide", `{"reason":"duplicate"}`)
	if code != http.StatusOK {
		t.Fatalf("thread hide = %d", code)
	}
	if th := out["thread"].(map[string]any); th["hidden"] != true || th["hidden_reason"] != "duplicate" {
		t.Fatalf("hidden thread = %v", th)
	}
	code, out = do(t, srv, "POST", tbase+"/unhide", `{}`)
	if code != http.StatusOK || out["thread"].(map[string]any)["hidden"] != false {
		t.Fatalf("unhide = %d %v", code, out)
	}

	// Reply edit + hide + delete.
	rbase := fmt.Sprintf("%s/comments/%d", tbase, rid)
	code, out = do(t, srv, "PATCH", rbase, `{"body":"better reply"}`)
	if code != http.StatusOK {
		t.Fatalf("reply patch = %d %v", code, out)
	}
	if c := out["comment"].(map[string]any); c["body"] != "better reply" || c["edited_at"] == nil {
		t.Fatalf("patched reply = %v", c)
	}
	code, _ = do(t, srv, "POST", rbase+"/hide", `{"reason":"off-topic"}`)
	if code != http.StatusOK {
		t.Fatalf("reply hide = %d", code)
	}
	// A reply is scoped to its thread: rid under a sibling thread 404s.
	code, out = do(t, srv, "POST", fmt.Sprintf("/api/v1/w/default/discussions/%d/threads", did), `{"body":"t2"}`)
	tid2 := int64(out["thread"].(map[string]any)["id"].(float64))
	code, _ = do(t, srv, "PATCH", fmt.Sprintf("/api/v1/w/default/discussions/%d/threads/%d/comments/%d", did, tid2, rid), `{"body":"x"}`)
	if code != http.StatusNotFound {
		t.Fatalf("cross-thread reply patch = %d, want 404", code)
	}

	// Workspace scope on the discussion chain: /w/other/ 404s.
	do(t, srv, "POST", "/api/v1/workspaces", `{"slug":"other"}`)
	code, _ = do(t, srv, "POST", fmt.Sprintf("/api/v1/w/other/discussions/%d/threads/%d/hide", did, tid), `{"reason":"spam"}`)
	if code != http.StatusNotFound {
		t.Fatalf("cross-workspace thread hide = %d, want 404", code)
	}
	code, _ = do(t, srv, "DELETE", rbase, "")
	if code != http.StatusNoContent {
		t.Fatalf("reply delete = %d, want 204", code)
	}

	// Deleting the thread takes its replies along.
	code, out = do(t, srv, "GET", fmt.Sprintf("/api/v1/w/default/discussions/%d", did), "")
	var t0 map[string]any
	for _, raw := range out["threads"].([]any) {
		if th := raw.(map[string]any); int64(th["id"].(float64)) == tid {
			t0 = th
		}
	}
	if t0 == nil {
		t.Fatalf("thread %d missing in %v", tid, out["threads"])
	}
	// An empty reply list serializes as omitted (omitempty).
	if cs, _ := t0["comments"].([]any); len(cs) != 0 {
		t.Fatalf("replies after reply delete = %v", cs)
	}
	code, _ = do(t, srv, "DELETE", tbase, "")
	if code != http.StatusNoContent {
		t.Fatalf("thread delete = %d, want 204", code)
	}
}
