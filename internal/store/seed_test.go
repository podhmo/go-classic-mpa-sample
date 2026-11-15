package store

import (
	"context"
	"testing"
)

func TestSeedDemoData(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SeedDemoData(ctx); err != nil {
		t.Fatalf("SeedDemoData: %v", err)
	}

	issues, total, err := s.ListIssues(ctx, ListFilter{Status: "all", PerPage: 100})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if total < 6 {
		t.Fatalf("seeded %d issues, want >= 6", total)
	}

	var open, closed, withLabels, multiLabel int
	for _, i := range issues {
		switch i.Status {
		case StatusOpen:
			open++
		case StatusClosed:
			closed++
		}
		if len(i.Labels) > 0 {
			withLabels++
		}
		if len(i.Labels) >= 2 {
			multiLabel++
		}
	}
	if open == 0 || closed == 0 {
		t.Fatalf("want open and closed seeded: open=%d closed=%d", open, closed)
	}
	if withLabels == 0 || multiLabel == 0 {
		t.Fatalf("want labeled issues incl. multi-label: %d/%d", withLabels, multiLabel)
	}

	labels, err := s.ListLabels(ctx)
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) < 4 {
		t.Fatalf("seeded %d labels, want >= 4", len(labels))
	}

	var withComments int
	for _, i := range issues {
		cs, err := s.ListComments(ctx, i.ID)
		if err != nil {
			t.Fatalf("ListComments: %v", err)
		}
		if len(cs) > 0 {
			withComments++
		}
	}
	if withComments == 0 {
		t.Fatal("no seeded comments")
	}

	if _, err := s.AuthenticateUser(ctx, "demo", "demo"); err != nil {
		t.Fatalf("demo login after seed: %v", err)
	}

	cats, err := s.ListDiscussionCategories(ctx)
	if err != nil {
		t.Fatalf("ListDiscussionCategories: %v", err)
	}
	if len(cats) != 4 {
		t.Fatalf("seeded %d categories, want 4", len(cats))
	}
	ds, dtotal, err := s.ListDiscussions(ctx, DiscussionListFilter{})
	if err != nil {
		t.Fatalf("ListDiscussions: %v", err)
	}
	if dtotal < 3 {
		t.Fatalf("seeded %d discussions, want >= 3", dtotal)
	}
	var anyAnswered, anyThreaded bool
	for _, d := range ds {
		if d.Answered {
			anyAnswered = true
		}
		for _, c := range mustListDComments(t, s, d.ID) {
			if len(c.Replies) > 0 {
				anyThreaded = true
			}
		}
	}
	if !anyAnswered || !anyThreaded {
		t.Fatalf("want an answered and a threaded seeded discussion: answered=%v threaded=%v", anyAnswered, anyThreaded)
	}

	if err := s.SeedDemoData(ctx); err != nil {
		t.Fatalf("second SeedDemoData: %v", err)
	}
	_, total2, err := s.ListIssues(ctx, ListFilter{Status: "all", PerPage: 100})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if total2 != total {
		t.Fatalf("issue count changed after re-seed: %d -> %d", total, total2)
	}
	labels2, err := s.ListLabels(ctx)
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels2) != len(labels) {
		t.Fatalf("label count changed: %d -> %d", len(labels), len(labels2))
	}
	_, dtotal2, err := s.ListDiscussions(ctx, DiscussionListFilter{})
	if err != nil {
		t.Fatalf("ListDiscussions: %v", err)
	}
	if dtotal2 != dtotal {
		t.Fatalf("discussion count changed after re-seed: %d -> %d", dtotal, dtotal2)
	}

	s2 := newTestStore(t)
	if _, err := s2.CreateIssue(ctx, IssueInput{Title: "existing"}, 0); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := s2.SeedDemoData(ctx); err != nil {
		t.Fatalf("SeedDemoData on non-empty: %v", err)
	}
	_, n, err := s2.ListIssues(ctx, ListFilter{Status: "all"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 issue, got %d", n)
	}

	// ...but the demo user is still created on an upgraded database.
	if _, err := s2.AuthenticateUser(ctx, "demo", "demo"); err != nil {
		t.Fatalf("demo login on pre-existing DB: %v", err)
	}
}

// TestSeedCategories covers `init`'s reference-data seed: a schema-only
// store gets the fixed categories (and nothing else), idempotently.
func TestSeedCategories(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cats, err := s.ListDiscussionCategories(ctx)
	if err != nil {
		t.Fatalf("ListDiscussionCategories: %v", err)
	}
	if len(cats) != 0 {
		t.Fatalf("fresh db has %d categories, want 0", len(cats))
	}

	if err := s.SeedCategories(ctx); err != nil {
		t.Fatalf("SeedCategories: %v", err)
	}
	if err := s.SeedCategories(ctx); err != nil {
		t.Fatalf("SeedCategories (idempotent): %v", err)
	}

	cats, err = s.ListDiscussionCategories(ctx)
	if err != nil {
		t.Fatalf("ListDiscussionCategories: %v", err)
	}
	if len(cats) != 4 {
		t.Fatalf("seeded %d categories, want 4", len(cats))
	}

	if _, total, err := s.ListDiscussions(ctx, DiscussionListFilter{}); err != nil {
		t.Fatalf("ListDiscussions: %v", err)
	} else if total != 0 {
		t.Fatalf("SeedCategories also created %d discussions", total)
	}
}
