package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// entry is one row of the index page.
type entry struct {
	Story
	Href   string // where the row's anchor points (serve: ?s=, export: Name.html)
	Src    string // what the preview iframe loads (serve: /story/, export: Name.html)
	Grid   string // this story's variants grid, for the toolbar's grid action
	Hay    string // lowercased haystack the filter matches: name + doc + group
	Target string // "story" in the static export, "_blank" for served http stories
	Hidden bool   // a non-match under ?q=: rendered but hidden so JS can restore it
	Err    string
}

// group is one render function's set of stories in the index. Ungrouped
// entries (handler stories) sit in the trailing group with an empty
// Name and no grid view: they are not variants of one template.
type group struct {
	Name    string
	Href    string // where the grid pill points (serve: ?g=, export: grid_X.html)
	Src     string // the grid document itself (serve: /grid/, export: grid_X.html)
	Target  string
	Hidden  bool // every entry filtered out under ?q=
	Entries []entry
}

// groupEntries buckets entries by their story's render function
// (Story.Group), ordered by group name; ungrouped entries keep their
// place in a trailing unnamed group.
func groupEntries(entries []entry) []group {
	var order []string
	byName := map[string]*group{}
	var rest []entry
	for _, e := range entries {
		if e.Group == "" {
			rest = append(rest, e)
			continue
		}
		g, ok := byName[e.Group]
		if !ok {
			g = &group{Name: e.Group}
			byName[e.Group] = g
			order = append(order, e.Group)
		}
		g.Entries = append(g.Entries, e)
	}
	slices.Sort(order)
	groups := make([]group, 0, len(order)+1)
	for _, n := range order {
		groups = append(groups, *byName[n])
	}
	if len(rest) > 0 {
		groups = append(groups, group{Entries: rest})
	}
	return groups
}

// gridRows filters a group's entries down to what the grid can frame:
// handler stories replay their app's real response headers, which can
// deny framing, so they stay index links only.
func gridRows(g group) []entry {
	var rows []entry
	for _, e := range g.Entries {
		if !e.IsHTTP() {
			rows = append(rows, e)
		}
	}
	return rows
}

// matchStory answers whether a story survives the ?q= filter: every
// whitespace-separated token must appear in the lowercased haystack of
// name + doc + group.
func matchStory(s Story, q string) bool {
	hay := strings.ToLower(s.Name + " " + s.Doc + " " + s.Group)
	for _, t := range strings.Fields(strings.ToLower(q)) {
		if !strings.Contains(hay, t) {
			return false
		}
	}
	return true
}

// haystack is the searchable text carried on each row's data-haystack
// attribute; the live filter matches it the same way matchStory does.
func haystack(s Story) string {
	return strings.ToLower(s.Name + " " + s.Doc + " " + s.Group)
}

// chromeTokens is the color-scheme token block shared by indexTmpl and
// gridTmpl: a neutral light/dark scheme, so the chrome follows the OS
// theme instead of boxing a dark story in light UI. Stories render
// inside their own iframe documents, so these tokens never reach the
// previewed page.
const chromeTokens = `
:root {
  color-scheme: light dark;
  --bg-light: #f6f8fa; --surface-light: #fff; --surface-hover-light: #fbfcfd;
  --border-light: #d0d7de; --border-muted-light: #e4e9ee;
  --text-light: #1f2328; --text-muted-light: #656d76;
  --accent-light: #0969da; --accent-soft-light: #ddf4ff; --danger-light: #cf222e;
  --bg-dark: #0d1117; --surface-dark: #151b23; --surface-hover-dark: #1b222c;
  --border-dark: #3d444d; --border-muted-dark: #2a313c;
  --text-dark: #f0f6fc; --text-muted-dark: #9198a1;
  --accent-dark: #4493f8; --accent-soft-dark: #0c2d4f; --danger-dark: #f85149;
  --bg: var(--bg-light); --surface: var(--surface-light); --surface-hover: var(--surface-hover-light);
  --border: var(--border-light); --border-muted: var(--border-muted-light);
  --text: var(--text-light); --text-muted: var(--text-muted-light);
  --accent: var(--accent-light); --accent-soft: var(--accent-soft-light); --danger: var(--danger-light);
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: var(--bg-dark); --surface: var(--surface-dark); --surface-hover: var(--surface-hover-dark);
    --border: var(--border-dark); --border-muted: var(--border-muted-dark);
    --text: var(--text-dark); --text-muted: var(--text-muted-dark);
    --accent: var(--accent-dark); --accent-soft: var(--accent-soft-dark); --danger: var(--danger-dark);
  }
}
@supports (color: light-dark(#000, #fff)) {
  :root {
    --bg: light-dark(var(--bg-light), var(--bg-dark));
    --surface: light-dark(var(--surface-light), var(--surface-dark));
    --surface-hover: light-dark(var(--surface-hover-light), var(--surface-hover-dark));
    --border: light-dark(var(--border-light), var(--border-dark));
    --border-muted: light-dark(var(--border-muted-light), var(--border-muted-dark));
    --text: light-dark(var(--text-light), var(--text-dark));
    --text-muted: light-dark(var(--text-muted-light), var(--text-muted-dark));
    --accent: light-dark(var(--accent-light), var(--accent-dark));
    --accent-soft: light-dark(var(--accent-soft-light), var(--accent-soft-dark));
    --danger: light-dark(var(--danger-light), var(--danger-dark));
  }
}
`

