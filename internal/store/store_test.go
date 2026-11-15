package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustLabel(t *testing.T, s *Store, name, color string) int64 {
	t.Helper()
	res, err := s.db.Exec("INSERT INTO labels(name, color) VALUES(?, ?)", name, color)
	if err != nil {
		t.Fatalf("insert label: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return id
}

// backdate moves created_at/updated_at of an issue age into the past.
func backdate(t *testing.T, s *Store, id int64, age time.Duration) {
	t.Helper()
	ts := time.Now().UTC().Add(-age).Unix()
	if _, err := s.db.Exec("UPDATE issues SET created_at = ?, updated_at = ? WHERE id = ?", ts, ts, id); err != nil {
		t.Fatalf("backdate: %v", err)
	}
}

func labelNames(i *Issue) []string {
	var out []string
	for _, l := range i.Labels {
		out = append(out, l.Name)
	}
	return out
}

func issueIDs(issues []*Issue) []int64 {
	out := make([]int64, len(issues))
	for i, is := range issues {
		out[i] = is.ID
	}
	return out
}

func equalIDs(got []int64, want ...int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestCreateAndGetIssue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	bug := mustLabel(t, s, "Bug", "#d73a4a")
	feat := mustLabel(t, s, "Feature", "#0075ca")

	id, err := s.CreateIssue(ctx, IssueInput{
		Title: "first issue", Body: "hello body", LabelIDs: []int64{feat, bug},
	}, 0)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	i, err := s.GetIssue(ctx, id)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if i.Title != "first issue" || i.Body != "hello body" {
		t.Fatalf("unexpected issue: %+v", i)
	}
	if i.Status != StatusOpen {
		t.Fatalf("status = %q, want open", i.Status)
	}
	if i.ID != id {
		t.Fatalf("id = %d, want %d", i.ID, id)
	}
	names := labelNames(i)
	if len(names) != 2 || names[0] != "Bug" || names[1] != "Feature" {
		t.Fatalf("labels = %v, want [Bug Feature] (sorted by name)", names)
	}
	if i.Labels[0].Color != "#d73a4a" {
		t.Fatalf("label color = %q", i.Labels[0].Color)
	}
	if time.Since(i.CreatedAt) > time.Minute || i.CreatedAt.Location() != time.UTC {
		t.Fatalf("created_at = %v, want recent UTC", i.CreatedAt)
	}
}

func TestIssueIDsNotReused(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id1, err := s.CreateIssue(ctx, IssueInput{Title: "one"}, 0)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := s.DeleteIssue(ctx, id1); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	id2, err := s.CreateIssue(ctx, IssueInput{Title: "two"}, 0)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if id2 <= id1 {
		t.Fatalf("id reused: %d after %d", id2, id1)
	}
}

func TestUpdateIssue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	bug := mustLabel(t, s, "Bug", "#d73a4a")
	feat := mustLabel(t, s, "Feature", "#0075ca")
	doc := mustLabel(t, s, "Docs", "#1f6feb")

	id, err := s.CreateIssue(ctx, IssueInput{Title: "old", Body: "old body", LabelIDs: []int64{bug, feat}}, 0)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	backdate(t, s, id, 24*time.Hour)

	err = s.UpdateIssue(ctx, id, IssueInput{Title: "new", Body: "new body", LabelIDs: []int64{doc}})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}

	i, err := s.GetIssue(ctx, id)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if i.Title != "new" || i.Body != "new body" {
		t.Fatalf("fields not updated: %+v", i)
	}
	if names := labelNames(i); len(names) != 1 || names[0] != "Docs" {
		t.Fatalf("labels = %v, want [Docs]", names)
	}
	if !i.UpdatedAt.After(i.CreatedAt) {
		t.Fatalf("updated_at %v not bumped past created_at %v", i.UpdatedAt, i.CreatedAt)
	}
}

