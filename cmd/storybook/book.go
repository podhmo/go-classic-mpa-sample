package main

import (
	"net/http"
	"path/filepath"

	"github.com/podhmo/minigo"
	"github.com/podhmo/minigo/runtime"
	"github.com/podhmo/minigo/storybook"

	alert "github.com/yuin/goldmark-alert"
	"github.com/yuin/goldmark/v2/extension"
	gmparser "github.com/yuin/goldmark/v2/parser"
	gmhtml "github.com/yuin/goldmark/v2/renderer/html"
)

// Book is the shared storybook.Book plus the command's static-asset
// serving root: story discovery, the base-engine/session render loop,
// and the recorder drive all live in the shared package.
type Book struct {
	*storybook.Book
	StaticDir string // directory served under /static/ in -http mode
}

// The story surface stays addressable by the command's local names so
// page.go and the tests keep their shape.
type (
	Story     = storybook.Story
	StoryKind = storybook.StoryKind
	Page      = storybook.Page
)

const (
	KindPrint = storybook.KindPrint
	KindHTTP  = storybook.KindHTTP
)

// staticRoot is the absolute path of the directory served under /static/:
// -static when given, <pkgdir>/static otherwise.
func (b *Book) staticRoot() string {
	dir := b.StaticDir
	if dir == "" {
		dir = filepath.Join(b.Dir, "static")
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	return root
}

// staticHandler serves the application's real assets under /static/, the
// same path layout.html references, so previews pick up stylesheet edits
// on a plain browser reload.
func (b *Book) staticHandler() http.Handler {
	root := b.staticRoot()
	if root == "" {
		return http.NotFoundHandler()
	}
	return http.StripPrefix("/static/", http.FileServer(http.Dir(root)))
}

// bindApp is the Book.Setup hook: it registers the host packages the
// interpreted internal/web code needs but should never run from source,
// once on the base engine every render session forks from.
//
//   - modernc.org/sqlite is bound as an EMPTY package, so internal/store
//     can be interpreted (its `import _ "modernc.org/sqlite"` resolves to
//     the stub and its init side effect never happens) and store.Issue &
//     co. stay script structs the bound template can field-evaluate. The
//     sqlite code path is lazy anyway — stories never call store.Open.
//   - the goldmark packages markdown.go wires up at package init; keeping
//     them bound avoids interpreting a large third-party dependency.
func bindApp(e *minigo.Engine) error {
	e.Bind("modernc.org/sqlite", map[string]runtime.Value{})

	e.Bind("github.com/yuin/goldmark/v2/parser", map[string]runtime.Value{
		"New":            gmparser.New,
		"WithExtensions": gmparser.WithExtensions,
	})
	e.Bind("github.com/yuin/goldmark/v2/renderer/html", map[string]runtime.Value{
		"New":            gmhtml.New,
		"WithExtensions": gmhtml.WithExtensions,
		"WithHardWraps":  gmhtml.WithHardWraps,
	})
	e.Bind("github.com/yuin/goldmark/v2/extension", map[string]runtime.Value{
		"NewGFMParser":       extension.NewGFMParser,
		"NewGFMHTMLRenderer": extension.NewGFMHTMLRenderer,
	})
	// BindAs gives the declared package name: without it the bound
	// package would be named after the last path element
	// ("goldmark-alert") and `alert.X` in scripts would not resolve.
	e.Bind("github.com/yuin/goldmark-alert", map[string]runtime.Value{
		"NewParser":       alert.NewParser,
		"NewHTMLRenderer": alert.NewHTMLRenderer,
	}, minigo.BindAs("alert"))
	return nil
}
