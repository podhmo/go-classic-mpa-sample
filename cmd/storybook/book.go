package main

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/podhmo/minigo"
	"github.com/podhmo/minigo/resolve"
	"github.com/podhmo/minigo/runtime"

	alert "github.com/yuin/goldmark-alert"
	"github.com/yuin/goldmark/v2/extension"
	gmparser "github.com/yuin/goldmark/v2/parser"
	gmhtml "github.com/yuin/goldmark/v2/renderer/html"
)

// Book is one package's set of stories.
type Book struct {
	Dir       string   // package directory
	Tag       string   // build tag guarding the story files
	Src       []string // extra packages to interpret from source
	StaticDir string   // directory served under /static/ in -http mode

	once    sync.Once
	base    *minigo.Engine // configured once; each Render forks a session
	dir     string         // absolute Dir, computed with base
	baseErr error
}

// StoryKind distinguishes the two story shapes.
type StoryKind int

const (
	// KindPrint is `func StoryXxx()` writing HTML to stdout.
	KindPrint StoryKind = iota
	// KindHTTP is `func StoryHttpXxx(w http.ResponseWriter, r *http.Request)` —
	// handler-shaped, so the story exercises real handlers and the recorder
	// captures status code and headers too.
	KindHTTP
)

// Story is a story function found in a tagged file.
type Story struct {
	Name string // StoryXxx or StoryHttpXxx
	Doc  string // first line of the doc comment
	Pos  string // file:line
	Kind StoryKind
}

// IsHTTP reports whether the story is the handler-shaped kind.
func (s Story) IsHTTP() bool { return s.Kind == KindHTTP }

// Page is one rendered story: body plus, for handler-shaped stories, the
// recorded status code and response headers.
type Page struct {
	Status int
	Header map[string][]string
	Body   []byte
}

// Stories lists the story funcs declared in files that the build tag
// brings in: files the default build excludes but the tagged build
// selects. A Story-prefixed func of any other shape is an error, so a
// typo in the signature does not silently drop the story.
func (b *Book) Stories() ([]Story, error) {
	plain := build.Default
	tagged := build.Default
	tagged.BuildTags = append(slices.Clone(tagged.BuildTags), b.Tag)
	matches, err := filepath.Glob(filepath.Join(b.Dir, "*.go"))
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var stories []Story
	for _, path := range matches {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		inTagged, err := tagged.MatchFile(b.Dir, name)
		if err != nil {
			return nil, err
		}
		inPlain, err := plain.MatchFile(b.Dir, name)
		if err != nil {
			return nil, err
		}
		if !inTagged || inPlain {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || !strings.HasPrefix(fd.Name.Name, "Story") || !fd.Name.IsExported() {
				continue
			}
			pos := fset.Position(fd.Pos())
			kind := KindPrint
			if strings.HasPrefix(fd.Name.Name, "StoryHttp") {
				if !isHandlerShape(fd.Type) {
					return nil, fmt.Errorf("%s:%d: %s must be func(http.ResponseWriter, *http.Request)", pos.Filename, pos.Line, fd.Name.Name)
				}
				kind = KindHTTP
			} else if !isStoryShape(fd.Type) {
				return nil, fmt.Errorf("%s:%d: %s must be func()", pos.Filename, pos.Line, fd.Name.Name)
			}
			doc, _, _ := strings.Cut(strings.TrimSpace(fd.Doc.Text()), "\n")
			stories = append(stories, Story{Name: fd.Name.Name, Doc: doc, Pos: fmt.Sprintf("%s:%d", pos.Filename, pos.Line), Kind: kind})
		}
	}
	if len(stories) == 0 {
		return nil, fmt.Errorf("%s: no Story funcs in files guarded by //go:build %s", b.Dir, b.Tag)
	}
	slices.SortFunc(stories, func(x, y Story) int { return strings.Compare(x.Name, y.Name) })
	return stories, nil
}

// isStoryShape accepts only func() with no results or type parameters.
func isStoryShape(ft *ast.FuncType) bool {
	return (ft.TypeParams == nil || len(ft.TypeParams.List) == 0) &&
		(ft.Params == nil || len(ft.Params.List) == 0) &&
		(ft.Results == nil || len(ft.Results.List) == 0)
}

// isHandlerShape accepts func(http.ResponseWriter, *http.Request) — the
// http.HandlerFunc signature, detected structurally so the import alias
// does not matter.
func isHandlerShape(ft *ast.FuncType) bool {
	if (ft.TypeParams != nil && len(ft.TypeParams.List) > 0) ||
		(ft.Results != nil && len(ft.Results.List) > 0) ||
		ft.Params == nil || len(ft.Params.List) != 2 {
		return false
	}
	sel0, ok0 := ft.Params.List[0].Type.(*ast.SelectorExpr)
	star, ok1 := ft.Params.List[1].Type.(*ast.StarExpr)
	if !ok0 || !ok1 {
		return false
	}
	sel1, ok2 := star.X.(*ast.SelectorExpr)
	return ok2 && sel0.Sel.Name == "ResponseWriter" && sel1.Sel.Name == "Request"
}

