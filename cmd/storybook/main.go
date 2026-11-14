// Command storybook renders "stories" of the issue tracker with minigo,
// without a database or a running server.
//
// A story is an exported func StoryXxx() in a file guarded by a build tag
// (default "story"), so the go toolchain never builds it into the real
// binary. The story calls the renderer with dummy view models; storybook
// interprets internal/web with that tag enabled and captures what the
// story prints to stdout. This file is a fork of minigo's
// examples/storybook/cmd/storybook, extended with the binds this
// application needs and static-asset serving.
//
//	storybook -list ./internal/web          # list stories
//	storybook ./internal/web StoryIssueList # print one story's body
//	storybook -http :6006 ./internal/web    # serve them, re-interpreting on every request
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "storybook: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("storybook", flag.ContinueOnError)
	tag := fs.String("tag", "story", "build tag guarding the story files")
	src := fs.String("src", "", "comma-separated extra packages to interpret from source")
	staticDir := fs.String("static", "", "directory served under /static/ in -http mode (default <pkgdir>/static)")
	list := fs.Bool("list", false, "list stories and exit")
	out := fs.String("o", "", "write index.html and one page per story into this directory")
	addr := fs.String("http", "", "serve the stories on this address (e.g. :6006)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: storybook [flags] <pkgdir> [Story]")
	}
	b := &Book{Dir: fs.Arg(0), Tag: *tag, StaticDir: *staticDir}
	if *src != "" {
		b.Src = strings.Split(*src, ",")
	}
	stories, err := b.Stories()
	// -http alone tolerates a discovery failure: the server starts anyway
	// and the index shows the error, so a book with no stories yet (or one
	// being added live) still serves. Combined with -list or -o the error
	// stays fatal — those modes would otherwise succeed on an empty book.
	if err != nil && (*addr == "" || *list || *out != "") {
		return err
	}
	switch {
	case *list:
		for _, s := range stories {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", s.Name, s.Pos, s.Doc)
		}
		return nil
	case *out != "":
		return b.Export(ctx, *out, stories)
	case *addr != "":
		if err != nil {
			slog.WarnContext(ctx, "story discovery failed; serving anyway", "err", err)
		}
		slog.InfoContext(ctx, "serving stories", "addr", *addr, "dir", b.Dir, "stories", len(stories))
		return http.ListenAndServe(*addr, b.Handler())
	case fs.NArg() >= 2:
		res, err := b.Render(ctx, fs.Arg(1))
		if err != nil {
			return err
		}
		_, err = stdout.Write(res)
		return err
	default:
		return errors.New("one of -list, -o, -http or a story name is required")
	}
}

// staticHandler serves the application's real assets under /static/, the
// same path layout.html references, so previews pick up CSS edits on a
// plain browser reload.
func (b *Book) staticHandler() http.Handler {
	dir := b.StaticDir
	if dir == "" {
		dir = filepath.Join(b.Dir, "static")
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return http.NotFoundHandler()
	}
	return http.StripPrefix("/static/", http.FileServer(http.Dir(root)))
}
