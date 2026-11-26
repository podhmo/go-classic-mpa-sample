package web

import (
	"strings"
	"testing"
)

func TestRenderMarkdownBasics(t *testing.T) {
	out := string(renderMarkdown("# Hi\n\nsome **bold** text"))
	for _, want := range []string{"<h1>Hi</h1>", "<strong>bold</strong>"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %s", want, out)
		}
	}
}

func TestRenderMarkdownCodeBlock(t *testing.T) {
	out := string(renderMarkdown("```go\nfmt.Println(\"<x>\")\n```"))
	if !strings.Contains(out, `<pre><code class="language-go">`) {
		t.Errorf("expected fenced code block with language class, got: %s", out)
	}
	// Code content is literal text; HTML inside a fence must be escaped.
	if !strings.Contains(out, "&lt;x&gt;") {
		t.Errorf("code content should be HTML-escaped, got: %s", out)
	}
}

func TestRenderMarkdownHardWraps(t *testing.T) {
	// Single newlines were preserved by pre-wrap in plaintext mode; keep
	// that behavior so existing bodies don't lose their line breaks.
	out := string(renderMarkdown("line1\nline2"))
	if !strings.Contains(out, "line1<br>") {
		t.Errorf("single newline should render as <br>, got: %s", out)
	}
}

func TestRenderMarkdownOmitsRawHTML(t *testing.T) {
	out := string(renderMarkdown("<script>alert(1)</script>"))
	if strings.Contains(out, "<script>") {
		t.Errorf("raw HTML must not reach the page, got: %s", out)
	}
}

func TestRenderMarkdownSanitizesLinkURLs(t *testing.T) {
	out := string(renderMarkdown("[x](javascript:alert(1))\n\n![i](javascript:alert(1))"))
	if strings.Contains(out, "javascript:") {
		t.Errorf("dangerous URL schemes must be stripped, got: %s", out)
	}
}

func TestRenderMarkdownAlert(t *testing.T) {
	out := string(renderMarkdown("> [!WARNING]\n> Be careful."))
	for _, want := range []string{
		`class="markdown-alert markdown-alert-warning"`,
		`class="markdown-alert-title"`,
		"<p>Be careful.</p>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("alert output missing %q, got: %s", want, out)
		}
	}

	// A blockquote without the [!KIND] marker stays a plain blockquote.
	if out := string(renderMarkdown("> plain")); !strings.Contains(out, "<blockquote>") {
		t.Errorf("plain quote should render as blockquote, got: %s", out)
	}
}

func TestRenderMarkdownGFM(t *testing.T) {
	out := string(renderMarkdown("- [x] done\n- [ ] todo\n\n~~old~~"))
	for _, want := range []string{`type="checkbox"`, "<del>old</del>"} {
		if !strings.Contains(out, want) {
			t.Errorf("GFM output missing %q, got: %s", want, out)
		}
	}
}

func TestMarkdownLRUEvictsLeastRecentlyUsed(t *testing.T) {
	cache := newMarkdownLRU(2)
	key1 := markdownCacheKey([]byte("one"))
	key2 := markdownCacheKey([]byte("two"))
	key3 := markdownCacheKey([]byte("three"))
	cache.add(key1, "<p>one</p>")
	cache.add(key2, "<p>two</p>")
	if got, ok := cache.get(key1); !ok || got != "<p>one</p>" {
		t.Fatalf("get key1 = %q/%v", got, ok)
	}
	cache.add(key3, "<p>three</p>")
	if _, ok := cache.get(key2); ok {
		t.Error("key2 should be evicted after key1 was refreshed")
	}
	if _, ok := cache.get(key1); !ok {
		t.Error("key1 should remain as a recently used entry")
	}
	if _, ok := cache.get(key3); !ok {
		t.Error("key3 should be cached")
	}
}

func TestRenderMarkdownUsesCacheWithoutChangingOutput(t *testing.T) {
	input := "# cached\n\nsome **bold** text"
	first := renderMarkdown(input)
	second := renderMarkdown(input)
	if first != second {
		t.Fatalf("cached output changed: %q then %q", first, second)
	}
	if !strings.Contains(string(second), "<strong>bold</strong>") {
		t.Errorf("cached output lost markdown rendering: %s", second)
	}
}
