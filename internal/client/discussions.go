// Discussion endpoints → store domain types.
package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/podhmo/go-classic-mpa-sample/internal/api"
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// ListDiscussions maps the filter onto the list endpoint's query
// parameters (category is a slug, like the ?category= URL filter).
func (c *Client) ListDiscussions(ctx context.Context, f store.DiscussionListFilter) ([]*store.Discussion, int, error) {
	q := url.Values{}
	if f.CategorySlug != "" {
		q.Set("category", f.CategorySlug)
	}
	if f.Answered != "" {
		q.Set("answered", f.Answered)
	}
	if f.Query != "" {
		q.Set("q", f.Query)
	}
	if f.Sort != "" {
		q.Set("sort", f.Sort)
	}
	return listWindow(ctx, f.Page, f.PerPage,
		func(apiPage, apiPerPage int) ([]*store.Discussion, int, error) {
			q.Set("page", strconv.Itoa(apiPage))
			if apiPerPage > 0 {
				q.Set("per_page", strconv.Itoa(apiPerPage))
			} else {
				q.Del("per_page")
			}
			return c.listDiscussionsPage(ctx, q)
		})
}

func (c *Client) listDiscussionsPage(ctx context.Context, q url.Values) ([]*store.Discussion, int, error) {
	var res struct {
		Discussions []api.Discussion `json:"discussions"`
		Total       int              `json:"total"`
	}
	if err := c.do(ctx, "GET", "/discussions?"+q.Encode(), nil, &res); err != nil {
		return nil, 0, err
	}
	out := make([]*store.Discussion, 0, len(res.Discussions))
	for _, d := range res.Discussions {
		out = append(out, discussionFromDTO(d))
	}
	return out, res.Total, nil
}

func (c *Client) GetDiscussion(ctx context.Context, id int64) (*store.Discussion, error) {
	var res struct {
		Discussion api.Discussion `json:"discussion"`
	}
	if err := c.do(ctx, "GET", "/discussions/"+fmt.Sprint(id), nil, &res); err != nil {
		return nil, err
	}
	return discussionFromDTO(res.Discussion), nil
}

// ListDiscussionComments reads comments off the detail endpoint — like
// ListComments there is no dedicated comments GET.
func (c *Client) ListDiscussionComments(ctx context.Context, discussionID int64) ([]*store.DiscussionComment, error) {
	var res struct {
		Comments []api.DiscussionComment `json:"comments"`
	}
	if err := c.do(ctx, "GET", "/discussions/"+fmt.Sprint(discussionID), nil, &res); err != nil {
		return nil, err
	}
	out := make([]*store.DiscussionComment, 0, len(res.Comments))
	for _, cm := range res.Comments {
		out = append(out, discussionCommentFromDTO(cm, discussionID))
	}
	return out, nil
}

// CreateDiscussion posts the discussion; the server resolves the author
// from the token, so authorID is ignored here.
func (c *Client) CreateDiscussion(ctx context.Context, in store.DiscussionInput, _ int64) (int64, error) {
	var res struct {
		Discussion api.Discussion `json:"discussion"`
	}
	err := c.do(ctx, "POST", "/discussions", map[string]any{
		"title": in.Title, "body": in.Body, "category_id": in.CategoryID,
	}, &res)
	if err != nil {
		return 0, err
	}
	return res.Discussion.ID, nil
}

func (c *Client) UpdateDiscussion(ctx context.Context, id int64, in store.DiscussionInput) error {
	return c.do(ctx, "PATCH", "/discussions/"+fmt.Sprint(id), map[string]any{
		"title": in.Title, "body": in.Body, "category_id": in.CategoryID,
	}, nil)
}

func (c *Client) DeleteDiscussion(ctx context.Context, id int64) error {
	return c.do(ctx, "DELETE", "/discussions/"+fmt.Sprint(id), nil, nil)
}

func (c *Client) CreateDiscussionComment(ctx context.Context, discussionID, parentID int64, body string, _ int64) (int64, error) {
	var res struct {
		Comment api.DiscussionComment `json:"comment"`
	}
	err := c.do(ctx, "POST", "/discussions/"+fmt.Sprint(discussionID)+"/comments",
		map[string]any{"body": body, "parent_id": parentID}, &res)
	if err != nil {
		return 0, err
	}
	return res.Comment.ID, nil
}

func (c *Client) SetCommentAnswer(ctx context.Context, discussionID, commentID int64, answer bool) error {
	return c.do(ctx, "POST",
		"/discussions/"+fmt.Sprint(discussionID)+"/comments/"+fmt.Sprint(commentID)+"/answer",
		map[string]any{"answer": answer}, nil)
}

func (c *Client) ListDiscussionCategories(ctx context.Context) ([]*store.DiscussionCategory, error) {
	var res struct {
		Categories []api.DiscussionCategory `json:"categories"`
	}
	if err := c.do(ctx, "GET", "/discussion-categories", nil, &res); err != nil {
		return nil, err
	}
	out := make([]*store.DiscussionCategory, 0, len(res.Categories))
	for _, c := range res.Categories {
		c := c
		out = append(out, &store.DiscussionCategory{
			ID: c.ID, Name: c.Name, Slug: c.Slug,
			Description: c.Description, Format: c.Format,
		})
	}
	return out, nil
}

// --- api DTO → store domain types ---

func discussionFromDTO(d api.Discussion) *store.Discussion {
	return &store.Discussion{
		ID: d.ID, Title: d.Title, Body: d.Body,
		Category:  categoryFromDTO(d.Category),
		Author:    userFromDTO(d.Author),
		Answered:  d.Answered,
		CreatedAt: parseTime(d.CreatedAt), UpdatedAt: parseTime(d.UpdatedAt),
	}
}

func discussionCommentFromDTO(c api.DiscussionComment, discussionID int64) *store.DiscussionComment {
	out := &store.DiscussionComment{
		ID: c.ID, DiscussionID: discussionID, ParentID: c.ParentID,
		IsAnswer: c.IsAnswer, Body: c.Body,
		Author:    userFromDTO(c.Author),
		CreatedAt: parseTime(c.CreatedAt),
	}
	for _, rep := range c.Replies {
		out.Replies = append(out.Replies, discussionCommentFromDTO(rep, discussionID))
	}
	return out
}

func categoryFromDTO(c api.DiscussionCategory) store.DiscussionCategory {
	return store.DiscussionCategory{
		ID: c.ID, Name: c.Name, Slug: c.Slug,
		Description: c.Description, Format: c.Format,
	}
}
