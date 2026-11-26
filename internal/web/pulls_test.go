package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/gitrepo"
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

func initTestGitRepo(t *testing.T) *gitrepo.Repo {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-qm", "initial")

	repo, err := gitrepo.Resolve(context.Background(), gitrepo.Options{RepoPath: dir})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if repo == nil {
		t.Fatal("Resolve returned nil for an explicit repo")
	}
	return repo
}

func addFakePull(f *fakeStore, title, status string, created time.Time) {
	f.pulls = append(f.pulls, &store.PullRequest{
		ID:        f.nextPull,
		BaseRef:   "refs/heads/main",
		HeadRef:   "refs/heads/feat",
		Title:     title,
		Status:    status,
		CreatedAt: created,
		UpdatedAt: created,
	})
	f.nextPull++
}

func TestListPullsOutOfRangePageShowsLastPage(t *testing.T) {
	repo := initTestGitRepo(t)
	f := newFakeStore()
	now := time.Now()
	for i := range 25 {
		addFakePull(f, "Paged pull "+string(rune('a'+i)), store.PRStatusOpen, now.Add(-time.Duration(i)*time.Hour))
	}
	srv := httptest.NewServer(New(f, WithRepo(repo)))
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/pulls?status=all&page=99999")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{"Showing 21–25 of 25 pull requests", "Page 2 of 2", ">Previous</a>"} {
		if !strings.Contains(body, want) {
			t.Errorf("clamped pulls page missing %q", want)
		}
	}
	if strings.Contains(body, "empty-state") {
		t.Error("out-of-range pulls page must not render the empty state")
	}
	prevURL := "/pulls?sort=newest&status=all"
	if !strings.Contains(body, `href="`+strings.ReplaceAll(prevURL, "&", "&amp;")+`"`) {
		t.Error("clamped pulls page should link its rendered Previous button to page 1")
	}
	if strings.Contains(body, "q=&amp;") || strings.Contains(body, "q=&") {
		t.Error("rendered pulls pagination should omit an empty q parameter")
	}

	resp, err = srv.Client().Get(srv.URL + prevURL)
	if err != nil {
		t.Fatal(err)
	}
	body = getBody(t, resp)
	if !strings.Contains(body, "Showing 1–20 of 25 pull requests") || !strings.Contains(body, "Page 1 of 2") {
		t.Error("following the rendered Previous link should show the first pulls page")
	}

	resp, err = srv.Client().Get(srv.URL + "/pulls?status=all&page=1000001")
	if err != nil {
		t.Fatal(err)
	}
	body = getBody(t, resp)
	if !strings.Contains(body, "Showing 1–20 of 25 pull requests") || !strings.Contains(body, "Page 1 of 2") {
		t.Error("page above MaxListPage must fall back to page 1")
	}
}

func TestPullsURLOmitsEmptyQuery(t *testing.T) {
	got := pullsURL(store.PRListFilter{Status: "all", Sort: "newest"}, 2)
	if strings.Contains(got, "q=") {
		t.Fatalf("empty q leaked into %q", got)
	}
	if !strings.Contains(got, "page=2") || !strings.Contains(got, "status=all") {
		t.Fatalf("filter or page missing from %q", got)
	}
}

func TestPullsNavStateIsPerHandler(t *testing.T) {
	repo := initTestGitRepo(t)
	f := newFakeStore()
	withRepo := httptest.NewServer(New(f, WithRepo(repo)))
	t.Cleanup(withRepo.Close)
	withoutRepo := httptest.NewServer(New(f))
	t.Cleanup(withoutRepo.Close)

	resp, err := withRepo.Client().Get(withRepo.URL + "/w/default/issues")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("repo-backed status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, `<a href="/pulls"`) {
		t.Error("repo-backed handler should render the pulls nav link")
	}

	resp, err = withoutRepo.Client().Get(withoutRepo.URL + "/w/default/issues")
	if err != nil {
		t.Fatal(err)
	}
	body = getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("repo-less status = %d, want 200", resp.StatusCode)
	}
	if strings.Contains(body, `<a href="/pulls"`) {
		t.Error("repo-less handler should not inherit pulls state from another handler")
	}
}

