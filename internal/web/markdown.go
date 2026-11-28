package web

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"html/template"
	"log"
	"regexp"
	"sync"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
	"github.com/yuin/goldmark-alert"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
)

const (
	markdownCacheEntries   = 128
	markdownCacheMaxSource = 64 << 10
)

type markdownCacheEntry struct {
	key  [32]byte
	html template.HTML
}

// markdownLRU bounds display-render cache memory by entry count and source
// size. Goldmark output is immutable, so cached template.HTML values are safe
// to share across concurrent requests.
type markdownLRU struct {
	mu      sync.Mutex
	order   *list.List
	entries map[[32]byte]*list.Element
	cap     int
}

func newMarkdownLRU(capacity int) *markdownLRU {
	return &markdownLRU{
		order:   list.New(),
		entries: map[[32]byte]*list.Element{},
		cap:     capacity,
	}
}

func (c *markdownLRU) get(key [32]byte) (template.HTML, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		return "", false
	}
	c.order.MoveToFront(el)
	return el.Value.(markdownCacheEntry).html, true
}

func (c *markdownLRU) add(key [32]byte, html template.HTML) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		el.Value = markdownCacheEntry{key: key, html: html}
		c.order.MoveToFront(el)
		return
	}
	el := c.order.PushFront(markdownCacheEntry{key: key, html: html})
	c.entries[key] = el
	for c.order.Len() > c.cap {
		old := c.order.Back()
		c.order.Remove(old)
		delete(c.entries, old.Value.(markdownCacheEntry).key)
	}
}

var renderedMarkdown = newMarkdownLRU(markdownCacheEntries)

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

// markdownCacheKeyForWorkspace scopes the display cache key because issue
// reference expansion depends on the workspace.
func markdownCacheKeyForWorkspace(workspace string, src []byte) [32]byte {
	return markdownCacheKey(append(append([]byte(workspace), 0), src...))
}

func markdownCacheKey(src []byte) [32]byte {
	return [32]byte(sha256.Sum256(src))
}

var issueRefPattern = regexp.MustCompile(`(^|[^\w#])#([0-9]+)`)

// renderMarkdown renders s as markdown and returns safe HTML for direct
// insertion into templates.
func renderMarkdown(s string) template.HTML {
	return renderMarkdownForWorkspace(nil, s)
}

func renderMarkdownForWorkspace(ws *store.Workspace, s string) template.HTML {
	workspace := ""
	if ws != nil {
		workspace = ws.Slug
	}
	cacheable := len(s)+len(workspace) <= markdownCacheMaxSource
	src := []byte(s)
	var key [32]byte
	if cacheable {
		key = markdownCacheKeyForWorkspace(workspace, src)
		if cached, ok := renderedMarkdown.get(key); ok {
			return cached
		}
	}
	var buf bytes.Buffer
	doc := mdParser.Parse(src)
	if workspace != "" {
		linkifyIssueRefs(doc, src, workspace)
	}
	if err := mdRenderer.Render(&buf, src, doc); err != nil {
		log.Printf("web: markdown render: %v", err)
		return template.HTML(template.HTMLEscapeString(s))
	}
	out := template.HTML(buf.String())
	if cacheable {
		renderedMarkdown.add(key, out)
	}
	return out
}
