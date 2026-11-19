// Pull requests (docs/sketch/ja/pull-requests.md): PR rows are
// workspace-independent DB records whose code data (commits, diffs,
// mergeability) is computed live from the git repo while they are open.
// These routes mount only when web.New got a *gitrepo.Repo — repo-less
// deployments answer 404 via the catch-all and the nav link stays hidden.
package web

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/podhmo/go-classic-mpa-sample/internal/events"
	"github.com/podhmo/go-classic-mpa-sample/internal/gitrepo"
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// pullListPage is the /pulls view model — the same filter-bar + pagination
// structure as the issue list, minus labels.
type pullListPage struct {
	Title           string
	Section         string
	Filter          store.PRListFilter
	Pulls           []*store.PullRequest
	Total           int
	Page            int
	TotalPages      int
	RangeStart      int
	RangeEnd        int
	HasActiveFilter bool
	Workspace       *store.Workspace // nav only — PRs have no workspace
	User            *store.User
}

// pullDetailPage is GET /pulls/{id}: conversation data from the DB plus
// live git data (commits, mergeability) computed while the PR is open.
type pullDetailPage struct {
	Title        string
	Section      string
	PR           *store.PullRequest
	Commits      []gitrepo.Commit
	Merge        *gitrepo.MergeCheck // nil when the probe could not run
	Comments     []*store.PRComment
	CommentDraft string
	CommentError string
	Workspace    *store.Workspace
	User         *store.User
}

// pullFormPage backs both /pulls/new (base/head selects editable) and
// /pulls/{id}/edit (title/body only — the ref pair is immutable, so the
// selects render as static text when PR is non-nil).
type pullFormPage struct {
	Title     string
	Heading   string
	Action    string
	Section   string
	PR        *store.PullRequest
	Refs      []gitrepo.RefName
	Current   string // repo's current branch — the create form's head default
	Input     store.PRInput
	Errors    pullFormErrors
	Workspace *store.Workspace
	User      *store.User
}

// pullFormErrors is the form's per-field error view — the store's
// FieldErrors map keeps lowercase keys while the template reads Title /
// Body / Refs, matching the issue/discussion form adapters.
type pullFormErrors struct {
	Title string
	Body  string
	Refs  string
}

func (e pullFormErrors) any() bool { return e.Title != "" || e.Body != "" || e.Refs != "" }

// pullMergePage is GET /pulls/{id}/merge — the confirmation page carrying
// the live mergeability probe; a failed POST re-renders it with Error.
type pullMergePage struct {
	Title     string
	Section   string
	PR        *store.PullRequest
	Check     *gitrepo.MergeCheck
	Error     string
	Workspace *store.Workspace
	User      *store.User
}

// pullFilesPage is GET /pulls/{id}/files — per-file diffs with per-line
// review-comment forms, plus the comments already anchored there.
type pullFilesPage struct {
	Title     string
	Section   string
	PR        *store.PullRequest
	Files     []pullFileView
	Error     string // set when the diff could not be computed (e.g. refs gone)
	Workspace *store.Workspace
	User      *store.User
}

// pullFileView is one changed file: its diff split into numbered lines,
// the anchored review comments, and a sanitized anchor id for #links.
type pullFileView struct {
	Change       gitrepo.FileChange
	CommentPath  string // NewPath, or OldPath for deletions
	StatusLetter string // first letter of the name-status ("M","A","D","R")
	Anchor       string
	Binary       bool
	Lines        []diffLine
	Comments     []*store.PRComment
}

// diffLine is one parsed unified-diff row; Kind drives the CSS class and
// which side's line number the comment form anchors to.
type diffLine struct {
	Kind  string // "meta" | "hunk" | "ctx" | "add" | "del"
	OldNo int
	NewNo int
	Text  string
}

