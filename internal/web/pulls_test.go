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
