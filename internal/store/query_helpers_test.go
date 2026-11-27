package store

import (
	"context"
	"errors"
	"testing"
)

func TestPatchIssueInWorkspace(t *testing.T) {
	ctx := context.Background()
	s := mustOpen(t, ":memory:")
	ws2, err := s.CreateWorkspace(ctx, WorkspaceInput{Slug: "other"})
	if err != nil {
		t.Fatal(err)
	}
	labelID, err := s.CreateLabel(ctx, 1, KindLabelIssue, LabelInput{Name: "bug", Color: "#ff0000"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateIssue(ctx, 1, IssueInput{Title: "old", Body: "kept", LabelIDs: []int64{labelID}}, 0)
	if err != nil {
		t.Fatal(err)
	}

	title := "new"
	if err := s.PatchIssueInWorkspace(ctx, 1, id, IssuePatch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetIssueInWorkspace(ctx, 1, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "new" || got.Body != "kept" || len(got.Labels) != 1 || got.Labels[0].ID != labelID {
		t.Fatalf("patched issue = %+v", got)
	}

	empty := ""
	if err := s.PatchIssueInWorkspace(ctx, ws2, id, IssuePatch{Body: &empty}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace patch = %v", err)
	}
	got, _ = s.GetIssueInWorkspace(ctx, 1, id)
	if got.Body != "kept" {
		t.Fatalf("cross-workspace patch changed body to %q", got.Body)
	}
	if _, err := s.GetIssueInWorkspace(ctx, ws2, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace get = %v", err)
	}
}

func TestGetCommentScopesToIssue(t *testing.T) {
	ctx := context.Background()
	s := mustOpen(t, ":memory:")
	issueID, err := s.CreateIssue(ctx, 1, IssueInput{Title: "one"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := s.CreateIssue(ctx, 1, IssueInput{Title: "two"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	commentID, err := s.CreateComment(ctx, issueID, "hello", 0)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.GetComment(ctx, issueID, commentID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Body != "hello" || c.IssueID != issueID {
		t.Fatalf("comment = %+v", c)
	}
	if _, err := s.GetComment(ctx, otherID, commentID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong issue = %v", err)
	}
}

func TestLabelByNamePrefersWorkspaceLabel(t *testing.T) {
	ctx := context.Background()
	s := mustOpen(t, ":memory:")
	sharedID, err := s.CreateLabel(ctx, 0, KindLabelIssue, LabelInput{Name: "bug", Color: "#111111"})
	if err != nil {
		t.Fatal(err)
	}
	ownID, err := s.CreateLabel(ctx, 1, KindLabelIssue, LabelInput{Name: "bug", Color: "#222222"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.LabelByName(ctx, 1, KindLabelIssue, "bug")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != ownID {
		t.Fatalf("workspace lookup id = %d, want %d", got.ID, ownID)
	}
	got, err = s.LabelByName(ctx, 2, KindLabelIssue, "bug")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != sharedID {
		t.Fatalf("shared lookup id = %d, want %d", got.ID, sharedID)
	}
	if _, err := s.LabelByName(ctx, 1, KindLabelDiscussion, "bug"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong kind = %v", err)
	}
}

func TestGetPRCommentScopesToPullRequest(t *testing.T) {
	ctx := context.Background()
	s := mustOpen(t, ":memory:")
	prID, err := s.CreatePullRequest(ctx, prInput(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	other := prInput()
	other.HeadRef = "refs/heads/other"
	otherID, err := s.CreatePullRequest(ctx, other, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	commentID, err := s.CreatePRComment(ctx, prID, PRCommentInput{Body: "hello"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.GetPRComment(ctx, prID, commentID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Body != "hello" || c.PRID != prID {
		t.Fatalf("comment = %+v", c)
	}
	if _, err := s.GetPRComment(ctx, otherID, commentID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong pull request = %v", err)
	}
}