// TestIssueLabelsSkipUnknownAndDuplicates verifies that unknown and
// duplicated label ids are silently dropped instead of failing with
// constraint errors.
func TestIssueLabelsSkipUnknownAndDuplicates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	bug := mustLabel(t, s, "Bug", "#d73a4a")

	id, err := s.CreateIssue(ctx, IssueInput{
		Title: "x", LabelIDs: []int64{bug, bug, 99999},
	}, 0)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	i, err := s.GetIssue(ctx, id)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if len(i.Labels) != 1 || i.Labels[0].ID != bug {
		t.Fatalf("labels = %+v, want [Bug]", i.Labels)
	}

	// Replacing the label set with only unknown ids succeeds and clears it.
	if err := s.UpdateIssue(ctx, id, IssueInput{Title: "x", LabelIDs: []int64{99999}}); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	i, err = s.GetIssue(ctx, id)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if len(i.Labels) != 0 {
		t.Fatalf("labels = %+v, want empty", i.Labels)
	}
}

func TestSetIssueStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateIssue(ctx, IssueInput{Title: "x"}, 0)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	backdate(t, s, id, time.Hour)

	if err := s.SetIssueStatus(ctx, id, StatusClosed); err != nil {
		t.Fatalf("SetIssueStatus: %v", err)
	}
	i, err := s.GetIssue(ctx, id)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if i.Status != StatusClosed {
		t.Fatalf("status = %q, want closed", i.Status)
	}
	if !i.UpdatedAt.After(i.CreatedAt) {
		t.Fatalf("updated_at not bumped")
	}

	if err := s.SetIssueStatus(ctx, id, StatusOpen); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := s.SetIssueStatus(ctx, id, "bogus"); err == nil {
		t.Fatal("expected error for invalid status")
	} else if errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid status should not yield ErrNotFound: %v", err)
	}
}

// TestSetIssueStatusUpdatedAt verifies updated_at is bumped only by an
// actual status change, while ErrNotFound still reports missing issues.
func TestSetIssueStatusUpdatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateIssue(ctx, IssueInput{Title: "x"}, 0)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	backdate(t, s, id, time.Hour)
	before, err := s.GetIssue(ctx, id)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}

	// Re-posting the current status is a no-op: updated_at is unchanged.
	if err := s.SetIssueStatus(ctx, id, StatusOpen); err != nil {
		t.Fatalf("SetIssueStatus(same): %v", err)
	}
	same, err := s.GetIssue(ctx, id)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if !same.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("no-op status bumped updated_at: %v -> %v", before.UpdatedAt, same.UpdatedAt)
	}

	// An actual change bumps updated_at.
	if err := s.SetIssueStatus(ctx, id, StatusClosed); err != nil {
		t.Fatalf("SetIssueStatus: %v", err)
	}
	changed, err := s.GetIssue(ctx, id)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if !changed.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("updated_at not bumped: %v -> %v", before.UpdatedAt, changed.UpdatedAt)
	}

	// Missing issue still yields ErrNotFound (RowsAffected semantics kept).
	if err := s.SetIssueStatus(ctx, 4242, StatusClosed); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetIssueStatus missing = %v, want ErrNotFound", err)
	}
}

func TestDeleteIssue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	bug := mustLabel(t, s, "Bug", "#d73a4a")
	id, err := s.CreateIssue(ctx, IssueInput{Title: "gone", LabelIDs: []int64{bug}}, 0)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if _, err := s.CreateComment(ctx, id, "a comment", 0); err != nil {
		t.Fatalf("CreateComment: %v", err)
	}

	if err := s.DeleteIssue(ctx, id); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	if _, err := s.GetIssue(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetIssue after delete = %v, want ErrNotFound", err)
	}
	cs, err := s.ListComments(ctx, id)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(cs) != 0 {
		t.Fatalf("comments not cascaded: %v", cs)
	}
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM issue_labels WHERE issue_id = ?", id).Scan(&n); err != nil {
		t.Fatalf("count issue_labels: %v", err)
	}
	if n != 0 {
		t.Fatalf("label links not cascaded: %d rows", n)
	}
	if err := s.DeleteIssue(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v, want ErrNotFound", err)
	}
}

