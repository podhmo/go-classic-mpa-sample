// Discussion subcommands — the same shapes as the issue commands, plus
// category and answer-mark handling (docs/sketch/ja/cli-and-web-api.md).
package cli

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// discussionCmd dispatches `issues discussion <action>`.
func discussionCmd(ctx context.Context, o Options, args []string) error {
	if len(args) == 0 {
		return usageError{msg: "usage: issues discussion list|view|create|edit|delete|comment|answer"}
	}
	switch args[0] {
	case "list":
		return discussionList(ctx, o, args[1:])
	case "view":
		return discussionView(ctx, o, args[1:])
	case "create":
		return discussionCreate(ctx, o, args[1:])
	case "edit":
		return discussionEdit(ctx, o, args[1:])
	case "delete":
		return discussionDelete(ctx, o, args[1:])
	case "comment":
		return discussionCommentCmd(ctx, o, args[1:])
	case "answer":
		return discussionAnswer(ctx, o, args[1:])
	default:
		return usageError{msg: fmt.Sprintf("unknown discussion command %q", args[0])}
	}
}

// issues discussion list [--category SLUG] [--answered yes|no]
// [--search Q] [--sort newest|oldest] [--limit N] [--page N] [--json F,...]
func discussionList(ctx context.Context, o Options, args []string) error {
	fs := flag.NewFlagSet("discussion list", flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	category := fs.String("category", "", "filter by category slug")
	answered := fs.String("answered", "", "filter by answer mark: yes|no")
	query := fs.String("search", "", "search title and body")
	sortFlag := fs.String("sort", "newest", "sort order: newest|oldest")
	limit := fs.Int("limit", 30, "maximum discussions to list")
	page := fs.Int("page", 1, "page number")
	jsonFields := fs.String("json", "", "output JSON with the named fields (comma-separated)")
	tmpl := fs.String("template", "", "render output through a Go text/template")
	if _, err := parseLeaf(fs, args, 0); err != nil {
		return err
	}
	if *jsonFields != "" && *tmpl != "" {
		return usageError{msg: "--json and --template are mutually exclusive"}
	}
	switch *answered {
	case "", "yes", "no":
	default:
		return usageError{msg: fmt.Sprintf("--answered must be yes|no, got %q", *answered)}
	}
	if *sortFlag != "newest" && *sortFlag != "oldest" {
		return usageError{msg: fmt.Sprintf("--sort must be newest|oldest, got %q", *sortFlag)}
	}

	b, closer, err := o.backend(ctx, bf)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	f := store.DiscussionListFilter{
		CategorySlug: *category, Answered: *answered,
		Query: strings.TrimSpace(*query),
		Sort:  *sortFlag, Page: *page, PerPage: *limit,
	}
	discussions, _, err := b.ListDiscussions(ctx, f)
	if err != nil {
		return err
	}
	if *jsonFields != "" {
		return writeJSONFields(o.Stdout, discussionViews(discussions), *jsonFields)
	}
	if *tmpl != "" {
		return writeTemplate(o.Stdout, discussionViews(discussions), *tmpl)
	}
	for _, d := range discussions {
		fmt.Fprintf(o.Stdout, "%d\t%s\t%s\t%s\t%s\n",
			d.ID, d.Category.Slug, o.answeredMark(d.Answered), d.Title, fmtTimeShort(d.UpdatedAt))
	}
	return nil
}

// issues discussion view N [--comments] [--json F,...] [-w]
func discussionView(ctx context.Context, o Options, args []string) error {
	fs := flag.NewFlagSet("discussion view", flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	comments := fs.Bool("comments", false, "include threads and their replies")
	jsonFields := fs.String("json", "", "output JSON with the named fields (comma-separated)")
	tmpl := fs.String("template", "", "render output through a Go text/template")
	web := fs.Bool("web", false, "open the discussion page in a browser (requires --server)")
	fs.BoolVar(web, "w", false, "shorthand for --web")
	rest, err := parseLeaf(fs, args, 1)
	if err != nil {
		return err
	}
	if *jsonFields != "" && *tmpl != "" {
		return usageError{msg: "--json and --template are mutually exclusive"}
	}
	id, err := argNumber(rest, 0, "discussion")
	if err != nil {
		return err
	}

	if *web {
		return o.openPage(bf, "/discussions/", id)
	}
	b, closer, err := o.backend(ctx, bf)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	d, err := b.GetDiscussion(ctx, id)
	if err != nil {
		return err
	}
	var ts []*store.DiscussionThread
	if *comments {
		ts, err = b.ListDiscussionThreads(ctx, id)
		if err != nil {
			return err
		}
	}
	out := func(v any) error {
		if *tmpl != "" {
			return writeTemplate(o.Stdout, v, *tmpl)
		}
		return writeJSONFields(o.Stdout, v, *jsonFields)
	}
	if *jsonFields != "" || *tmpl != "" {
		if *comments {
			m, err := jsonObject(discussionViewJSON(d))
			if err != nil {
				return err
			}
			m["threads"] = discussionThreadViews(ts)
			return out(m)
		}
		return out(discussionViewJSON(d))
	}

	fmt.Fprintf(o.Stdout, "title:\t%s\n", d.Title)
	fmt.Fprintf(o.Stdout, "category:\t%s (%s)\n", d.Category.Name, d.Category.Slug)
	fmt.Fprintf(o.Stdout, "answered:\t%s\n", map[bool]string{true: "yes", false: "no"}[d.Answered])
	fmt.Fprintf(o.Stdout, "author:\t%s\n", authorName(d.Author))
	fmt.Fprintf(o.Stdout, "updated:\t%s\n", fmtTimeShort(d.UpdatedAt))
	fmt.Fprintf(o.Stdout, "--\n%s\n", d.Body)
	if *comments {
		fmt.Fprintln(o.Stdout, "\ncomments:")
		for _, t := range ts {
			printDiscussionThread(o, t, "  ")
			for _, c := range t.Comments {
				printDiscussionComment(o, c, "    ")
			}
		}
	}
	return nil
}

func printDiscussionThread(o Options, t *store.DiscussionThread, indent string) {
	answer := ""
	if t.IsAnswer {
		answer = " " + o.paint("32", "[answer]")
	}
	fmt.Fprintf(o.Stdout, "%s#%d %s (%s)%s\n%s  %s\n",
		indent, t.ID, authorName(t.Author), fmtTimeShort(t.CreatedAt), answer,
		indent, strings.ReplaceAll(t.Body, "\n", "\n"+indent+"  "))
}

func printDiscussionComment(o Options, c *store.DiscussionComment, indent string) {
	fmt.Fprintf(o.Stdout, "%s#%d %s (%s)\n%s  %s\n",
		indent, c.ID, authorName(c.Author), fmtTimeShort(c.CreatedAt),
		indent, strings.ReplaceAll(c.Body, "\n", "\n"+indent+"  "))
}

// issues discussion create --title T --category SLUG
// [--body B|--body-file -] [-w]
func discussionCreate(ctx context.Context, o Options, args []string) error {
	fs := flag.NewFlagSet("discussion create", flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	title := fs.String("title", "", "discussion title (required)")
	category := fs.String("category", "", "category slug (required)")
	body := fs.String("body", "", "discussion body")
	bodyFile := fs.String("body-file", "", "read body from file (- for stdin)")
	web := fs.Bool("web", false, "open the new discussion in a browser (requires --server)")
	fs.BoolVar(web, "w", false, "shorthand for --web")
	if _, err := parseLeaf(fs, args, 0); err != nil {
		return err
	}
	if strings.TrimSpace(*title) == "" {
		return usageError{msg: "--title is required"}
	}
	if *category == "" {
		return usageError{msg: "--category is required"}
	}
	if *web && bf.server == "" {
		return usageError{msg: "-w/--web requires --server (or ISSUES_SERVER)"}
	}
	bodyText, err := o.readBody(*body, *bodyFile)
	if err != nil {
		return err
	}

	b, closer, err := o.backend(ctx, bf)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	categoryID, err := resolveCategoryID(ctx, b, *category)
	if err != nil {
		return err
	}
	in := store.DiscussionInput{
		Title: strings.TrimSpace(*title), Body: strings.TrimSpace(bodyText), CategoryID: categoryID,
	}
	if err := validateDiscussionInput(in); err != nil {
		return err
	}
	id, err := b.CreateDiscussion(ctx, in, 0)
	if err != nil {
		return err
	}
	fmt.Fprintf(o.Stdout, "created discussion #%d\n", id)
	if *web {
		return o.openPage(bf, "/discussions/", id)
	}
	return nil
}

// issues discussion edit N [--title T] [--body B] [--category SLUG]
func discussionEdit(ctx context.Context, o Options, args []string) error {
	fs := flag.NewFlagSet("discussion edit", flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	title := fs.String("title", "", "new title")
	body := fs.String("body", "", "new body")
	category := fs.String("category", "", "new category slug")
	rest, err := parseLeaf(fs, args, 1)
	if err != nil {
		return err
	}
	id, err := argNumber(rest, 0, "discussion")
	if err != nil {
		return err
	}
	b, closer, err := o.backend(ctx, bf)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	d, err := b.GetDiscussion(ctx, id)
	if err != nil {
		return err
	}
	in := store.DiscussionInput{Title: d.Title, Body: d.Body, CategoryID: d.Category.ID}
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
	if *category != "" {
		in.CategoryID, err = resolveCategoryID(ctx, b, *category)
		if err != nil {
			return err
		}
		changed = true
	}
	if !changed {
		return usageError{msg: "nothing to update: pass --title, --body or --category"}
	}
	if err := validateDiscussionInput(in); err != nil {
		return err
	}
	if err := b.UpdateDiscussion(ctx, id, in); err != nil {
		return err
	}
	fmt.Fprintf(o.Stdout, "updated discussion #%d\n", id)
	return nil
}

// issues discussion delete N
func discussionDelete(ctx context.Context, o Options, args []string) error {
	fs := flag.NewFlagSet("discussion delete", flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	rest, err := parseLeaf(fs, args, 1)
	if err != nil {
		return err
	}
	id, err := argNumber(rest, 0, "discussion")
	if err != nil {
		return err
	}
	b, closer, err := o.backend(ctx, bf)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	if err := b.DeleteDiscussion(ctx, id); err != nil {
		return err
	}
	fmt.Fprintf(o.Stdout, "deleted discussion #%d\n", id)
	return nil
}

// issues discussion comment create N --body B [--thread T]
func discussionCommentCmd(ctx context.Context, o Options, args []string) error {
	if len(args) == 0 || args[0] != "create" {
		return usageError{msg: "usage: issues discussion comment create N --body B [--thread T]"}
	}
	fs := flag.NewFlagSet("discussion comment create", flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	body := fs.String("body", "", "comment body")
	bodyFile := fs.String("body-file", "", "read body from file (- for stdin)")
	thread := fs.Int64("thread", 0, "reply to this thread")
	rest, err := parseLeaf(fs, args[1:], 1)
	if err != nil {
		return err
	}
	id, err := argNumber(rest, 0, "discussion")
	if err != nil {
		return err
	}
	bodyText, err := o.readBody(*body, *bodyFile)
	if err != nil {
		return err
	}
	bodyText = strings.TrimSpace(bodyText)
	if msg := store.ValidateCommentBody(bodyText); msg != "" {
		if bodyText == "" {
			return usageError{msg: "--body is required (or --body-file)"}
		}
		return fmt.Errorf("invalid comment: %s", msg)
	}
	b, closer, err := o.backend(ctx, bf)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	var cid int64
	if *thread != 0 {
		cid, err = b.CreateDiscussionComment(ctx, id, *thread, bodyText, 0)
	} else {
		cid, err = b.CreateDiscussionThread(ctx, id, bodyText, 0)
	}
	if err != nil {
		return err
	}
	if *thread != 0 {
		fmt.Fprintf(o.Stdout, "created reply #%d on thread #%d of discussion #%d\n", cid, *thread, id)
	} else {
		fmt.Fprintf(o.Stdout, "created thread #%d on discussion #%d\n", cid, id)
	}
	return nil
}

// issues discussion answer N --thread T [--unmark]
func discussionAnswer(ctx context.Context, o Options, args []string) error {
	fs := flag.NewFlagSet("discussion answer", flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	thread := fs.Int64("thread", 0, "thread to mark as the answer (required)")
	unmark := fs.Bool("unmark", false, "clear the answer mark instead of setting it")
	rest, err := parseLeaf(fs, args, 1)
	if err != nil {
		return err
	}
	id, err := argNumber(rest, 0, "discussion")
	if err != nil {
		return err
	}
	if *thread < 1 {
		return usageError{msg: "--thread is required"}
	}
	b, closer, err := o.backend(ctx, bf)
	if err != nil {
		return err
	}
	defer closeQuietly(closer)

	if err := b.SetThreadAnswer(ctx, id, *thread, !*unmark); err != nil {
		return err
	}
	if *unmark {
		fmt.Fprintf(o.Stdout, "unmarked thread #%d as answer on discussion #%d\n", *thread, id)
	} else {
		fmt.Fprintf(o.Stdout, "marked thread #%d as answer on discussion #%d\n", *thread, id)
	}
	return nil
}

// resolveCategoryID maps a category slug to its id through the backend's
// category list — the CLI takes slugs like the ?category= URL filter.
func resolveCategoryID(ctx context.Context, b Backend, slug string) (int64, error) {
	cats, err := b.ListDiscussionCategories(ctx)
	if err != nil {
		return 0, err
	}
	for _, c := range cats {
		if c.Slug == slug {
			return c.ID, nil
		}
	}
	valid := make([]string, 0, len(cats))
	for _, c := range cats {
		valid = append(valid, c.Slug)
	}
	sort.Strings(valid)
	return 0, fmt.Errorf("unknown category %q (valid: %s)", slug, strings.Join(valid, ", "))
}

// validateDiscussionInput applies the shared validation rules — the local
// store backend does not validate on its own.
func validateDiscussionInput(in store.DiscussionInput) error {
	fe := store.ValidateDiscussionInput(in)
	if !fe.Any() {
		return nil
	}
	keys := make([]string, 0, len(fe))
	for k := range fe {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+": "+fe[k])
	}
	return fmt.Errorf("invalid input: %s", strings.Join(parts, "; "))
}
