package store

import (
	"context"
	"testing"
)

func TestWorkspaceRefBySlugSkipsCounts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateIssue(ctx, 1, IssueInput{Title: "counted"}, 0); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	cat := mustCategory(t, s, "General", "general", CategoryFormatOpen)
	mustDiscussion(t, s, cat, "counted")

	ref, err := s.WorkspaceRefBySlug(ctx, DefaultWorkspaceSlug)
	if err != nil {
		t.Fatalf("WorkspaceRefBySlug: %v", err)
	}
	if ref.ID == 0 || ref.Slug != DefaultWorkspaceSlug {
		t.Fatalf("ref = %+v, want default workspace identity", ref)
	}
	if ref.IssueCount != 0 || ref.DiscussionCount != 0 {
		t.Fatalf("ref counts = %d/%d, want zero-valued lightweight lookup", ref.IssueCount, ref.DiscussionCount)
	}
	full, err := s.WorkspaceBySlug(ctx, DefaultWorkspaceSlug)
	if err != nil {
		t.Fatalf("WorkspaceBySlug: %v", err)
	}
	if full.IssueCount != 1 || full.DiscussionCount != 1 {
		t.Fatalf("full counts = %d/%d, want 1/1", full.IssueCount, full.DiscussionCount)
	}
}