func TestComments(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateIssue(ctx, IssueInput{Title: "threaded"}, 0)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	for _, b := range []string{"first", "second", "third"} {
		if _, err := s.CreateComment(ctx, id, b, 0); err != nil {
			t.Fatalf("CreateComment(%q): %v", b, err)
		}
	}
	// Make timestamps distinct so ordering by created_at is exercised.
	old := time.Now().UTC().Add(-time.Hour).Unix()
	if _, err := s.db.Exec("UPDATE comments SET created_at = ? WHERE body = 'third'", old); err != nil {
		t.Fatalf("backdate comment: %v", err)
	}

	cs, err := s.ListComments(ctx, id)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(cs) != 3 {
		t.Fatalf("got %d comments, want 3", len(cs))
	}
	if cs[0].Body != "third" || cs[1].Body != "first" || cs[2].Body != "second" {
		t.Fatalf("order = %q,%q,%q", cs[0].Body, cs[1].Body, cs[2].Body)
	}
	for _, c := range cs {
		if c.IssueID != id || c.ID <= 0 {
			t.Fatalf("bad comment: %+v", c)
		}
	}
}

// TestCreateCommentBumpsIssueUpdatedAt verifies that a new comment counts
// as activity on its issue.
func TestCreateCommentBumpsIssueUpdatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateIssue(ctx, IssueInput{Title: "x"}, 0)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	backdate(t, s, id, time.Hour)
	before, err := s.GetIssue(ctx, id)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}

	cid, err := s.CreateComment(ctx, id, "ping", 0)
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	after, err := s.GetIssue(ctx, id)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("comment did not bump updated_at: %v -> %v", before.UpdatedAt, after.UpdatedAt)
	}
	cs, err := s.ListComments(ctx, id)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(cs) != 1 || cs[0].ID != cid {
		t.Fatalf("comments = %+v, want 1 comment id=%d", cs, cid)
	}
	if after.UpdatedAt.Before(cs[0].CreatedAt) {
		t.Fatalf("updated_at %v before comment created_at %v", after.UpdatedAt, cs[0].CreatedAt)
	}
}

func TestCreateCommentMissingIssue(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateComment(context.Background(), 9999, "hi", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateComment = %v, want ErrNotFound", err)
	}
}

func TestNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.GetIssue(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetIssue = %v", err)
	}
	if err := s.UpdateIssue(ctx, 42, IssueInput{Title: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateIssue = %v", err)
	}
	if err := s.SetIssueStatus(ctx, 42, StatusClosed); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetIssueStatus = %v", err)
	}
	if err := s.DeleteIssue(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteIssue = %v", err)
	}
}

