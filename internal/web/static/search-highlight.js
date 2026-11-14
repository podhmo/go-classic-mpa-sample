// Highlight occurrences of the ?q= search term inside issue titles on the
// list page, via the CSS Custom Highlight API. Progressive enhancement:
// browsers without CSS.highlights just render the titles unhighlighted —
// the list stays fully usable (modern-web-guidance highlight-text-ranges).
(() => {
  if (!CSS.highlights) return;

  const input = document.querySelector("#filter-q");
  const term = input?.value ?? "";
  if (!term) return;
  // Match the server's semantics: SQLite lower() is ASCII-only, so fold
  // A-Z→a-z only. JS toLowerCase() folds Unicode too, which would mark a
  // title SQLite never matched (e.g. title "Ä" vs. query "ä" — only the
  // body matches). Offsets are computed on the ORIGINAL text, comparing
  // each candidate window after the ASCII fold.
  const fold = (s) => s.replace(/[A-Z]/g, (c) => c.toLowerCase());
  const needle = fold(term);
  const ranges = [];
  for (const title of document.querySelectorAll(".issue-list .issue-title")) {
    const walker = document.createTreeWalker(title, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const text = node.textContent;
      for (let i = 0; i + term.length <= text.length; i++) {
        if (fold(text.slice(i, i + term.length)) !== needle) continue;
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
