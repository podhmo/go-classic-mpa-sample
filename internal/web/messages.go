package web

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
