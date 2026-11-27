package web

// UI localization (docs/sketch/ja/i18n.md): two locales, "en" default and
// "ja", resolved per request into the context and bound into templates as
// one function per message field — {{AppName}}, {{IssuesTitle}},
// {{IssuesCount .RangeStart .RangeEnd .Total}} — at parse time.
//
// The catalog is a Go struct, not JSON: field names are compile-time
// identifiers, so a typo'd field cannot exist, and a typo'd template call
// is a parse error ("function not defined"). JSON keys would be silent on
// both sides.

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"net/url"
	"path"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// supportedLangs lists every locale with a messages value below. The first
// entry is the fallback: what unrecognized requests get, and what
// executeLang falls back to.
var supportedLangs = []string{"en", "ja"}

// langCookieName persists the explicit choice made via ?lang=<code>.
const langCookieName = "lang"

// messages is the whole UI-string catalog for one locale: one field per
// user-facing string. printf-format strings keep their verbs and take the
// args noted in the comment (templates pass positionals — a locale whose
// phrasing needs a different order uses %[n]d).
//
// Fields whose name ends in HTML render markup: the catalog value may
// contain trusted tags (the <strong> a confirm line wraps a title in) and
// every format arg is HTML-escaped before substitution. Keep user data
// out of the message text itself.
type messages struct {
	AppName string

	LayoutSkip             string
	LayoutNavMain          string
	LayoutNavIssues        string
	LayoutNavDiscussions   string
	LayoutNavLabels        string
	LayoutNavPulls         string
	LayoutNavWorkspaces    string
	LayoutNavNewIssue      string
	LayoutNavNewDiscussion string
	LayoutNavLogout        string
	LayoutNavLogin         string
	LayoutNavRegister      string
	LayoutFooter           string

	// Shared furniture across pages.
	BreadcrumbAria     string
	BackToTop          string
	BackToTopAria      string
	FormNoteHTML       string // "Fields marked * are required." — trusted markup
	BtnCancel          string
	BtnSave            string
	BtnEdit            string
	BtnDelete          string
	BtnComment         string
	BtnSaveChanges     string
	TabsStatusAria     string
	TabOpen            string
	TabClosed          string
	TabMerged          string
	TabAll             string
	StatusOpen         string
	StatusClosed       string
	StatusMerged       string
	FilterSearch       string
	FilterSearchPh     string
	FilterSort         string
	SortNewest         string
	SortOldest         string
	FilterApply        string
	FilterClear        string
	PagerAria          string
	PagerPrev          string
	PagerNext          string
	PagerInfo          string // args: page, total pages
	OpenedAt           string // args: formatted time
	EmptyTryPre        string
	EmptyClearLink     string
	EmptyTryTail       string // en ".", ja "してください。" — closes the clear-filters sentence
	Commented          string // args: author name
	ByAuthor           string // args: author name ("by alice")
	Edited             string
	Anonymous          string
	MetaOpenedBy       string // args: author name
	MetaLastUpdated    string
	CopyLink           string
	AriaCopyLink       string // args: noun, id
	PermalinkPost      string // args: noun, id — "(comment 3)" for a11y
	CopyMarkdown       string
	NoDescription      string
	MarkedAs           string // args: noun, localized reason
	ShowNoun           string // args: noun
	ActUnhide          string
	ActHide            string
	AriaEditNoun       string // args: noun, id
	AriaUnhideNoun     string // args: noun, id
	AriaHideNoun       string // args: noun, id
	AriaDeleteNoun     string // args: noun, id
	HideReasonLabel    string
	HideSelect         string
	HideNounSubmit     string // args: noun — "Hide comment"
	AnnounceRestored   string // args: titlecased noun — "Thread restored."
	AnnounceHidden     string // args: titlecased noun — "Comment hidden."
	AnnounceComment    string
	AnnounceThread     string
	AnnounceAnswer     string
	CommentsHeading    string
	CommentsEmpty      string
	ThreadsEmpty       string
	CommentFormLabel   string // "Add a comment"
	ReplySummary       string // the collapsed "Add a comment" per thread
	AriaReplyOnThread  string // args: thread id
	AriaCancelReply    string // args: thread id
	AriaPostReply      string // args: thread id
	StartThreadLabel   string
	BtnStartThread     string
	FieldTitle         string
	FieldBody          string
	FieldComment       string
	LabelsLegend       string
	BodyRequired       string
	TitleRequired      string
	CategoryRequired   string
	NameRequired       string
	ColorRequired      string
	SlugRequired       string
	PasswordRequired   string
	MarkdownHint       string // Raw HTML + Ctrl+Enter note
	MarkdownHintBreaks string // MarkdownHint + line breaks preserved
	PostingAs          string // args: handle — "Posting as @alice."
	PostingAnonPre     string // "Posting as anonymous — "
	LoginLink          string
	PostingAnonTail    string // " to attach your name."
	DeleteConfirmHTML  string // args: title — "Are you sure you want to delete <strong>X</strong>?"
	DeleteNounConfirm  string // args: noun — "...delete this comment?"
	DeleteNounWarn     string // args: noun, replies-clause — "This permanently removes the X and its 3 replies."
	BtnDeleteNoun      string // args: noun — "Delete comment"
	EditComment        string // args: comment id
	DeleteComment      string
	EditThread         string // args: thread id
	DeleteThread       string

	// JavaScript layer (served to /static/i18n.js via <html data-i18n>).
	JSToastNotifications string
	JSToastClose         string
	JSPosting            string
	JSUpdated            string
	JSCommentPosted      string
	JSCommentPostFailed  string
	JSCopied             string
	JSCopyFailed         string
	JSBackTo             string // args: list name — "Back to issues"
	JSNounDeleted        string // args: noun — "This issue was deleted."
	JSEntityUpdated      string // args: noun — "Issue updated."
	JSAnswerUpdated      string
	JSNewThread          string
	JSNewComment         string
	JSNewReply           string
	JSNounIssue          string
	JSNounDiscussion     string
	JSNounPR             string
	JSListIssues         string
	JSListDiscussions    string
	JSListPRs            string

	// Issues.
	IssuesTitle            string
	IssuesFilterAria       string
	IssuesFilterLabel      string
	IssuesFilterAllLabels  string
	IssuesCount            string // args: range start, range end, total
	IssuesEmptyFiltered    string
	IssuesEmptyNone        string
	IssuesEmptyCreate      string
	NewIssue               string
	CreateIssue            string
	EditIssue              string // args: id — "Edit issue #2"
	DeleteIssue            string // args: id — "Delete issue #2"
	IssueClose             string
	IssueReopen            string
	IssueDeleteWarn        string
	DialogDeleteIssueT     string // args: id — "Delete issue #2?"
	DialogDeleteIssueDHTML string // args: title — trusted markup
	BtnDeleteIssue         string
	AnnounceIssueClosed    string
	AnnounceIssueReopened  string

	// Discussions.
	DiscussionsTitle            string
	DiscFilterAria              string
	DiscFilterCategory          string
	DiscFilterAll               string
	DiscFilterAnswered          string
	AnsweredAny                 string
	AnsweredYes                 string
	AnsweredNo                  string
	DiscussionsCount            string // args: range start, range end, total
	BadgeAnswered               string
	DiscEmptyFiltered           string
	DiscEmptyNone               string
	DiscEmptyCreate             string
	NewDiscussion               string
	CreateDiscussion            string
	EditDiscussion              string // args: id
	DeleteDiscussion            string // args: id
	DiscussionDeleteWarn        string
	DialogDeleteDiscussionT     string // args: id
	DialogDeleteDiscussionDHTML string // args: title
	BtnDeleteDiscussion         string
	SelectCategory              string
	BadgeAnswer                 string
	AnswerMark                  string
	AnswerUnmark                string
	AriaAnswerMark              string // args: thread id
	AriaAnswerUnmark            string // args: thread id

	// Workspaces.
	WorkspacesTitle      string
	NewWorkspace         string
	CreateWorkspace      string
	WsIssues             string // args: count — "3 issues"
	WsDiscussions        string // args: count
	WsEmpty              string
	FieldSlug            string
	SlugHint             string
	FieldName            string
	NameHint             string
	FieldDescription     string
	DeleteWorkspace      string // args: slug — "Delete workspace default"
	DeleteWorkspaceH     string // args: slug — heading with "?"
	DeleteWorkspaceDHTML string // args: slug, issues, discussions — trusted markup
	DeleteLastWorkspace  string
	BtnDeleteWorkspace   string

	// Labels.
	LabelsTitle           string
	LabelsNew             string
	FieldColor            string
	FieldKind             string
	FieldScope            string
	KindIssue             string
	KindDiscussion        string
	ScopeWorkspace        string
	ScopeShared           string
	BtnCreateLabel        string
	LabelsIssueHeading    string
	LabelsDiscHeading     string
	LabelsArchivedHeading string
	LabelsArchivedHint    string
	LabelsEmptyIssue      string
	LabelsEmptyDisc       string
	BtnRestore            string
	LabelNoun             string // "label" in "issue label · workspace"
	BadgeArchived         string
	EditLabel             string // args: name — "Edit label bug"

	// Pull requests.
	PullsTitle          string
	NewPull             string
	CreatePull          string
	PullsFilterAria     string
	PullsCount          string // args: range start, range end, total
	PullsEmptyFiltered  string
	PullsEmptyNone      string
	PullsEmptyCreate    string
	PullViewsAria       string
	PullTabConversation string
	PullTabFiles        string
	EditPull            string // args: id
	MergePull           string // args: id — page title "Merge pull request #2"
	MergePullHeading    string // bare "Merge pull request" / "プルリクエストのマージ"
	BtnMerge            string
	PullClose           string
	PullReopen          string
	PullCommits         string // args: count — "Commits (3)"
	MergeClean          string
	MergeConflicts      string // "This branch has conflicts in:"
	MergeUnchecked      string
	MergeUncheckedNone  string
	MergeBlocked        string
	BackToPull          string
	MergeMethodLegend   string
	MergeMethodMerge    string
	MergeMethodSquash   string
	MergeMethodRebase   string
	BtnMergePull        string
	FilesChanged        string
	FilesChangedTitle   string // args: PR title — "Files changed · X"
	FilesEmpty          string
	BinaryNote          string
	CommentLineTitle    string
	CommentLinePh       string // args: line no
	CommentLinePhLeft   string // args: line no
	RefsLabel           string
	BaseBranch          string
	HeadBranch          string
	BtnSavePR           string
	OnAnchor            string // args: "path:line" span — "on <code>x:L5</code>"
	SideLeft            string // "(left)"

	// Auth.
	AuthRegister        string
	AuthRegisterHeading string
	AuthLogin           string
	AuthName            string
	AuthPassword        string
	AuthPasswordHint    string
	AuthOtherLogin      string
	AuthOtherRegister   string
	AuthNameTooLong     string // args: limit
	AuthPasswordShort   string // args: limit
	AuthPasswordTooLong string // args: limit
	AuthNameTaken       string
	AuthLoginFailed     string

	// Error pages and form-level errors.
	ErrorBack          string
	ErrNotFoundH       string
	ErrNotFoundM       string
	ErrInternalH       string
	ErrInternalM       string
	ErrBadRequestH     string
	ErrUnprocessableH  string
	ErrFormTooLargeH   string
	ErrFormTooLargeM   string
	ErrFormReadM       string
	ErrTooLong         string // args: field label, limit — "Title must be 200 characters or fewer."
	ErrSlugPattern     string // args: pattern
	ErrColorHex        string
	ErrSlugTaken       string
	ErrLabelDup        string
	ErrLabelKind       string
	ErrBaseRefRequired string
	ErrHeadRefRequired string
	ErrRefsInvalid     string
	ErrAnchorInvalid   string
	ErrAnchorPath      string
	ErrHideReason      string
	ErrStatusValue     string
	ErrAnswerValue     string
	ErrAnswerQAH       string
	ErrAnswerQAM       string
	ErrInvalidIDs      string
	ErrInvalidThreadID string
	ErrReplyTarget     string
	ErrArchivedValue   string
	ErrPRTransitionH   string
	ErrPRTransitionM   string
	ErrDupPRH          string
	ErrDupPRM          string
	ErrSameRefH        string
	ErrSameRefM        string
	ErrCloseOnlyM      string
	ErrNotMergeableH   string
	ErrMergeOnlyM      string
	ErrUnknownStatus   string
	ErrUnknownMethod   string
	ErrMergeConflict   string // args: file list
	ErrDirtyWorktree   string
	ErrBaseMoved       string
	ErrThreadOnly      string
	ErrDiffCompute     string
	ErrDeleteLastWsH   string
	ErrDeleteLastWsM   string
	ErrSessionVerify   string
}

