package main

import (
	"github.com/podhmo/minigo"
	"github.com/podhmo/minigo/runtime"
	"github.com/podhmo/minigo/x/storybook"

	alert "github.com/yuin/goldmark-alert"
	"github.com/yuin/goldmark/v2/extension"
	gmparser "github.com/yuin/goldmark/v2/parser"
	gmhtml "github.com/yuin/goldmark/v2/renderer/html"
)

// Book is the shared storybook.Book: story discovery, the render loop,
// the index/grid browsing chrome, the static export, and /static/ asset
// serving all live in the library. This command adds the two things an
// application supplies on top: the host binds (bindApp, via Book.Setup)
// and the app's own mux mounted as Book.Fallback (see app.go).
type Book struct {
	*storybook.Book
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
