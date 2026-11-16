package store

import (
	"context"
	"testing"

	"github.com/podhmo/go-classic-mpa-sample/internal/events"
)

// The listener is the live-updates channel: every committed mutation must
// reach it with the right topic/kind/target so open pages can refetch.
func TestNotifyOnCreateComment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateIssue(ctx, IssueInput{Title: "n"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []events.Change
	s.SetListener(func(c events.Change) { got = append(got, c) })

	cid, err := s.CreateComment(ctx, id, "hello", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 change, got %d", len(got))
	}
	c := got[0]
	if c.Topic != events.IssueTopic(id) || c.Kind != events.KindComment || c.Target != cid {
		t.Fatalf("got %+v", c)
	}
}

// SetThreadAnswer reports the previously marked thread in Parent so the
// client can re-render both <li>s.
func TestNotifyAnswerReportsPreviousMark(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	qa := mustCategory(t, s, "Q&A", "qa", CategoryFormatQA)
	did, err := s.CreateDiscussion(ctx, DiscussionInput{Title: "d", CategoryID: qa}, 0)
	if err != nil {
		t.Fatal(err)
	}
	t1, err := s.CreateDiscussionThread(ctx, did, "a", 0)
	if err != nil {
		t.Fatal(err)
	}
	t2, err := s.CreateDiscussionThread(ctx, did, "b", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetThreadAnswer(ctx, did, t1, true); err != nil {
		t.Fatal(err)
	}

	var got []events.Change
	s.SetListener(func(c events.Change) { got = append(got, c) })
	if err := s.SetThreadAnswer(ctx, did, t2, true); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 change, got %d", len(got))
	}
	c := got[0]
	if c.Topic != events.DiscussionTopic(did) || c.Kind != events.KindAnswer ||
		c.Target != t2 || c.Parent != t1 {
		t.Fatalf("got %+v, want answer target=%d parent=%d", c, t2, t1)
	}
}

// A nil listener must not panic: the CLI-only paths and tests without a
// hub still run the same mutating methods.
func TestNotifyWithoutListener(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateIssue(context.Background(), IssueInput{Title: "x"}, 0); err != nil {
		t.Fatal(err)
	}
}
