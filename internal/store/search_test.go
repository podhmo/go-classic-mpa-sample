package store

import (
	"context"
	"reflect"
	"testing"
)

func TestParseIssueSearchQuery(t *testing.T) {
	got := ParseIssueSearchQuery(`is:open label:Bug author:demo label:"Bug Fix" crash can't unknown:term`)
	want := IssueSearchQuery{
		Terms:    []string{"crash", "can't", "unknown:term"},
		Statuses: []string{StatusOpen},
		Labels:   []string{"Bug", "Bug Fix"},
		Authors:  []string{"demo"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseIssueSearchQuery() = %#v, want %#v", got, want)
	}
}

func TestListIssuesSearchQuerySyntax(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid, err := s.CreateUser(ctx, "demo", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	bug := mustLabel(t, s, "QueryBug", "#d73a4a")
	feature := mustLabel(t, s, "QueryFeature", "#0075ca")

	mk := func(title, body string, author int64, labels ...int64) int64 {
		id, err := s.CreateIssue(ctx, 1, IssueInput{Title: title, Body: body, LabelIDs: labels}, author)
		if err != nil {
			t.Fatalf("CreateIssue(%q): %v", title, err)
		}
		return id
	}
	openID := mk("keyboard layout", "shell crash", uid, bug, feature)
	closedID := mk("keyboard docs", "layout notes", uid, bug, feature)
	anonID := mk("keyboard anonymous", "layout", 0, bug)
	if err := s.SetIssueStatus(ctx, closedID, StatusClosed); err != nil {
		t.Fatalf("SetIssueStatus: %v", err)
	}

	check := func(query string, want ...int64) {
		t.Helper()
		issues, total, err := s.ListIssues(ctx, 1, ListFilter{Query: query, PerPage: 20})
		if err != nil {
			t.Fatalf("ListIssues(%q): %v", query, err)
		}
		if total != len(want) || !equalIDs(issueIDs(issues), want...) {
			t.Fatalf("ListIssues(%q): ids=%v total=%d, want %v", query, issueIDs(issues), total, want)
		}
	}

	check("is:open label:querybug author:demo keyboard", openID)
	check("is:closed label:QueryBug", closedID)
	check("is:open label:QueryBug label:QueryFeature", openID)
	check("keyboard layout", anonID, openID)
	check("keyboard missing")
	check("is:open is:closed")
	check("author:missing")
	check("is:closed\u3000label:QueryBug", closedID)
}
