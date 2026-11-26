package web

import (
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
	"io"
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

	// Prove the second call really reads the cache rather than merely
	// rendering the same output twice: replacing the entry changes what the
	// next call returns. The sentinel input is test-only, so poisoning it
	// cannot affect real pages.
	key := markdownCacheKeyForWorkspace("", []byte(input))
	t.Cleanup(func() { renderedMarkdown.add(key, first) })
	renderedMarkdown.add(key, "<p>cache sentinel</p>")
	if got := renderMarkdown(input); got != "<p>cache sentinel</p>" {
		t.Fatalf("renderMarkdown after cache replacement = %q, want sentinel", got)
	}
}

func TestRenderMarkdownIssueRefs(t *testing.T) {
	ws := &store.Workspace{Slug: "default"}
	got := string(renderMarkdownForWorkspace(ws, "see #12 and #3"))
	if !strings.Contains(got, `href="/w/default/issues/12"`) || !strings.Contains(got, `>#12</a>`) {
		t.Fatalf("issue refs not linked: %s", got)
	}
}

func TestRenderMarkdownIssueRefsSkipCodeAndLinks(t *testing.T) {
	ws := &store.Workspace{Slug: "default"}
	got := string(renderMarkdownForWorkspace(ws, "`#12`\n\n```\n#12\n```\n\n[#12](/already)"))
	if strings.Count(got, `href="/w/default/issues/12"`) != 0 {
		t.Fatalf("code or existing link was rewritten: %s", got)
	}
}

func TestRenderMarkdownIssueRefCacheSeparatesWorkspaces(t *testing.T) {
	a := string(renderMarkdownForWorkspace(&store.Workspace{Slug: "alpha"}, "see #12"))
	b := string(renderMarkdownForWorkspace(&store.Workspace{Slug: "beta"}, "see #12"))
	if !strings.Contains(a, `/w/alpha/issues/12`) || !strings.Contains(b, `/w/beta/issues/12`) {
		t.Fatalf("workspace-specific cache entries collided:\na=%s\nb=%s", a, b)
	}
}

func TestMarkdownPreviewEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := srv.Client().Post(srv.URL+"/w/default/preview", "application/x-www-form-urlencoded", strings.NewReader("body=see+%2312"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, body %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `href="/w/default/issues/12"`) {
		t.Fatalf("preview missing workspace issue link: %s", body)
	}
}
