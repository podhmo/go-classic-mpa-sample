package cli

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// run exercises the CLI against a real store in a temp DB via the Options
// seams — the same path main takes minus the os.Args dance.
func run(t *testing.T, o Options, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	o.Args = args
	o.Stdout = &out
	o.Stderr = &errBuf
	if o.Stdin == nil {
		o.Stdin = strings.NewReader("")
	}
	code = Run(context.Background(), o)
	return code, out.String(), errBuf.String()
}

// localOptions points every command at dbPath's temp database.
func localOptions(t *testing.T, dbPath string) Options {
	t.Helper()
	return Options{
		OpenLocal: func(string) (Backend, io.Closer, error) {
			st, err := store.Open(dbPath)
			if err != nil {
				return nil, nil, err
			}
			return st, st, nil
		},
		OpenTokenStore: func(string) (TokenStore, error) {
			return store.Open(dbPath)
		},
		OpenRemote: func(string, string) (Backend, error) {
			t.Fatal("remote backend used in local-mode test")
			return nil, nil
		},
		OpenBrowser: func(string) error {
			t.Fatal("browser used without -w")
			return nil
		},
	}
}

func seedIssue(t *testing.T, dbPath string, title string) int64 {
	t.Helper()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	id, err := st.CreateIssue(context.Background(),
		store.IssueInput{Title: title, Body: "body of " + title}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestIssueListAndViewLocal(t *testing.T) {
	db := t.TempDir() + "/test.db"
	id := seedIssue(t, db, "first issue")
	o := localOptions(t, db)

	code, out, _ := run(t, o, "issue", "list")
	if code != ExitOK {
		t.Fatalf("issue list: exit %d", code)
	}
	if !strings.Contains(out, "first issue") {
		t.Fatalf("issue list missing title:\n%s", out)
	}

	code, out, _ = run(t, o, "issue", "view", itoa(id))
	if code != ExitOK || !strings.Contains(out, "title:\tfirst issue") {
		t.Fatalf("issue view: exit %d\n%s", code, out)
	}
}

func TestIssueCreateJSONAndLifecycle(t *testing.T) {
	db := t.TempDir() + "/test.db"
	o := localOptions(t, db)

	code, out, _ := run(t, o, "issue", "create", "--title", "second", "--body", "b2")
	if code != ExitOK || !strings.Contains(out, "created issue #1") {
		t.Fatalf("create: exit %d\n%s", code, out)
	}

	code, out, _ = run(t, o, "issue", "list", "--state", "all", "--json", "id,title")
	if code != ExitOK {
		t.Fatalf("list --json: exit %d", code)
	}
	if !strings.Contains(out, `"id": 1`) || !strings.Contains(out, `"title": "second"`) {
		t.Fatalf("--json output missing fields:\n%s", out)
	}
	if strings.Contains(out, `"body"`) {
		t.Fatalf("--json leaked unrequested field:\n%s", out)
	}

	for _, tc := range []struct {
		action, want string
	}{
		{"close", "closed issue #1"},
		{"reopen", "reopened issue #1"},
		{"delete", "deleted issue #1"},
	} {
		code, out, _ = run(t, o, "issue", tc.action, "1")
		if code != ExitOK || !strings.Contains(out, tc.want) {
			t.Fatalf("issue %s: exit %d want %q got\n%s", tc.action, code, tc.want, out)
		}
	}
}

func TestIssueEditAndComment(t *testing.T) {
	db := t.TempDir() + "/test.db"
	id := seedIssue(t, db, "edit me")
	o := localOptions(t, db)

	code, _, _ := run(t, o, "issue", "edit", itoa(id), "--title", "edited")
	if code != ExitOK {
		t.Fatalf("edit: exit %d", code)
	}
	code, out, _ := run(t, o, "issue", "view", itoa(id))
	if code != ExitOK || !strings.Contains(out, "title:\tedited") {
		t.Fatalf("view after edit:\n%s", out)
	}

	code, out, _ = run(t, o, "comment", "create", itoa(id), "--body", "looks good")
	if code != ExitOK || !strings.Contains(out, "created comment #1") {
		t.Fatalf("comment create: exit %d\n%s", code, out)
	}
	code, out, _ = run(t, o, "issue", "view", itoa(id), "--comments")
	if code != ExitOK || !strings.Contains(out, "looks good") {
		t.Fatalf("view --comments:\n%s", out)
	}
}

func TestBodyFileStdin(t *testing.T) {
	db := t.TempDir() + "/test.db"
	o := localOptions(t, db)
	o.Stdin = strings.NewReader("stdin body\n")

	code, _, _ := run(t, o, "issue", "create", "--title", "piped", "--body-file", "-")
	if code != ExitOK {
		t.Fatalf("create --body-file -: exit %d", code)
	}
	code, out, _ := run(t, o, "issue", "view", "1")
	if code != ExitOK || !strings.Contains(out, "stdin body") {
		t.Fatalf("view piped body:\n%s", out)
	}
}

func TestUsageErrors(t *testing.T) {
	db := t.TempDir() + "/test.db"
	o := localOptions(t, db)
	for _, args := range [][]string{
		{"issue"},                    // missing action
		{"issue", "bogus"},           // unknown action
		{"issue", "view"},            // missing number
		{"issue", "view", "abc"},     // bad number
		{"issue", "create"},          // missing --title
		{"issue", "view", "1", "-w"}, // -w without --server
		{"api", "/issues"},           // remote-only
		{"auth", "token"},            // missing --user
		{"discussion", "list"},       // not implemented
		{"wat"},                      // unknown command
	} {
		code, _, errOut := run(t, o, args...)
		if code != ExitUsage {
			t.Fatalf("args %v: want exit %d, got %d (stderr %q)", args, ExitUsage, code, errOut)
		}
	}
}

func TestAuthTokenLocal(t *testing.T) {
	db := t.TempDir() + "/test.db"
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	uid, err := st.CreateUser(context.Background(), "carol", "hashed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UserByName(context.Background(), "carol"); err != nil {
		t.Fatal(err)
	}
	st.Close()

	o := localOptions(t, db)
	code, out, errOut := run(t, o, "auth", "token", "--user", "carol")
	if code != ExitOK {
		t.Fatalf("auth token: exit %d\n%s", code, errOut)
	}
	token := strings.TrimSpace(out)
	if token == "" || !strings.Contains(errOut, `"carol"`) {
		t.Fatalf("auth token output: stdout %q stderr %q", out, errOut)
	}

	st, err = store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	u, err := st.APITokenUser(context.Background(), token)
	if err != nil {
		t.Fatalf("issued token does not resolve: %v", err)
	}
	if u.ID != uid {
		t.Fatalf("token resolves to user %d, want %d", u.ID, uid)
	}

	code, _, _ = run(t, o, "auth", "token", "--user", "nobody")
	if code != ExitError {
		t.Fatalf("auth token --user nobody: want exit %d got %d", ExitError, code)
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

// Regression: Devin Review findings — validation, -w ordering, --json shape.
func TestReviewRegressions(t *testing.T) {
	db := t.TempDir() + "/test.db"
	id := seedIssue(t, db, "target")
	o := localOptions(t, db)

	// issue create --web without --server must not create anything.
	code, _, _ := run(t, o, "issue", "create", "--title", "no server", "-w")
	if code != ExitUsage {
		t.Fatalf("create -w: exit %d, want %d", code, ExitUsage)
	}
	code, out, _ := run(t, o, "issue", "list", "--state", "all")
	if strings.Contains(out, "no server") {
		t.Fatalf("create -w still wrote an issue:\n%s", out)
	}

	// Blank/oversized input is rejected like the web/API, not persisted.
	code, _, _ = run(t, o, "issue", "edit", itoa(id), "--title", "   ")
	if code != ExitError {
		t.Fatalf("edit --title '   ': exit %d, want %d", code, ExitError)
	}
	code, out, _ = run(t, o, "issue", "view", itoa(id))
	if !strings.Contains(out, "title:\ttarget") {
		t.Fatalf("bad edit was saved:\n%s", out)
	}
	code, _, _ = run(t, o, "comment", "create", itoa(id),
		"--body", strings.Repeat("x", 10001))
	if code != ExitError {
		t.Fatalf("oversized comment: exit %d, want %d", code, ExitError)
	}

	// view --comments --json keeps issue fields at the root.
	code, out, _ = run(t, o, "issue", "view", itoa(id), "--comments",
		"--json", "title,comments")
	if code != ExitOK {
		t.Fatalf("view --comments --json: exit %d", code)
	}
	if !strings.Contains(out, `"title": "target"`) || !strings.Contains(out, `"comments"`) {
		t.Fatalf("--json output:\n%s", out)
	}
}
