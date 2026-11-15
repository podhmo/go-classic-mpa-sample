package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/podhmo/go-classic-mpa-sample/internal/auth"
)

func TestSessionLookupFailureIs500NotAnonymous(t *testing.T) {
	srv, f := newTestServer(t)
	u := f.addUser(t, "alice", "password123")
	f.sessions[auth.HashToken("tok")] = u.ID

	// A transient lookup failure must fail the request — proceeding
	// anonymously would save an authored post without its author.
	f.sessionUserErr = errors.New("db gone")
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/issues",
		strings.NewReader(url.Values{"title": {"x"}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "tok"})
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /issues: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if len(f.issues) != 0 {
		t.Fatal("post was saved during session lookup failure")
	}

	f.sessionUserErr = nil
	code, body := getWithCookie(t, srv, "/issues", &http.Cookie{Name: sessionCookieName, Value: "nope"})
	if code != http.StatusOK || strings.Contains(body, "@alice") {
		t.Error("unknown session did not resolve anonymously")
	}
}

// sessionCookie extracts the session cookie set by register/login, or fails.
func sessionCookie(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	t.Fatalf("no %q cookie in response", sessionCookieName)
	return nil
}

// getWithCookie issues a GET carrying the given cookie.
func getWithCookie(t *testing.T, srv *httptest.Server, path string, c *http.Cookie) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c != nil {
		req.AddCookie(c)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp.StatusCode, getBody(t, resp)
}

func TestRegisterFlowSetsSessionCookie(t *testing.T) {
	srv, _ := newTestServer(t)

	resp := postForm(t, noRedirectClient(), srv.URL+"/register", url.Values{
		"name": {"alice"}, "password": {"password123"}, "next": {"/issues"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/issues" {
		t.Fatalf("Location = %q, want /issues", loc)
	}
	c := sessionCookie(t, resp)
	if !c.HttpOnly {
		t.Error("session cookie is not HttpOnly")
	}
	if c.Secure {
		t.Error("session cookie must not be Secure on plain-HTTP localhost")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.MaxAge != 0 {
		t.Errorf("MaxAge = %d, want browser-session cookie (no MaxAge)", c.MaxAge)
	}

	code, body := getWithCookie(t, srv, "/issues", c)
	if code != http.StatusOK {
		t.Fatalf("GET /issues = %d", code)
	}
	if !strings.Contains(body, "@alice") {
		t.Error("logged-in header missing @alice")
	}
	if !strings.Contains(body, `action="/logout"`) {
		t.Error("logged-in header missing logout form")
	}
}

func TestRegisterValidationAndDuplicate(t *testing.T) {
	srv, f := newTestServer(t)
	f.addUser(t, "alice", "password123")

	resp := postForm(t, srv.Client(), srv.URL+"/register", url.Values{
		"name": {"bob"}, "password": {"short"},
	})
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	if !strings.Contains(body, "Password must be 8 characters or more.") {
		t.Error("missing password length error")
	}
	if len(resp.Cookies()) != 0 {
		t.Error("failed registration set a cookie")
	}

	resp = postForm(t, srv.Client(), srv.URL+"/register", url.Values{
		"name": {"ALICE"}, "password": {"password123"},
	})
	body = getBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	if !strings.Contains(body, "That name is already taken.") {
		t.Error("missing duplicate-name error")
	}
}

func TestLoginLogoutFlow(t *testing.T) {
	srv, f := newTestServer(t)
	f.addUser(t, "alice", "password123")

	resp := postForm(t, srv.Client(), srv.URL+"/login", url.Values{
		"name": {"alice"}, "password": {"wrong-pass"},
	})
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	if !strings.Contains(body, "Name or password is incorrect.") {
		t.Error("missing generic login error")
	}
	if len(resp.Cookies()) != 0 {
		t.Error("failed login set a cookie")
	}

	resp = postForm(t, noRedirectClient(), srv.URL+"/login", url.Values{
		"name": {"alice"}, "password": {"password123"}, "next": {"/issues"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	c := sessionCookie(t, resp)

	resp = postForm(t, srv.Client(), srv.URL+"/login", url.Values{
		"name": {"mallory"}, "password": {"password123"},
	})
	body = getBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Name or password is incorrect.") {
		t.Error("unknown user did not get the generic error")
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(c)
	resp, err = noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("POST /logout: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout status = %d, want 303", resp.StatusCode)
	}
	var cleared bool
	for _, rc := range resp.Cookies() {
		if rc.Name == sessionCookieName && rc.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("logout did not clear the session cookie")
	}

	code, body := getWithCookie(t, srv, "/issues", c)
	if code != http.StatusOK {
		t.Fatalf("GET /issues = %d", code)
	}
	if strings.Contains(body, "@alice") {
		t.Error("stale cookie still resolves the user after logout")
	}
	if !strings.Contains(body, "Log in") {
		t.Error("anonymous header missing Log in link")
	}
}

func TestSafeNext(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "/issues"}, {"/issues/3", "/issues/3"},
		{"//evil.example", "/issues"}, {"https://evil.example", "/issues"},
		{"javascript:alert(1)", "/issues"}, {"/\\evil.example", "/issues"},
	} {
		if got := safeNext(tc.in); got != tc.want {
			t.Errorf("safeNext(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
