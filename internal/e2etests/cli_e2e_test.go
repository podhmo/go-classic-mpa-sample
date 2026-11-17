//go:build e2e

// Package e2etests runs the built binary end to end: init → serve →
// register → auth token → remote-mode CLI and `zatu api` against the
// live server. Runs only under -tags=e2e (`make test-e2e`).
package e2etests

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// buildBinaries compiles the server binary and the CLI binary (separate
// commands: cmd/zatu is the client, cmd/zatudan is the server).
func buildBinaries(t *testing.T) (serverBin, cliBin string) {
	t.Helper()
	dir := t.TempDir()
	serverBin = filepath.Join(dir, "zatudan")
	cliBin = filepath.Join(dir, "zatu")
	for target, out := range map[string]string{
		"../../cmd/zatudan": serverBin,
		"../../cmd/zatu": cliBin,
	} {
		b, err := exec.Command("go", "build", "-o", out, target).CombinedOutput()
		if err != nil {
			t.Fatalf("go build %s: %v\n%s", target, err, b)
		}
	}
	return serverBin, cliBin
}

// freePort grabs an unused loopback port for the test server.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// run invokes the binary and returns exit code + captured streams.
func run(t *testing.T, bin string, env []string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return code, stdout.String(), stderr.String()
}

// mustRun asserts exit 0 and returns stdout.
func mustRun(t *testing.T, bin string, env []string, args ...string) string {
	t.Helper()
	code, out, errOut := run(t, bin, env, args...)
	if code != 0 {
		t.Fatalf("%v: exit %d\nstdout: %s\nstderr: %s", args, code, out, errOut)
	}
	return out
}

