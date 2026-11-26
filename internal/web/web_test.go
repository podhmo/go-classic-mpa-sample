package web

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/auth"
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// fakeStore is an in-memory implementation of the Store contract used by the
// web layer. It honors the documented semantics: ErrNotFound, status/query/
// label filtering, newest/oldest sorting and pre-pagination totals.
//
// Declared simplifications: sessions carry no expiry (SessionUser ignores
// ttl, so expiry and sliding-renewal scenarios are untestable through this
// fake); APITokenUser always returns ErrNotFound; and foreign keys are not
// enforced — an unknown authorID/userID posts anonymously instead of
// failing. Comment values are shared pointers, so caller-side mutation is
// visible to the store (the answer-swap handler relies on this for
// storybook stories; the real store returns fresh rows each call).
type fakeStore struct {
	mu          sync.Mutex
	nextIssue   int64
	nextComment int64
	nextUser    int64
	issues      map[int64]*store.Issue
	comments    map[int64][]*store.Comment
	labels      []*store.Label
	discussions map[int64]*store.Discussion
	dthreads    map[int64][]*store.DiscussionThread
	dcategories []*store.DiscussionCategory
	nextD       int64
	nextDC      int64
	users       map[int64]*fakeUser
	sessions    map[string]int64 // hashed token → user ID
	workspaces  map[string]*store.Workspace
	nextWS      int64
	pulls       []*store.PullRequest
	nextPull    int64
	// sessionUserErr, when set, makes SessionUser fail — simulates a
	// transient session-store error.
	sessionUserErr error
}

func newFakeStore() *fakeStore {
	f := &fakeStore{
		nextIssue:   1,
		nextComment: 1,
		issues:      map[int64]*store.Issue{},
		comments:    map[int64][]*store.Comment{},
		discussions: map[int64]*store.Discussion{},
		dthreads:    map[int64][]*store.DiscussionThread{},
		users:       map[int64]*fakeUser{},
		sessions:    map[string]int64{},
		workspaces:  map[string]*store.Workspace{"default": {ID: 1, Slug: "default", Name: "default"}},
		nextWS:      2,
		nextD:       1,
		nextDC:      1,
		nextPull:    1,
	}
	f.labels = []*store.Label{
		{ID: 1, Name: "Bug", Color: "#d73a4a", Kind: store.KindLabelIssue},
		{ID: 2, Name: "Documentation", Color: "#0075ca", Kind: store.KindLabelIssue},
		{ID: 3, Name: "Feature", Color: "#a2eeef", Kind: store.KindLabelIssue},
	}
	f.dcategories = []*store.DiscussionCategory{
		{ID: 1, Name: "General", Slug: "general", Format: store.CategoryFormatOpen},
		{ID: 2, Name: "Q&A", Slug: "qa", Format: store.CategoryFormatQA},
	}
	return f
}

// addIssue is a test helper that inserts an issue directly.
func (f *fakeStore) addIssue(title, body, status string, created time.Time, labelIDs ...int64) *store.Issue {
	f.mu.Lock()
	defer f.mu.Unlock()
	is := &store.Issue{
		ID:          f.nextIssue,
		WorkspaceID: 1,
		Title:       title,
		Body:        body,
		Status:      status,
		CreatedAt:   created,
		UpdatedAt:   created,
	}
	f.nextIssue++
	is.Labels = f.resolveLabels(labelIDs)
	f.issues[is.ID] = is
	return is
}

func (f *fakeStore) labelByID(id int64) *store.Label {
	for _, l := range f.labels {
		if l.ID == id {
			return l
		}
	}
	return nil
}