var messagesEN = messages{
	AppName: "Issue Tracker",

	LayoutSkip:             "Skip to main content",
	LayoutNavMain:          "Main",
	LayoutNavIssues:        "Issues",
	LayoutNavDiscussions:   "Discussions",
	LayoutNavLabels:        "Labels",
	LayoutNavPulls:         "Pull requests",
	LayoutNavWorkspaces:    "Workspaces",
	LayoutNavNewIssue:      "New issue",
	LayoutNavNewDiscussion: "New discussion",
	LayoutNavLogout:        "Log out",
	LayoutNavLogin:         "Log in",
	LayoutNavRegister:      "Register",
	LayoutFooter:           "A classic multi-page app built with Go's net/http and html/template. Declarative first; minimal JavaScript where it earns its place.",

	BreadcrumbAria:     "Breadcrumb",
	BackToTop:          "↑ Top",
	BackToTopAria:      "Back to top",
	FormNoteHTML:       `Fields marked <span class="req">*</span> are required.`,
	BtnCancel:          "Cancel",
	BtnSave:            "Save changes",
	BtnEdit:            "Edit",
	BtnDelete:          "Delete",
	BtnComment:         "Comment",
	BtnSaveChanges:     "Save changes",
	TabsStatusAria:     "Filter by status",
	TabOpen:            "Open",
	TabClosed:          "Closed",
	TabMerged:          "Merged",
	TabAll:             "All",
	StatusOpen:         "Open",
	StatusClosed:       "Closed",
	StatusMerged:       "Merged",
	FilterSearch:       "Search",
	FilterSearchPh:     "Title or body",
	FilterSort:         "Sort",
	SortNewest:         "Newest first",
	SortOldest:         "Oldest first",
	FilterApply:        "Apply",
	FilterClear:        "Clear",
	PagerAria:          "Pagination",
	PagerPrev:          "Previous",
	PagerNext:          "Next",
	PagerInfo:          "Page %d of %d",
	OpenedAt:           "opened %s",
	EmptyTryPre:        "Try a different keyword or",
	EmptyClearLink:     "clear the filters",
	EmptyTryTail:       ".",
	Commented:          "%s commented",
	ByAuthor:           "by %s",
	Edited:             "edited",
	Anonymous:          "anonymous",
	MetaOpenedBy:       "Opened by %s",
	MetaLastUpdated:    "· Last updated",
	CopyLink:           "copy link",
	AriaCopyLink:       "Copy link to %s %d",
	PermalinkPost:      "(%s %d)",
	CopyMarkdown:       "Copy as markdown",
	NoDescription:      "No description provided.",
	MarkedAs:           "This %s was marked as %s.",
	ShowNoun:           "Show %s",
	ActUnhide:          "Unhide",
	ActHide:            "Hide",
	AriaEditNoun:       "Edit %s %d",
	AriaUnhideNoun:     "Unhide %s %d",
	AriaHideNoun:       "Hide %s %d",
	AriaDeleteNoun:     "Delete %s %d",
	HideReasonLabel:    "Reason",
	HideSelect:         "Select…",
	HideNounSubmit:     "Hide %s",
	AnnounceRestored:   "%s restored.",
	AnnounceHidden:     "%s hidden.",
	AnnounceComment:    "Comment posted.",
	AnnounceThread:     "Thread posted.",
	AnnounceAnswer:     "Answer updated.",
	CommentsHeading:    "Comments",
	CommentsEmpty:      "No comments yet.",
	ThreadsEmpty:       "No threads yet.",
	CommentFormLabel:   "Add a comment",
	ReplySummary:       "Add a comment",
	AriaReplyOnThread:  "Add a comment on thread %d",
	AriaCancelReply:    "Cancel comment on thread %d",
	AriaPostReply:      "Post comment on thread %d",
	StartThreadLabel:   "Start a thread",
	BtnStartThread:     "Start thread",
	FieldTitle:         "Title",
	FieldBody:          "Body",
	FieldComment:       "Comment",
	LabelsLegend:       "Labels",
	BodyRequired:       "Body is required.",
	TitleRequired:      "Title is required.",
	CategoryRequired:   "Category is required.",
	NameRequired:       "Name is required.",
	ColorRequired:      "Color is required.",
	SlugRequired:       "Slug is required.",
	PasswordRequired:   "Password is required.",
	MarkdownHint:       "Markdown (GFM) supported. Raw HTML is not rendered. Ctrl+Enter (Cmd+Enter on macOS) submits.",
	MarkdownHintBreaks: "Markdown (GFM) supported. Raw HTML is not rendered; line breaks are preserved. Ctrl+Enter (Cmd+Enter on macOS) submits.",
	PostingAs:          "Posting as @%s.",
	PostingAnonPre:     "Posting as anonymous — ",
	LoginLink:          "log in",
	PostingAnonTail:    " to attach your name.",
	DeleteConfirmHTML:  `Are you sure you want to delete <strong>%s</strong>?`,
	DeleteNounConfirm:  "Are you sure you want to delete this %s?",
	DeleteNounWarn:     "This permanently removes the %s%s. This action cannot be undone.",
	BtnDeleteNoun:      "Delete %s",
	EditComment:        "Edit comment #%d",
	DeleteComment:      "Delete comment #%d",
	EditThread:         "Edit thread #%d",
	DeleteThread:       "Delete thread #%d",

	JSToastNotifications: "Notifications",
	JSToastClose:         "Close",
	JSPosting:            "Posting",
	JSUpdated:            "Updated.",
	JSCommentPosted:      "Comment posted.",
	JSCommentPostFailed:  "Comment could not be posted. Please try again.",
	JSCopied:             "Copied",
	JSCopyFailed:         "Copy failed",
	JSBackTo:             "Back to %s",
	JSNounDeleted:        "This %s was deleted.",
	JSEntityUpdated:      "%s updated.",
	JSAnswerUpdated:      "Answer updated.",
	JSNewThread:          "New thread.",
	JSNewComment:         "New comment.",
	JSNewReply:           "New reply.",
	JSNounIssue:          "issue",
	JSNounDiscussion:     "discussion",
	JSNounPR:             "pull request",
	JSListIssues:         "issues",
	JSListDiscussions:    "discussions",
	JSListPRs:            "pull requests",

	IssuesTitle:            "Issues",
	IssuesFilterAria:       "Filter issues",
	IssuesFilterLabel:      "Label",
	IssuesFilterAllLabels:  "All labels",
	IssuesCount:            "Showing %[1]d–%[2]d of %[3]d issues",
	IssuesEmptyFiltered:    "No issues match the current filters.",
	IssuesEmptyNone:        "No issues here yet.",
	IssuesEmptyCreate:      "Create the first issue",
	NewIssue:               "New issue",
	CreateIssue:            "Create issue",
	EditIssue:              "Edit issue #%d",
	DeleteIssue:            "Delete issue #%d",
	IssueClose:             "Close issue",
	IssueReopen:            "Reopen issue",
	IssueDeleteWarn:        "This permanently removes the issue and all of its comments. This action cannot be undone.",
	DialogDeleteIssueT:     "Delete issue #%d?",
	DialogDeleteIssueDHTML: `This permanently removes <strong>%s</strong> and all of its comments. This action cannot be undone.`,
	BtnDeleteIssue:         "Delete issue",
	AnnounceIssueClosed:    "Issue closed.",
	AnnounceIssueReopened:  "Issue reopened.",

	DiscussionsTitle:            "Discussions",
	DiscFilterAria:              "Filter discussions",
	DiscFilterCategory:          "Category",
	DiscFilterAll:               "All categories",
	DiscFilterAnswered:          "Answered",
	AnsweredAny:                 "Any",
	AnsweredYes:                 "Answered",
	AnsweredNo:                  "Unanswered",
	DiscussionsCount:            "Showing %[1]d–%[2]d of %[3]d discussions",
	BadgeAnswered:               "Answered",
	DiscEmptyFiltered:           "No discussions match the current filters.",
	DiscEmptyNone:               "No discussions here yet.",
	DiscEmptyCreate:             "Start the first discussion",
	NewDiscussion:               "New discussion",
	CreateDiscussion:            "Create discussion",
	EditDiscussion:              "Edit discussion #%d",
	DeleteDiscussion:            "Delete discussion #%d",
	DiscussionDeleteWarn:        "This permanently removes the discussion and all of its comments. This action cannot be undone.",
	DialogDeleteDiscussionT:     "Delete discussion #%d?",
	DialogDeleteDiscussionDHTML: `This permanently removes <strong>%s</strong> and all of its comments. This action cannot be undone.`,
	BtnDeleteDiscussion:         "Delete discussion",
	SelectCategory:              "Select a category",
	BadgeAnswer:                 "Answer",
	AnswerMark:                  "Mark as answer",
	AnswerUnmark:                "Unmark answer",
	AriaAnswerMark:              "Mark as answer on thread %d",
	AriaAnswerUnmark:            "Unmark answer on thread %d",

	WorkspacesTitle:      "Workspaces",
	NewWorkspace:         "New workspace",
	CreateWorkspace:      "Create workspace",
	WsIssues:             "%d issues",
	WsDiscussions:        "%d discussions",
	WsEmpty:              "No workspaces yet.",
	FieldSlug:            "Slug",
	SlugHint:             `Lowercase letters, digits, "-" and "_". Used in URLs: /w/<slug>/issues.`,
	FieldName:            "Name",
	NameHint:             "Optional display name; defaults to the slug.",
	FieldDescription:     "Description",
	DeleteWorkspace:      "Delete workspace %s",
	DeleteWorkspaceH:     "Delete workspace %s?",
	DeleteWorkspaceDHTML: `This deletes the workspace <strong>w/%s</strong> and everything in it — %d issues and %d discussions. This cannot be undone.`,
	DeleteLastWorkspace:  "The last remaining workspace cannot be deleted.",
	BtnDeleteWorkspace:   "Delete workspace",

	LabelsTitle:           "Labels",
	LabelsNew:             "New label",
	FieldColor:            "Color",
	FieldKind:             "Kind",
	FieldScope:            "Scope",
	KindIssue:             "Issue",
	KindDiscussion:        "Discussion",
	ScopeWorkspace:        "This workspace",
	ScopeShared:           "Shared (all workspaces)",
	BtnCreateLabel:        "Create label",
	LabelsIssueHeading:    "Issue labels",
	LabelsDiscHeading:     "Discussion labels",
	LabelsArchivedHeading: "Archived",
	LabelsArchivedHint:    "Archived labels stay attached to the issues and discussions that already carry them. Restoring a label makes it selectable again.",
	LabelsEmptyIssue:      "No issue labels yet.",
	LabelsEmptyDisc:       "No discussion labels yet.",
	BtnRestore:            "Restore",
	LabelNoun:             "label",
	BadgeArchived:         "archived",
	EditLabel:             "Edit label %s",

	PullsTitle:          "Pull requests",
	NewPull:             "New pull request",
	CreatePull:          "Create pull request",
	PullsFilterAria:     "Filter pull requests",
	PullsCount:          "Showing %[1]d–%[2]d of %[3]d pull requests",
	PullsEmptyFiltered:  "No pull requests match the current filters.",
	PullsEmptyNone:      "No pull requests here yet.",
	PullsEmptyCreate:    "Create the first pull request",
	PullViewsAria:       "Pull request views",
	PullTabConversation: "Conversation",
	PullTabFiles:        "Files changed",
	EditPull:            "Edit pull request #%d",
	MergePull:           "Merge pull request #%d",
	MergePullHeading:    "Merge pull request",
	BtnMerge:            "Merge",
	PullClose:           "Close pull request",
	PullReopen:          "Reopen pull request",
	PullCommits:         "Commits (%d)",
	MergeClean:          "This branch can be merged cleanly.",
	MergeConflicts:      "This branch has conflicts in:",
	MergeUnchecked:      "Mergeability could not be checked (shallow clone or old git). Merging may still fail.",
	MergeUncheckedNone:  "Mergeability could not be checked — merging may fail.",
	MergeBlocked:        "Merging is blocked until the conflicts are resolved in the branches.",
	BackToPull:          "Back to pull request",
	MergeMethodLegend:   "Merge method",
	MergeMethodMerge:    "Merge commit — create a merge commit joining the histories.",
	MergeMethodSquash:   "Squash — combine every commit into one commit on base.",
	MergeMethodRebase:   "Rebase — replay the commits on top of base (linear history).",
	BtnMergePull:        "Merge pull request",
	FilesChanged:        "Files changed",
	FilesChangedTitle:   "Files changed · %s",
	FilesEmpty:          "No changed files.",
	BinaryNote:          "Binary file — diff not shown.",
	CommentLineTitle:    "Comment on this line",
	CommentLinePh:       "Comment on line %d",
	CommentLinePhLeft:   "Comment on line %d (left)",
	RefsLabel:           "Branches",
	BaseBranch:          "Base branch",
	HeadBranch:          "Compare branch",
	BtnSavePR:           "Save",
	OnAnchor:            "on %s",
	SideLeft:            "(left)",

	AuthRegister:        "Register",
	AuthRegisterHeading: "Create an account",
	AuthLogin:           "Log in",
	AuthName:            "Name",
	AuthPassword:        "Password",
	AuthPasswordHint:    "At least 8 characters.",
	AuthOtherLogin:      "Already have an account? Log in",
	AuthOtherRegister:   "No account yet? Register",
	AuthNameTooLong:     "Name must be %d characters or fewer.",
	AuthPasswordShort:   "Password must be %d characters or more.",
	AuthPasswordTooLong: "Password must be %d characters or fewer.",
	AuthNameTaken:       "That name is already taken.",
	AuthLoginFailed:     "Name or password is incorrect.",

	ErrorBack:          "Back to issues",
	ErrNotFoundH:       "Not found",
	ErrNotFoundM:       "The page or issue you are looking for does not exist or may have been deleted.",
	ErrInternalH:       "Internal server error",
	ErrInternalM:       "Something went wrong on our side. Please try again.",
	ErrBadRequestH:     "Bad request",
	ErrUnprocessableH:  "Unprocessable entity",
	ErrFormTooLargeH:   "Form too large",
	ErrFormTooLargeM:   "The submitted form exceeded the size limit.",
	ErrFormReadM:       "The submitted form could not be read.",
	ErrTooLong:         "%s must be %d characters or fewer.",
	ErrSlugPattern:     "Slug must match %s.",
	ErrColorHex:        "Color must be a hex color like #d73a4a.",
	ErrSlugTaken:       "This slug is already taken.",
	ErrLabelDup:        "A label with this name already exists in that scope.",
	ErrLabelKind:       "Kind must be issue or discussion.",
	ErrBaseRefRequired: "Base ref is required.",
	ErrHeadRefRequired: "Head ref is required.",
	ErrRefsInvalid:     "Base and head must name existing refs or commits.",
	ErrAnchorInvalid:   "Invalid review-comment anchor.",
	ErrAnchorPath:      "The anchor path is not part of the pull request diff.",
	ErrHideReason:      "A valid hide reason is required.",
	ErrStatusValue:     "Invalid status value.",
	ErrAnswerValue:     "Invalid answer value.",
	ErrAnswerQAH:       "Cannot mark as answer",
	ErrAnswerQAM:       "Only threads on a Q&A discussion can be marked as the answer.",
	ErrInvalidIDs:      "Invalid ids parameter.",
	ErrInvalidThreadID: "Invalid thread parameter.",
	ErrReplyTarget:     "Invalid reply target.",
	ErrArchivedValue:   "Invalid archived value.",
	ErrPRTransitionH:   "Invalid status change",
	ErrPRTransitionM:   "The pull request is already in that state.",
	ErrDupPRH:          "Duplicate pull request",
	ErrDupPRM:          "An open pull request for the same base and head already exists.",
	ErrSameRefH:        "Same ref",
	ErrSameRefM:        "Base and head must be different refs.",
	ErrCloseOnlyM:      "Only an open pull request can be closed.",
	ErrNotMergeableH:   "Not mergeable",
	ErrMergeOnlyM:      "Only an open pull request can be merged.",
	ErrUnknownStatus:   "Unknown status action.",
	ErrUnknownMethod:   "Unknown merge method.",
	ErrMergeConflict:   "The branches conflict: %s",
	ErrDirtyWorktree:   "The base branch's worktree is dirty — commit or stash it first.",
	ErrBaseMoved:       "The base branch moved while merging — try again.",
	ErrThreadOnly:      "Comments can only be added to a thread.",
	ErrDiffCompute:     "The diff could not be computed — the refs may have moved or been deleted.",
	ErrDeleteLastWsH:   "Cannot delete workspace",
	ErrDeleteLastWsM:   "The last remaining workspace cannot be deleted.",
	ErrSessionVerify:   "The session could not be verified. Please try again.",
}

