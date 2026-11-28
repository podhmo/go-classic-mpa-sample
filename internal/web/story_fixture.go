//go:build story

package web

import (
	"net/http"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
	"github.com/podhmo/minigo/x/storybook/fixture"
)

// StoryGeneratedIssueList uses fixture.LongText as the starting point for
// a long-text row, then tunes the fields that make the state domain-valid.
//
//storybook:group renderIssueList
func StoryGeneratedIssueList() {
	data := fixture.LongText[issueListPage]()
	issue := data.Issues[0]
	issue.ID = 4
	issue.WorkspaceID = storyWorkspace.ID
	issue.Status = store.StatusOpen
	issue.CreatedAt = storyTime
	issue.UpdatedAt = storyTime2
	issue.Labels = []store.Label{*storyLabels[3]}

	data.Title = "Issues"
	data.Labels = storyLabels
	data.Filter = store.ListFilter{Status: store.StatusOpen, Page: 1, PerPage: perPage}
	data.Total = len(data.Issues)
	data.Page = 1
	data.TotalPages = 1
	data.RangeStart = 1
	data.RangeEnd = len(data.Issues)
	data.HasActiveFilter = false
	data.Workspace = storyWorkspace
	data.Section = "issues"
	data.User = nil
	renderIssueList(http.StatusOK, data)
}
