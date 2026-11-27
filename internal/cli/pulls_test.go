package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