var messagesJA = messages{
	AppName: "Issue Tracker",

	LayoutSkip:             "メインコンテンツへスキップ",
	LayoutNavMain:          "メイン",
	LayoutNavIssues:        "イシュー",
	LayoutNavDiscussions:   "ディスカッション",
	LayoutNavLabels:        "ラベル",
	LayoutNavPulls:         "プルリクエスト",
	LayoutNavWorkspaces:    "ワークスペース",
	LayoutNavNewIssue:      "新規イシュー",
	LayoutNavNewDiscussion: "新規ディスカッション",
	LayoutNavLogout:        "ログアウト",
	LayoutNavLogin:         "ログイン",
	LayoutNavRegister:      "登録",
	LayoutFooter:           "Go の net/http と html/template で作ったクラシックなマルチページアプリ。宣言的ファースト、必要なところにだけ最小限の JavaScript。",

	BreadcrumbAria:     "パンくずリスト",
	BackToTop:          "↑ トップ",
	BackToTopAria:      "トップへ戻る",
	FormNoteHTML:       `<span class="req">*</span> が付いたフィールドは必須です。`,
	BtnCancel:          "キャンセル",
	BtnSave:            "変更を保存",
	BtnEdit:            "編集",
	BtnDelete:          "削除",
	BtnComment:         "コメント",
	BtnSaveChanges:     "変更を保存",
	TabsStatusAria:     "ステータスで絞り込み",
	TabOpen:            "オープン",
	TabClosed:          "クローズ",
	TabMerged:          "マージ済み",
	TabAll:             "すべて",
	StatusOpen:         "オープン",
	StatusClosed:       "クローズ",
	StatusMerged:       "マージ済み",
	FilterSearch:       "検索",
	FilterSearchPh:     "タイトルまたは本文",
	FilterSort:         "並び替え",
	SortNewest:         "新しい順",
	SortOldest:         "古い順",
	FilterApply:        "適用",
	FilterClear:        "クリア",
	PagerAria:          "ページネーション",
	PagerPrev:          "前へ",
	PagerNext:          "次へ",
	PagerInfo:          "%d / %d ページ",
	OpenedAt:           "%s にオープン",
	EmptyTryPre:        "別のキーワードを試すか、",
	EmptyClearLink:     "フィルタをクリア",
	EmptyTryTail:       "してください。",
	Commented:          "%s がコメント",
	ByAuthor:           "%s",
	Edited:             "編集済み",
	Anonymous:          "匿名",
	MetaOpenedBy:       "%s がオープン",
	MetaLastUpdated:    "· 最終更新",
	CopyLink:           "リンクをコピー",
	AriaCopyLink:       "%s %d へのリンクをコピー",
	PermalinkPost:      "（%s %d）",
	CopyMarkdown:       "Markdown としてコピー",
	NoDescription:      "説明はありません。",
	MarkedAs:           "この%sは「%s」としてマークされました。",
	ShowNoun:           "%sを表示",
	ActUnhide:          "再表示",
	ActHide:            "非表示",
	AriaEditNoun:       "%s %d を編集",
	AriaUnhideNoun:     "%s %d を再表示",
	AriaHideNoun:       "%s %d を非表示",
	AriaDeleteNoun:     "%s %d を削除",
	HideReasonLabel:    "理由",
	HideSelect:         "選択…",
	HideNounSubmit:     "%sを非表示",
	AnnounceRestored:   "%sを復元しました。",
	AnnounceHidden:     "%sを非表示にしました。",
	AnnounceComment:    "コメントを投稿しました。",
	AnnounceThread:     "スレッドを投稿しました。",
	AnnounceAnswer:     "回答を更新しました。",
	CommentsHeading:    "コメント",
	CommentsEmpty:      "まだコメントはありません。",
	ThreadsEmpty:       "まだスレッドはありません。",
	CommentFormLabel:   "コメントを追加",
	ReplySummary:       "コメントを追加",
	AriaReplyOnThread:  "スレッド %d にコメントを追加",
	AriaCancelReply:    "スレッド %d へのコメントをキャンセル",
	AriaPostReply:      "スレッド %d にコメントを投稿",
	StartThreadLabel:   "スレッドを開始",
	BtnStartThread:     "スレッドを開始",
	FieldTitle:         "タイトル",
	FieldBody:          "本文",
	FieldComment:       "コメント",
	LabelsLegend:       "ラベル",
	BodyRequired:       "本文は必須です。",
	TitleRequired:      "タイトルは必須です。",
	CategoryRequired:   "カテゴリは必須です。",
	NameRequired:       "名前は必須です。",
	ColorRequired:      "色は必須です。",
	SlugRequired:       "スラッグは必須です。",
	PasswordRequired:   "パスワードは必須です。",
	MarkdownHint:       "Markdown (GFM) が使えます。生の HTML は描画されません。Ctrl+Enter（macOS では Cmd+Enter）で送信します。",
	MarkdownHintBreaks: "Markdown (GFM) が使えます。生の HTML は描画されず、改行は保持されます。Ctrl+Enter（macOS では Cmd+Enter）で送信します。",
	PostingAs:          "@%s として投稿。",
	PostingAnonPre:     "匿名として投稿 — ",
	LoginLink:          "ログイン",
	PostingAnonTail:    "すると名前が付きます。",
	DeleteConfirmHTML:  `<strong>%s</strong> を削除してもよろしいですか？`,
	DeleteNounConfirm:  "この%sを削除してもよろしいですか？",
	DeleteNounWarn:     "この%s%sを完全に削除します。この操作は取り消せません。",
	BtnDeleteNoun:      "%sを削除",
	EditComment:        "コメント #%d を編集",
	DeleteComment:      "コメント #%d を削除",
	EditThread:         "スレッド #%d を編集",
	DeleteThread:       "スレッド #%d を削除",

	JSToastNotifications: "通知",
	JSToastClose:         "閉じる",
	JSPosting:            "投稿中",
	JSUpdated:            "更新しました。",
	JSCommentPosted:      "コメントを投稿しました。",
	JSCommentPostFailed:  "コメントを投稿できませんでした。もう一度お試しください。",
	JSCopied:             "コピーしました",
	JSCopyFailed:         "コピーに失敗しました",
	JSBackTo:             "%s に戻る",
	JSNounDeleted:        "この%sは削除されました。",
	JSEntityUpdated:      "%s が更新されました。",
	JSAnswerUpdated:      "回答が更新されました。",
	JSNewThread:          "新しいスレッド",
	JSNewComment:         "新しいコメント",
	JSNewReply:           "新しい返信",
	JSNounIssue:          "イシュー",
	JSNounDiscussion:     "ディスカッション",
	JSNounPR:             "プルリクエスト",
	JSListIssues:         "イシュー",
	JSListDiscussions:    "ディスカッション",
	JSListPRs:            "プルリクエスト",

	IssuesTitle:            "イシュー",
	IssuesFilterAria:       "イシューを絞り込み",
	IssuesFilterLabel:      "ラベル",
	IssuesFilterAllLabels:  "すべてのラベル",
	IssuesCount:            "%[3]d 件中 %[1]d–%[2]d 件を表示",
	IssuesEmptyFiltered:    "現在のフィルタに一致するイシューはありません。",
	IssuesEmptyNone:        "まだイシューはありません。",
	IssuesEmptyCreate:      "最初のイシューを作成",
	NewIssue:               "新規イシュー",
	CreateIssue:            "イシューを作成",
	EditIssue:              "イシュー #%d を編集",
	DeleteIssue:            "イシュー #%d を削除",
	IssueClose:             "イシューをクローズ",
	IssueReopen:            "イシューを再オープン",
	IssueDeleteWarn:        "イシューとそのすべてのコメントを完全に削除します。この操作は取り消せません。",
	DialogDeleteIssueT:     "イシュー #%d を削除しますか？",
	DialogDeleteIssueDHTML: `<strong>%s</strong> とそのすべてのコメントを完全に削除します。この操作は取り消せません。`,
	BtnDeleteIssue:         "イシューを削除",
	AnnounceIssueClosed:    "イシューをクローズしました。",
	AnnounceIssueReopened:  "イシューを再オープンしました。",

	DiscussionsTitle:            "ディスカッション",
	DiscFilterAria:              "ディスカッションを絞り込み",
	DiscFilterCategory:          "カテゴリ",
	DiscFilterAll:               "すべてのカテゴリ",
	DiscFilterAnswered:          "回答状況",
	AnsweredAny:                 "すべて",
	AnsweredYes:                 "回答あり",
	AnsweredNo:                  "未回答",
	DiscussionsCount:            "%[3]d 件中 %[1]d–%[2]d 件を表示",
	BadgeAnswered:               "回答あり",
	DiscEmptyFiltered:           "現在のフィルタに一致するディスカッションはありません。",
	DiscEmptyNone:               "まだディスカッションはありません。",
	DiscEmptyCreate:             "最初のディスカッションを開始",
	NewDiscussion:               "新規ディスカッション",
	CreateDiscussion:            "ディスカッションを作成",
	EditDiscussion:              "ディスカッション #%d を編集",
	DeleteDiscussion:            "ディスカッション #%d を削除",
	DiscussionDeleteWarn:        "ディスカッションとそのすべてのコメントを完全に削除します。この操作は取り消せません。",
	DialogDeleteDiscussionT:     "ディスカッション #%d を削除しますか？",
	DialogDeleteDiscussionDHTML: `<strong>%s</strong> とそのすべてのコメントを完全に削除します。この操作は取り消せません。`,
	BtnDeleteDiscussion:         "ディスカッションを削除",
	SelectCategory:              "カテゴリを選択",
	BadgeAnswer:                 "回答",
	AnswerMark:                  "回答に指定",
	AnswerUnmark:                "回答を解除",
	AriaAnswerMark:              "スレッド %d を回答に指定",
	AriaAnswerUnmark:            "スレッド %d の回答を解除",

	WorkspacesTitle:      "ワークスペース",
	NewWorkspace:         "新規ワークスペース",
	CreateWorkspace:      "ワークスペースを作成",
	WsIssues:             "イシュー %d 件",
	WsDiscussions:        "ディスカッション %d 件",
	WsEmpty:              "まだワークスペースはありません。",
	FieldSlug:            "スラッグ",
	SlugHint:             `小文字英数字と "-"・"_" のみ。URL に使われます: /w/<slug>/issues。`,
	FieldName:            "名前",
	NameHint:             "任意の表示名。省略時はスラッグが使われます。",
	FieldDescription:     "説明",
	DeleteWorkspace:      "ワークスペース %s を削除",
	DeleteWorkspaceH:     "ワークスペース %s を削除しますか？",
	DeleteWorkspaceDHTML: `ワークスペース <strong>w/%s</strong> とその中身（イシュー %d 件・ディスカッション %d 件）をすべて削除します。この操作は取り消せません。`,
	DeleteLastWorkspace:  "最後に残ったワークスペースは削除できません。",
	BtnDeleteWorkspace:   "ワークスペースを削除",

	LabelsTitle:           "ラベル",
	LabelsNew:             "新規ラベル",
	FieldColor:            "色",
	FieldKind:             "種別",
	FieldScope:            "スコープ",
	KindIssue:             "イシュー",
	KindDiscussion:        "ディスカッション",
	ScopeWorkspace:        "このワークスペース",
	ScopeShared:           "共有（全ワークスペース）",
	BtnCreateLabel:        "ラベルを作成",
	LabelsIssueHeading:    "イシューラベル",
	LabelsDiscHeading:     "ディスカッションラベル",
	LabelsArchivedHeading: "アーカイブ済み",
	LabelsArchivedHint:    "アーカイブ済みラベルは、すでに付いているイシューやディスカッションには残ります。復元すると再び選択できるようになります。",
	LabelsEmptyIssue:      "まだイシューラベルはありません。",
	LabelsEmptyDisc:       "まだディスカッションラベルはありません。",
	BtnRestore:            "復元",
	LabelNoun:             "ラベル",
	BadgeArchived:         "アーカイブ済み",
	EditLabel:             "ラベル %s を編集",

	PullsTitle:          "プルリクエスト",
	NewPull:             "新規プルリクエスト",
	CreatePull:          "プルリクエストを作成",
	PullsFilterAria:     "プルリクエストを絞り込み",
	PullsCount:          "%[3]d 件中 %[1]d–%[2]d 件を表示",
	PullsEmptyFiltered:  "現在のフィルタに一致するプルリクエストはありません。",
	PullsEmptyNone:      "まだプルリクエストはありません。",
	PullsEmptyCreate:    "最初のプルリクエストを作成",
	PullViewsAria:       "プルリクエストのビュー",
	PullTabConversation: "会話",
	PullTabFiles:        "変更ファイル",
	EditPull:            "プルリクエスト #%d を編集",
	MergePull:           "プルリクエスト #%d をマージ",
	MergePullHeading:    "プルリクエストのマージ",
	BtnMerge:            "マージ",
	PullClose:           "プルリクエストをクローズ",
	PullReopen:          "プルリクエストを再オープン",
	PullCommits:         "コミット (%d 件)",
	MergeClean:          "このブランチはコンフリクトなくマージできます。",
	MergeConflicts:      "このブランチにはコンフリクトがあります:",
	MergeUnchecked:      "マージ可否を確認できませんでした（シャロークローンまたは古い git）。マージは失敗する可能性があります。",
	MergeUncheckedNone:  "マージ可否を確認できませんでした — マージは失敗する可能性があります。",
	MergeBlocked:        "ブランチ上でコンフリクトが解消されるまでマージはブロックされます。",
	BackToPull:          "プルリクエストへ戻る",
	MergeMethodLegend:   "マージ方法",
	MergeMethodMerge:    "マージコミット — 2つの履歴を結合するマージコミットを作成します。",
	MergeMethodSquash:   "スカッシュ — すべてのコミットを base 上の1つのコミットにまとめます。",
	MergeMethodRebase:   "リベース — コミットを base の先頭に付け替えます（線形履歴）。",
	BtnMergePull:        "プルリクエストをマージ",
	FilesChanged:        "変更ファイル",
	FilesChangedTitle:   "変更ファイル · %s",
	FilesEmpty:          "変更ファイルはありません。",
	BinaryNote:          "バイナリファイル — diff は表示されません。",
	CommentLineTitle:    "この行にコメント",
	CommentLinePh:       "行 %d へのコメント",
	CommentLinePhLeft:   "行 %d へのコメント（左）",
	RefsLabel:           "ブランチ",
	BaseBranch:          "ベースブランチ",
	HeadBranch:          "比較ブランチ",
	BtnSavePR:           "保存",
	OnAnchor:            "%s",
	SideLeft:            "（左）",

	AuthRegister:        "登録",
	AuthRegisterHeading: "アカウントを作成",
	AuthLogin:           "ログイン",
	AuthName:            "名前",
	AuthPassword:        "パスワード",
	AuthPasswordHint:    "8 文字以上。",
	AuthOtherLogin:      "すでにアカウントをお持ちですか？ ログイン",
	AuthOtherRegister:   "アカウントをお持ちでないですか？ 登録",
	AuthNameTooLong:     "名前は %d 文字以内にしてください。",
	AuthPasswordShort:   "パスワードは %d 文字以上にしてください。",
	AuthPasswordTooLong: "パスワードは %d 文字以内にしてください。",
	AuthNameTaken:       "その名前はすでに使われています。",
	AuthLoginFailed:     "名前またはパスワードが正しくありません。",

	ErrorBack:          "イシュー一覧へ戻る",
	ErrNotFoundH:       "見つかりません",
	ErrNotFoundM:       "お探しのページまたはイシューは存在しないか、削除された可能性があります。",
	ErrInternalH:       "サーバーエラー",
	ErrInternalM:       "サーバー側で問題が発生しました。もう一度お試しください。",
	ErrBadRequestH:     "不正なリクエスト",
	ErrUnprocessableH:  "処理できないエンティティ",
	ErrFormTooLargeH:   "フォームが大きすぎます",
	ErrFormTooLargeM:   "送信されたフォームがサイズ上限を超えています。",
	ErrFormReadM:       "送信されたフォームを読み取れませんでした。",
	ErrTooLong:         "%s は %d 文字以内にしてください。",
	ErrSlugPattern:     "スラッグは %s に一致する必要があります。",
	ErrColorHex:        "色は #d73a4a のような16進カラーコードにしてください。",
	ErrSlugTaken:       "このスラッグはすでに使われています。",
	ErrLabelDup:        "そのスコープには同名のラベルがすでに存在します。",
	ErrLabelKind:       "種別は issue または discussion にしてください。",
	ErrBaseRefRequired: "ベース ref は必須です。",
	ErrHeadRefRequired: "比較 ref は必須です。",
	ErrRefsInvalid:     "ベースと比較は既存の ref またはコミットを指す必要があります。",
	ErrAnchorInvalid:   "レビューコメントのアンカーが不正です。",
	ErrAnchorPath:      "アンカーのパスはプルリクエストの diff に含まれていません。",
	ErrHideReason:      "有効な非表示理由が必要です。",
	ErrStatusValue:     "ステータス値が不正です。",
	ErrAnswerValue:     "回答値が不正です。",
	ErrAnswerQAH:       "回答に指定できません",
	ErrAnswerQAM:       "Q&A ディスカッションのスレッドのみ回答に指定できます。",
	ErrInvalidIDs:      "ids パラメータが不正です。",
	ErrInvalidThreadID: "thread パラメータが不正です。",
	ErrReplyTarget:     "返信先が不正です。",
	ErrArchivedValue:   "archived 値が不正です。",
	ErrPRTransitionH:   "不正なステータス変更",
	ErrPRTransitionM:   "このプルリクエストはすでにその状態です。",
	ErrDupPRH:          "重複するプルリクエスト",
	ErrDupPRM:          "同じベースと比較の組み合わせのオープンなプルリクエストがすでに存在します。",
	ErrSameRefH:        "同一 ref",
	ErrSameRefM:        "ベースと比較は異なる ref にしてください。",
	ErrCloseOnlyM:      "オープンなプルリクエストのみクローズできます。",
	ErrNotMergeableH:   "マージできません",
	ErrMergeOnlyM:      "オープンなプルリクエストのみマージできます。",
	ErrUnknownStatus:   "不明なステータス操作です。",
	ErrUnknownMethod:   "不明なマージ方法です。",
	ErrMergeConflict:   "ブランチがコンフリクトしています: %s",
	ErrDirtyWorktree:   "ベースブランチのワークツリーが dirty です — 先にコミットかスタッシュしてください。",
	ErrBaseMoved:       "マージ中にベースブランチが動きました — もう一度お試しください。",
	ErrThreadOnly:      "コメントはスレッドにのみ追加できます。",
	ErrDiffCompute:     "diff を計算できませんでした — ref が移動または削除された可能性があります。",
	ErrDeleteLastWsH:   "ワークスペースを削除できません",
	ErrDeleteLastWsM:   "最後に残ったワークスペースは削除できません。",
	ErrSessionVerify:   "セッションを検証できませんでした。もう一度お試しください。",
}

