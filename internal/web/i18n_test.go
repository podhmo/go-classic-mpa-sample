package web

import (
	"net/http"
	"strings"
	"testing"
)

func TestLangQuerySetsCookieAndRedirects(t *testing.T) {
	srv, _ := newTestServer(t)
	client := noRedirectClient()

	resp, err := client.Get(srv.URL + "/w/default/issues?lang=ja&q=x")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	// The lang param is stripped; other query params survive the redirect.
	if loc := resp.Header.Get("Location"); loc != "/w/default/issues?q=x" {
		t.Fatalf("Location = %q, want /w/default/issues?q=x", loc)
	}
	var found bool
	for _, c := range resp.Cookies() {
		if c.Name == langCookieName && c.Value == "ja" {
			found = true
		}
	}
	if !found {
		t.Fatal("no lang=ja cookie set")
	}
}

// A request path starting with "//" must not produce a scheme-relative
// Location: "//evil.example/x" as a Location is followed cross-origin.
func TestLangRedirectNeverLeavesOrigin(t *testing.T) {
	srv, _ := newTestServer(t)
	client := noRedirectClient()

	resp, err := client.Get(srv.URL + "//evil.example/w/default/issues?lang=ja")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	if strings.HasPrefix(loc, "//") || !strings.HasPrefix(loc, "/") {
		t.Fatalf("Location = %q, want a same-origin absolute path", loc)
	}
}

func TestIssueListRendersJapaneseWithCookie(t *testing.T) {
	srv, f := newTestServer(t)
	seedIssues(t, f)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/w/default/issues", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: langCookieName, Value: "ja"})
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body := getBody(t, resp)

	for _, want := range []string{`<html lang="ja">`, "イシュー", "オープン", "すべてのラベル", "2 件中 1–2 件を表示"} {
		if !strings.Contains(body, want) {
			t.Errorf("ja page missing %q", want)
		}
	}
	if strings.Contains(body, ">Search<") {
		t.Error("ja page still shows the English Search label")
	}
}

func TestIssueListRendersJapaneseViaAcceptLanguage(t *testing.T) {
	srv, f := newTestServer(t)
	seedIssues(t, f)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/w/default/issues", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Language", "ja,en-US;q=0.9,en;q=0.8")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body := getBody(t, resp)

	if !strings.Contains(body, `<html lang="ja">`) {
		t.Error("Accept-Language: ja did not render the ja page")
	}
	if !strings.Contains(strings.Join(resp.Header.Values("Vary"), ","), "Accept-Language") {
		t.Error("Vary lacks Accept-Language")
	}
}

func TestIssueListDefaultsToEnglish(t *testing.T) {
	srv, f := newTestServer(t)
	seedIssues(t, f)

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body := getBody(t, resp)

	if !strings.Contains(body, `<html lang="en">`) {
		t.Error("default render is not lang=en")
	}
	if !strings.Contains(body, ">Issues</h1>") {
		t.Error("en page missing 'Issues' heading")
	}
}

func TestIssueListCookieBeatsAcceptLanguage(t *testing.T) {
	srv, f := newTestServer(t)
	seedIssues(t, f)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/w/default/issues", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Language", "ja")
	req.AddCookie(&http.Cookie{Name: langCookieName, Value: "en"})
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body := getBody(t, resp)
	if !strings.Contains(body, `<html lang="en">`) {
		t.Error("explicit cookie=en should override Accept-Language: ja")
	}
}

func TestUnknownLangCookieFallsBack(t *testing.T) {
	srv, f := newTestServer(t)
	seedIssues(t, f)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/w/default/issues", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: langCookieName, Value: "fr"})
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body := getBody(t, resp)
	if !strings.Contains(body, `<html lang="en">`) {
		t.Error("unknown lang cookie should fall back to en")
	}
}

func TestMessagesForFallsBackToEnglish(t *testing.T) {
	if got := messagesFor("fr").IssuesTitle; got != "Issues" {
		t.Fatalf("unknown locale = %q, want en catalog", got)
	}
	if got := messagesFor("ja").IssuesTitle; got != "イシュー" {
		t.Fatalf("ja = %q, want イシュー", got)
	}
	if got := messagesFor("en").IssuesTitle; got != "Issues" {
		t.Fatalf("en = %q, want Issues", got)
	}
}