var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><meta name="color-scheme" content="light dark"><link rel="icon" href="data:,"><title>stories: {{.Dir}}</title>{{if .ScriptTag}}{{.ScriptTag}}{{end}}
<style>` + chromeTokens + `
body { margin: 0; display: flex; height: 100vh; height: 100dvh; font-family: system-ui, sans-serif; font-size: 14px; background: var(--bg); color: var(--text); }
nav { width: 19rem; flex: none; display: flex; flex-direction: column; background: var(--surface); border-right: 1px solid var(--border); }
nav > header { padding: .75rem .875rem .625rem; border-bottom: 1px solid var(--border-muted); }
.brand { display: flex; align-items: baseline; gap: .5rem; font-size: .85rem; font-weight: 600; }
.brand .total { font-size: .72rem; font-weight: 400; color: var(--text-muted); }
.dir { margin-top: .15rem; font-size: .72rem; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; color: var(--text-muted); word-break: break-all; }
.err { color: var(--danger); }
search { display: block; margin-top: .625rem; }
search form { display: flex; gap: .375rem; }
#q { flex: 1; min-width: 0; font: inherit; font-size: .82rem; padding: .3rem .55rem; color: var(--text); background: var(--bg); border: 1px solid var(--border); border-radius: 6px; }
#q::placeholder { color: var(--text-muted); }
#q:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; border-color: var(--accent); }
.qgo { font: inherit; font-size: .78rem; padding: .3rem .6rem; color: var(--text-muted); background: var(--surface); border: 1px solid var(--border); border-radius: 6px; cursor: pointer; }
.qgo:hover { color: var(--accent); border-color: var(--accent); }
.js .qgo { display: none; }
.meta { margin-top: .4rem; font-size: .72rem; color: var(--text-muted); }
.meta:empty { display: none; }
.list { flex: 1; overflow-y: auto; padding: .25rem .5rem .75rem; container-type: scroll-state; }
.grp-h { display: flex; align-items: center; gap: .5rem; margin-top: .7rem; padding: .3rem .5rem; position: sticky; top: -.25rem; background: var(--surface); z-index: 2; border-bottom: 1px solid var(--border-muted); }
.grp:first-child .grp-h { margin-top: .15rem; }
.grp-name { font-size: .7rem; font-weight: 600; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; color: var(--text-muted); letter-spacing: .02em; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.grp-n { margin-inline-start: auto; font-size: .68rem; color: var(--text-muted); background: var(--bg); border: 1px solid var(--border-muted); border-radius: 999px; padding: 0 .4rem; }
.gpill { flex: none; font-size: .7rem; padding: .08rem .45rem; color: var(--text-muted); text-decoration: none; background: var(--surface); border: 1px solid var(--border); border-radius: 999px; }
.gpill:hover { color: var(--accent); border-color: var(--accent); }
.gpill.active { color: var(--accent); background: var(--accent-soft); border-color: var(--accent); font-weight: 600; }
.story { display: block; margin-top: .15rem; padding: .3rem .5rem; border-left: 2px solid transparent; border-radius: 0 6px 6px 0; color: inherit; text-decoration: none; font-size: .8rem; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; line-height: 1.4; overflow-wrap: anywhere; }
.story:hover { background: var(--surface-hover); }
.story[hidden], .grp[hidden] { display: none; }
.story:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
.story.active { background: var(--accent-soft); border-left-color: var(--accent); color: var(--accent); font-weight: 600; }
.story .ext { color: var(--text-muted); font-size: .8em; }
.story small { display: block; font-size: .72rem; font-weight: 400; font-family: system-ui, sans-serif; color: var(--text-muted); }
.none { padding: .75rem .5rem; font-size: .78rem; color: var(--text-muted); }
.list::before, .list::after { content: ""; display: block; position: sticky; block-size: .5rem; z-index: 1; pointer-events: none; opacity: 0; transition: opacity .2s; }
.list::before { inset-block-start: 0; margin-block-end: -.5rem; background: radial-gradient(farthest-side at 50% 0, rgb(0 0 0 / .18), transparent); }
.list::after { inset-block-end: 0; margin-block-start: -.5rem; background: radial-gradient(farthest-side at 50% 100%, rgb(0 0 0 / .18), transparent); }
@container scroll-state(scrollable: block-start) { .list::before { opacity: 1; } }
@container scroll-state(scrollable: block-end) { .list::after { opacity: 1; } }
@supports not (container-type: scroll-state) {
  .list::before, .list::after { animation: edge-shadow linear both; animation-timeline: scroll(); }
  .list::before { animation-range: 0 2rem; }
  .list::after { animation-direction: reverse; animation-range: calc(100% - 2rem) 100%; }
}
@keyframes edge-shadow { to { opacity: 1; } }
.stage { flex: 1; min-width: 0; display: flex; flex-direction: column; background: var(--bg); }
.bar { display: flex; align-items: center; gap: .5rem; padding: .4rem .75rem; font-size: .78rem; background: var(--surface); border-bottom: 1px solid var(--border); }
.bar .crumb { font-size: .72rem; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; color: var(--text-muted); }
.bar .cur { font-size: .8rem; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bar .pos { font-size: .7rem; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bact { display: flex; gap: .375rem; margin-inline-start: auto; }
.blink { font-size: .72rem; padding: .15rem .55rem; color: var(--text-muted); text-decoration: none; background: var(--surface); border: 1px solid var(--border); border-radius: 999px; }
.blink:hover { color: var(--accent); border-color: var(--accent); }
.blink[hidden] { display: none; }
.viewport { display: flex; gap: .25rem; margin-inline-start: .5rem; font-size: .72rem; }
.viewport a { padding: .15rem .45rem; text-align: center; color: var(--text-muted); text-decoration: none; background: var(--surface); border: 1px solid var(--border); border-radius: 6px; }
.viewport a:hover { color: var(--accent); border-color: var(--accent); }
.viewport a.active { color: var(--accent); background: var(--accent-soft); border-color: var(--accent); font-weight: 600; }
#frame { flex: 1; border: 0; background: var(--surface); }
#frame.sized { flex: none; margin-inline: auto; border-inline: 1px dashed var(--border); }
</style></head>
<body>
<nav>
<header>
<div class="brand">stories <span class="total">{{.Total}}</span></div>
<div class="dir">{{.Dir}}</div>
{{if .Err}}<small class="err">{{.Err}}</small>{{end}}
<search><form action="" method="get">
<input id="q" name="q" type="search" value="{{.Q}}" placeholder="filter stories — press /" autocomplete="off" autocorrect="off" autocapitalize="none" spellcheck="false" enterkeyhint="go" aria-label="filter stories">
{{if .S}}<input type="hidden" name="s" value="{{.S}}">{{end}}{{if .G}}<input type="hidden" name="g" value="{{.G}}">{{end}}{{if .W}}<input type="hidden" name="w" value="{{.W}}">{{end}}
<button class="qgo" type="submit">filter</button>
</form></search>
<div class="meta" id="count">{{if .Q}}{{.Matched}} of {{.Total}} shown{{end}}</div>
</header>
<div class="list" id="list">
{{range .Groups}}<section class="grp"{{if .Hidden}} hidden{{end}}>
{{if .Name}}<div class="grp-h"><span class="grp-name">{{.Name}}</span><span class="grp-n">{{len .Entries}}</span><a class="gpill{{if eq .Name $.G}} active{{end}}" href="{{.Href}}"{{if .Target}} target="{{.Target}}"{{end}} data-kind="grid" data-src="{{.Src}}" data-name="{{.Name}}" title="all variants side by side">&#8858; grid</a></div>{{end}}
{{range .Entries}}<a class="story{{if eq .Name $.S}} active{{end}}" href="{{.Href}}"{{if .Target}} target="{{.Target}}"{{end}}{{if .Src}} data-kind="story" data-src="{{.Src}}"{{end}} data-name="{{.Name}}" data-group="{{.Group}}" data-pos="{{.Pos}}" data-grid="{{.Grid}}" data-haystack="{{.Hay}}" title="{{.Pos}}"{{if eq .Name $.S}} aria-current="true"{{end}}{{if .Hidden}} hidden{{end}}>{{.Name}}{{if .IsHTTP}}<span class="ext" title="opens in a new tab">&#8599;</span>{{end}}
{{if .Doc}}<small>{{.Doc}}</small>{{end}}{{if .Err}}<small class="err">{{.Err}}</small>{{end}}</a>
{{end}}</section>
{{end}}<p class="none" id="nonemsg"{{if not .None}} hidden{{end}}>no stories match &#8220;{{.Q}}&#8221;</p>
</div>
</nav>
<div class="stage">
<div class="bar">
<span class="crumb" id="bg">{{.BarGroup}}</span>
<strong class="cur" id="bn">{{.BarName}}</strong>
<span class="pos" id="bp">{{.BarPos}}</span>
<span class="bact">
<a class="blink" id="bgrid" href="{{.BarGridHref}}" data-kind="grid"{{if .BarGridSrc}} data-src="{{.BarGridSrc}}" data-name="{{.BarGroup}}"{{else}} hidden{{end}}>&#8858; grid</a>
<a class="blink" id="braw" href="{{.BarRaw}}" target="_blank" rel="noopener">open &#8599;</a>
</span>
<div class="viewport">{{range .Widths}}<a class="wlink{{if eq . $.W}} active{{end}}" href="?w={{.}}&s={{$.S}}&g={{$.G}}&q={{$.Q}}" data-w="{{.}}">{{if .}}{{.}}px{{else}}full{{end}}</a>{{end}}</div>
</div>
<iframe name="story" id="frame"{{if .Src}} src="{{.Src}}"{{end}}{{if .Sized}} class="sized" style="width: {{.W}}px"{{end}}></iframe>
</div>
<script>
(function () {
  document.body.classList.add('js');
  var frame = document.getElementById('frame');
  var q = document.getElementById('q');
  var list = document.getElementById('list');
  var count = document.getElementById('count');
  var nonemsg = document.getElementById('nonemsg');
  var barG = document.getElementById('bg'), barN = document.getElementById('bn');
  var barP = document.getElementById('bp'), barRaw = document.getElementById('braw');
  var barGrid = document.getElementById('bgrid');
  var rows = [].slice.call(list.querySelectorAll('a.story'));
  var pills = [].slice.call(list.querySelectorAll('a.gpill'));
  var wlinks = [].slice.call(document.querySelectorAll('a.wlink'));
  var cur = { s: '', g: '', w: '' };

  function find(list, name) {
    for (var i = 0; i < list.length; i++) if (list[i].dataset.name === name) return list[i];
    return null;
  }
  function buildURL() {
    var p = new URLSearchParams();
    if (cur.s) p.set('s', cur.s);
    if (cur.g) p.set('g', cur.g);
    if (cur.w) p.set('w', cur.w);
    var t = q.value.trim();
    if (t) p.set('q', t);
    var qs = p.toString();
    return qs ? '?' + qs : location.pathname;
  }
  function activate(el, push) {
    var w = cur.w;
    if (el.dataset.kind === 'grid') {
      cur.g = el.dataset.name; cur.s = '';
      frame.classList.remove('sized'); frame.style.width = '';
      var src = el.dataset.src + (w ? (el.dataset.src.indexOf('?') < 0 ? '?' : '&') + 'w=' + w : '');
      if (frame.getAttribute('src') !== src) frame.src = src;
      barG.textContent = ''; barN.textContent = el.dataset.name; barP.textContent = '';
      barRaw.href = el.dataset.src;
      barGrid.hidden = true;
      pills.forEach(function (p) { p.classList.toggle('active', p.dataset.name === cur.g); });
      rows.forEach(function (a) { a.classList.remove('active'); a.removeAttribute('aria-current'); });
    } else {
      cur.s = el.dataset.name; cur.g = '';
      frame.classList.toggle('sized', !!w); frame.style.width = w ? w + 'px' : '';
      if (frame.getAttribute('src') !== el.dataset.src) frame.src = el.dataset.src;
      barG.textContent = el.dataset.group || ''; barN.textContent = el.dataset.name; barP.textContent = el.dataset.pos || '';
      barRaw.href = el.dataset.src;
      if (el.dataset.grid) {
        barGrid.hidden = false;
        barGrid.dataset.src = el.dataset.grid; barGrid.dataset.name = el.dataset.group;
        barGrid.href = '?g=' + el.dataset.group;
      } else { barGrid.hidden = true; }
      rows.forEach(function (a) {
        var on = a.dataset.name === cur.s;
        a.classList.toggle('active', on);
        if (on) a.setAttribute('aria-current', 'true'); else a.removeAttribute('aria-current');
      });
      pills.forEach(function (p) { p.classList.remove('active'); });
    }
    if (push !== false) history.pushState(null, '', buildURL());
  }
  function setW(w, push) {
    cur.w = w || '';
    wlinks.forEach(function (a) { a.classList.toggle('active', a.dataset.w === cur.w); });
    if (cur.g) {
      var el = find(pills, cur.g);
      if (el) activate(el, false);
    } else {
      frame.classList.toggle('sized', !!cur.w);
      frame.style.width = cur.w ? cur.w + 'px' : '';
    }
    if (push !== false) history.pushState(null, '', buildURL());
  }
  function applyFilter() {
    var tokens = q.value.trim().toLowerCase().split(/\s+/).filter(Boolean);
    var shown = 0;
    [].slice.call(list.querySelectorAll('section.grp')).forEach(function (sec) {
      var vis = 0;
      [].slice.call(sec.querySelectorAll('a.story')).forEach(function (a) {
        var ok = tokens.every(function (t) { return a.dataset.haystack.indexOf(t) >= 0; });
        a.hidden = !ok;
        if (ok) vis++;
      });
      sec.hidden = vis === 0;
      shown += vis;
    });
    count.textContent = tokens.length ? shown + ' of ' + rows.length + ' shown' : '';
    nonemsg.hidden = shown > 0;
    if (!nonemsg.hidden) nonemsg.textContent = 'no stories match \u201c' + q.value.trim() + '\u201d';
    var p = new URLSearchParams(location.search);
    if (q.value.trim()) p.set('q', q.value.trim()); else p.delete('q');
    var qs = p.toString();
    history.replaceState(null, '', qs ? '?' + qs : location.pathname);
  }
  function visibleRows() { return rows.filter(function (a) { return !a.hidden; }); }

  document.addEventListener('click', function (e) {
    var a = e.target.closest('a[data-src]');
    if (!a || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button) return;
    e.preventDefault();
    activate(a);
  });
  document.addEventListener('click', function (e) {
    var a = e.target.closest('a.wlink');
    if (!a || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button) return;
    e.preventDefault();
    setW(a.dataset.w);
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === '/' && document.activeElement !== q && !/^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement.tagName)) {
      e.preventDefault(); q.focus(); q.select();
    }
  });
  q.addEventListener('input', applyFilter);
  q.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') { q.value = ''; applyFilter(); }
    else if (e.key === 'Enter') {
      e.preventDefault();
      var a = visibleRows()[0];
      if (a) { if (a.dataset.src) activate(a); else a.click(); }
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      var v = visibleRows();
      if (v[0]) v[0].focus();
    }
  });
  q.form.addEventListener('submit', function (e) { e.preventDefault(); });
  list.addEventListener('keydown', function (e) {
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    var v = visibleRows();
    var i = v.indexOf(document.activeElement);
    if (i < 0) return;
    e.preventDefault();
    var n = v[i + (e.key === 'ArrowDown' ? 1 : -1)];
    if (n) n.focus();
  });
  window.addEventListener('popstate', function () {
    var p = new URLSearchParams(location.search);
    if (q.value !== (p.get('q') || '')) { q.value = p.get('q') || ''; applyFilter(); }
    cur.w = p.get('w') || '';
    wlinks.forEach(function (a) { a.classList.toggle('active', a.dataset.w === cur.w); });
    if (p.get('g')) { var el = find(pills, p.get('g')); if (el) activate(el, false); }
    else if (p.get('s')) { var el2 = find(rows, p.get('s')); if (el2 && el2.dataset.src) activate(el2, false); }
  });
  list.addEventListener('scroll', function () {
    try { sessionStorage.setItem('sb_scroll', list.scrollTop); } catch (e) {}
  });

  // Boot: a static export answers ?s/?g/?w/?q only through this script —
  // there is no server to render the selection — so the params are
  // applied here. Under -http the same assignments are idempotent.
  var p = new URLSearchParams(location.search);
  if (p.get('q') && q.value !== p.get('q')) { q.value = p.get('q'); }
  if (q.value.trim()) applyFilter();
  cur.w = p.get('w') || '';
  if (p.get('g')) { var el = find(pills, p.get('g')); if (el) activate(el, false); }
  else if (p.get('s')) { var el2 = find(rows, p.get('s')); if (el2 && el2.dataset.src) activate(el2, false); }
  try { var y = sessionStorage.getItem('sb_scroll'); if (y) list.scrollTop = +y; } catch (e) {}
})();
</script>
</body></html>
`))

// gridTmpl lays one render function's stories side by side, so a CSS or
// template edit can be checked against every data state at once. Each
// variant is an iframe at the chosen width (?w=, flexible when unset).
var gridTmpl = template.Must(template.New("grid").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><meta name="color-scheme" content="light dark"><link rel="icon" href="data:,"><title>{{.Name}} variants</title>{{if .ScriptTag}}{{.ScriptTag}}{{end}}
<style>` + chromeTokens + `
body { margin: 0; display: flex; flex-direction: column; height: 100vh; height: 100dvh; font-family: system-ui, sans-serif; background: var(--bg); color: var(--text); }
header { display: flex; gap: .75rem; align-items: center; padding: .4rem .75rem; background: var(--surface); border-bottom: 1px solid var(--border); }
header .back { font-size: .72rem; padding: .15rem .55rem; color: var(--text-muted); text-decoration: none; background: var(--surface); border: 1px solid var(--border); border-radius: 999px; }
header .back:hover { color: var(--accent); border-color: var(--accent); }
header strong { font-size: .8rem; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
header .count { font-size: .72rem; color: var(--text-muted); }
header .viewport { margin-inline-start: auto; }
.viewport { display: flex; gap: .25rem; font-size: .72rem; }
.viewport a { padding: .15rem .45rem; text-align: center; color: var(--text-muted); text-decoration: none; background: var(--surface); border: 1px solid var(--border); border-radius: 6px; }
.viewport a:hover { color: var(--accent); border-color: var(--accent); }
.viewport a.active { color: var(--accent); background: var(--accent-soft); border-color: var(--accent); font-weight: 600; }
.row { flex: 1; display: flex; overflow-x: auto; }
figure { flex: 1 1 0; min-width: 0; margin: 0; display: flex; flex-direction: column; background: var(--surface); }
figure.sized { flex: none; }
figure + figure { border-left: 1px solid var(--border); }
figure iframe { flex: 1; border: 0; width: 100%; }
figcaption { display: flex; align-items: baseline; gap: .5rem; padding: .3rem .6rem; font-size: .72rem; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; color: var(--text-muted); border-top: 1px solid var(--border-muted); }
figcaption a { color: inherit; text-decoration: none; }
figcaption a:hover { color: var(--accent); }
figcaption .go { margin-inline-start: auto; font-size: .85em; }
</style></head>
<body>
<header><a class="back" href="{{.Index}}"{{if .IndexTarget}} target="{{.IndexTarget}}"{{end}}>&#8592; index</a><strong>{{.Name}}</strong><span class="count">{{len .Stories}} variants</span>
<div class="viewport">{{range .Widths}}<a href="?w={{.}}"{{if eq . $.W}} class="active"{{end}}>{{if .}}{{.}}px{{else}}flex{{end}}</a>{{end}}</div>
</header>
<div class="row">
{{range .Stories}}<figure{{if $.W}} class="sized"{{end}}><iframe src="{{.Src}}"{{if $.W}} style="width: {{$.W}}px"{{end}}></iframe><figcaption><a href="{{$.Index}}?s={{.Name}}"{{if $.IndexTarget}} target="{{$.IndexTarget}}"{{end}} title="show in the index">{{.Name}}</a><a class="go" href="{{.Src}}" target="_blank" rel="noopener" title="open raw">&#8599;</a></figcaption></figure>
{{end}}</div>
</body></html>
`))