// messagesFor resolves the catalog for a locale; anything unrecognized —
// including a bogus lang cookie — gets the default locale.
func messagesFor(lang string) *messages {
	switch lang {
	case "ja":
		return &messagesJA
	default:
		return &messagesEN
	}
}

// messageFuncs registers every field of the locale's messages struct as a
// template function named after the field ({{AppName}}, {{IssuesTitle}},
// {{IssuesCount args...}}). Values are printf format strings: args are only
// applied when the template passes them, so literal text needs no escaping.
// Fields ending in HTML return template.HTML with every arg escaped — the
// message itself is trusted markup, user data is not.
func messageFuncs(funcs template.FuncMap, lang string) {
	v := reflect.ValueOf(*messagesFor(lang))
	fallback := reflect.ValueOf(messagesEN)
	typ := v.Type()
	for i := range typ.NumField() {
		name := typ.Field(i).Name
		msg := v.Field(i).String()
		if msg == "" {
			// A field not yet translated renders the en text, so a
			// half-filled ja catalog degrades instead of going blank.
			msg = fallback.Field(i).String()
		}
		if strings.HasSuffix(name, "HTML") {
			funcs[name] = func(args ...any) template.HTML {
				if len(args) == 0 {
					return template.HTML(msg)
				}
				esc := make([]any, len(args))
				for j, a := range args {
					if s, ok := a.(string); ok {
						esc[j] = template.HTMLEscapeString(s)
					} else {
						esc[j] = a
					}
				}
				return template.HTML(fmt.Sprintf(msg, esc...))
			}
			continue
		}
		funcs[name] = func(args ...any) string {
			if len(args) == 0 {
				return msg
			}
			return fmt.Sprintf(msg, args...)
		}
	}

	// Locale-bound helpers shared with funcMap names: fmtTime and
	// authorName vary by locale, so parsePages overrides them after
	// cloning the shared map.
	funcs["fmtTime"] = func(t time.Time) string { return fmtTimeFor(lang, t) }
	funcs["authorName"] = func(u *store.User) string { return authorNameFor(lang, u) }
	funcs["hideReasonLabel"] = func(code string) string { return hideReasonLabelFor(lang, code) }
	funcs["noun"] = func(key string) string { return nounFor(lang, key) }
	funcs["nounTitle"] = func(key string) string { return nounTitleFor(lang, key) }
	funcs["repliesClause"] = func(n int) string { return repliesClauseFor(lang, n) }
	funcs["labelKind"] = func(kind string) string { return labelKindFor(lang, kind) }
	funcs["labelScope"] = func(wsID int64) string { return labelScopeFor(lang, wsID) }
}

