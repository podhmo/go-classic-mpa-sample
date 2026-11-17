// ScopedStore is a workspace-bound view of Store used by the local CLI
// backend: every issue/discussion operation is confined to one workspace
// so a local `--workspace` behaves like the remote API's /w/{slug}/...
// routes (out-of-scope ids answer ErrNotFound). Workspace management
// methods pass straight through.
package store

import (
	"context"
)

type ScopedStore struct {
	s    *Store
	wsID int64
}

// In resolves the workspace slug eagerly — a miss is ErrNotFound — and
// returns the scoped view. An empty slug returns an unscoped view (wsID 0)
// for workspace-management callers; resource operations then miss every
// scope check and answer ErrNotFound.
func (s *Store) In(ctx context.Context, slug string) (*ScopedStore, error) {
	if slug == "" {
		return &ScopedStore{s: s, wsID: 0}, nil
	}
	ws, err := s.WorkspaceBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	return &ScopedStore{s: s, wsID: ws.ID}, nil
}

// inScope is ErrNotFound when id names an issue of another workspace.
func (sc *ScopedStore) inScope(ctx context.Context, id int64) error {
	i, err := sc.s.GetIssue(ctx, id)
	if err != nil {
		return err
	}
	if i.WorkspaceID != sc.wsID {
		return ErrNotFound
	}
	return nil
}

func (sc *ScopedStore) inScopeDiscussion(ctx context.Context, id int64) error {
	d, err := sc.s.GetDiscussion(ctx, id)
	if err != nil {
		return err
	}
	if d.WorkspaceID != sc.wsID {
		return ErrNotFound
	}
	return nil
}

func (sc *ScopedStore) ListIssues(ctx context.Context, f ListFilter) ([]*Issue, int, error) {
	return sc.s.ListIssues(ctx, sc.wsID, f)
}

func (sc *ScopedStore) GetIssue(ctx context.Context, id int64) (*Issue, error) {
	i, err := sc.s.GetIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	if i.WorkspaceID != sc.wsID {
		return nil, ErrNotFound
	}
	return i, nil
}

func (sc *ScopedStore) CreateIssue(ctx context.Context, in IssueInput, authorID int64) (int64, error) {
	return sc.s.CreateIssue(ctx, sc.wsID, in, authorID)
}

func (sc *ScopedStore) UpdateIssue(ctx context.Context, id int64, in IssueInput) error {
	if err := sc.inScope(ctx, id); err != nil {
		return err
	}
	return sc.s.UpdateIssue(ctx, id, in)
}

func (sc *ScopedStore) SetIssueStatus(ctx context.Context, id int64, status string) error {
	if err := sc.inScope(ctx, id); err != nil {
		return err
	}
	return sc.s.SetIssueStatus(ctx, id, status)
}

func (sc *ScopedStore) DeleteIssue(ctx context.Context, id int64) error {
	if err := sc.inScope(ctx, id); err != nil {
		return err
	}
	return sc.s.DeleteIssue(ctx, id)
}

func (sc *ScopedStore) ListComments(ctx context.Context, issueID int64) ([]*Comment, error) {
	if err := sc.inScope(ctx, issueID); err != nil {
		return nil, err
	}
	return sc.s.ListComments(ctx, issueID)
}

func (sc *ScopedStore) CreateComment(ctx context.Context, issueID int64, body string, authorID int64) (int64, error) {
	if err := sc.inScope(ctx, issueID); err != nil {
		return 0, err
	}
	return sc.s.CreateComment(ctx, issueID, body, authorID)
}

func (sc *ScopedStore) ListLabels(ctx context.Context) ([]*Label, error) {
	return sc.s.ListLabels(ctx)
}

func (sc *ScopedStore) ListDiscussions(ctx context.Context, f DiscussionListFilter) ([]*Discussion, int, error) {
	return sc.s.ListDiscussions(ctx, sc.wsID, f)
}

func (sc *ScopedStore) GetDiscussion(ctx context.Context, id int64) (*Discussion, error) {
	d, err := sc.s.GetDiscussion(ctx, id)
	if err != nil {
		return nil, err
	}
	if d.WorkspaceID != sc.wsID {
		return nil, ErrNotFound
	}
	return d, nil
}

func (sc *ScopedStore) CreateDiscussion(ctx context.Context, in DiscussionInput, authorID int64) (int64, error) {
	return sc.s.CreateDiscussion(ctx, sc.wsID, in, authorID)
}

func (sc *ScopedStore) UpdateDiscussion(ctx context.Context, id int64, in DiscussionInput) error {
	if err := sc.inScopeDiscussion(ctx, id); err != nil {
		return err
	}
	return sc.s.UpdateDiscussion(ctx, id, in)
}

func (sc *ScopedStore) DeleteDiscussion(ctx context.Context, id int64) error {
	if err := sc.inScopeDiscussion(ctx, id); err != nil {
		return err
	}
	return sc.s.DeleteDiscussion(ctx, id)
}

func (sc *ScopedStore) ListDiscussionThreads(ctx context.Context, discussionID int64) ([]*DiscussionThread, error) {
	if err := sc.inScopeDiscussion(ctx, discussionID); err != nil {
		return nil, err
	}
	return sc.s.ListDiscussionThreads(ctx, discussionID)
}

func (sc *ScopedStore) CreateDiscussionThread(ctx context.Context, discussionID int64, body string, authorID int64) (int64, error) {
	if err := sc.inScopeDiscussion(ctx, discussionID); err != nil {
		return 0, err
	}
	return sc.s.CreateDiscussionThread(ctx, discussionID, body, authorID)
}

func (sc *ScopedStore) CreateDiscussionComment(ctx context.Context, discussionID, threadID int64, body string, authorID int64) (int64, error) {
	if err := sc.inScopeDiscussion(ctx, discussionID); err != nil {
		return 0, err
	}
	return sc.s.CreateDiscussionComment(ctx, discussionID, threadID, body, authorID)
}

func (sc *ScopedStore) SetThreadAnswer(ctx context.Context, discussionID, threadID int64, answer bool) error {
	if err := sc.inScopeDiscussion(ctx, discussionID); err != nil {
		return err
	}
	return sc.s.SetThreadAnswer(ctx, discussionID, threadID, answer)
}

func (sc *ScopedStore) ListDiscussionCategories(ctx context.Context) ([]*DiscussionCategory, error) {
	return sc.s.ListDiscussionCategories(ctx)
}

// Workspace management methods pass through unscoped.

func (sc *ScopedStore) ListWorkspaces(ctx context.Context) ([]*Workspace, error) {
	return sc.s.ListWorkspaces(ctx)
}

func (sc *ScopedStore) WorkspaceBySlug(ctx context.Context, slug string) (*Workspace, error) {
	return sc.s.WorkspaceBySlug(ctx, slug)
}

func (sc *ScopedStore) CreateWorkspace(ctx context.Context, in WorkspaceInput) (int64, error) {
	return sc.s.CreateWorkspace(ctx, in)
}

func (sc *ScopedStore) DeleteWorkspace(ctx context.Context, slug string) error {
	return sc.s.DeleteWorkspace(ctx, slug)
}
