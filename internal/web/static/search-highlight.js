// Highlight occurrences of the ?q= search term inside issue titles on the
// list page, via the CSS Custom Highlight API. Progressive enhancement:
// browsers without CSS.highlights just render the titles unhighlighted —
// the list stays fully usable (modern-web-guidance highlight-text-ranges).
(() => {
  if (!CSS.highlights) return;

  const input = document.querySelector("#filter-q");
  const term = input?.value ?? "";
  if (!term) return;
  const needle = term.toLowerCase();

  // Match the server's semantics: instr(lower(title), lower(?)) — a
  // case-insensitive substring of the full query, not per-word matching.
  // Offsets are computed on the ORIGINAL text (lowercase folding can change
  // string length, e.g. 'İ'), comparing each candidate window instead.
  const ranges = [];
  for (const title of document.querySelectorAll(".issue-list .issue-title")) {
    const walker = document.createTreeWalker(title, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const text = node.textContent;
      for (let i = 0; i + term.length <= text.length; i++) {
        if (text.slice(i, i + term.length).toLowerCase() !== needle) continue;
        const r = new Range();
        r.setStart(node, i);
        r.setEnd(node, i + term.length);
        ranges.push(r);
        i += term.length - 1; // non-overlapping matches
      }
    }
  }
  if (ranges.length) {
    CSS.highlights.set("search-match", new Highlight(...ranges));
  }
})();
