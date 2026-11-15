// Issue/comment/label endpoints → store domain types.
package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/api"
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// ListIssues maps the filter onto the list endpoint's query parameters.
// The API filters labels by name, so a LabelID filter resolves through
// ListLabels.
func (c *Client) ListIssues(ctx context.Context, f store.ListFilter) ([]*store.Issue, int, error) {
	q := url.Values{}
	status := f.Status
	if status == "" {
		status = store.StatusOpen
	}
	q.Set("status", status)
	if f.Query != "" {
		q.Set("q", f.Query)
	}
	if f.Sort != "" {
		q.Set("sort", f.Sort)
	}
	if f.LabelID > 0 {
		labels, err := c.ListLabels(ctx)
		if err != nil {
			return nil, 0, err
		}
		found := false
		for _, l := range labels {
			if l.ID == f.LabelID {
				q.Set("label", l.Name)
				found = true
				break
			}
		}
		if !found {
			// Local semantics: an unknown label matches nothing.
			// Dropping the filter would return unrelated issues.
			return []*store.Issue{}, 0, nil
		}
	}

	return listWindow(ctx, f.Page, f.PerPage,
		func(apiPage, apiPerPage int) ([]*store.Issue, int, error) {
			q.Set("page", strconv.Itoa(apiPage))
			if apiPerPage > 0 {
				q.Set("per_page", strconv.Itoa(apiPerPage))
			} else {
				q.Del("per_page")
			}
			return c.listIssuesPage(ctx, q)
		})
}

func (c *Client) listIssuesPage(ctx context.Context, q url.Values) ([]*store.Issue, int, error) {
	var res struct {
		Issues []api.Issue `json:"issues"`
		Total  int         `json:"total"`
	}
	if err := c.do(ctx, "GET", "/issues?"+q.Encode(), nil, &res); err != nil {
		return nil, 0, err
	}
	issues := make([]*store.Issue, 0, len(res.Issues))
	for _, i := range res.Issues {
		issues = append(issues, issueFromDTO(i))
	}
	return issues, res.Total, nil
}

func (c *Client) GetIssue(ctx context.Context, id int64) (*store.Issue, error) {
	var res struct {
		Issue api.Issue `json:"issue"`
	}
	if err := c.do(ctx, "GET", "/issues/"+fmt.Sprint(id), nil, &res); err != nil {
		return nil, err
	}
	return issueFromDTO(res.Issue), nil
}

// ListComments reads comments off the detail endpoint — there is no
// dedicated comments GET (cli-and-web-api.md: bundled with the issue).
func (c *Client) ListComments(ctx context.Context, issueID int64) ([]*store.Comment, error) {
	var res struct {
		Comments []api.Comment `json:"comments"`
	}
	if err := c.do(ctx, "GET", "/issues/"+fmt.Sprint(issueID), nil, &res); err != nil {
		return nil, err
	}
	out := make([]*store.Comment, 0, len(res.Comments))
	for _, cm := range res.Comments {
		out = append(out, commentFromDTO(cm, issueID))
	}
	return out, nil
}

// CreateIssue posts the issue; the server resolves the author from the
// token, so authorID is ignored here.
func (c *Client) CreateIssue(ctx context.Context, in store.IssueInput, _ int64) (int64, error) {
	var res struct {
		Issue api.Issue `json:"issue"`
	}
	err := c.do(ctx, "POST", "/issues", map[string]any{
		"title": in.Title, "body": in.Body, "label_ids": in.LabelIDs,
	}, &res)
	if err != nil {
		return 0, err
	}
	return res.Issue.ID, nil
}

func (c *Client) UpdateIssue(ctx context.Context, id int64, in store.IssueInput) error {
	labelIDs := in.LabelIDs
	if labelIDs == nil {
		// JSON null means "leave labels unchanged" to the API, but the
		// local backend replaces — send an empty set so both modes
		// agree that nil LabelIDs clears the labels.
		labelIDs = []int64{}
	}
	return c.do(ctx, "PATCH", "/issues/"+fmt.Sprint(id), map[string]any{
		"title": in.Title, "body": in.Body, "label_ids": labelIDs,
	}, nil)
}

func (c *Client) SetIssueStatus(ctx context.Context, id int64, status string) error {
	return c.do(ctx, "POST", "/issues/"+fmt.Sprint(id)+"/status",
		map[string]any{"status": status}, nil)
}

func (c *Client) DeleteIssue(ctx context.Context, id int64) error {
	return c.do(ctx, "DELETE", "/issues/"+fmt.Sprint(id), nil, nil)
}

func (c *Client) CreateComment(ctx context.Context, issueID int64, body string, _ int64) (int64, error) {
	var res struct {
		Comment api.Comment `json:"comment"`
	}
	err := c.do(ctx, "POST", "/issues/"+fmt.Sprint(issueID)+"/comments",
		map[string]any{"body": body}, &res)
	if err != nil {
		return 0, err
	}
	return res.Comment.ID, nil
}

func (c *Client) ListLabels(ctx context.Context) ([]*store.Label, error) {
	var res struct {
		Labels []api.Label `json:"labels"`
	}
	if err := c.do(ctx, "GET", "/labels", nil, &res); err != nil {
		return nil, err
	}
	out := make([]*store.Label, 0, len(res.Labels))
	for _, l := range res.Labels {
		l := l
		out = append(out, &store.Label{ID: l.ID, Name: l.Name, Color: l.Color})
	}
	return out, nil
}

// --- api DTO → store domain types ---

func issueFromDTO(i api.Issue) *store.Issue {
	out := &store.Issue{
		ID: i.ID, Title: i.Title, Body: i.Body, Status: i.Status,
		Author:    userFromDTO(i.Author),
		CreatedAt: parseTime(i.CreatedAt), UpdatedAt: parseTime(i.UpdatedAt),
	}
	for _, l := range i.Labels {
		out.Labels = append(out.Labels, store.Label{ID: l.ID, Name: l.Name, Color: l.Color})
	}
	return out
}

func commentFromDTO(c api.Comment, issueID int64) *store.Comment {
	return &store.Comment{
		ID: c.ID, IssueID: issueID, Body: c.Body,
		Author:    userFromDTO(c.Author),
		CreatedAt: parseTime(c.CreatedAt),
	}
}

func userFromDTO(u *api.User) *store.User {
	if u == nil {
		return nil
	}
	return &store.User{ID: u.ID, Name: u.Name}
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}