// Export renders every story into dir: index.html plus <Story>.html and
// a copy of the static directory, so serving the export root resolves
// /static/ the same way the live preview does. A failing story still
// gets its row (with the error) so the rest of the book is written, but
// Export then reports the failures.
func (b *Book) Export(ctx context.Context, dir string, stories []Story) error {
	// The containment checks below compare against the absolute static
	// root, so the output must be absolute too — a relative -o would
	// otherwise let dst compare unequal to src and RemoveAll it.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	dir = absDir
	// Fail fast on pathological layouts before writing anything: the
	// snapshot must neither write inside the static source (recursive
	// self-copy) nor contain it (the re-export cleanup would erase it).
	src := b.staticRoot()
	srcOK := false
	if src != "" {
		// The checks run whenever a static root is named — not only
		// when it is a directory: a `static` that is a regular file or
		// a path os.Stat cannot read must not pass the checks and reach
		// the unconditional cleanup below.
		dst := filepath.Join(dir, "static")
		// Compare link-resolved paths: a symlinked static root can
		// hide containment that the literal paths do not show. The
		// literal check still matters — a source reached through a
		// link inside the output dies with the re-export cleanup.
		realSrc, realDst := resolvePath(src), resolvePath(dst)
		inside := func(a, b string) bool { // a inside-or-equal b
			rel, err := filepath.Rel(b, a)
			return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
		}
		if inside(dst, src) || inside(realDst, realSrc) {
			return fmt.Errorf("export output must not be inside the static directory: %s", dst)
		}
		if inside(src, dst) || inside(realSrc, realDst) {
			return fmt.Errorf("static directory must not be inside the export output: %s", src)
		}
		if info, err := os.Stat(src); err == nil && info.IsDir() {
			srcOK = true
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var errs []error
	entries := make([]entry, len(stories))
	for i, s := range stories {
		e := entry{Story: s, Href: s.Name + ".html", Src: s.Name + ".html", Target: "story", Hay: haystack(s)}
		if s.Group != "" {
			e.Grid = "grid_" + s.Group + ".html"
		}
		entries[i] = e
		res, err := b.Render(ctx, s.Name)
		if err != nil {
			entries[i].Err = firstLine(err.Error())
			errs = append(errs, err)
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, s.Name+".html"), res.Body, 0o644); err != nil {
			return err
		}
	}
	// The variants grid exports too: one page per render function whose
	// iframes point at the exported story pages.
	groups := groupEntries(entries)
	for i := range groups {
		if groups[i].Name == "" {
			continue
		}
		groups[i].Href = "grid_" + groups[i].Name + ".html"
		groups[i].Src = groups[i].Href
		groups[i].Target = "story"
		f, err := os.Create(filepath.Join(dir, groups[i].Href))
		if err != nil {
			return err
		}
		err = gridTmpl.Execute(f, map[string]any{
			"Index": "index.html",
			// The grid renders inside the story iframe; the back link
			// must leave it, not nest a second index inside the first.
			"IndexTarget": "_top",
			"Name":        groups[i].Name,
			"Stories":     gridRows(groups[i]),
		})
		f.Close()
		if err != nil {
			return err
		}
	}
	f, err := os.Create(filepath.Join(dir, "index.html"))
	if err != nil {
		return err
	}
	defer f.Close()
	var def *entry
	for i := range entries {
		if !entries[i].IsHTTP() {
			def = &entries[i]
			break
		}
	}
	td := map[string]any{"Dir": b.Dir, "Groups": groups, "Total": len(stories), "Widths": []string{"", "320", "360", "768"}}
	if def != nil {
		td["Src"] = def.Src
		td["BarGroup"] = def.Group
		td["BarName"] = def.Name
		td["BarPos"] = def.Pos
		td["BarRaw"] = def.Src
		if def.Group != "" {
			td["BarGridHref"] = "?g=" + def.Group
			td["BarGridSrc"] = def.Grid
		}
	}
	if err := indexTmpl.Execute(f, td); err != nil {
		return err
	}
	// The snapshot mirrors the static source: a fresh copy drops assets
	// deleted upstream since the last export, and a vanished source
	// means no static in the output either. The containment checks
	// above keep the removal safe.
	dst := filepath.Join(dir, "static")
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if srcOK {
		if err := copyTree(src, dst); err != nil {
			return err
		}
	}
	return errors.Join(errs...)
}

// copyTree copies regular files under src into dst, preserving the
// layout. Symlinks resolve the way http.FileServer resolves them: linked
// assets land in the snapshot, and an alias naming a directory already
// copied is copied again under its own name. What is refused is a link
// cycle — a directory on its own copy path (self -> .) — tracked by the
// real paths currently on the copy stack, not by every directory seen;
// a dangling link, which the live server would answer 404, is skipped.
func copyTree(src, dst string) error {
	real, err := filepath.EvalSymlinks(src)
	if err != nil {
		return err
	}
	return copyDir(real, dst, map[string]bool{real: true})
}

func copyDir(dir, dst string, active map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		to := filepath.Join(dst, e.Name())
		mode := e.Type()
		if mode&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				continue
			}
			info, err := os.Stat(target)
			if err != nil {
				continue
			}
			if info.IsDir() {
				if active[target] {
					continue
				}
				active[target] = true
				err := copyDir(target, to, active)
				delete(active, target)
				if err != nil {
					return err
				}
				continue
			}
			mode = info.Mode().Type()
		}
		if e.IsDir() {
			real, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			active[real] = true
			err = copyDir(real, to, active)
			delete(active, real)
			if err != nil {
				return err
			}
			continue
		}
		if !mode.IsRegular() {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Handler serves the index at /, each story at /story/<Name>, and the
// application's static assets under /static/ (default <pkgdir>/static),
// so previews pick up stylesheet edits on a plain browser reload. A fresh
// interpretation on each request reflects source and template edits;
// CSS needs no re-execution at all. Story failures answer 500. Print
// stories answer 200; handler-shaped stories replay the status and
// headers their recorder captured.
func (b *Book) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		stories, err := b.Stories()
		if err != nil {
			// No stories yet (or a malformed one): show the error in the
			// nav rather than failing, so the book is usable while the
			// stories are written.
			if err := indexTmpl.Execute(w, map[string]any{
				"Dir":       b.Dir,
				"Err":       err.Error(),
				"ScriptTag": b.ScriptTag(),
			}); err != nil {
				slog.ErrorContext(r.Context(), "index", "err", err)
			}
			return
		}
		wpx := r.URL.Query().Get("w")
		q := r.URL.Query().Get("q")
		state := ""
		if wpx != "" {
			state += "&w=" + wpx
		}
		if q != "" {
			state += "&q=" + url.QueryEscape(q)
		}
		// Non-matching stories are rendered with a hidden marker rather
		// than omitted: no-JS readers still get the filtered view, and
		// the live filter can un-hide rows when the query changes.
		var entries []entry
		matched := 0
		for _, s := range stories {
			e := entry{Story: s, Hay: haystack(s), Hidden: q != "" && !matchStory(s, q)}
			if s.IsHTTP() {
				// Handler stories replay the app's real response headers,
				// which can include frame-ancestors 'none' / X-Frame-Options:
				// DENY — they may not render inside the iframe, so they
				// open in a new tab with the headers intact.
				e.Href = "/story/" + s.Name
				e.Target = "_blank"
			} else {
				e.Href = "?s=" + s.Name + state
				e.Src = "/story/" + s.Name
				if s.Group != "" {
					e.Grid = "/grid/" + s.Group
				}
			}
			if !e.Hidden {
				matched++
			}
			entries = append(entries, e)
		}
		// ?s=<Name> selects the previewed story so width switching (a full
		// index reload) keeps it; an unknown or missing name falls back to
		// the first non-HTTP entry.
		sel := r.URL.Query().Get("s")
		var selEntry *entry
		for i := range entries {
			if entries[i].Name == sel && !entries[i].IsHTTP() {
				selEntry = &entries[i]
				break
			}
		}
		if selEntry == nil {
			sel = ""
			for i := range entries {
				if !entries[i].IsHTTP() {
					sel = entries[i].Name
					selEntry = &entries[i]
					break
				}
			}
			if selEntry == nil {
				// The filter may hide every story; still preview a valid one.
				for _, s := range stories {
					if !s.IsHTTP() {
						sel = s.Name
						selEntry = &entry{Story: s, Src: "/story/" + s.Name}
						break
					}
				}
			}
		}
		groups := groupEntries(entries)
		for i := range groups {
			visible := false
			for _, e := range groups[i].Entries {
				if !e.Hidden {
					visible = true
					break
				}
			}
			groups[i].Hidden = !visible
		}
		// ?g=<renderFunc> previews the variants grid inside the index
		// frame, the same pane stories render in; the grid page itself
		// stays reachable for a full-width compare.
		g := r.URL.Query().Get("g")
		src := ""
		if g != "" {
			for _, gr := range groups {
				if gr.Name == g {
					src = "/grid/" + g
					if wpx != "" {
						src += "?w=" + wpx
					}
				}
			}
			if src == "" {
				g = ""
			}
		}
		for i := range groups {
			if groups[i].Name == "" {
				continue
			}
			groups[i].Href = "?g=" + groups[i].Name + state
			groups[i].Src = "/grid/" + groups[i].Name
		}
		td := map[string]any{
			"Dir":       b.Dir,
			"Groups":    groups,
			"S":         sel,
			"G":         g,
			"W":         wpx,
			"Q":         q,
			"Total":     len(stories),
			"Matched":   matched,
			"None":      q != "" && matched == 0,
			"Widths":    []string{"", "320", "360", "768"},
			"ScriptTag": b.ScriptTag(),
		}
		if g != "" {
			td["Src"] = src
			td["BarName"] = g
			td["BarRaw"] = "/grid/" + g
		} else if selEntry != nil {
			td["Src"] = selEntry.Src
			td["Sized"] = wpx != ""
			td["BarGroup"] = selEntry.Group
			td["BarName"] = selEntry.Name
			td["BarPos"] = selEntry.Pos
			td["BarRaw"] = selEntry.Src
			if selEntry.Group != "" {
				td["BarGridHref"] = "?g=" + selEntry.Group + state
				td["BarGridSrc"] = "/grid/" + selEntry.Group
			}
		}
		if err := indexTmpl.Execute(w, td); err != nil {
			slog.ErrorContext(r.Context(), "index", "err", err)
		}
	})
	mux.HandleFunc("GET /grid/{group}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("group")
		stories, err := b.Stories()
		if err != nil {
			b.errorPage(w, http.StatusInternalServerError, err)
			return
		}
		var entries []entry
		for _, s := range stories {
			if s.Group == name && !s.IsHTTP() {
				entries = append(entries, entry{Story: s, Src: "/story/" + s.Name})
			}
		}
		if len(entries) == 0 {
			b.errorPage(w, http.StatusNotFound, errors.New("no stories in this group"))
			return
		}
		if err := gridTmpl.Execute(w, map[string]any{
			"Index": "/",
			// The grid also renders inside the index's preview iframe
			// (?g=): its index/story links must leave the frame, not
			// nest a second index inside it. Standalone, _top is the
			// same tab anyway.
			"IndexTarget": "_top",
			"Name":        name,
			"Stories":     entries,
			"W":           r.URL.Query().Get("w"),
			"Widths":      []string{"", "320", "360", "768"},
			"ScriptTag":   b.ScriptTag(),
		}); err != nil {
			slog.ErrorContext(r.Context(), "grid", "err", err)
		}
	})
	mux.HandleFunc("GET /story/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		res, err := b.Render(r.Context(), name)
		if err != nil {
			b.errorPage(w, http.StatusInternalServerError, err)
			return
		}
		// Handler-shaped stories recorded real response headers and a
		// status — replay them so the preview exercises what the app's
		// handler emits (security headers, CSP, non-200 states).
		h := w.Header()
		for k, vs := range res.Header {
			h[k] = vs
		}
		if len(res.Body) > 0 && h.Get("Content-Type") == "" {
			h.Set("Content-Type", "text/html; charset=utf-8")
		}
		// A story opened in its own tab reloads too — only when the
		// response is HTML, so non-HTML bodies stay byte-identical.
		body := b.AppendLiveReload(h, res.Body)
		w.WriteHeader(res.Status)
		w.Write(body)
	})
	mux.Handle("GET /static/", b.staticHandler())
	// Everything else — app paths like /issues/N, any POST — routes
	// through the app's real mux via the StoryHttpApp story, so the
	// storybook doubles as a fake-data instance of the app and browser
	// JS (partial-submit.js) has live endpoints to hit.
	mux.Handle("/", b.appHandler())
	b.MountLiveReload(mux)
	return mux
}

// errorPage answers an error response. While watching it is a small
// HTML page carrying the reload script — a text/plain error cannot run
// it, and a page that loses the script never rejoins the reload loop,
// so a transient mid-edit failure would stick until a manual refresh.
func (b *Book) errorPage(w http.ResponseWriter, status int, err error) {
	if tag := b.ScriptTag(); tag != "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		fmt.Fprintf(w, "<pre>%s</pre>%s", template.HTMLEscapeString(err.Error()), tag)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintln(w, err)
}

// resolvePath resolves symlinks in the existing prefix of p and rejoins
// the not-yet-existing tail, so containment checks work on planned paths.
func resolvePath(p string) string {
	rest := ""
	for {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			if rest == "" {
				return real
			}
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