// serve starts the server process and waits until it answers HTTP.
func serve(t *testing.T, bin, db, addr, apiToken string) {
	t.Helper()
	args := []string{"-db", db, "-addr", addr}
	if apiToken != "" {
		args = append(args, "-api-token", apiToken)
	}
	cmd := exec.Command(bin, args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/issues")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("server did not come up in 10s")
}

func TestCLIE2E(t *testing.T) {
	serverBin, cliBin := buildBinaries(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "e2e.db")
	addr := freePort(t)
	server := "http://" + addr

	mustRun(t, serverBin, nil, "-db", db, "init")

	// The fixed discussion categories are reference data: init seeds them,
	// so local-mode discussion create works on a schema-only database
	// (before the server ever runs).
	lout := mustRun(t, cliBin, nil, "discussion", "create", "--db", db,
		"--title", "local q", "--category", "qa", "--body", "pre-server")
	if !strings.Contains(lout, "created discussion #") {
		t.Fatalf("local discussion create on init db: %q", lout)
	}

	// Serve with the env token so /api/v1 is mounted from the start;
	// startup also seeds the demo data.
	serve(t, serverBin, db, addr, "envtok")

	resp, err := http.PostForm(server+"/register", url.Values{
		"name": {"e2euser"}, "password": {"pw-pw-pw"},
	})
	if err != nil {
		t.Fatalf("POST /register: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("register: %s", resp.Status)
	}

	token := strings.TrimSpace(mustRun(t, cliBin, nil, "auth", "token", "--db", db, "--user", "e2euser"))
	if token == "" {
		t.Fatal("auth token returned empty token")
	}

	if code, _, _ := run(t, cliBin, nil, "auth", "token", "--db", db, "--user", "ghost"); code != 1 {
		t.Fatalf("auth token --user ghost: exit %d, want 1", code)
	}

	// Remote mode via flags: create an issue with a label. Backend flags
	// live on the leaf commands (after the resource words), gh-style.
	remote := func(args ...string) []string {
		return append(args, "--server", server, "--token", token)
	}
	out := mustRun(t, cliBin, nil, remote("issue", "create",
		"--title", "from cli", "--body", "made remotely", "--label", "Bug")...)
	var id int64
	if _, err := fmt.Sscanf(out, "created issue #%d", &id); err != nil || id == 0 {
		t.Fatalf("issue create: %q", out)
	}

	list := mustRun(t, cliBin, nil, remote("issue", "list", "--state", "all")...)
	if !strings.Contains(list, "from cli") {
		t.Fatalf("issue list missing created issue:\n%s", list)
	}

	view := mustRun(t, cliBin, nil, remote("issue", "view", strconv.FormatInt(id, 10), "--json", "title,author")...)
	var got struct {
		Title  string `json:"title"`
		Author *struct {
			Name string `json:"name"`
		} `json:"author"`
	}
	if err := json.Unmarshal([]byte(view), &got); err != nil {
		t.Fatalf("issue view --json: %v\n%s", err, view)
	}
	if got.Title != "from cli" || got.Author == nil || got.Author.Name != "e2euser" {
		t.Fatalf("issue view: %+v", got)
	}

	mustRun(t, cliBin, nil, remote("comment", "create", strconv.FormatInt(id, 10), "--body", "remote comment")...)

	// Local-mode writes must reach the SSE stream: the CLI commits the
	// change row through its own process and the server's table watcher
	// re-publishes it (docs/sketch/ja/live-updates.md 案(a)).
	sseCtx, sseCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sseCancel()
	sreq, err := http.NewRequestWithContext(sseCtx, "GET",
		server+"/w/default/issues/"+strconv.FormatInt(id, 10)+"/events", nil)
	if err != nil {
		t.Fatalf("events request: %v", err)
	}
	sresp, err := http.DefaultClient.Do(sreq)
	if err != nil {
		t.Fatalf("events stream: %v", err)
	}
	defer sresp.Body.Close()
	sseLines := make(chan string, 32)
	go func() {
		sc := bufio.NewScanner(sresp.Body)
		for sc.Scan() {
			sseLines <- sc.Text()
		}
	}()
	// The first frame is the retry directive — reading it proves the
	// subscription is live before the CLI writes.
	if l := <-sseLines; !strings.HasPrefix(l, "retry:") {
		t.Fatalf("first SSE frame: %q", l)
	}
	mustRun(t, cliBin, nil, "comment", "create",
		strconv.FormatInt(id, 10), "--body", "local-mode comment", "--db", db)
	for {
		select {
		case l := <-sseLines:
			if strings.HasPrefix(l, "event: comment") {
				goto sawComment
			}
		case <-sseCtx.Done():
			t.Fatal("timed out waiting for SSE comment event from local CLI write")
		}
	}
sawComment:

	out = mustRun(t, cliBin, nil, remote("issue", "close", strconv.FormatInt(id, 10))...)
	if !strings.Contains(out, "closed issue #") {
		t.Fatalf("issue close: %q", out)
	}

	out = mustRun(t, cliBin, nil, remote("discussion", "create",
		"--title", "remote qa", "--category", "qa", "--body", "how?")...)
	var did int64
	if _, err := fmt.Sscanf(out, "created discussion #%d", &did); err != nil || did == 0 {
		t.Fatalf("discussion create: %q", out)
	}
	dlist := mustRun(t, cliBin, nil, remote("discussion", "list", "--search", "remote qa")...)
	if !strings.Contains(dlist, "remote qa") {
		t.Fatalf("discussion list:\n%s", dlist)
	}
	out = mustRun(t, cliBin, nil, remote("discussion", "comment", "create",
		strconv.FormatInt(did, 10), "--body", "the answer")...)
	var tid int64
	if _, err := fmt.Sscanf(out, "created thread #%d", &tid); err != nil || tid == 0 {
		t.Fatalf("discussion comment: %q", out)
	}
	mustRun(t, cliBin, nil, remote("discussion", "answer",
		strconv.FormatInt(did, 10), "--thread", strconv.FormatInt(tid, 10))...)
	dview := mustRun(t, cliBin, nil, remote("discussion", "view",
		strconv.FormatInt(did, 10), "--json", "title,answered")...)
	if !strings.Contains(dview, `"answered": true`) {
		t.Fatalf("discussion view after answer:\n%s", dview)
	}

	raw := mustRun(t, cliBin, nil, remote("api", "/w/default/issues")...)
	if !strings.Contains(raw, `"issues"`) {
		t.Fatalf("issues api /w/default/issues:\n%s", raw)
	}
	code, _, errOut := run(t, cliBin, nil, "api", "/w/default/issues", "--server", server, "--token", "bogus")
	if code != 1 || !strings.Contains(errOut, "401") {
		t.Fatalf("api with bad token: exit %d\n%s", code, errOut)
	}

	envOut := mustRun(t, cliBin,
		[]string{"ZATUDAN_SERVER=" + server, "ZATUDAN_TOKEN=" + token},
		"issue", "list", "--state", "all")
	if !strings.Contains(envOut, "from cli") {
		t.Fatalf("env-based remote list:\n%s", envOut)
	}

	envTok := mustRun(t, cliBin, nil,
		"issue", "list", "--state", "all", "--server", server, "--token", "envtok")
	if !strings.Contains(envTok, "from cli") {
		t.Fatalf("env-token list:\n%s", envTok)
	}

	out = mustRun(t, cliBin, nil, remote("api", "w/default/issues",
		"-X", "POST", "-f", "title=api posted", "-f", "body=from issues api")...)
	if !strings.Contains(out, `"api posted"`) {
		t.Fatalf("api POST /w/default/issues:\n%s", out)
	}
	out = mustRun(t, cliBin, nil, remote("api", "w/default/issues",
		"-f", "status=closed", "--template", "{{range .issues}}{{.title}} {{end}}")...)
	if !strings.Contains(out, "from cli") {
		t.Fatalf("api GET -f/--template:\n%s", out)
	}

	tokList := mustRun(t, cliBin, nil, "auth", "token", "list", "--db", db)
	if !strings.Contains(tokList, "e2euser") {
		t.Fatalf("auth token list:\n%s", tokList)
	}
	tokID := strings.Fields(strings.TrimSpace(tokList))[0]
	mustRun(t, cliBin, nil, "auth", "token", "revoke", tokID, "--db", db)
	code, _, _ = run(t, cliBin, nil,
		"issue", "list", "--server", server, "--token", token)
	if code != 1 {
		t.Fatalf("issue list with revoked token: exit %d, want 1", code)
	}

	comp := mustRun(t, cliBin, nil, "completion", "bash")
	if !strings.Contains(comp, "complete -F") {
		t.Fatalf("completion bash:\n%s", comp)
	}
}