func TestListIssues(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	bug := mustLabel(t, s, "Bug", "#d73a4a")
	feat := mustLabel(t, s, "Feature", "#0075ca")

	mk := func(title, body string, age time.Duration, labels ...int64) int64 {
		id, err := s.CreateIssue(ctx, IssueInput{Title: title, Body: body, LabelIDs: labels}, 0)
		if err != nil {
			t.Fatalf("CreateIssue(%q): %v", title, err)
		}
		backdate(t, s, id, age)
		return id
	}

	i1 := mk("Fix crash on startup", "boom", 3*24*time.Hour, bug)
	i2 := mk("Add export feature", "needs csv dump support", 2*24*time.Hour, feat)
	i3 := mk("Improve docs", "more examples", 24*time.Hour)
	i4 := mk("Crash report details", "contains a stacktrace", 4*24*time.Hour, bug)
	if err := s.SetIssueStatus(ctx, i3, StatusClosed); err != nil {
		t.Fatalf("SetIssueStatus: %v", err)
	}

	check := func(name string, f ListFilter, wantTotal int, wantIDs ...int64) {
		t.Helper()
		issues, total, err := s.ListIssues(ctx, f)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if total != wantTotal {
			t.Fatalf("%s: total = %d, want %d", name, total, wantTotal)
		}
		if !equalIDs(issueIDs(issues), wantIDs...) {
			t.Fatalf("%s: ids = %v, want %v", name, issueIDs(issues), wantIDs)
		}
	}

	check("default status is open", ListFilter{}, 3, i2, i1, i4)
	check("status all", ListFilter{Status: "all"}, 4, i3, i2, i1, i4)
	check("status closed", ListFilter{Status: StatusClosed}, 1, i3)
	check("query hits title (case-insensitive)", ListFilter{Query: "CRASH"}, 2, i1, i4)
	check("query hits body only", ListFilter{Query: "stacktrace"}, 1, i4)
	check("query hits body of another issue", ListFilter{Query: "csv"}, 1, i2)
	check("query no match", ListFilter{Query: "zzz"}, 0)
	check("label filter bug", ListFilter{LabelID: bug}, 2, i1, i4)
	check("label filter feature", ListFilter{LabelID: feat}, 1, i2)
	check("oldest first", ListFilter{Status: "all", Sort: "oldest"}, 4, i4, i1, i2, i3)
	check("newest explicit", ListFilter{Status: "all", Sort: "newest"}, 4, i3, i2, i1, i4)

	issues, total, err := s.ListIssues(ctx, ListFilter{Status: "all", PerPage: 2, Page: 1})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if total != 4 || !equalIDs(issueIDs(issues), i3, i2) {
		t.Fatalf("page1: ids=%v total=%d", issueIDs(issues), total)
	}
	issues, _, err = s.ListIssues(ctx, ListFilter{Status: "all", PerPage: 2, Page: 2})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if !equalIDs(issueIDs(issues), i1, i4) {
		t.Fatalf("page2: ids=%v", issueIDs(issues))
	}
	issues, total, err = s.ListIssues(ctx, ListFilter{Status: "all", PerPage: 2, Page: 3})
	if err != nil {
		t.Fatalf("page3: %v", err)
	}
	if total != 4 || len(issues) != 0 {
		t.Fatalf("page3: len=%d total=%d", len(issues), total)
	}
	// Page < 1 clamps to 1.
	issues, _, err = s.ListIssues(ctx, ListFilter{Status: "all", PerPage: 2, Page: 0})
	if err != nil {
		t.Fatalf("page0: %v", err)
	}
	if !equalIDs(issueIDs(issues), i3, i2) {
		t.Fatalf("page0: ids=%v", issueIDs(issues))
	}

	// Labels are populated on listed issues.
	issues, _, err = s.ListIssues(ctx, ListFilter{LabelID: bug})
	if err != nil {
		t.Fatalf("label populated: %v", err)
	}
	if len(issues[0].Labels) != 1 || issues[0].Labels[0].Name != "Bug" {
		t.Fatalf("labels not populated: %+v", issues[0].Labels)
	}
}

func TestListLabels(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mustLabel(t, s, "Zeta", "#000000")
	mustLabel(t, s, "Alpha", "#ffffff")
	mustLabel(t, s, "Mid", "#888888")

	ls, err := s.ListLabels(ctx)
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(ls) != 3 || ls[0].Name != "Alpha" || ls[1].Name != "Mid" || ls[2].Name != "Zeta" {
		t.Fatalf("not sorted by name: %+v", ls)
	}
}

// TestFileDBPragmas verifies pragmas reach every pooled connection on a real
// file database (foreign_keys is per-connection in SQLite).
func TestFileDBPragmas(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()

	for i := range 10 {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn: %v", err)
		}
		var on int
		if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&on); err != nil {
			conn.Close()
			t.Fatalf("PRAGMA foreign_keys: %v", err)
		}
		conn.Close()
		if on != 1 {
			t.Fatalf("conn %d: foreign_keys = %d, want 1", i, on)
		}
	}
	var bt int
	if err := s.db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&bt); err != nil {
		t.Fatalf("PRAGMA busy_timeout: %v", err)
	}
	if bt != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", bt)
	}
}

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

	// Idempotent: second call changes nothing.
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

	// Seed on a non-empty DB is a no-op even if it would duplicate labels.
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
}