func TestCreatePullInvalidRefRendersUnprocessable(t *testing.T) {
	repo := initTestGitRepo(t)
	srv := httptest.NewServer(New(newFakeStore(), WithRepo(repo)))
	t.Cleanup(srv.Close)

	for _, tc := range []struct {
		name string
		base string
		head string
	}{
		{name: "base invalid head valid", base: "no-such-base", head: "main"},
		{name: "base valid head invalid", base: "main", head: "no-such-head"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := postForm(t, srv.Client(), srv.URL+"/pulls", url.Values{
				"title": {"test pull"},
				"base":  {tc.base},
				"head":  {tc.head},
			})
			body := getBody(t, resp)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", resp.StatusCode)
			}
			if !strings.Contains(body, "Base and head must name existing refs or commits.") {
				t.Error("response should show the refs validation error")
			}
		})
	}
}

func TestSplitUnifiedDiffsUsesChangedFileOrder(t *testing.T) {
	changes := []gitrepo.FileChange{
		{Status: "M", OldPath: "dir/a file.txt", NewPath: "dir/a file.txt"},
		{Status: "A", OldPath: "b.txt", NewPath: "b.txt"},
	}
	patch := "diff --git a/dir/a file.txt b/dir/a file.txt\n" +
		"--- a/dir/a file.txt\n+++ b/dir/a file.txt\n@@ -1 +1 @@\n-one\n+two\n" +
		"diff --git a/b.txt b/b.txt\n" +
		"--- /dev/null\n+++ b/b.txt\n@@ -0,0 +1 @@\n+x\n"
	got := splitUnifiedDiffs(patch, changes)
	if !strings.Contains(got["dir/a file.txt"], "+two") || strings.Contains(got["dir/a file.txt"], "+x") {
		t.Fatalf("first file split wrong: %q", got["dir/a file.txt"])
	}
	if !strings.Contains(got["b.txt"], "+x") || strings.Contains(got["b.txt"], "+two") {
		t.Fatalf("second file split wrong: %q", got["b.txt"])
	}
}

func TestPullFilesRendersOneServerSideCommentForm(t *testing.T) {
	repo := initTestGitRepo(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo.Dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	git("checkout", "-qb", "feat")
	if err := os.WriteFile(filepath.Join(repo.Dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo.Dir, "b.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "a.txt", "b.txt")
	git("commit", "-qm", "change files")

	f := newFakeStore()
	f.pulls = append(f.pulls, &store.PullRequest{
		ID: 1, BaseRef: "refs/heads/main", HeadRef: "refs/heads/feat",
		Title: "files test", Status: store.PRStatusOpen,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	srv := httptest.NewServer(New(f, WithRepo(repo)))
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/pulls/1/files")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, `<table class="diff">`) {
		t.Error("diff should use table markup")
	}
	if strings.Contains(body, `<pre class="diff">`) || strings.Contains(body, `<details class="line-comment">`) {
		t.Error("old invalid pre/details diff markup remains")
	}
	if strings.Contains(body, `class="line-comment-form`) {
		t.Error("plain files view should not emit line comment forms")
	}
	if !strings.Contains(body, `line=2&amp;path=a.txt&amp;side=right`) {
		t.Error("commentable line should link to the server-rendered form GET")
	}

	resp, err = srv.Client().Get(srv.URL + "/pulls/1/files?path=a.txt&line=2&side=right")
	if err != nil {
		t.Fatal(err)
	}
	body = getBody(t, resp)
	if strings.Count(body, `class="line-comment-form`) != 1 {
		t.Fatalf("open form count = %d, want 1", strings.Count(body, `class="line-comment-form`))
	}
	for _, want := range []string{
		`<label class="visually-hidden" for="line-comment-body">Comment on a.txt line 2</label>`,
		`<input type="hidden" name="path" value="a.txt">`,
		`<input type="hidden" name="line" value="2">`,
		`<input type="hidden" name="side" value="right">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("open form missing %s", want)
		}
	}
}
