package web

import (
	"bytes"
	"html/template"
	"log"

	"github.com/yuin/goldmark-alert"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
)

// mdParser/mdRenderer convert user-written markdown to HTML. GFM enables
// tables, strikethrough, task lists and bare-URL autolinking; the alert
// extension renders GitHub-style alert blockquotes (> [!NOTE] and friends).
// Raw HTML in the source is omitted (the default without html.WithUnsafe)
// and dangerous link schemes (javascript:, data:, ...) are neutralized, so
// user input cannot inject markup; the result is marked template.HTML
// because goldmark escapes text content itself.
//
// WithHardWraps keeps the pre-markdown behavior where a single newline in
// the textarea showed up as a line break on the page. Both the parser and
// the renderer are stateless per call and safe for concurrent use.
var (
	mdParser = parser.New(
		parser.WithExtensions(extension.NewGFMParser(), alert.NewParser()),
	)
	mdRenderer = html.New(
		html.WithExtensions(extension.NewGFMHTMLRenderer(), alert.NewHTMLRenderer()),
		html.WithHardWraps(),
	)
)

// renderMarkdown renders s as markdown and returns safe HTML for direct
// insertion into templates.
func renderMarkdown(s string) template.HTML {
	src := []byte(s)
	var buf bytes.Buffer
	if err := mdRenderer.Render(&buf, src, mdParser.Parse(src)); err != nil {
		log.Printf("web: markdown render: %v", err)
		return template.HTML(template.HTMLEscapeString(s))
	}
	return template.HTML(buf.String())
}
