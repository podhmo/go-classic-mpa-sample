package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// seedComment adds an issue with one comment and returns the server,
// fake store and comment id.
func seedComment(t *testing.T) (*httptest.Server, *fakeStore, int64) {
	t.Helper()
	srv, f := newTestServer(t)
	f.addIssue("Comment target", "", store.StatusOpen, time.Now())
	cid, err := f.CreateComment(context.Background(), 1, "original body", 0)
	if err != nil {
		t.Fatal(err)
	}
	return srv, f, cid
}

// seedThread adds a discussion with one thread and one reply.
func seedThread(t *testing.T) (*httptest.Server, *fakeStore, int64, int64) {
	t.Helper()
	srv, f := newTestServer(t)
	f.addDiscussion("Discuss", "", 1, time.Now())
	tid, err := f.CreateDiscussionThread(context.Background(), 1, "opening post", 0)
	if err != nil {
		t.Fatal(err)
	}
	cid, err := f.CreateDiscussionComment(context.Background(), 1, tid, "a reply", 0)
	if err != nil {
		t.Fatal(err)
	}
	return srv, f, tid, cid
}

func TestCommentEditFlow(t *testing.T) {
	srv, _, cid := seedComment(t)
	base := "/w/default/issues/1/comments/" + itoa(cid)

	resp, err := srv.Client().Get(srv.URL + base + "/edit")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edit form status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "original body") || !strings.Contains(body, `action="`+base+`/edit"`) {
		t.Error("edit form should carry the current body and post URL")
	}

	resp = postForm(t, noRedirectClient(), srv.URL+base+"/edit", url.Values{"body": {"rewritten"}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("edit POST status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/w/default/issues/1#comment-"+itoa(cid) {
		t.Fatalf("Location = %q, want the comment anchor", loc)
	}

	resp2, err := srv.Client().Get(srv.URL + "/w/default/issues/1")
	if err != nil {
		t.Fatal(err)
	}
	body = getBody(t, resp2)
	if !strings.Contains(body, "rewritten") {
		t.Error("edited body should render")
	}
	if !strings.Contains(body, `class="comment-edited"`) {
		t.Error("edited comment should carry the edited marker")
	}
}

func TestCommentEditEmpty(t *testing.T) {
	srv, _, cid := seedComment(t)
	resp := postForm(t, srv.Client(), srv.URL+"/w/default/issues/1/comments/"+itoa(cid)+"/edit",
		url.Values{"body": {"   "}})
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	if !strings.Contains(body, "Body is required") {
		t.Error("expected the validation error on the edit form")
	}
}

func TestCommentDeleteFlow(t *testing.T) {
	srv, f, cid := seedComment(t)
	base := "/w/default/issues/1/comments/" + itoa(cid)

	resp, err := srv.Client().Get(srv.URL + base + "/delete")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "original body") || !strings.Contains(body, "Delete comment") {
		t.Error("confirm page should show the excerpt and the danger button")
	}

	resp = postForm(t, noRedirectClient(), srv.URL+base+"/delete", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete POST status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/w/default/issues/1" {
		t.Fatalf("Location = %q, want the issue detail", loc)
	}
	cs, _ := f.ListComments(context.Background(), 1)
	if len(cs) != 0 {
		t.Fatal("comment should be gone")
	}
}

func TestCommentHideUnhideFlow(t *testing.T) {
	srv, _, cid := seedComment(t)
	base := "/w/default/issues/1/comments/" + itoa(cid)

	resp := postForm(t, noRedirectClient(), srv.URL+base+"/hide", url.Values{"reason": {"spam"}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("hide status = %d, want 303", resp.StatusCode)
	}

	resp2, err := srv.Client().Get(srv.URL + "/w/default/issues/1")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp2)
	for _, want := range []string{
		"comment-hidden", "marked as spam", `summary>Show comment`,
		"Unhide", base + "/unhide",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("hidden comment rendering missing %q", want)
		}
	}

	resp = postForm(t, noRedirectClient(), srv.URL+base+"/unhide", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unhide status = %d, want 303", resp.StatusCode)
	}
	resp2, _ = srv.Client().Get(srv.URL + "/w/default/issues/1")
	body = getBody(t, resp2)
	if strings.Contains(body, "comment-hidden") {
		t.Error("unhidden comment should render normally again")
	}
}