// resolveLabels maps label ids onto the stored labels — the same way the
// real store's setIssueLabels + attachLabels behave: unknown ids are
// silently dropped, duplicates collapse, and the result is name-ordered.
func (f *fakeStore) resolveLabels(ids []int64) []store.Label {
	var out []store.Label
	for _, id := range ids {
		l := f.labelByID(id)
		if l == nil || slices.ContainsFunc(out, func(x store.Label) bool { return x.ID == l.ID }) {
			continue
		}
		out = append(out, *l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func cloneIssue(is *store.Issue) *store.Issue {
	c := *is
	c.Labels = append([]store.Label(nil), is.Labels...)
	return &c
}

func (f *fakeStore) ListIssues(_ context.Context, wsID int64, fl store.ListFilter) ([]*store.Issue, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	status := fl.Status
	if status == "" {
		status = store.StatusOpen
	}
	var matched []*store.Issue
	for _, is := range f.issues {
		if status != "all" && is.Status != status {
			continue
		}
		if fl.Query != "" {
			q := strings.ToLower(fl.Query)
			if !strings.Contains(strings.ToLower(is.Title), q) &&
				!strings.Contains(strings.ToLower(is.Body), q) {
				continue
			}
		}
		if fl.LabelID > 0 {
			found := false
			for _, l := range is.Labels {
				if l.ID == fl.LabelID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		matched = append(matched, is)
	}
	sort.Slice(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		if fl.Sort == "oldest" {
			if a.CreatedAt.Equal(b.CreatedAt) {
				return a.ID < b.ID
			}
			return a.CreatedAt.Before(b.CreatedAt)
		}
		if a.CreatedAt.Equal(b.CreatedAt) {
			return a.ID > b.ID
		}
		return a.CreatedAt.After(b.CreatedAt)
	})

	total := len(matched)
	page := max(fl.Page, 1)
	per := fl.PerPage
	if per <= 0 {
		per = 20
	}
	start := (page - 1) * per
	if start >= total {
		return nil, total, nil
	}
	end := min(start+per, total)
	out := make([]*store.Issue, 0, end-start)
	for _, is := range matched[start:end] {
		out = append(out, cloneIssue(is))
	}
	return out, total, nil
}

func (f *fakeStore) GetIssue(_ context.Context, id int64) (*store.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	is, ok := f.issues[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneIssue(is), nil
}

func (f *fakeStore) CreateIssue(_ context.Context, wsID int64, in store.IssueInput, authorID int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	is := &store.Issue{
		ID:          f.nextIssue,
		WorkspaceID: wsID,
		Title:       in.Title,
		Body:        in.Body,
		Status:      store.StatusOpen,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	f.nextIssue++
	if fu := f.users[authorID]; fu != nil {
		u := fu.user
		is.Author = &u
	}
	is.Labels = f.resolveLabels(in.LabelIDs)
	f.issues[is.ID] = is
	return is.ID, nil
}

func (f *fakeStore) UpdateIssue(_ context.Context, id int64, in store.IssueInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	is, ok := f.issues[id]
	if !ok {
		return store.ErrNotFound
	}
	is.Title = in.Title
	is.Body = in.Body
	is.UpdatedAt = time.Now()
	is.Labels = f.resolveLabels(in.LabelIDs)
	return nil
}

func (f *fakeStore) SetIssueStatus(_ context.Context, id int64, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Same contract as the real store: the value is validated before the
	// existence check.
	if status != store.StatusOpen && status != store.StatusClosed {
		return fmt.Errorf("store: invalid status %q", status)
	}
	is, ok := f.issues[id]
	if !ok {
		return store.ErrNotFound
	}
	// Same contract as the real store: updated_at bumps only on change,
	// so a no-op status post leaves the timestamp alone.
	if is.Status != status {
		is.Status = status
		is.UpdatedAt = time.Now()
	}
	return nil
}

func (f *fakeStore) DeleteIssue(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.issues[id]; !ok {
		return store.ErrNotFound
	}
	delete(f.issues, id)
	delete(f.comments, id)
	return nil
}

func (f *fakeStore) ListComments(_ context.Context, issueID int64) ([]*store.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	src := f.comments[issueID]
	out := make([]*store.Comment, len(src))
	copy(out, src)
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (f *fakeStore) CreateComment(_ context.Context, issueID int64, body string, authorID int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.issues[issueID]; !ok {
		return 0, store.ErrNotFound
	}
	c := &store.Comment{
		ID:        f.nextComment,
		IssueID:   issueID,
		Body:      body,
		CreatedAt: time.Now(),
	}
	f.nextComment++
	if fu := f.users[authorID]; fu != nil {
		u := fu.user
		c.Author = &u
	}
	f.comments[issueID] = append(f.comments[issueID], c)
	// Same contract as the real store: a new comment bumps updated_at.
	f.issues[issueID].UpdatedAt = time.Now()
	return c.ID, nil
}

// The moderation trio mirrors the real store's contract: ErrNotFound
// when the (issue, comment) chain does not resolve; an edit stamps
// EditedAt only on a real change; hide/unhide never bumps updated_at.
func (f *fakeStore) UpdateComment(_ context.Context, issueID, commentID int64, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.issues[issueID]; !ok {
		return store.ErrNotFound
	}
	c := findComment(f.comments[issueID], commentID)
	if c == nil {
		return store.ErrNotFound
	}
	if c.Body != body {
		c.Body = body
		c.EditedAt = time.Now()
	}
	return nil
}

func (f *fakeStore) DeleteComment(_ context.Context, issueID, commentID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.issues[issueID]; !ok {
		return store.ErrNotFound
	}
	src := f.comments[issueID]
	for i, c := range src {
		if c.ID == commentID {
			f.comments[issueID] = append(src[:i:i], src[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}

func (f *fakeStore) SetCommentHidden(_ context.Context, issueID, commentID int64, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.issues[issueID]; !ok {
		return store.ErrNotFound
	}
	c := findComment(f.comments[issueID], commentID)
	if c == nil {
		return store.ErrNotFound
	}
	if reason != "" && !store.ValidHideReason(reason) {
		return store.ErrInvalidHideReason
	}
	c.Hidden = reason != ""
	c.HiddenReason = reason
	return nil
}

func (f *fakeStore) ListLabels(_ context.Context, wsID int64, kind string) ([]*store.Label, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*store.Label
	for _, l := range f.labels {
		if l.Kind != kind || l.Archived {
			continue
		}
		if l.WorkspaceID != 0 && l.WorkspaceID != wsID {
			continue
		}
		c := *l
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeStore) ListArchivedLabels(_ context.Context, wsID int64, kind string) ([]*store.Label, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*store.Label
	for _, l := range f.labels {
		if l.Kind != kind || !l.Archived {
			continue
		}
		if l.WorkspaceID != 0 && l.WorkspaceID != wsID {
			continue
		}
		c := *l
		out = append(out, &c)
	}
	return out, nil
}

func (f *fakeStore) GetLabel(_ context.Context, id int64) (*store.Label, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.labels {
		if l.ID == id {
			c := *l
			return &c, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) CreateLabel(_ context.Context, wsID int64, kind string, in store.LabelInput) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.labels {
		if l.Name == in.Name && l.Kind == kind && l.WorkspaceID == wsID {
			if l.Archived {
				l.Archived = false
				l.Color = in.Color
				return l.ID, nil
			}
			return 0, store.ErrDuplicateLabel
		}
	}
	id := int64(len(f.labels) + 1)
	f.labels = append(f.labels, &store.Label{ID: id, WorkspaceID: wsID, Kind: kind, Name: in.Name, Color: in.Color})
	return id, nil
}

func (f *fakeStore) UpdateLabel(_ context.Context, id int64, in store.LabelInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.labels {
		if l.ID == id {
			l.Name, l.Color = in.Name, in.Color
			return nil
		}
	}
	return store.ErrNotFound
}

func (f *fakeStore) ArchiveLabel(_ context.Context, id int64, archived bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.labels {
		if l.ID == id {
			l.Archived = archived
			return nil
		}
	}
	return store.ErrNotFound
}

// addDiscussion is a test helper that inserts a discussion directly.
func (f *fakeStore) addDiscussion(title, body string, categoryID int64, created time.Time) *store.Discussion {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := &store.Discussion{
		ID:          f.nextD,
		WorkspaceID: 1,
		Title:       title,
		Body:        body,
		CreatedAt:   created,
		UpdatedAt:   created,
	}
	if c := f.dcategoryByID(categoryID); c != nil {
		d.Category = *c
	}
	f.nextD++
	f.discussions[d.ID] = d
	return d
}

func (f *fakeStore) dcategoryByID(id int64) *store.DiscussionCategory {
	for _, c := range f.dcategories {
		if c.ID == id {
			return c
		}
	}
	return nil
}

func cloneDiscussion(d *store.Discussion) *store.Discussion {
	c := *d
	return &c
}

// answered reports whether any stored thread carries IsAnswer — the same
// derivation the SQL EXISTS clause performs.
func (f *fakeStore) answered(discussionID int64) bool {
	for _, t := range f.dthreads[discussionID] {
		if t.IsAnswer {
			return true
		}
	}
	return false
}

func (f *fakeStore) ListDiscussions(_ context.Context, wsID int64, fl store.DiscussionListFilter) ([]*store.Discussion, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var matched []*store.Discussion
	for _, d := range f.discussions {
		if fl.CategorySlug != "" && d.Category.Slug != fl.CategorySlug {
			continue
		}
		if fl.Query != "" {
			q := strings.ToLower(fl.Query)
			if !strings.Contains(strings.ToLower(d.Title), q) &&
				!strings.Contains(strings.ToLower(d.Body), q) {
				continue
			}
		}
		switch fl.Answered {
		case "yes":
			if !f.answered(d.ID) {
				continue
			}
		case "no":
			if f.answered(d.ID) {
				continue
			}
		}
		matched = append(matched, d)
	}
	sort.Slice(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		if fl.Sort == "oldest" {
			if a.CreatedAt.Equal(b.CreatedAt) {
				return a.ID < b.ID
			}
			return a.CreatedAt.Before(b.CreatedAt)
		}
		if a.CreatedAt.Equal(b.CreatedAt) {
			return a.ID > b.ID
		}
		return a.CreatedAt.After(b.CreatedAt)
	})

	total := len(matched)
	page := max(fl.Page, 1)
	per := fl.PerPage
	if per <= 0 {
		per = 20
	}
	start := (page - 1) * per
	if start >= total {
		return nil, total, nil
	}
	end := min(start+per, total)
	out := make([]*store.Discussion, 0, end-start)
	for _, d := range matched[start:end] {
		c := cloneDiscussion(d)
		c.Answered = f.answered(d.ID)
		out = append(out, c)
	}
	return out, total, nil
}

func (f *fakeStore) GetDiscussion(_ context.Context, id int64) (*store.Discussion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.discussions[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	c := cloneDiscussion(d)
	c.Answered = f.answered(id)
	return c, nil
}

func (f *fakeStore) CreateDiscussion(_ context.Context, wsID int64, in store.DiscussionInput, authorID int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cat := f.dcategoryByID(in.CategoryID)
	if cat == nil {
		return 0, store.ErrInvalidCategory
	}
	now := time.Now()
	d := &store.Discussion{
		ID:          f.nextD,
		WorkspaceID: wsID,
		Title:       in.Title,
		Body:        in.Body,
		Category:    *cat,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	f.nextD++
	if fu := f.users[authorID]; fu != nil {
		u := fu.user
		d.Author = &u
	}
	f.discussions[d.ID] = d
	return d.ID, nil
}

func (f *fakeStore) UpdateDiscussion(_ context.Context, id int64, in store.DiscussionInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Same contract as the real store: the category check precedes the
	// existence check.
	cat := f.dcategoryByID(in.CategoryID)
	if cat == nil {
		return store.ErrInvalidCategory
	}
	d, ok := f.discussions[id]
	if !ok {
		return store.ErrNotFound
	}
	d.Title = in.Title
	d.Body = in.Body
	d.Category = *cat
	d.UpdatedAt = time.Now()
	if cat.Format != store.CategoryFormatQA {
		for _, t := range f.dthreads[id] {
			t.IsAnswer = false
		}
	}
	return nil
}

func (f *fakeStore) DeleteDiscussion(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.discussions[id]; !ok {
		return store.ErrNotFound
	}
	delete(f.discussions, id)
	delete(f.dthreads, id)
	return nil
}

func (f *fakeStore) ListDiscussionThreads(_ context.Context, discussionID int64) ([]*store.DiscussionThread, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*store.DiscussionThread, len(f.dthreads[discussionID]))
	copy(out, f.dthreads[discussionID])
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (f *fakeStore) CreateDiscussionThread(_ context.Context, discussionID int64, body string, authorID int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.discussions[discussionID]; !ok {
		return 0, store.ErrNotFound
	}
	t := &store.DiscussionThread{
		ID:           f.nextDC,
		DiscussionID: discussionID,
		Body:         body,
		CreatedAt:    time.Now(),
	}
	f.nextDC++
	if fu := f.users[authorID]; fu != nil {
		u := fu.user
		t.Author = &u
	}
	f.dthreads[discussionID] = append(f.dthreads[discussionID], t)
	// Same contract as the real store: a new thread bumps updated_at.
	f.discussions[discussionID].UpdatedAt = time.Now()
	return t.ID, nil
}

func (f *fakeStore) CreateDiscussionComment(_ context.Context, discussionID, threadID int64, body string, authorID int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.discussions[discussionID]; !ok {
		return 0, store.ErrNotFound
	}
	var thread *store.DiscussionThread
	for _, t := range f.dthreads[discussionID] {
		if t.ID == threadID {
			thread = t
		}
	}
	if thread == nil {
		return 0, store.ErrInvalidThread
	}
	c := &store.DiscussionComment{
		ID:        f.nextDC,
		ThreadID:  threadID,
		Body:      body,
		CreatedAt: time.Now(),
	}
	f.nextDC++
	if fu := f.users[authorID]; fu != nil {
		u := fu.user
		c.Author = &u
	}
	thread.Comments = append(thread.Comments, c)
	// Same contract as the real store: a new comment bumps updated_at.
	f.discussions[discussionID].UpdatedAt = time.Now()
	return c.ID, nil
}

func (f *fakeStore) SetThreadAnswer(_ context.Context, discussionID, threadID int64, answer bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.discussions[discussionID]
	if !ok {
		return store.ErrNotFound
	}
	var target *store.DiscussionThread
	for _, t := range f.dthreads[discussionID] {
		if t.ID == threadID {
			target = t
		}
	}
	if target == nil {
		return store.ErrNotFound
	}
	if answer && d.Category.Format != store.CategoryFormatQA {
		return store.ErrNotAnswerable
	}
	if answer {
		for _, t := range f.dthreads[discussionID] {
			t.IsAnswer = false
		}
		target.IsAnswer = true
	} else {
		target.IsAnswer = false
	}
	d.UpdatedAt = time.Now()
	return nil
}

// Discussion-side moderation, same contract as the issue-comment trio:
// ErrNotFound when the (discussion, thread[, reply]) chain does not
// resolve. A deleted thread drops its replies along — the real schema
// cascades them.
func (f *fakeStore) UpdateDiscussionThread(_ context.Context, discussionID, threadID int64, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.discussions[discussionID]; !ok {
		return store.ErrNotFound
	}
	t := findThread(f.dthreads[discussionID], threadID)
	if t == nil {
		return store.ErrNotFound
	}
	if t.Body != body {
		t.Body = body
		t.EditedAt = time.Now()
	}
	return nil
}

func (f *fakeStore) DeleteDiscussionThread(_ context.Context, discussionID, threadID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.discussions[discussionID]; !ok {
		return store.ErrNotFound
	}
	src := f.dthreads[discussionID]
	for i, t := range src {
		if t.ID == threadID {
			f.dthreads[discussionID] = append(src[:i:i], src[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}

func (f *fakeStore) SetDiscussionThreadHidden(_ context.Context, discussionID, threadID int64, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.discussions[discussionID]; !ok {
		return store.ErrNotFound
	}
	t := findThread(f.dthreads[discussionID], threadID)
	if t == nil {
		return store.ErrNotFound
	}
	if reason != "" && !store.ValidHideReason(reason) {
		return store.ErrInvalidHideReason
	}
	t.Hidden = reason != ""
	t.HiddenReason = reason
	return nil
}

func (f *fakeStore) UpdateDiscussionComment(_ context.Context, discussionID, threadID, commentID int64, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, err := f.lockedThread(discussionID, threadID)
	if err != nil {
		return err
	}
	c := findReply(t, commentID)
	if c == nil {
		return store.ErrNotFound
	}
	if c.Body != body {
		c.Body = body
		c.EditedAt = time.Now()
	}
	return nil
}

func (f *fakeStore) DeleteDiscussionComment(_ context.Context, discussionID, threadID, commentID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, err := f.lockedThread(discussionID, threadID)
	if err != nil {
		return err
	}
	for i, c := range t.Comments {
		if c.ID == commentID {
			t.Comments = append(t.Comments[:i:i], t.Comments[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}

func (f *fakeStore) SetDiscussionCommentHidden(_ context.Context, discussionID, threadID, commentID int64, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, err := f.lockedThread(discussionID, threadID)
	if err != nil {
		return err
	}
	c := findReply(t, commentID)
	if c == nil {
		return store.ErrNotFound
	}
	if reason != "" && !store.ValidHideReason(reason) {
		return store.ErrInvalidHideReason
	}
	c.Hidden = reason != ""
	c.HiddenReason = reason
	return nil
}

// lockedThread resolves the (discussion, thread) pair for the reply
// methods — callers hold f.mu.
func (f *fakeStore) lockedThread(discussionID, threadID int64) (*store.DiscussionThread, error) {
	if _, ok := f.discussions[discussionID]; !ok {
		return nil, store.ErrNotFound
	}
	t := findThread(f.dthreads[discussionID], threadID)
	if t == nil {
		return nil, store.ErrNotFound
	}
	return t, nil
}

func (f *fakeStore) ListDiscussionCategories(_ context.Context) ([]*store.DiscussionCategory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*store.DiscussionCategory, len(f.dcategories))
	for i, c := range f.dcategories {
		cc := *c
		out[i] = &cc
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// fakeUser keeps the fake's users and the password they were created with.
// CreateUser receives a hash, not a password, so tests seed users through
// addUser below and the fake honors the real semantics of
// AuthenticateUser via internal/auth.
type fakeUser struct {
	user         store.User
	passwordHash string
}

func (f *fakeStore) userByName(name string) *fakeUser {
	for _, fu := range f.users {
		if strings.EqualFold(fu.user.Name, name) {
			return fu
		}
	}
	return nil
}

// addUser is a test helper that registers a user with a real password.
func (f *fakeStore) addUser(t *testing.T, name, password string) *store.User {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("auth.HashPassword: %v", err)
	}
	id, err := f.CreateUser(context.Background(), name, hash)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	fu := f.users[id]
	return &fu.user
}

func (f *fakeStore) ListWorkspaces(_ context.Context) ([]*store.Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []*store.Workspace{}
	for _, ws := range f.workspaces {
		out = append(out, ws)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

func (f *fakeStore) WorkspaceBySlug(_ context.Context, slug string) (*store.Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ws, ok := f.workspaces[slug]
	if !ok {
		return nil, store.ErrNotFound
	}
	return ws, nil
}

func (f *fakeStore) WorkspaceByID(_ context.Context, id int64) (*store.Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ws := range f.workspaces {
		if ws.ID == id {
			return ws, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) CreateWorkspace(_ context.Context, in store.WorkspaceInput) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.workspaces[in.Slug]; ok {
		return 0, store.ErrDuplicateSlug
	}
	id := f.nextWS
	f.nextWS++
	name := in.Name
	if name == "" {
		name = in.Slug
	}
	f.workspaces[in.Slug] = &store.Workspace{ID: id, Slug: in.Slug, Name: name, Description: in.Description}
	return id, nil
}

func (f *fakeStore) DeleteWorkspace(_ context.Context, slug string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.workspaces[slug]; !ok {
		return store.ErrNotFound
	}
	if len(f.workspaces) == 1 {
		return store.ErrLastWorkspace
	}
	delete(f.workspaces, slug)
	return nil
}

func (f *fakeStore) CreateUser(_ context.Context, name, passwordHash string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.userByName(name) != nil {
		return 0, store.ErrDuplicateName
	}
	if f.nextUser == 0 {
		f.nextUser = 1
	}
	u := &fakeUser{user: store.User{ID: f.nextUser, Name: name, CreatedAt: time.Now()}, passwordHash: passwordHash}
	f.nextUser++
	f.users[u.user.ID] = u
	return u.user.ID, nil
}

func (f *fakeStore) AuthenticateUser(_ context.Context, name, password string) (*store.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fu := f.userByName(name)
	if fu == nil || !auth.VerifyPassword(password, fu.passwordHash) {
		return nil, store.ErrInvalidCredentials
	}
	u := fu.user
	return &u, nil
}

func (f *fakeStore) CreateSession(_ context.Context, userID int64, ttl time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	token, err := auth.NewToken()
	if err != nil {
		return "", err
	}
	f.sessions[auth.HashToken(token)] = userID
	return token, nil
}

func (f *fakeStore) SessionUser(_ context.Context, token string, _ time.Duration) (*store.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessionUserErr != nil {
		return nil, f.sessionUserErr
	}
	uid, ok := f.sessions[auth.HashToken(token)]
	if !ok {
		return nil, store.ErrNotFound
	}
	fu, ok := f.users[uid]
	if !ok {
		return nil, store.ErrNotFound
	}
	u := fu.user
	return &u, nil
}

func (f *fakeStore) DeleteSession(_ context.Context, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, auth.HashToken(token))
	return nil
}

func (f *fakeStore) APITokenUser(_ context.Context, token string) (*store.User, error) {
	return nil, store.ErrNotFound
}

var _ Store = (*fakeStore)(nil)

func newTestServer(t *testing.T) (*httptest.Server, *fakeStore) {
	t.Helper()
	f := newFakeStore()
	srv := httptest.NewServer(New(f))
	t.Cleanup(srv.Close)
	return srv, f
}

// noRedirectClient follows no redirects so we can assert on 303 + Location.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func postForm(t *testing.T, client *http.Client, rawurl string, form url.Values) *http.Response {
	t.Helper()
	resp, err := client.PostForm(rawurl, form)
	if err != nil {
		t.Fatalf("POST %s: %v", rawurl, err)
	}
	return resp
}

func getBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func seedIssues(t *testing.T, f *fakeStore) {
	t.Helper()
	now := time.Now()
	f.addIssue("Fix flaky test", "the CI job fails sometimes", store.StatusOpen, now.Add(-72*time.Hour), 1)
	f.addIssue("Write docs", "document the API surface", store.StatusClosed, now.Add(-48*time.Hour), 2)
	f.addIssue("Add dark mode", "nice to have", store.StatusOpen, now.Add(-24*time.Hour), 3)
}

func TestCSRFRejectsCrossSitePost(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("CSRF target", "", store.StatusOpen, time.Now())

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/w/default/issues",
		strings.NewReader("title=hi"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}

	// Same-site requests keep working.
	req2, err := http.NewRequest(http.MethodPost, srv.URL+"/w/default/issues",
		strings.NewReader("title=ok"))
	if err != nil {
		t.Fatal(err)
	}
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.Header.Set("Sec-Fetch-Site", "same-origin")
	resp2, err := noRedirectClient().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("same-site POST status = %d, want 303", resp2.StatusCode)
	}
}

func TestUnsupportedMethodRenders405(t *testing.T) {
	srv, _ := newTestServer(t)
	// The catch-alls are method-scoped (GET/POST only), so a disallowed
	// method on a known path falls through to the mux's automatic 405.
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/w/default/issues", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

func TestUnknownPath404(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/definitely-not-here")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestStaticCSS(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/css") {
		t.Fatalf("Content-Type = %q, want text/css", ct)
	}
	body := getBody(t, resp)
	if !strings.Contains(body, "@view-transition") {
		t.Error("app.css does not opt in to cross-document view transitions")
	}
	// Text must follow the browser/OS text size: no px font sizes, and no
	// root font-size override that would defeat <meta name="text-scale">.
	if m := regexp.MustCompile(`font-size:\s*[0-9.]+px`).FindString(body); m != "" {
		t.Errorf("app.css uses a px font size: %q", m)
	}
	if regexp.MustCompile(`(?s)(?:^|[}\s])(?::root|html)\s*\{[^}]*font-size`).MatchString(body) {
		t.Error("app.css overrides the root font size")
	}
	// content-visibility: auto without a placeholder size collapses skipped
	// elements to 0px and makes the scrollbar jump.
	for _, m := range regexp.MustCompile(`\{[^}]*content-visibility:\s*auto[^}]*\}`).FindAllString(body, -1) {
		if !strings.Contains(m, "contain-intrinsic-") {
			t.Errorf("content-visibility: auto without contain-intrinsic-size: %q", m)
		}
	}
	// Skipped content leaves the accessibility tree, so the long-thread rule
	// must skip only the comment body — the <li> and its permalink stay in
	// the AX tree (bugs.md #23). Guard against the rule ever being moved
	// back onto .comment itself.
	cv := regexp.MustCompile(`([^{}]+)\{[^}]*content-visibility:\s*auto`).FindStringSubmatch(body)
	if cv == nil || !strings.Contains(cv[1], ".comment-body") {
		t.Errorf("content-visibility must target .comment-body, got selector %q", cv)
	}
}

func TestStaticAssetsUseGzipAndETag(t *testing.T) {
	srv, _ := newTestServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/static/app.css", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "br, gzip;q=0.8")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("ETag is empty")
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	if !strings.Contains(string(body), "@view-transition") {
		t.Error("decompressed app.css lost its content")
	}

	req, err = http.NewRequest(http.MethodGet, srv.URL+"/static/app.css", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("If-None-Match", etag)
	resp, err = srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", resp.StatusCode)
	}
	if body, err := io.ReadAll(resp.Body); err != nil || len(body) != 0 {
		t.Errorf("304 body length/error = %d/%v, want empty", len(body), err)
	}
}

// toast.js is imported by partial-submit.js (a module import, not a
// <script> tag), so nothing else would catch a missing route.
func TestStaticToastJS(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/static/toast.js")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("Content-Type = %q, want a JavaScript type", ct)
	}
}

func TestLayoutOptsIntoOSTextScale(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/w/default/issues")
	if err != nil {
		t.Fatal(err)
	}
	if body := getBody(t, resp); !strings.Contains(body, `<meta name="text-scale" content="scale">`) {
		t.Error(`layout lacks <meta name="text-scale" content="scale">`)
	}
}

func TestUnknownPathRendersStyled404(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/nonexistent-path")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(body, "Not found") {
		t.Error("expected the styled Not found page")
	}
	if strings.Contains(body, "404 page not found") {
		t.Error("must not render the mux's plaintext 404")
	}
}

func TestIssuesTrailingSlashRendersStyled404(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/w/default/issues/")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(body, "Not found") {
		t.Error("expected the styled Not found page")
	}
}

func TestStaticDirectoryListing404(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/static/")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if !strings.Contains(body, "Not found") {
		t.Error("expected the styled Not found page, not a directory listing")
	}
}

func TestSecurityHeaders(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/w/default/issues")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if v := resp.Header.Get("X-Content-Type-Options"); v != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", v)
	}
	if v := resp.Header.Get("X-Frame-Options"); v != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", v)
	}
	if v := resp.Header.Get("Referrer-Policy"); v != "same-origin" {
		t.Errorf("Referrer-Policy = %q, want same-origin", v)
	}

	csp := resp.Header.Get("Content-Security-Policy")
	for _, directive := range []string{
		"default-src 'none'",
		"script-src 'self'", // external /static scripts allowed, inline still blocked
		"style-src 'self'",
		"style-src-attr 'unsafe-inline'", // label badges' style="--label-color"
		"img-src 'self' data:",           // data: is for the favicon
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(csp, directive) {
			t.Errorf("Content-Security-Policy missing %q, got %q", directive, csp)
		}
	}
	if v := resp.Header.Get("Cross-Origin-Opener-Policy"); v != "same-origin-allow-popups" {
		t.Errorf("Cross-Origin-Opener-Policy = %q, want same-origin-allow-popups", v)
	}
	if v := resp.Header.Get("Cross-Origin-Resource-Policy"); v != "same-origin" {
		t.Errorf("Cross-Origin-Resource-Policy = %q, want same-origin", v)
	}
	if v := resp.Header.Get("Permissions-Policy"); v == "" {
		t.Error("Permissions-Policy is not set")
	}
	// Vary must cover the request headers CrossOriginProtection inspects.
	if v := resp.Header.Values("Vary"); !slices.Contains(v, "Sec-Fetch-Site") ||
		!slices.Contains(v, "Origin") {
		t.Errorf("Vary = %v, want Sec-Fetch-Site and Origin", v)
	}
}

// Every page (including the styled error page) shares the layout contract:
// a skip link as the first element in <body>, one focusable-out-of-tab-order
// <main> as its target, one <h1>, and header/nav/footer landmarks.
func TestSkipLinkAndLandmarks(t *testing.T) {
	srv, f := newTestServer(t)
	seedIssues(t, f)

	pages := []struct {
		name string
		path string
	}{
		{"list", "/w/default/issues"},
		{"detail", "/w/default/issues/1"},
		{"new form", "/w/default/issues/new"},
		{"edit form", "/w/default/issues/1/edit"},
		{"delete confirm", "/w/default/issues/1/delete"},
		{"styled 404", "/nonexistent"},
	}
	for _, p := range pages {
		resp, err := srv.Client().Get(srv.URL + p.path)
		if err != nil {
			t.Fatal(err)
		}
		body := getBody(t, resp)
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: status = %d", p.name, resp.StatusCode)
		}
		if !strings.Contains(body, `<a class="skip-link" href="#main-content">Skip to main content</a>`) {
			t.Errorf("%s: missing skip link", p.name)
		}
		// The skip link is the first Tab stop, so it must precede the header.
		if strings.Index(body, `class="skip-link"`) > strings.Index(body, `<header class="site-header"`) {
			t.Errorf("%s: skip link must come before the site header", p.name)
		}
		// The target is focusable (screen magnifiers follow the focus) but
		// stays out of the tab order itself.
		if n := strings.Count(body, `<main class="container main-content" id="main-content" tabindex="-1">`); n != 1 {
			t.Errorf("%s: want exactly one skip-link target main, got %d", p.name, n)
		}
		if n := strings.Count(body, "<h1"); n != 1 {
			t.Errorf("%s: want exactly one h1, got %d", p.name, n)
		}
		for _, want := range []string{`<header class="site-header"`, `aria-label="Main"`, `<footer class="site-footer"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing landmark markup %q", p.name, want)
			}
		}
	}
}

func TestSpeculationRules(t *testing.T) {
	srv, f := newTestServer(t)
	f.addIssue("Speculated target", "body", store.StatusOpen, time.Now())

	resp, err := srv.Client().Get(srv.URL + "/w/default/issues")
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, resp)
	// The rules are delivered by header (external JSON ruleset), keeping
	// inline <script> out of the markup — CSP script-src 'self' blocks
	// inline scripts and anything not served from /static/.
	if v := resp.Header.Get("Speculation-Rules"); v != `"/static/speculation-rules.json"` {
		t.Errorf("Speculation-Rules = %q, want quoted ruleset URL", v)
	}
	// Every <script> must be an external same-origin /static/ file — inline
	// scripts stay blocked by CSP (Go's regexp has no lookahead, so strip
	// allowed tags and make sure nothing is left).
	stripped := regexp.MustCompile(`<script[^>]*src="/static/[a-zA-Z0-9._/-]+"[^>]*>\s*</script>`).ReplaceAllString(body, "")
	if strings.Contains(stripped, "<script") {
		t.Error("list page must only load same-origin /static scripts, never inline")
	}

	resp2, err := srv.Client().Get(srv.URL + "/w/default/issues/1")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if v := resp2.Header.Get("Speculation-Rules"); v != "" {
		t.Errorf("Speculation-Rules on detail page = %q, want empty (list page only)", v)
	}

	resp3, err := srv.Client().Get(srv.URL + "/static/speculation-rules.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if v := resp3.Header.Get("Content-Type"); v != "application/speculationrules+json" {
		t.Errorf("ruleset Content-Type = %q, want application/speculationrules+json", v)
	}
	var rules struct {
		Prefetch []struct {
			Where struct {
				SelectorMatches string `json:"selector_matches"`
			} `json:"where"`
			Eagerness string `json:"eagerness"`
		} `json:"prefetch"`
	}
	if err := json.NewDecoder(resp3.Body).Decode(&rules); err != nil {
		t.Fatalf("ruleset is not valid JSON: %v", err)
	}
	if len(rules.Prefetch) != 1 {
		t.Fatalf("prefetch rules = %d, want 1", len(rules.Prefetch))
	}
	if e := rules.Prefetch[0].Eagerness; e != "moderate" {
		t.Errorf("eagerness = %q, want moderate", e)
	}
	for _, sel := range []string{".issue-list", ".pagination"} {
		if !strings.Contains(rules.Prefetch[0].Where.SelectorMatches, sel) {
			t.Errorf("selector_matches %q lacks %q", rules.Prefetch[0].Where.SelectorMatches, sel)
		}
	}
}

// Pull requests — list state lives in memory so pagination can be tested;
// the remaining PR methods are plain stubs because most web tests mount no repo.
func (f *fakeStore) ListPullRequests(_ context.Context, fl store.PRListFilter) ([]*store.PullRequest, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	status := fl.Status
	if status == "" {
		status = store.PRStatusOpen
	}
	var matched []*store.PullRequest
	for _, pr := range f.pulls {
		if status != "all" && pr.Status != status {
			continue
		}
		if fl.Query != "" {
			q := strings.ToLower(fl.Query)
			if !strings.Contains(strings.ToLower(pr.Title), q) && !strings.Contains(strings.ToLower(pr.Body), q) {
				continue
			}
		}
		matched = append(matched, pr)
	}
	sort.Slice(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		if fl.Sort == "oldest" {
			if a.CreatedAt.Equal(b.CreatedAt) {
				return a.ID < b.ID
			}
			return a.CreatedAt.Before(b.CreatedAt)
		}
		if a.CreatedAt.Equal(b.CreatedAt) {
			return a.ID > b.ID
		}
		return a.CreatedAt.After(b.CreatedAt)
	})

	total := len(matched)
	page := fl.Page
	if page < 1 || page > store.MaxListPage {
		page = 1
	}
	perPage := fl.PerPage
	if perPage <= 0 {
		perPage = 20
	} else if perPage > store.MaxListPerPage {
		perPage = store.MaxListPerPage
	}
	start := int64(page-1) * int64(perPage)
	if start >= int64(total) {
		return nil, total, nil
	}
	end := min(start+int64(perPage), int64(total))
	return append([]*store.PullRequest(nil), matched[int(start):int(end)]...), total, nil
}

func (f *fakeStore) GetPullRequest(context.Context, int64) (*store.PullRequest, error) {
	return nil, store.ErrNotFound
}

func (f *fakeStore) CreatePullRequest(context.Context, store.PRInput, int64, func(int64) error) (int64, error) {
	return 0, errors.New("fakeStore: unimplemented")
}

func (f *fakeStore) UpdatePullRequest(context.Context, int64, store.PRInput) error {
	return store.ErrNotFound
}

func (f *fakeStore) DeletePullRequest(context.Context, int64) error {
	return store.ErrNotFound
}

func (f *fakeStore) ClosePullRequest(context.Context, int64, string, string) error {
	return store.ErrNotFound
}

func (f *fakeStore) ReopenPullRequest(context.Context, int64) error {
	return store.ErrNotFound
}

func (f *fakeStore) MarkMerged(context.Context, int64, store.PRMergeInfo) error {
	return store.ErrNotFound
}

func (f *fakeStore) ListPRComments(context.Context, int64) ([]*store.PRComment, error) {
	return nil, nil
}

func (f *fakeStore) CreatePRComment(context.Context, int64, store.PRCommentInput, int64) (int64, error) {
	return 0, errors.New("fakeStore: unimplemented")
}

func (f *fakeStore) UpdatePRComment(context.Context, int64, int64, string) error {
	return store.ErrNotFound
}

func (f *fakeStore) DeletePRComment(context.Context, int64, int64) error {
	return store.ErrNotFound
}

func (f *fakeStore) SetPRCommentHidden(context.Context, int64, int64, string) error {
	return store.ErrNotFound
}
