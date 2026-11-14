// Tags cross-document view transitions between paginated list pages with a
// direction type ("forward" / "backward") so app.css can slide the new page
// in from that side (modern-web-guidance directional-navigation-transitions).
// Progressive enhancement: everything below no-ops when the browser lacks
// cross-document View Transitions or the Navigation API, leaving the
// default cross-fade.
(() => {
  function pageParam(url) {
    try {
      const u = new URL(url);
      if (u.pathname !== location.pathname) return null;
      const p = parseInt(u.searchParams.get("page") ?? "1", 10);
      return Number.isNaN(p) ? null : p;
    } catch {
      return null;
    }
  }

  addEventListener("pagereveal", (event) => {
    const vt = event.viewTransition; // null on bfcache restores and in unsupporting browsers
    if (!vt || !window.navigation?.activation) return;
    const from = pageParam(navigation.activation.from?.url);
    const to = pageParam(navigation.activation.entry?.url);
    if (from === null || to === null || from === to) return;
    vt.types.add(to > from ? "forward" : "backward");
  });
})();
