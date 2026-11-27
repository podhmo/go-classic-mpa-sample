package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestPRDiffRemoteTruncationWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pulls/1/diff" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("X-Truncated", "true")
		io.WriteString(w, "partial diff\n")
	}))
	defer srv.Close()

	code, out, errOut := run(t, Options{}, "pr", "diff", "1", "--server", srv.URL)
	if code != ExitOK || out != "partial diff\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if !strings.Contains(errOut, "diff was truncated") {
		t.Fatalf("stderr = %q, want truncation warning", errOut)
	}
}

func TestPRStateChangeRemote(t *testing.T) {
	var mu sync.Mutex
	var statuses []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pulls/7/status" || r.Method != http.MethodPost {
			t.Errorf("%s %q", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode status body: %v", err)
		}
		mu.Lock()
		statuses = append(statuses, body["status"])
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	code, out, errOut := run(t, Options{}, "pr", "close", "7", "--server", srv.URL)
	if code != ExitOK || !strings.Contains(out, "pull request #7 closed") {
		t.Fatalf("close: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	code, out, errOut = run(t, Options{}, "pr", "reopen", "7", "--server", srv.URL)
	if code != ExitOK || !strings.Contains(out, "pull request #7 reopened") {
		t.Fatalf("reopen: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	mu.Lock()
	joined := strings.Join(statuses, ",")
	mu.Unlock()
	if joined != "closed,open" {
		t.Fatalf("statuses = %v", statuses)
	}
}

func TestPRViewMarkdownRemote(t *testing.T) {
	const doc = "# pull request\n\nrendered by server\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pulls/7/markdown" {
			t.Errorf("path = %q", r.URL.Path)
		}
		io.WriteString(w, doc)
	}))
	defer srv.Close()

	code, out, errOut := run(t, Options{}, "pr", "view", "7", "--markdown", "--server", srv.URL)
	if code != ExitOK || out != doc {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestPRStateChangeRemoteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		io.WriteString(w, `{"error":{"message":"The pull request's status does not allow this change."}}`)
	}))
	defer srv.Close()

	code, _, errOut := run(t, Options{}, "pr", "close", "7", "--server", srv.URL)
	if code != ExitError {
		t.Fatalf("code = %d, want %d", code, ExitError)
	}
	if !strings.Contains(errOut, "status does not allow") {
		t.Fatalf("stderr = %q, want server conflict message", errOut)
	}
}
