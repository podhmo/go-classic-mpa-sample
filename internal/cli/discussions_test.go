package cli

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// seedCategories gives a fresh DB the standard category rows (and the
// rest of the demo data — SeedDemoData is all-or-nothing).
func seedCategories(t *testing.T, dbPath string) {
	t.Helper()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SeedDemoData(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// createDiscussion runs `discussion create` and returns the new id.
func createDiscussion(t *testing.T, o Options, title, category string) int64 {
	t.Helper()
	code, out, errOut := run(t, o, "discussion", "create",
		"--title", title, "--category", category, "--body", "body of "+title)
	var id int64
	if code != ExitOK {
		t.Fatalf("discussion create: exit %d\n%s", code, errOut)
	}
	if _, err := fmt.Sscanf(out, "created discussion #%d", &id); err != nil || id == 0 {
		t.Fatalf("create output: %q", out)
	}
	return id
}

// createDComment runs `discussion comment create` and returns the new id.
func createDComment(t *testing.T, o Options, id int64, body string) int64 {
	t.Helper()
	code, out, _ := run(t, o, "discussion", "comment", "create", itoa(id), "--body", body)
	var cid int64
	if code != ExitOK {
		t.Fatalf("comment create: exit %d\n%s", code, out)
	}
	if _, err := fmt.Sscanf(out, "created comment #%d", &cid); err != nil || cid == 0 {
		t.Fatalf("comment output: %q", out)
	}
	return cid
}

func TestDiscussionLifecycleLocal(t *testing.T) {
	db := t.TempDir() + "/test.db"
	seedCategories(t, db)
	o := localOptions(t, db)

	id := createDiscussion(t, o, "cli question", "qa")

	code, out, _ := run(t, o, "discussion", "list", "--search", "cli question")
	if code != ExitOK || !strings.Contains(out, "cli question") || !strings.Contains(out, "qa") {
		t.Fatalf("list: exit %d\n%s", code, out)
	}

	code, out, _ = run(t, o, "discussion", "view", itoa(id))
	if code != ExitOK || !strings.Contains(out, "title:\tcli question") ||
		!strings.Contains(out, "category:\tQ&A (qa)") ||
		!strings.Contains(out, "answered:\tno") {
		t.Fatalf("view: exit %d\n%s", code, out)
	}

	cid := createDComment(t, o, id, "the answer")
	code, out, _ = run(t, o, "discussion", "comment", "create", itoa(id),
		"--parent", itoa(cid), "--body", "a reply")
	if code != ExitOK || !strings.Contains(out, "created reply #") {
		t.Fatalf("reply create: exit %d\n%s", code, out)
	}

	code, out, _ = run(t, o, "discussion", "view", itoa(id), "--comments")
	if code != ExitOK || !strings.Contains(out, "the answer") || !strings.Contains(out, "a reply") {
		t.Fatalf("view --comments: exit %d\n%s", code, out)
	}

	code, out, _ = run(t, o, "discussion", "answer", itoa(id), "--comment", itoa(cid))
	if code != ExitOK || !strings.Contains(out, "marked comment #") {
		t.Fatalf("answer: exit %d\n%s", code, out)
	}
	code, out, _ = run(t, o, "discussion", "view", itoa(id))
	if !strings.Contains(out, "answered:\tyes") {
		t.Fatalf("view after answer:\n%s", out)
	}
	code, out, _ = run(t, o, "discussion", "list", "--answered", "yes", "--search", "cli question")
	if code != ExitOK || !strings.Contains(out, "cli question") {
		t.Fatalf("list --answered yes: exit %d\n%s", code, out)
	}
	code, out, _ = run(t, o, "discussion", "answer", itoa(id), "--comment", itoa(cid), "--unmark")
	if code != ExitOK || !strings.Contains(out, "unmarked") {
		t.Fatalf("unmark: exit %d\n%s", code, out)
	}
	code, out, _ = run(t, o, "discussion", "view", itoa(id))
	if !strings.Contains(out, "answered:\tno") {
		t.Fatalf("view after unmark:\n%s", out)
	}
}

func TestDiscussionEditDeleteAndJSON(t *testing.T) {
	db := t.TempDir() + "/test.db"
	seedCategories(t, db)
	o := localOptions(t, db)

	id := createDiscussion(t, o, "to rename", "general")

	code, _, _ := run(t, o, "discussion", "edit", itoa(id), "--title", "renamed", "--category", "ideas")
	if code != ExitOK {
		t.Fatalf("edit: exit %d", code)
	}
	code, out, _ := run(t, o, "discussion", "view", itoa(id))
	if code != ExitOK || !strings.Contains(out, "title:\trenamed") ||
		!strings.Contains(out, "category:\tIdeas (ideas)") {
		t.Fatalf("view after edit: exit %d\n%s", code, out)
	}

	code, out, _ = run(t, o, "discussion", "list", "--category", "ideas", "--json", "id,title,category")
	if code != ExitOK || !strings.Contains(out, `"title": "renamed"`) || !strings.Contains(out, `"slug": "ideas"`) {
		t.Fatalf("list --json: exit %d\n%s", code, out)
	}

	code, out, _ = run(t, o, "discussion", "delete", itoa(id))
	if code != ExitOK || !strings.Contains(out, "deleted discussion #") {
		t.Fatalf("delete: exit %d\n%s", code, out)
	}
	code, _, _ = run(t, o, "discussion", "view", itoa(id))
	if code != ExitError {
		t.Fatalf("view after delete: exit %d, want %d", code, ExitError)
	}
}

func TestDiscussionUsageAndErrors(t *testing.T) {
	db := t.TempDir() + "/test.db"
	seedCategories(t, db)
	o := localOptions(t, db)

	for _, args := range [][]string{
		{"discussion", "view"},                                             // missing number
		{"discussion", "view", "abc"},                                      // bad number
		{"discussion", "create", "--title", "x"},                           // missing --category
		{"discussion", "create", "--category", "qa"},                       // missing --title
		{"discussion", "edit", "1"},                                        // nothing to update
		{"discussion", "comment"},                                          // missing action
		{"discussion", "answer", "1"},                                      // missing --comment
		{"discussion", "view", "1", "-w"},                                  // -w without --server
		{"discussion", "create", "--title", "x", "--category", "qa", "-w"}, // -w without --server
	} {
		code, _, _ := run(t, o, args...)
		if code != ExitUsage {
			t.Fatalf("args %v: want exit %d, got %d", args, ExitUsage, code)
		}
	}

	code, _, errOut := run(t, o, "discussion", "create",
		"--title", "x", "--category", "nope")
	if code != ExitError || !strings.Contains(errOut, "qa") {
		t.Fatalf("unknown category: exit %d\n%s", code, errOut)
	}

	id := createDiscussion(t, o, "not qa", "general")
	cid := createDComment(t, o, id, "c")
	code, _, _ = run(t, o, "discussion", "answer", itoa(id), "--comment", itoa(cid))
	if code != ExitError {
		t.Fatalf("answer on open category: exit %d, want %d", code, ExitError)
	}
}