// fmtTimeFor renders a timestamp in the locale's compact form.
func fmtTimeFor(lang string, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	if lang == "ja" {
		return t.Local().Format("2006年1月2日 15:04")
	}
	return t.Local().Format("Jan 2, 2006 15:04")
}

// authorNameFor renders a post's author; a nil author is an anonymous post.
func authorNameFor(lang string, u *store.User) string {
	if u == nil {
		return messagesFor(lang).Anonymous
	}
	return u.Name
}

// hideReasonLabelFor renders a moderation hide-reason code (stored
// verbatim in the DB) as display text.
func hideReasonLabelFor(lang, code string) string {
	if lang == "ja" {
		switch code {
		case store.HideReasonSpam:
			return "スパム"
		case store.HideReasonAbuse:
			return "不適切"
		case store.HideReasonOffTopic:
			return "話題外"
		case store.HideReasonOutdated:
			return "古い情報"
		case store.HideReasonDuplicate:
			return "重複"
		case store.HideReasonResolved:
			return "解決済み"
		case store.HideReasonLowQuality:
			return "低品質"
		}
	}
	return code
}

// nounFor renders a post-kind key ("comment", "thread", "reply") as a
// lowercase noun for mid-sentence use.
func nounFor(lang, key string) string {
	if lang == "ja" {
		switch key {
		case "comment":
			return "コメント"
		case "thread":
			return "スレッド"
		case "reply":
			return "返信"
		}
		return key
	}
	return key
}