func TestSchemaObjects(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tables, indexes, err := s.SchemaObjects(ctx)
	if err != nil {
		t.Fatalf("SchemaObjects: %v", err)
	}

	// All six application tables plus sqlite_sequence (AUTOINCREMENT
	// bookkeeping), sorted by name.
	wantTables := []string{"comments", "issue_labels", "issues", "labels", "sessions", "sqlite_sequence", "users"}
	if !slices.Equal(tables, wantTables) {
		t.Errorf("tables = %v, want %v", tables, wantTables)
	}

	// Only the four named indexes from the schema; the UNIQUE/PK backing
	// indexes (sqlite_autoindex%) must be filtered out.
	wantIndexes := []string{"idx_comments_issue_created", "idx_issue_labels_label", "idx_issues_status_created", "idx_sessions_user"}
	if !slices.Equal(indexes, wantIndexes) {
		t.Errorf("indexes = %v, want %v", indexes, wantIndexes)
	}
	for _, name := range indexes {
		if strings.HasPrefix(name, "sqlite_autoindex") {
			t.Errorf("internal index %q leaked into SchemaObjects", name)
		}
	}
}

func TestAnalyze(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.Analyze(ctx); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := s.Analyze(ctx); err != nil {
		t.Fatalf("second Analyze: %v", err)
	}

	// ANALYZE may create sqlite_stat% bookkeeping tables; SchemaObjects
	// filters them, so the reported schema stays stable across runs.
	tables, _, err := s.SchemaObjects(ctx)
	if err != nil {
		t.Fatalf("SchemaObjects after ANALYZE: %v", err)
	}
	wantTables := []string{"comments", "issue_labels", "issues", "labels", "sessions", "sqlite_sequence", "users"}
	if !slices.Equal(tables, wantTables) {
		t.Errorf("tables after ANALYZE = %v, want %v", tables, wantTables)
	}
}

func TestMigrationFreshAndLegacyDB(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "mig.db")

	// A fresh database walks every migration and lands on schemaVersion.
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open fresh: %v", err)
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("fresh DB user_version = %d, want %d", version, schemaVersion)
	}
	for _, q := range []string{
		"SELECT author_id FROM issues LIMIT 0",
		"SELECT author_id FROM comments LIMIT 0",
		"SELECT id FROM users LIMIT 0",
		"SELECT id, expires_at FROM sessions LIMIT 0",
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Errorf("v2 column check %q: %v", q, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopening is a no-op, not a re-run of the ALTERs (which would fail).
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen migrated DB: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestMigrationFromVersionZero(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	// Simulate a database created before migrations existed: the v1 schema
	// applied, user_version still 0. Raw driver access, not Open.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.ExecContext(ctx, schemaDDL); err != nil {
		t.Fatalf("apply legacy schema: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO issues(title, body, status, created_at, updated_at) VALUES('keep me', '', 'open', 1, 1)"); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Open must replay every version and keep existing rows.
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy DB: %v", err)
	}
	defer s.Close()
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("migrated DB user_version = %d, want %d", version, schemaVersion)
	}
	is, err := s.GetIssue(ctx, 1)
	if err != nil {
		t.Fatalf("GetIssue after migration: %v", err)
	}
	if is.Title != "keep me" {
		t.Errorf("existing row lost: title = %q", is.Title)
	}
	var authorID any
	if err := s.db.QueryRowContext(ctx, "SELECT author_id FROM issues WHERE id = 1").Scan(&authorID); err != nil {
		t.Fatalf("author_id column: %v", err)
	}
	if authorID != nil {
		t.Errorf("pre-migration row should keep author_id NULL, got %v", authorID)
	}
}
