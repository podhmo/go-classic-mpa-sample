// Issue, comment and label subcommands.
package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// issueCmd dispatches `issues issue <action>`.
func issueCmd(ctx context.Context, o Options, args []string) error {
	if len(args) == 0 {
		return usageError{msg: "usage: issues issue list|view|create|edit|close|reopen|delete"}
	}
	switch args[0] {
	case "list":
		return issueList(ctx, o, args[1:])
	case "view":
		return issueView(ctx, o, args[1:])
	case "create":
		return issueCreate(ctx, o, args[1:])
	case "edit":
		return issueEdit(ctx, o, args[1:])
	case "close", "reopen", "delete":
		return issueStateChange(ctx, o, args[0], args[1:])
	default:
		return usageError{msg: fmt.Sprintf("unknown issue command %q", args[0])}
	}
}

// issues issue list [--state open|closed|all] [--label NAME]
// [--search Q] [--sort newest|oldest] [--limit N] [--page N] [--json F,...]
func issueList(ctx context.Context, o Options, args []string) error {
	fs := flag.NewFlagSet("issue list", flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	state := fs.String("state", "open", "filter by state: open|closed|all")
	label := fs.String("label", "", "filter by label name")
	query := fs.String("search", "", "search title and body")
	sort := fs.String("sort", "newest", "sort order: newest|oldest")
	limit := fs.Int("limit", 30, "maximum issues to list")
	page := fs.Int("page", 1, "page number")
	jsonFields := fs.String("json", "", "output JSON with the named fields (comma-separated)")
	if _, err := parseLeaf(fs, args, 0); err != nil {
		return err
	}
	switch *state {
	case "open", "closed", "all":
	default:
		return usageError{msg: fmt.Sprintf("--state must be open|closed|all, got %q", *state)}
	}
	if *sort != "newest" && *sort != "oldest" {
		return usageError{msg: fmt.Sprintf("--sort must be newest|oldest, got %q", *sort)}
	}

	b, closer, err := o.backend(bf)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	f := store.ListFilter{
		Status: *state, Query: strings.TrimSpace(*query),
		Sort: *sort, Page: *page, PerPage: *limit,
	}
	if *label != "" {
		ids, err := resolveLabelIDs(ctx, b, []string{*label})
		if err != nil {
			return err
		}
		f.LabelID = ids[0]
	}
	issues, _, err := b.ListIssues(ctx, f)
	if err != nil {
		return err
	}
	if *jsonFields != "" {
		return writeJSONFields(o.Stdout, issueViews(issues), *jsonFields)
	}
	for _, i := range issues {
		fmt.Fprintf(o.Stdout, "%d\t%s\t%s\t%s\t%s\n",
			i.ID, i.Status, i.Title, labelNames(i.Labels), fmtTimeShort(i.UpdatedAt))
	}
	return nil
}

// issues issue view N [--comments] [--json F,...] [-w]
func issueView(ctx context.Context, o Options, args []string) error {
	fs := flag.NewFlagSet("issue view", flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	comments := fs.Bool("comments", false, "include comments")
	jsonFields := fs.String("json", "", "output JSON with the named fields (comma-separated)")
	web := fs.Bool("web", false, "open the issue page in a browser (requires --server)")
	fs.BoolVar(web, "w", false, "shorthand for --web")
	rest, err := parseLeaf(fs, args, 1)
	if err != nil {
		return err
	}
	id, err := issueNumber(rest, 0)
	if err != nil {
		return err
	}

	if *web {
		return o.openPage(bf, "/issues/", id)
	}
	b, closer, err := o.backend(bf)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	issue, err := b.GetIssue(ctx, id)
	if err != nil {
		return err
	}
	var cs []*store.Comment
	if *comments {
		cs, err = b.ListComments(ctx, id)
		if err != nil {
			return err
		}
	}
	if *jsonFields != "" {
		if *comments {
			return writeJSONFields(o.Stdout, map[string]any{
				"issue":    issueViewJSON(issue),
				"comments": commentViews(cs),
			}, *jsonFields)
		}
		return writeJSONFields(o.Stdout, issueViewJSON(issue), *jsonFields)
	}

	fmt.Fprintf(o.Stdout, "title:\t%s\n", issue.Title)
	fmt.Fprintf(o.Stdout, "status:\t%s\n", issue.Status)
	fmt.Fprintf(o.Stdout, "author:\t%s\n", authorName(issue.Author))
	fmt.Fprintf(o.Stdout, "labels:\t%s\n", labelNames(issue.Labels))
	fmt.Fprintf(o.Stdout, "updated:\t%s\n", fmtTimeShort(issue.UpdatedAt))
	fmt.Fprintf(o.Stdout, "--\n%s\n", issue.Body)
	if *comments {
		fmt.Fprintln(o.Stdout, "\ncomments:")
		for _, c := range cs {
			fmt.Fprintf(o.Stdout, "  #%d %s (%s)\n    %s\n",
				c.ID, authorName(c.Author), fmtTimeShort(c.CreatedAt),
				strings.ReplaceAll(c.Body, "\n", "\n    "))
		}
	}
	return nil
}

// issues issue create --title T [--body B|--body-file -] [--label NAME]... [-w]
func issueCreate(ctx context.Context, o Options, args []string) error {
	fs := flag.NewFlagSet("issue create", flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	title := fs.String("title", "", "issue title (required)")
	body := fs.String("body", "", "issue body")
	bodyFile := fs.String("body-file", "", "read body from file (- for stdin)")
	var labels nameFlag
	fs.Var(&labels, "label", "attach a label by name (repeatable)")
	web := fs.Bool("web", false, "open the new issue in a browser (requires --server)")
	fs.BoolVar(web, "w", false, "shorthand for --web")
	if _, err := parseLeaf(fs, args, 0); err != nil {
		return err
	}
	if strings.TrimSpace(*title) == "" {
		return usageError{msg: "--title is required"}
	}
	bodyText, err := o.readBody(*body, *bodyFile)
	if err != nil {
		return err
	}

	b, closer, err := o.backend(bf)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	labelIDs, err := resolveLabelIDs(ctx, b, labels)
	if err != nil {
		return err
	}
	// The actor is anonymous locally; remotely it is whoever the API
	// token resolves to — the authorID arg is ignored over HTTP.
	id, err := b.CreateIssue(ctx, store.IssueInput{
		Title: strings.TrimSpace(*title), Body: strings.TrimSpace(bodyText), LabelIDs: labelIDs,
	}, 0)
	if err != nil {
		return err
	}
	fmt.Fprintf(o.Stdout, "created issue #%d\n", id)
	if *web {
		return o.openPage(bf, "/issues/", id)
	}
	return nil
}

// issues issue edit N [--title T] [--body B] [--label NAME]...
func issueEdit(ctx context.Context, o Options, args []string) error {
	fs := flag.NewFlagSet("issue edit", flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	title := fs.String("title", "", "new title")
	body := fs.String("body", "", "new body")
	var labels nameFlag
	fs.Var(&labels, "label", "replace labels with this name set (repeatable)")
	rest, err := parseLeaf(fs, args, 1)
	if err != nil {
		return err
	}
	id, err := issueNumber(rest, 0)
	if err != nil {
		return err
	}
	b, closer, err := o.backend(bf)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	issue, err := b.GetIssue(ctx, id)
	if err != nil {
		return err
	}
	in := store.IssueInput{Title: issue.Title, Body: issue.Body}
	for _, l := range issue.Labels {
		in.LabelIDs = append(in.LabelIDs, l.ID)
	}
	changed := false
	if *title != "" {
		in.Title = strings.TrimSpace(*title)
		changed = true
	}
	fs.Visit(func(f *flag.Flag) { // --body counts even when empty
		if f.Name == "body" {
			in.Body = strings.TrimSpace(*body)
			changed = true
		}
	})
	if len(labels) > 0 {
		in.LabelIDs, err = resolveLabelIDs(ctx, b, labels)
		if err != nil {
			return err
		}
		changed = true
	}
	if !changed {
		return usageError{msg: "nothing to update: pass --title, --body or --label"}
	}
	if err := b.UpdateIssue(ctx, id, in); err != nil {
		return err
	}
	fmt.Fprintf(o.Stdout, "updated issue #%d\n", id)
	return nil
}

// issues issue close|reopen|delete N
func issueStateChange(ctx context.Context, o Options, action string, args []string) error {
	fs := flag.NewFlagSet("issue "+action, flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	rest, err := parseLeaf(fs, args, 1)
	if err != nil {
		return err
	}
	id, err := issueNumber(rest, 0)
	if err != nil {
		return err
	}
	b, closer, err := o.backend(bf)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	switch action {
	case "close", "reopen":
		status := store.StatusClosed
		if action == "reopen" {
			status = store.StatusOpen
		}
		if err := b.SetIssueStatus(ctx, id, status); err != nil {
			return err
		}
	case "delete":
		if err := b.DeleteIssue(ctx, id); err != nil {
			return err
		}
	}
	verb := map[string]string{"close": "closed", "reopen": "reopened", "delete": "deleted"}[action]
	fmt.Fprintf(o.Stdout, "%s issue #%d\n", verb, id)
	return nil
}

// issues comment create N --body B | --body-file -
func commentCmd(ctx context.Context, o Options, args []string) error {
	if len(args) == 0 || args[0] != "create" {
		return usageError{msg: "usage: issues comment create N --body B"}
	}
	fs := flag.NewFlagSet("comment create", flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	body := fs.String("body", "", "comment body")
	bodyFile := fs.String("body-file", "", "read body from file (- for stdin)")
	rest, err := parseLeaf(fs, args[1:], 1)
	if err != nil {
		return err
	}
	id, err := issueNumber(rest, 0)
	if err != nil {
		return err
	}
	bodyText, err := o.readBody(*body, *bodyFile)
	if err != nil {
		return err
	}
	if strings.TrimSpace(bodyText) == "" {
		return usageError{msg: "--body is required (or --body-file)"}
	}
	b, closer, err := o.backend(bf)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	cid, err := b.CreateComment(ctx, id, strings.TrimSpace(bodyText), 0)
	if err != nil {
		return err
	}
	fmt.Fprintf(o.Stdout, "created comment #%d on issue #%d\n", cid, id)
	return nil
}

// issues label list
func labelCmd(ctx context.Context, o Options, args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return usageError{msg: "usage: issues label list"}
	}
	fs := flag.NewFlagSet("label list", flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	jsonFields := fs.String("json", "", "output JSON with the named fields (comma-separated)")
	if _, err := parseLeaf(fs, args[1:], 0); err != nil {
		return err
	}
	b, closer, err := o.backend(bf)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	labels, err := b.ListLabels(ctx)
	if err != nil {
		return err
	}
	if *jsonFields != "" {
		return writeJSONFields(o.Stdout, labelViews(labels), *jsonFields)
	}
	for _, l := range labels {
		fmt.Fprintf(o.Stdout, "%s\t%s\n", l.Name, l.Color)
	}
	return nil
}

// openPage builds the web page URL for a resource and opens it in the
// browser. It needs the server base URL, so remote mode is required.
func (o Options) openPage(bf *backendFlags, prefix string, id int64) error {
	if bf.server == "" {
		return usageError{msg: "-w/--web requires --server (or ISSUES_SERVER)"}
	}
	return o.OpenBrowser(strings.TrimSuffix(bf.server, "/") + prefix + fmt.Sprint(id))
}

func labelNames(labels []store.Label) string {
	names := make([]string, 0, len(labels))
	for _, l := range labels {
		names = append(names, l.Name)
	}
	return strings.Join(names, ", ")
}