// nounTitleFor is nounFor for sentence-leading use ("Thread restored.").
func nounTitleFor(lang, key string) string {
	if lang == "ja" {
		return nounFor(lang, key)
	}
	switch key {
	case "comment":
		return "Comment"
	case "thread":
		return "Thread"
	case "reply":
		return "Reply"
	}
	return key
}

// repliesClauseFor renders the " and its N replies" tail of the comment
// delete warning; n == 0 means no tail at all.
func repliesClauseFor(lang string, n int) string {
	if n <= 0 {
		return ""
	}
	if lang == "ja" {
		return fmt.Sprintf(" とその %d 件の返信", n)
	}
	if n == 1 {
		return fmt.Sprintf(" and its %d reply", n)
	}
	return fmt.Sprintf(" and its %d replies", n)
}

// labelKindFor renders a label kind ("issue"/"discussion") for display.
func labelKindFor(lang, kind string) string {
	if lang == "ja" {
		switch kind {
		case store.KindLabelIssue:
			return "イシュー"
		case store.KindLabelDiscussion:
			return "ディスカッション"
		}
		return kind
	}
	switch kind {
	case store.KindLabelIssue:
		return "Issue"
	case store.KindLabelDiscussion:
		return "Discussion"
	}
	return kind
}

// labelScopeFor renders a label's scope from its workspace id (0 = shared).
func labelScopeFor(lang string, wsID int64) string {
	if wsID != 0 {
		if lang == "ja" {
			return "ワークスペース"
		}
		return "workspace"
	}
	if lang == "ja" {
		return "共有"
	}
	return "shared"
}