func TestCommentHideInvalidReason(t *testing.T) {
	srv, _, cid := seedComment(t)
	resp := postForm(t, srv.Client(), srv.URL+"/w/default/issues/1/comments/"+itoa(cid)+"/hide",
		url.Values{"reason": {"bogus"}})
	getBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// The partial hide contract: X-Partial answers the hidden-state <li>
// that replace mode swaps over the visible comment.
func TestCommentHidePartial(t *testing.T) {
	srv, _, cid := seedComment(t)
	resp := postFormWithHeaders(t, srv.URL+"/w/default/issues/1/comments/"+itoa(cid)+"/hide",
		url.Values{"reason": {"resolved"}}, map[string]string{"X-Partial": "1"})
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{
		`id="comment-` + itoa(cid) + `"`, "comment-hidden", "marked as resolved", "Unhide",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("hide swap fragment missing %q", want)
		}
	}
	if strings.Contains(body, "<!DOCTYPE") {
		t.Error("partial response must be a fragment")
	}
}

func TestCommentModerationNotFound(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("No comments", "", store.StatusOpen, time.Now())
	for _, path := range []string{
		"/w/default/issues/1/comments/99/edit",
		"/w/default/issues/1/comments/99/delete",
	} {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", path, resp.StatusCode)
		}
	}
	resp := postForm(t, srv.Client(), srv.URL+"/w/default/issues/1/comments/99/hide",
		url.Values{"reason": {"spam"}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("hide POST on missing comment status = %d, want 404", resp.StatusCode)
	}
}

// Detail pages render the moderation action row on every post —
// including the hide reason picker.
func TestCommentActionsRender(t *testing.T) {
	srv, _, cid := seedComment(t)
	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/1")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	base := "/w/default/issues/1/comments/" + itoa(cid)
	for _, want := range []string{
		base + "/edit", base + "/delete", base + "/hide",
		"Edit comment " + itoa(cid), `name="reason" required`,
		`value="spam"`, `value="low-quality"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("comment actions missing %q", want)
		}
	}
}

func TestThreadEditDeleteFlow(t *testing.T) {
	srv, f, tid, cid := seedThread(t)
	base := "/w/default/discussions/1/threads/" + itoa(tid)

	resp := postForm(t, noRedirectClient(), srv.URL+base+"/edit", url.Values{"body": {"new opening"}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("thread edit status = %d, want 303", resp.StatusCode)
	}
	ts, _ := f.ListDiscussionThreads(context.Background(), 1)
	if ts[0].Body != "new opening" || ts[0].EditedAt.IsZero() {
		t.Fatal("thread should carry the new body and an edited stamp")
	}

	// The delete confirm warns about the replies going along.
	resp, err := srv.Client().Get(srv.URL + base + "/delete")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if !strings.Contains(body, "1 reply") {
		t.Error("confirm page should warn about the thread's replies")
	}
	resp.Body.Close()

	resp = postForm(t, noRedirectClient(), srv.URL+base+"/delete", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("thread delete status = %d, want 303", resp.StatusCode)
	}
	ts, _ = f.ListDiscussionThreads(context.Background(), 1)
	if len(ts) != 0 {
		t.Fatal("thread should be gone")
	}
	_ = cid
}

func TestReplyModerationFlow(t *testing.T) {
	srv, f, tid, cid := seedThread(t)
	base := "/w/default/discussions/1/threads/" + itoa(tid) + "/comments/" + itoa(cid)

	resp := postForm(t, noRedirectClient(), srv.URL+base+"/edit", url.Values{"body": {"edited reply"}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("reply edit status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.HasSuffix(loc, "#comment-"+itoa(cid)) {
		t.Fatalf("Location = %q, want the reply anchor", loc)
	}

	resp = postForm(t, noRedirectClient(), srv.URL+base+"/hide", url.Values{"reason": {"outdated"}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("reply hide status = %d, want 303", resp.StatusCode)
	}
	ts, _ := f.ListDiscussionThreads(context.Background(), 1)
	c := findReply(findThread(ts, tid), cid)
	if !c.Hidden || c.HiddenReason != "outdated" {
		t.Fatal("reply should be hidden with the reason")
	}

	resp = postForm(t, noRedirectClient(), srv.URL+base+"/delete", nil)
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.HasSuffix(loc, "#thread-"+itoa(tid)) {
		t.Fatalf("delete Location = %q, want the thread anchor", loc)
	}
	ts, _ = f.ListDiscussionThreads(context.Background(), 1)
	if findReply(findThread(ts, tid), cid) != nil {
		t.Fatal("reply should be gone")
	}
}