// sourceModes are packages to interpret from source instead of the bound
// stdlib — empty by default; -src forces more for experiments. The bound
// html/template suffices because minigo adapts script FuncMap funcs into
// host calls; the one gap was ParseFS on a script embed.FS, which
// parsePages now avoids by reading embedded files itself.
var sourceModes = map[string]minigo.PackageMode{}

// Render runs one story in a session forked from the book's base engine:
// the session keeps the configuration and host bindings but starts with an
// empty package cache, so an edit to the package — template, Go code, or an
// //go:embed'ed file — shows up on the next call while bound packages stay
// loaded. Stories own their fixtures; the helper injects no HTTP values.
// Print stories answer Status 200 with an empty Header; handler stories
// report the recorder's result.
func (b *Book) Render(ctx context.Context, name string) (*Page, error) {
	stories, err := b.Stories()
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(stories, func(s Story) bool { return s.Name == name }) {
		return nil, fmt.Errorf("%s: not a story", name)
	}
	kind := KindPrint
	for _, st := range stories {
		if st.Name == name {
			kind = st.Kind
			break
		}
	}
	base, dir, err := b.baseEngine()
	if err != nil {
		return nil, err
	}
	s := base.NewSession()
	var output bytes.Buffer
	minigo.WithOutput(&output)(s)
	pkg, err := s.Package(ctx, dir)
	if err != nil {
		return nil, err
	}
	if kind == KindHTTP {
		// Handler-shaped story: drive it through a recorder so the
		// response status and headers (security headers, CSP, …) are
		// captured, not just the body. The request is a plain GET /;
		// stories wanting a specific path build their own with
		// httptest.NewRequest inside.
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if _, err := s.Call(ctx, pkg, name, rec, req); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		res := rec.Result()
		// the handler writes to the recorder, not to stdout: the page
		// body is the recorder's buffer, not the captured print output.
		return &Page{Status: res.StatusCode, Header: res.Header, Body: rec.Body.Bytes()}, nil
	}
	if _, err := s.Call(ctx, pkg, name); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &Page{Status: http.StatusOK, Body: output.Bytes()}, nil
}

// baseEngine lazily builds the long-lived engine all renders fork from.
// Build-time options (tags, package modes, host binds) live here; the
// per-request output buffer is installed on each session instead.
func (b *Book) baseEngine() (*minigo.Engine, string, error) {
	// the handler can serve concurrent first requests, so the build is
	// serialized: otherwise b.base could be observed before b.dir.
	b.once.Do(func() {
		dir, err := filepath.Abs(b.Dir)
		if err != nil {
			b.baseErr = err
			return
		}
		modes := maps.Clone(sourceModes)
		for _, p := range b.Src {
			modes[p] = minigo.ModeSource
		}
		e := minigo.NewEngine(dir,
			minigo.WithBuildConfig(resolve.BuildConfig{Tags: []string{b.Tag}}),
			minigo.WithPackageModes(modes),
		)
		bindApp(e)
		b.base, b.dir = e, dir
	})
	return b.base, b.dir, b.baseErr
}

// bindApp registers the host packages interpreted internal/web code
// needs but should never run from source:
//
//   - modernc.org/sqlite as an EMPTY package, so internal/store can be
//     interpreted (its `import _ "modernc.org/sqlite"` resolves to the
//     stub and its init side effect never happens) and store.Issue & co.
//     stay script structs the bound template can field-evaluate. The
//     sqlite code path is lazy anyway — stories never call store.Open.
//   - the goldmark packages markdown.go wires up at package init; keeping
//     them bound avoids interpreting a large third-party dependency.
func bindApp(e *minigo.Engine) {
	e.Bind("modernc.org/sqlite", map[string]runtime.Value{})

	e.Bind("github.com/yuin/goldmark/v2/parser", map[string]runtime.Value{
		"New":            minigo.WrapFunc("parser.New", gmparser.New),
		"WithExtensions": minigo.WrapFunc("parser.WithExtensions", gmparser.WithExtensions),
	})
	e.Bind("github.com/yuin/goldmark/v2/renderer/html", map[string]runtime.Value{
		"New":            minigo.WrapFunc("html.New", gmhtml.New),
		"WithExtensions": minigo.WrapFunc("html.WithExtensions", gmhtml.WithExtensions),
		"WithHardWraps":  minigo.WrapFunc("html.WithHardWraps", gmhtml.WithHardWraps),
	})
	e.Bind("github.com/yuin/goldmark/v2/extension", map[string]runtime.Value{
		"NewGFMParser":       minigo.WrapFunc("extension.NewGFMParser", extension.NewGFMParser),
		"NewGFMHTMLRenderer": minigo.WrapFunc("extension.NewGFMHTMLRenderer", extension.NewGFMHTMLRenderer),
	})
	e.Bind("github.com/yuin/goldmark-alert", map[string]runtime.Value{
		"NewParser":       minigo.WrapFunc("alert.NewParser", alert.NewParser),
		"NewHTMLRenderer": minigo.WrapFunc("alert.NewHTMLRenderer", alert.NewHTMLRenderer),
	})
	// Bind names a bound package after the last path element, but this
	// module's declared package name is "alert" — fix it up so scripts
	// referring to `alert.X` resolve. (feedback: Bind has no way to pass
	// the declared package name.)
	e.BoundOf("github.com/yuin/goldmark-alert").Name = "alert"
}