// localeContextKey: one struct type per context key (see auth.go).
type localeContextKey struct{}

// localeFromContext reports the locale withLocale stored on the request.
// Requests that never passed the middleware (tests, fragments rendered
// without it) default to the first supported language.
func localeFromContext(ctx context.Context) string {
	if lang, ok := ctx.Value(localeContextKey{}).(string); ok {
		return lang
	}
	return supportedLangs[0]
}

// msgFor returns the catalog for the request's locale, for handler-side
// strings (page titles, headings) passed to templates via the view model.
func msgFor(r *http.Request) *messages {
	return messagesFor(localeFromContext(r.Context()))
}

// withLocale resolves the display language for the request. An explicit
// ?lang=<code> persists to a cookie and redirects to the same URL without
// the parameter (303, so every method ends on a clean GET); otherwise the
// cookie wins, then Accept-Language, then the default locale.
func withLocale(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("lang"); validLang(q) && r.URL.Path != "/locale" {
			setLangCookie(w, q)
			v := r.URL.Query()
			v.Del("lang")
			// Build the Location from a cleaned path: a path like
			// "//evil.example/x" survives RequestURI as a scheme-relative
			// Location header, which is an open redirect.
			target := path.Clean("/" + r.URL.Path)
			if q := v.Encode(); q != "" {
				target += "?" + q
			}
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		lang := supportedLangs[0]
		if c, err := r.Cookie(langCookieName); err == nil && validLang(c.Value) {
			lang = c.Value
		} else if wantsJapanese(r.Header.Get("Accept-Language")) {
			lang = "ja"
		}
		r = r.WithContext(context.WithValue(r.Context(), localeContextKey{}, lang))
		next.ServeHTTP(w, r)
	})
}

