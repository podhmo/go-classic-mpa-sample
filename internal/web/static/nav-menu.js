// Responsive state for the header's single <details> navigation tree.
// The markup starts open so the links remain usable without JS; this small
// sync collapses it only in the narrow layout and reopens it inline wide.
(() => {
  const menu = document.querySelector(".nav-menu");
  if (!(menu instanceof HTMLDetailsElement)) return;
  const narrowQuery = window.matchMedia("(max-width: 40em)");
  let wasNarrow = null;
  const sync = () => {
    const narrow = narrowQuery.matches;
    if (!narrow) {
      menu.open = true;
    } else if (wasNarrow !== true) {
      menu.open = false;
    }
    wasNarrow = narrow;
  };
  sync();
  narrowQuery.addEventListener("change", sync);

  document.addEventListener("focusin", (event) => {
    if (!menu.open || !narrowQuery.matches) return;
    if (event.target instanceof Node && !menu.contains(event.target)) {
      menu.open = false;
    }
  });
})();