// parseUnifiedDiff splits a per-file diff into numbered lines. Numbers
// track the hunk headers: add/ctx carry the new (right) line number, del
// the old (left) one — the sides review comments anchor to.
func parseUnifiedDiff(diff string) []diffLine {
	var out []diffLine
	oldNo, newNo := 0, 0
	inHunks := false
	for line := range strings.Lines(diff) {
		line = strings.TrimRight(line, "\n")
		if strings.HasPrefix(line, "@@") {
			f := strings.Fields(line)
			if len(f) >= 3 {
				oldNo = hunkStart(f[1]) - 1
				newNo = hunkStart(f[2]) - 1
			}
			inHunks = true
			out = append(out, diffLine{Kind: "hunk", Text: line})
			continue
		}
		if !inHunks {
			out = append(out, diffLine{Kind: "meta", Text: line})
			continue
		}
		switch {
		case strings.HasPrefix(line, "+"):
			newNo++
			out = append(out, diffLine{Kind: "add", NewNo: newNo, Text: line})
		case strings.HasPrefix(line, "-"):
			oldNo++
			out = append(out, diffLine{Kind: "del", OldNo: oldNo, Text: line})
		case strings.HasPrefix(line, `\`):
			out = append(out, diffLine{Kind: "meta", Text: line})
		default:
			oldNo++
			newNo++
			out = append(out, diffLine{Kind: "ctx", OldNo: oldNo, NewNo: newNo, Text: line})
		}
	}
	return out
}

// hunkStart parses the start line of a "-a,b" or "+c,d" hunk range.
func hunkStart(field string) int {
	s := strings.TrimPrefix(strings.TrimPrefix(field, "-"), "+")
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	n, _ := strconv.Atoi(s)
	return n
}

// fileAnchor makes a stable element id for a file card so #links and
// post-comment redirects can land on it.
func fileAnchor(path string) string {
	var b strings.Builder
	for _, r := range path {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return "f-" + b.String()
}

// navWorkspace returns the default workspace for nav links only — pulls
// pages are workspace-independent but the header's Issues/Discussions
// links still need a slug to point at.
func (h *handler) navWorkspace(r *http.Request) *store.Workspace {
	wss, err := h.store.ListWorkspaces(r.Context())
	if err != nil || len(wss) == 0 {
		return nil
	}
	return wss[0]
}

// pullError maps store errors to responses — same shape as issueError.
func (h *handler) pullError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		h.notFound(w, r)
	case errors.Is(err, store.ErrInvalidPRTransition):
		h.renderError(w, r, http.StatusConflict, "Invalid status change",
			"The pull request is already in that state.")
	case errors.Is(err, store.ErrDuplicatePR):
		h.renderError(w, r, http.StatusConflict, "Duplicate pull request",
			"An open pull request for the same base and head already exists.")
	case errors.Is(err, store.ErrSameRef):
		h.renderError(w, r, http.StatusUnprocessableEntity, "Same ref",
			"Base and head must be different refs.")
	default:
		h.internalError(w, r, err)
	}
}

// requirePull resolves /pulls/{id}; any failure already wrote a response.
func (h *handler) requirePull(w http.ResponseWriter, r *http.Request) (*store.PullRequest, bool) {
	id, ok := parseID(r)
	if !ok {
		h.notFound(w, r)
		return nil, false
	}
	pr, err := h.store.GetPullRequest(r.Context(), id)
	if err != nil {
		h.pullError(w, r, err)
		return nil, false
	}
	return pr, true
}

// diffRange names the revisions a PR's code view covers: open PRs are live
// (the refs themselves), closed/merged ones are pinned to the SHAs recorded
// at close/merge so the view survives branch deletion.
func (h *handler) diffRange(pr *store.PullRequest) (from, to string) {
	if pr.Open() {
		return pr.BaseRef, pr.HeadRef
	}
	from, to = pr.MergeBaseSHA, pr.HeadSHA
	if from == "" {
		from = pr.BaseRef
	}
	if to == "" {
		to = gitrepo.PinRef(pr.ID)
	}
	return from, to
}

// listPulls serves GET /pulls with the same tab + filter-bar structure as
// the issue list (status open|closed|merged|all, q, sort, pagination).
func (h *handler) listPulls(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := store.PRListFilter{
		Status: q.Get("status"),
		Query:  strings.TrimSpace(q.Get("q")),
		Sort:   q.Get("sort"),
	}
	switch filter.Status {
	case "open", "closed", "merged", "all":
	case "":
		filter.Status = "open"
	default:
		h.notFound(w, r)
		return
	}
	if filter.Sort != "oldest" {
		filter.Sort = "newest"
	}
	filter.Page, _ = strconv.Atoi(q.Get("page"))
	filter.PerPage = perPage
	pulls, total, err := h.store.ListPullRequests(r.Context(), filter)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	totalPages := (total + perPage - 1) / perPage
	page := pullListPage{
		Title:   "Pull requests",
		Section: "pulls",
		Filter:  filter,
		Pulls:   pulls,
		Total:   total,
		Page:    filter.Page,
		// ListPullRequests clamps Page to 1 — mirror that for display.
		TotalPages:      totalPages,
		RangeStart:      (filter.Page-1)*perPage + 1,
		RangeEnd:        (filter.Page-1)*perPage + len(pulls),
		HasActiveFilter: filter.Query != "" || filter.Status != "open" || filter.Sort != "newest",
		Workspace:       h.navWorkspace(r),
		User:            userFromContext(r.Context()),
	}
	if page.Page < 1 {
		page.Page = 1
	}
	h.r.renderPullList(w, http.StatusOK, page)
}

// newPullForm serves GET /pulls/new with the repo's branches as base/head
// select options (head defaults to the current branch).
func (h *handler) newPullForm(w http.ResponseWriter, r *http.Request) {
	refs, err := h.repo.Refs(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	current, _ := h.repo.CurrentBranch(r.Context())
	h.r.renderPullForm(w, http.StatusOK, pullFormPage{
		Title:     "New pull request",
		Heading:   "New pull request",
		Action:    "/pulls",
		Section:   "pulls",
		Refs:      refs,
		Current:   current,
		Workspace: h.navWorkspace(r),
		User:      userFromContext(r.Context()),
	})
}

// createPull validates POST /pulls: both refs must exist in the repo, the
// head tip is pinned immediately (refs/zatudan/pr/<id>/head), and a
// duplicate open pair or a same-ref pair surfaces as its mapped error.
func (h *handler) createPull(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r) {
		return
	}
	ctx := r.Context()
	in := store.PRInput{
		Title: strings.TrimSpace(r.PostFormValue("title")),
		Body:  strings.TrimSpace(r.PostFormValue("body")),
	}
	base, head := r.PostFormValue("base"), r.PostFormValue("head")
	var normErr error
	if in.BaseRef, normErr = h.repo.NormalizeRef(ctx, base); normErr != nil {
		in.BaseRef = base
	}
	if in.HeadRef, normErr = h.repo.NormalizeRef(ctx, head); normErr != nil {
		in.HeadRef = head
	}
	var ferrs pullFormErrors
	if fe := store.ValidatePullRequestInput(in); fe != nil {
		ferrs.Title = fe["title"]
		ferrs.Body = fe["body"]
		ferrs.Refs = strings.TrimSpace(strings.Join([]string{fe["base"], fe["head"]}, " "))
	}
	if normErr != nil && !errors.Is(normErr, gitrepo.ErrBadRef) {
		h.internalError(w, r, normErr)
		return
	}
	if normErr != nil {
		ferrs.Refs = "Base and head must name existing refs or commits."
	}
	if ferrs.any() {
		h.renderPullFormErr(w, r, http.StatusUnprocessableEntity, in, ferrs)
		return
	}
	var shaErr error
	if in.BaseSHA, shaErr = h.repo.RevParse(ctx, in.BaseRef); shaErr != nil {
		h.internalError(w, r, shaErr)
		return
	}
	if in.HeadSHA, shaErr = h.repo.RevParse(ctx, in.HeadRef); shaErr != nil {
		h.internalError(w, r, shaErr)
		return
	}
	var authorID int64
	if u := userFromContext(ctx); u != nil {
		authorID = u.ID
	}
	headSHA := in.HeadSHA
	id, err := h.store.CreatePullRequest(ctx, in, authorID, func(prID int64) error {
		return h.repo.PinHead(ctx, prID, headSHA)
	})
	if err != nil {
		h.pullError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/pulls/%d", id), http.StatusSeeOther)
}

// renderPullFormErr re-renders the create/edit form with field errors.
func (h *handler) renderPullFormErr(w http.ResponseWriter, r *http.Request, status int, in store.PRInput, errs pullFormErrors) {
	refs, _ := h.repo.Refs(r.Context())
	h.r.renderPullForm(w, status, pullFormPage{
		Title:     "New pull request",
		Heading:   "New pull request",
		Action:    "/pulls",
		Section:   "pulls",
		Refs:      refs,
		Input:     in,
		Errors:    errs,
		Workspace: h.navWorkspace(r),
		User:      userFromContext(r.Context()),
	})
}

// loadPullView gathers the live git data the detail/files pages need:
// commits and (for open PRs) the mergeability probe. Git failures degrade
// to empty results — a deleted head branch must not take the page down.
func (h *handler) loadPullView(r *http.Request, pr *store.PullRequest) (commits []gitrepo.Commit, check *gitrepo.MergeCheck) {
	from, to := h.diffRange(pr)
	if c, err := h.repo.Log(r.Context(), from, to); err == nil {
		commits = c
	} else {
		log.Printf("web: pulls: log %s..%s: %v", from, to, err)
	}
	if pr.Open() {
		if mc, err := h.repo.CheckMerge(r.Context(), pr.BaseRef, pr.HeadRef); err == nil {
			check = mc
		} else {
			log.Printf("web: pulls: check merge #%d: %v", pr.ID, err)
		}
	}
	return commits, check
}

// pullDetail serves GET /pulls/{id} — the PR card plus the comment
// timeline with live-update wiring.
func (h *handler) pullDetail(w http.ResponseWriter, r *http.Request) {
	pr, ok := h.requirePull(w, r)
	if !ok {
		return
	}
	comments, err := h.store.ListPRComments(r.Context(), pr.ID)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	commits, check := h.loadPullView(r, pr)
	h.r.renderPullDetail(w, http.StatusOK, pullDetailPage{
		Title:     pr.Title,
		Section:   "pulls",
		PR:        pr,
		Commits:   commits,
		Merge:     check,
		Comments:  comments,
		Workspace: h.navWorkspace(r),
		User:      userFromContext(r.Context()),
	})
}

// editPullForm serves GET /pulls/{id}/edit — title/body only; the base/head
// pair is part of the PR's identity and immutable.
func (h *handler) editPullForm(w http.ResponseWriter, r *http.Request) {
	pr, ok := h.requirePull(w, r)
	if !ok {
		return
	}
	h.r.renderPullForm(w, http.StatusOK, pullFormPage{
		Title:     fmt.Sprintf("Edit pull request #%d", pr.ID),
		Heading:   fmt.Sprintf("Edit pull request #%d", pr.ID),
		Action:    fmt.Sprintf("/pulls/%d/edit", pr.ID),
		Section:   "pulls",
		PR:        pr,
		Input:     store.PRInput{Title: pr.Title, Body: pr.Body},
		Workspace: h.navWorkspace(r),
		User:      userFromContext(r.Context()),
	})
}

// updatePull applies the edit and returns to the detail page.
func (h *handler) updatePull(w http.ResponseWriter, r *http.Request) {
	pr, ok := h.requirePull(w, r)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	in := store.PRInput{
		BaseRef: pr.BaseRef,
		HeadRef: pr.HeadRef,
		Title:   strings.TrimSpace(r.PostFormValue("title")),
		Body:    strings.TrimSpace(r.PostFormValue("body")),
	}
	if fe := store.ValidatePullRequestInput(in); len(fe) > 0 {
		ferrs := pullFormErrors{Title: fe["title"], Body: fe["body"],
			Refs: strings.TrimSpace(strings.Join([]string{fe["base"], fe["head"]}, " "))}
		h.r.renderPullForm(w, http.StatusUnprocessableEntity, pullFormPage{
			Title:     fmt.Sprintf("Edit pull request #%d", pr.ID),
			Heading:   fmt.Sprintf("Edit pull request #%d", pr.ID),
			Action:    fmt.Sprintf("/pulls/%d/edit", pr.ID),
			Section:   "pulls",
			PR:        pr,
			Input:     in,
			Errors:    ferrs,
			Workspace: h.navWorkspace(r),
			User:      userFromContext(r.Context()),
		})
		return
	}
	if err := h.store.UpdatePullRequest(r.Context(), pr.ID, in); err != nil {
		h.pullError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/pulls/%d", pr.ID), http.StatusSeeOther)
}

// pullFiles serves GET /pulls/{id}/files — the changed-files view with
// per-line comment forms.
func (h *handler) pullFiles(w http.ResponseWriter, r *http.Request) {
	pr, ok := h.requirePull(w, r)
	if !ok {
		return
	}
	comments, err := h.store.ListPRComments(r.Context(), pr.ID)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	byPath := map[string][]*store.PRComment{}
	for _, c := range comments {
		if c.Anchored() {
			byPath[c.Path] = append(byPath[c.Path], c)
		}
	}
	page := pullFilesPage{
		Title:     fmt.Sprintf("Files changed · %s", pr.Title),
		Section:   "pulls",
		PR:        pr,
		Workspace: h.navWorkspace(r),
		User:      userFromContext(r.Context()),
	}
	from, to := h.diffRange(pr)
	if pr.Open() {
		// A "files changed" view shows what the head adds, like GitHub's
		// three-dot diff: base-side commits land after the merge-base and
		// must not render as pull-request deletions.
		if mb, err := h.repo.MergeBase(r.Context(), pr.BaseRef, pr.HeadRef); err == nil && mb != "" {
			from = mb
		}
	}
	changed, err := h.repo.ChangedFiles(r.Context(), from, to)
	if err != nil {
		// A deleted head (or an empty recorded range) degrades the page
		// to a notice rather than a 500 — the conversation still reads.
		log.Printf("web: pulls: changed files #%d: %v", pr.ID, err)
		page.Error = "The diff could not be computed — the refs may have moved or been deleted."
		h.r.renderPullFiles(w, http.StatusOK, page)
		return
	}
	for _, ch := range changed {
		path := ch.NewPath
		if path == "" {
			path = ch.OldPath
		}
		fv := pullFileView{
			Change:       ch,
			CommentPath:  path,
			StatusLetter: strings.ToUpper(string(ch.Status[0])),
			Anchor:       fileAnchor(path),
			Comments:     byPath[path],
		}
		paths := []string{path}
		if ch.OldPath != "" && ch.OldPath != path {
			paths = append(paths, ch.OldPath)
		}
		diff, binary, err := h.repo.Diff(r.Context(), from, to, paths...)
		if err != nil {
			log.Printf("web: pulls: diff #%d %s: %v", pr.ID, path, err)
		} else if binary {
			fv.Binary = true
		} else {
			fv.Lines = parseUnifiedDiff(diff)
		}
		page.Files = append(page.Files, fv)
	}
	h.r.renderPullFiles(w, http.StatusOK, page)
}

// createPRComment posts to /pulls/{id}/comments — a plain conversation
// comment, or a review comment when path/line/side fields are set (the
// anchor sha + surrounding snippet are snapshotted so the reference
// survives later pushes and gc).
func (h *handler) createPRComment(w http.ResponseWriter, r *http.Request) {
	pr, ok := h.requirePull(w, r)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	ctx := r.Context()
	in := store.PRCommentInput{Body: strings.TrimSpace(r.PostFormValue("body"))}
	if msg := store.ValidateCommentBody(in.Body); msg != "" {
		h.renderPullDetailErr(w, r, http.StatusUnprocessableEntity, pr, in.Body, msg)
		return
	}
	if path := r.PostFormValue("path"); path != "" {
		line, _ := strconv.Atoi(r.PostFormValue("line"))
		side := r.PostFormValue("side")
		if err := gitrepo.ValidatePath(path); err != nil || line < 1 ||
			(side != "" && side != "right" && side != "left") {
			h.renderError(w, r, http.StatusBadRequest, "Bad request",
				"Invalid review-comment anchor.")
			return
		}
		if side == "" {
			side = "right"
		}
		in.Path, in.Line, in.Side = path, line, side
		// Anchor to the head tip: live ref while open (branches still
		// move), the recorded sha once closed/merged.
		anchorSHA := pr.HeadSHA
		if pr.Open() {
			if sha, err := h.repo.RevParse(ctx, pr.HeadRef); err == nil {
				anchorSHA = sha
			}
		}
		in.AnchorSHA = anchorSHA
		// The snippet comes from the blob the comment anchors to: the
		// head side for "right", the merge-base (old) side for "left".
		snippetSHA := anchorSHA
		if side == "left" {
			snippetSHA = pr.MergeBaseSHA
			if pr.Open() {
				if mb, err := h.repo.MergeBase(ctx, pr.BaseRef, pr.HeadRef); err == nil {
					snippetSHA = mb
				}
			}
		}
		if snippetSHA != "" {
			if snippet, _, err := h.repo.Snippet(ctx, snippetSHA, path, line, 2); err == nil {
				in.DiffSnippet = snippet
			}
		}
	}
	var authorID int64
	if u := userFromContext(ctx); u != nil {
		authorID = u.ID
	}
	id, err := h.store.CreatePRComment(ctx, pr.ID, in, authorID)
	if err != nil {
		if errors.Is(err, store.ErrInvalidAnchor) {
			h.renderError(w, r, http.StatusBadRequest, "Bad request",
				"Invalid review-comment anchor.")
			return
		}
		h.pullError(w, r, err)
		return
	}
	if wantsPartial(r) {
		h.renderPullCommentAppend(w, r, pr, id)
		return
	}
	if in.Path != "" || r.PostFormValue("back") == "files" {
		http.Redirect(w, r, fmt.Sprintf("/pulls/%d/files#%s", pr.ID, fileAnchor(in.Path)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/pulls/%d#comment-%d", pr.ID, id), http.StatusSeeOther)
}

// renderPullDetailErr re-renders the detail page with the comment form's
// error — mirroring the issue page's CommentDraft/CommentError flow.
func (h *handler) renderPullDetailErr(w http.ResponseWriter, r *http.Request, status int, pr *store.PullRequest, draft, msg string) {
	comments, err := h.store.ListPRComments(r.Context(), pr.ID)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	commits, check := h.loadPullView(r, pr)
	h.r.renderPullDetail(w, status, pullDetailPage{
		Title:        pr.Title,
		Section:      "pulls",
		PR:           pr,
		Commits:      commits,
		Merge:        check,
		Comments:     comments,
		CommentDraft: draft,
		CommentError: msg,
		Workspace:    h.navWorkspace(r),
		User:         userFromContext(r.Context()),
	})
}

// renderPullCommentAppend answers a partial comment POST with the append
// fragment: header meta (updated_at bumped) plus the new comment <li>.
func (h *handler) renderPullCommentAppend(w http.ResponseWriter, r *http.Request, pr *store.PullRequest, commentID int64) {
	comments, err := h.store.ListPRComments(r.Context(), pr.ID)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	var c *store.PRComment
	for _, x := range comments {
		if x.ID == commentID {
			c = x
			break
		}
	}
	if c == nil {
		h.notFound(w, r)
		return
	}
	h.r.renderFragment(w, "pull_detail", "pr_comment_append", http.StatusOK,
		dict("PR", pr, "Comment", c, "Workspace", h.navWorkspace(r)))
}

// requirePRComment resolves /pulls/{id}/comments/{cid}.
func (h *handler) requirePRComment(w http.ResponseWriter, r *http.Request) (*store.PullRequest, *store.PRComment, bool) {
	pr, ok := h.requirePull(w, r)
	if !ok {
		return nil, nil, false
	}
	cid, ok := parseSubID(r, "cid")
	if !ok {
		h.notFound(w, r)
		return nil, nil, false
	}
	comments, err := h.store.ListPRComments(r.Context(), pr.ID)
	if err != nil {
		h.internalError(w, r, err)
		return nil, nil, false
	}
	for _, c := range comments {
		if c.ID == cid {
			return pr, c, true
		}
	}
	h.notFound(w, r)
	return nil, nil, false
}

// editPRCommentForm renders the comment's edit page (shared comment_edit
// template; the URLs point back into /pulls).
func (h *handler) editPRCommentForm(w http.ResponseWriter, r *http.Request) {
	pr, c, ok := h.requirePRComment(w, r)
	if !ok {
		return
	}
	h.r.renderCommentEdit(w, http.StatusOK, commentEditPage{
		Title:     fmt.Sprintf("Edit comment #%d", c.ID),
		Heading:   fmt.Sprintf("Edit comment #%d", c.ID),
		Action:    fmt.Sprintf("/pulls/%d/comments/%d/edit", pr.ID, c.ID),
		CancelURL: fmt.Sprintf("/pulls/%d#comment-%d", pr.ID, c.ID),
		Body:      c.Body,
		Section:   "pulls",
		Workspace: h.navWorkspace(r),
		User:      userFromContext(r.Context()),
	})
}

// updatePRComment applies the edit and returns to the comment's anchor.
func (h *handler) updatePRComment(w http.ResponseWriter, r *http.Request) {
	pr, c, ok := h.requirePRComment(w, r)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	body := strings.TrimSpace(r.PostFormValue("body"))
	cancelURL := fmt.Sprintf("/pulls/%d#comment-%d", pr.ID, c.ID)
	if msg := store.ValidateCommentBody(body); msg != "" {
		h.r.renderCommentEdit(w, http.StatusUnprocessableEntity, commentEditPage{
			Title:     fmt.Sprintf("Edit comment #%d", c.ID),
			Heading:   fmt.Sprintf("Edit comment #%d", c.ID),
			Action:    fmt.Sprintf("/pulls/%d/comments/%d/edit", pr.ID, c.ID),
			CancelURL: cancelURL,
			Body:      body,
			Error:     msg,
			Section:   "pulls",
			Workspace: h.navWorkspace(r),
			User:      userFromContext(r.Context()),
		})
		return
	}
	if err := h.store.UpdatePRComment(r.Context(), pr.ID, c.ID, body); err != nil {
		h.pullError(w, r, err)
		return
	}
	http.Redirect(w, r, cancelURL, http.StatusSeeOther)
}

// confirmDeletePRComment renders the delete confirmation page.
func (h *handler) confirmDeletePRComment(w http.ResponseWriter, r *http.Request) {
	pr, c, ok := h.requirePRComment(w, r)
	if !ok {
		return
	}
	h.r.renderCommentDelete(w, http.StatusOK, commentDeletePage{
		Title:     fmt.Sprintf("Delete comment #%d", c.ID),
		Heading:   fmt.Sprintf("Delete comment #%d", c.ID),
		Noun:      "comment",
		Preview:   excerpt(c.Body),
		Action:    fmt.Sprintf("/pulls/%d/comments/%d/delete", pr.ID, c.ID),
		CancelURL: fmt.Sprintf("/pulls/%d#comment-%d", pr.ID, c.ID),
		Section:   "pulls",
		Workspace: h.navWorkspace(r),
		User:      userFromContext(r.Context()),
	})
}

// deletePRComment removes the row and returns to the detail page.
func (h *handler) deletePRComment(w http.ResponseWriter, r *http.Request) {
	pr, c, ok := h.requirePRComment(w, r)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	if err := h.store.DeletePRComment(r.Context(), pr.ID, c.ID); err != nil {
		h.pullError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/pulls/%d", pr.ID), http.StatusSeeOther)
}

func (h *handler) hidePRComment(w http.ResponseWriter, r *http.Request) {
	h.setPRCommentHidden(w, r, true)
}

func (h *handler) unhidePRComment(w http.ResponseWriter, r *http.Request) {
	h.setPRCommentHidden(w, r, false)
}

// setPRCommentHidden is the shared body of hide/unhide — the <li>
// re-renders for the partial swap like every other comment kind.
func (h *handler) setPRCommentHidden(w http.ResponseWriter, r *http.Request, hide bool) {
	pr, c, ok := h.requirePRComment(w, r)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	reason := ""
	if hide {
		reason = r.PostFormValue("reason")
		if !store.ValidHideReason(reason) {
			h.renderError(w, r, http.StatusBadRequest, "Bad request", "A valid hide reason is required.")
			return
		}
	}
	if err := h.store.SetPRCommentHidden(r.Context(), pr.ID, c.ID, reason); err != nil {
		h.pullError(w, r, err)
		return
	}
	if wantsPartial(r) {
		comments, err := h.store.ListPRComments(r.Context(), pr.ID)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		var nc *store.PRComment
		for _, x := range comments {
			if x.ID == c.ID {
				nc = x
				break
			}
		}
		if nc == nil {
			h.notFound(w, r)
			return
		}
		h.r.renderFragment(w, "pull_detail", "pr_comment", http.StatusOK,
			dict("C", nc, "PR", pr))
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/pulls/%d#comment-%d", pr.ID, c.ID), http.StatusSeeOther)
}

// updatePullStatus applies POST /pulls/{id}/status — close records the
// final head/merge-base resolution (and pins the head) so a closed PR's
// diff view survives branch deletion; reopen clears it back.
func (h *handler) updatePullStatus(w http.ResponseWriter, r *http.Request) {
	pr, ok := h.requirePull(w, r)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	ctx := r.Context()
	if !pr.Open() {
		// Stale resubmission: reject before touching git so a reused head
		// branch cannot repoint the archived pin (the store would reject
		// the transition only afterward).
		h.renderError(w, r, http.StatusConflict, "Invalid transition",
			"Only an open pull request can change status.")
		return
	}
	switch r.PostFormValue("status") {
	case "closed":
		headSHA, err := h.repo.RevParse(ctx, pr.HeadRef)
		if err != nil {
			headSHA = "" // head may be deleted already — close anyway
		}
		if headSHA != "" {
			// The pin is what keeps the recorded commits reachable; a
			// failed update means closing would archive the wrong sha.
			if err := h.repo.UpdatePin(ctx, pr.ID, headSHA); err != nil {
				h.pullError(w, r, err)
				return
			}
		}
		mergeBase, _ := h.repo.MergeBase(ctx, pr.BaseRef, pr.HeadRef)
		if err := h.store.ClosePullRequest(ctx, pr.ID, headSHA, mergeBase); err != nil {
			h.pullError(w, r, err)
			return
		}
	case "open":
		if err := h.store.ReopenPullRequest(ctx, pr.ID); err != nil {
			h.pullError(w, r, err)
			return
		}
	default:
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "Unknown status action.")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/pulls/%d", pr.ID), http.StatusSeeOther)
}

// mergePullForm serves GET /pulls/{id}/merge — the confirmation page with
// the live conflict preview and the merge-method choice.
func (h *handler) mergePullForm(w http.ResponseWriter, r *http.Request) {
	pr, ok := h.requirePull(w, r)
	if !ok {
		return
	}
	if !pr.Open() {
		h.renderError(w, r, http.StatusConflict, "Not mergeable",
			"Only an open pull request can be merged.")
		return
	}
	check, err := h.repo.CheckMerge(r.Context(), pr.BaseRef, pr.HeadRef)
	if err != nil {
		check = nil // probe failure still shows the page — degraded
		log.Printf("web: pulls: check merge #%d: %v", pr.ID, err)
	}
	h.r.renderPullMerge(w, http.StatusOK, pullMergePage{
		Title:     fmt.Sprintf("Merge pull request #%d", pr.ID),
		Section:   "pulls",
		PR:        pr,
		Check:     check,
		Workspace: h.navWorkspace(r),
		User:      userFromContext(r.Context()),
	})
}

// mergePull applies POST /pulls/{id}/merge: git-side merge first (all
// pre-flight checks live in gitrepo.Merge), then the DB records the
// result. Conflicts and concurrent updates re-render the confirm page.
func (h *handler) mergePull(w http.ResponseWriter, r *http.Request) {
	pr, ok := h.requirePull(w, r)
	if !ok {
		return
	}
	if !h.parseForm(w, r) {
		return
	}
	ctx := r.Context()
	if !pr.Open() {
		h.renderError(w, r, http.StatusConflict, "Not mergeable",
			"Only an open pull request can be merged.")
		return
	}
	method := gitrepo.MergeMethod(r.PostFormValue("method"))
	if !method.Valid() {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "Unknown merge method.")
		return
	}
	msg := ""
	if method == gitrepo.MethodSquash {
		msg = fmt.Sprintf("%s (#%d)", pr.Title, pr.ID)
	}
	// Pin the head tip before merging — squash/rebase leave the head
	// commits dangling on the feature branch, and the pin is what keeps
	// them reachable once that branch is deleted.
	if headSHA, err := h.repo.RevParse(ctx, pr.HeadRef); err == nil {
		if err := h.repo.UpdatePin(ctx, pr.ID, headSHA); err != nil {
			h.pullError(w, r, err)
			return
		}
	}
	res, err := h.repo.Merge(ctx, pr.BaseRef, pr.HeadRef, method, msg)
	if err != nil {
		var ce *gitrepo.ConflictError
		var de *gitrepo.DirtyWorktreeError
		errMsg := ""
		switch {
		case errors.As(err, &ce):
			errMsg = "The branches conflict: " + strings.Join(ce.Files, ", ")
		case errors.As(err, &de):
			errMsg = "The base branch's worktree is dirty — commit or stash it first."
		case errors.Is(err, gitrepo.ErrConcurrentUpdate):
			errMsg = "The base branch moved while merging — try again."
		default:
			h.internalError(w, r, err)
			return
		}
		check, _ := h.repo.CheckMerge(ctx, pr.BaseRef, pr.HeadRef)
		h.r.renderPullMerge(w, http.StatusConflict, pullMergePage{
			Title:     fmt.Sprintf("Merge pull request #%d", pr.ID),
			Section:   "pulls",
			PR:        pr,
			Check:     check,
			Error:     errMsg,
			Workspace: h.navWorkspace(r),
			User:      userFromContext(r.Context()),
		})
		return
	}
	if err := h.store.MarkMerged(ctx, pr.ID, store.PRMergeInfo{
		MergeSHA:     res.MergeSHA,
		MergeMethod:  string(res.Method),
		HeadSHA:      res.HeadSHA,
		MergeBaseSHA: res.MergeBaseSHA,
	}); err != nil {
		h.pullError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/pulls/%d", pr.ID), http.StatusSeeOther)
}

// pullEvents streams changes for one pull request as text/event-stream.
func (h *handler) pullEvents(w http.ResponseWriter, r *http.Request) {
	pr, ok := h.requirePull(w, r)
	if !ok {
		return
	}
	h.streamEvents(w, r, events.PullRequestTopic(pr.ID))
}

// pullCommentsFragment serves GET /pulls/{id}/fragments/comments — ?ids=
// picks a subset, no parameter returns the whole timeline (resync).
func (h *handler) pullCommentsFragment(w http.ResponseWriter, r *http.Request) {
	pr, ok := h.requirePull(w, r)
	if !ok {
		return
	}
	ids, filtered, err := parseIDSet(r)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "Invalid ids parameter.")
		return
	}
	comments, err := h.store.ListPRComments(r.Context(), pr.ID)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	out := comments
	if filtered {
		out = comments[:0]
		for _, c := range comments {
			if ids[c.ID] {
				out = append(out, c)
			}
		}
	}
	h.r.renderFragment(w, "pull_detail", "pr_comments_update", http.StatusOK,
		dict("PR", pr, "Comments", out))
}

// pullArticleFragment serves GET /pulls/{id}/fragments/article — the whole
// PR card for status/edit live swaps.
func (h *handler) pullArticleFragment(w http.ResponseWriter, r *http.Request) {
	pr, ok := h.requirePull(w, r)
	if !ok {
		return
	}
	commits, check := h.loadPullView(r, pr)
	h.r.renderFragment(w, "pull_detail", "pr_article", http.StatusOK, pullDetailPage{
		PR:      pr,
		Commits: commits,
		Merge:   check,
		User:    userFromContext(r.Context()),
	})
}