// setLangCookie writes the lang cookie shared by ?lang= and /locale.
func setLangCookie(w http.ResponseWriter, lang string) {
	http.SetCookie(w, &http.Cookie{
		Name:     langCookieName,
		Value:    lang,
		Path:     "/",
		MaxAge:   365 * 24 * 3600,
		SameSite: http.SameSiteLaxMode,
	})
}

// setLocale handles GET /locale?lang=<code>: the language-switcher link's
// target. Unlike the ?lang= middleware path, which cannot know the
// referer's query (a template never sees the current URL), this endpoint
// redirects back to the Referer so filters like ?q= survive the switch.
// The Referer is sanitized to a same-origin absolute path — anything else
// falls back to the root.
func setLocale(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	if !validLang(lang) {
		lang = supportedLangs[0]
	}
	setLangCookie(w, lang)
	target := "/"
	if ref, err := url.Parse(r.Referer()); err == nil && (ref.Host == "" || ref.Host == r.Host) {
		if t := ref.RequestURI(); strings.HasPrefix(t, "/") && !strings.HasPrefix(t, "//") {
			target = t
		}
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func validLang(lang string) bool {
	for _, l := range supportedLangs {
		if l == lang {
			return true
		}
	}
	return false
}

// wantsJapanese reports whether Accept-Language prefers Japanese over
// English. It is deliberately limited to the two supported locales; a real
// multi-locale deployment should use golang.org/x/text/language.
func wantsJapanese(header string) bool {
	type preference struct {
		q     float64
		order int
		set   bool
	}
	var ja, en preference
	order := 0
	for part := range strings.SplitSeq(header, ",") {
		tag, q, ok := parseAcceptLanguagePart(part)
		if !ok {
			continue
		}
		pref := preference{q: q, order: order, set: true}
		switch {
		case tag == "ja" || strings.HasPrefix(tag, "ja-"):
			if !ja.set || q > ja.q {
				ja = pref
			}
		case tag == "en" || strings.HasPrefix(tag, "en-"):
			if !en.set || q > en.q {
				en = pref
			}
		}
		order++
	}
	if !ja.set || ja.q == 0 {
		return false
	}
	if !en.set {
		return true
	}
	if ja.q != en.q {
		return ja.q > en.q
	}
	return ja.order < en.order
}

// parseAcceptLanguagePart parses one language-range plus its q parameter.
// Entries with malformed or out-of-range q values are ignored.
func parseAcceptLanguagePart(part string) (string, float64, bool) {
	pieces := strings.Split(part, ";")
	tag := strings.ToLower(strings.TrimSpace(pieces[0]))
	if tag == "" {
		return "", 0, false
	}
	q := 1.0
	for _, piece := range pieces[1:] {
		name, value, ok := strings.Cut(strings.TrimSpace(piece), "=")
		if !ok || !strings.EqualFold(name, "q") {
			continue
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsNaN(parsed) || parsed < 0 || parsed > 1 {
			return "", 0, false
		}
		q = parsed
	}
	return tag, q, true
}

// fieldErrText renders a store validation failure for the page's locale.
// A zero FieldError yields "". Unknown codes fall back to the store's
// English message (the JSON API keeps using it directly).
func fieldErrText(lang, field string, fe store.FieldError) string {
	if fe.Code == "" {
		return ""
	}
	m := messagesFor(lang)
	switch fe.Code {
	case store.ErrCodeRequired:
		switch field {
		case "title":
			return m.TitleRequired
		case "body":
			return m.BodyRequired
		case "category":
			return m.CategoryRequired
		case "name":
			return m.NameRequired
		case "color":
			return m.ColorRequired
		case "slug":
			return m.SlugRequired
		case "password":
			return m.PasswordRequired
		case "base":
			return m.ErrBaseRefRequired
		case "head":
			return m.ErrHeadRefRequired
		}
	case store.ErrCodeTooLong:
		return fmt.Sprintf(m.ErrTooLong, fieldLabelFor(lang, field), fe.Limit)
	case store.ErrCodeSlugPattern:
		return fmt.Sprintf(m.ErrSlugPattern, store.WorkspaceSlugPattern())
	case store.ErrCodeHexColor:
		return m.ErrColorHex
	}
	return fe.Message
}

// fieldLabelFor names an input field for mid-sentence use in messages
// like "X must be N characters or fewer."
func fieldLabelFor(lang, field string) string {
	if lang == "ja" {
		switch field {
		case "title":
			return "タイトル"
		case "body":
			return "本文"
		case "comment":
			return "コメント"
		case "name":
			return "名前"
		case "slug":
			return "スラッグ"
		case "description":
			return "説明"
		case "color":
			return "色"
		case "category":
			return "カテゴリ"
		case "password":
			return "パスワード"
		}
	}
	switch field {
	case "body":
		return "Body"
	case "comment":
		return "Comment"
	case "name":
		return "Name"
	case "slug":
		return "Slug"
	case "description":
		return "Description"
	case "color":
		return "Color"
	case "category":
		return "Category"
	case "password":
		return "Password"
	}
	return "Title"
}

// jsDictFor marshals the JS-layer strings for lang into the JSON blob the
// <html data-i18n> attribute carries; /static/i18n.js parses it.
func jsDictFor(lang string) template.HTMLAttr {
	m := messagesFor(lang)
	b, _ := json.Marshal(map[string]string{
		"toast_notifications": m.JSToastNotifications,
		"toast_close":         m.JSToastClose,
		"posting":             m.JSPosting,
		"updated":             m.JSUpdated,
		"comment_posted":      m.JSCommentPosted,
		"comment_post_failed": m.JSCommentPostFailed,
		"copied":              m.JSCopied,
		"copy_failed":         m.JSCopyFailed,
		"back_to":             m.JSBackTo,
		"noun_deleted":        m.JSNounDeleted,
		"entity_updated":      m.JSEntityUpdated,
		"answer_updated":      m.JSAnswerUpdated,
		"new_thread":          m.JSNewThread,
		"new_comment":         m.JSNewComment,
		"new_reply":           m.JSNewReply,
		"noun_issue":          m.JSNounIssue,
		"noun_discussion":     m.JSNounDiscussion,
		"noun_pr":             m.JSNounPR,
		"list_issues":         m.JSListIssues,
		"list_discussions":    m.JSListDiscussions,
		"list_prs":            m.JSListPRs,
	})
	return template.HTMLAttr(b)
}
