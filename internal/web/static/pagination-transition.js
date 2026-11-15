// Tags cross-document view transitions with a direction type ("forward" /
// "backward") so app.css can slide the new page in from that side
// (modern-web-guidance directional-navigation-transitions). Covers page-to-
// page moves on the list (same path, ?page=N) and list↔detail moves within
// a section (issues/discussions): going deeper counts as forward, coming
// back as backward. Other navigations (tabs, filters, cross-section) stay
// untagged and keep the default cross-fade.
//
// Types are set in `pageswap` on the OUTGOING page — its script is already
// loaded, whereas the new page's deferred scripts race with `pagereveal`.
// Progressive enhancement: everything below no-ops without cross-document
// View Transitions or the Navigation API.
(() => {
  function pageOf(url) {
    const p = parseInt(new URL(url).searchParams.get("page") ?? "1", 10);
    return Number.isNaN(p) ? 1 : p;
  }

  // /issues, /issues/3 → { s: "issues", detail: bool }. Query (filters,
  // tabs) doesn't change the shape — only the path does.
  function sectionOf(url) {
    const m = new URL(url).pathname.match(/^\/(issues|discussions)(?:\/(\d+))?/);
    return m ? { section: m[1], detail: !!m[2] } : null;
  }

  function direction(fromUrl, toUrl) {
    let from, to;
    try {
      from = new URL(fromUrl);
      to = new URL(toUrl);
    } catch {
      return null;
    }
    if (from.origin !== to.origin) return null;

    if (from.pathname === to.pathname) {
      const fp = pageOf(from);
      const tp = pageOf(to);
      if (fp !== tp) return tp > fp ? "forward" : "backward";
      return null;
    }

    const f = sectionOf(from);
    const t = sectionOf(to);
    if (!f || !t || f.section !== t.section || f.detail === t.detail) return null;
    return t.detail ? "forward" : "backward";
  }

  addEventListener("pagereveal", (event) => {
    const vt = event.viewTransition;
    if (!vt || !window.navigation?.activation) return;
    const d = direction(navigation.activation.from?.url, navigation.activation.entry?.url);
    if (d) vt.types.add(d);
  });

  addEventListener("pageswap", (event) => {
    const vt = event.viewTransition; // null without cross-document VT or on bfcache restores
    if (!vt || !event.activation) return;
    const d = direction(event.activation.from?.url, event.activation.entry?.url);
    if (d) vt.types.add(d);
  });
})();
