// Highlight occurrences of the ?q= search terms inside issue titles on the
// list page, via the CSS Custom Highlight API. Progressive enhancement:
// browsers without CSS.highlights just render the titles unhighlighted —
// the list stays fully usable (modern-web-guidance highlight-text-ranges).
(() => {
  if (!CSS.highlights) return;

  const input = document.querySelector("#filter-q");
  const query = input?.value ?? "";
  if (!query) return;

  const tokens = [];
  let current = "";
  let quote = "";
  let quotedEmpty = false;
  const flush = () => {
    if (!current && !quotedEmpty) return;
    tokens.push(current);
    current = "";
    quotedEmpty = false;
  };
  for (const ch of query) {
    if (quote) {
      if (ch === quote) quote = "";
      else current += ch;
    } else if ((ch === '"' || ch === "'") && (!current || current.endsWith(":"))) {
      quote = ch;
      quotedEmpty ||= !current;
    } else if (/\s/.test(ch)) {
      flush();
    } else {
      current += ch;
    }
  }
  flush();

  const issueList = document.querySelector(".issue-list");
  const terms = issueList ? [] : [query];
  for (const token of issueList ? tokens : []) {
    const match = token.match(/^([^:]+):(.+)$/);
    if (match && ["is", "label", "author"].includes(match[1].toLowerCase())) {
      if (match[1].toLowerCase() === "is" && !["open", "closed"].includes(match[2])) {
        terms.push(token);
      }
      continue;
    }
    terms.push(token);
  }

  // Match the server's semantics: SQLite lower() is ASCII-only, so fold
  // A-Z→a-z only. JS toLowerCase() folds Unicode too, which would mark a
  // title SQLite never matched (e.g. title "Ä" vs. query "ä" — only the
  // body matches). Offsets are computed on the ORIGINAL text, comparing
  // each candidate window after the ASCII fold.
  const fold = (s) => s.replace(/[A-Z]/g, (c) => c.toLowerCase());
  const ranges = [];
  for (const title of document.querySelectorAll(".issue-list .issue-title")) {
    const walker = document.createTreeWalker(title, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const text = node.textContent;
      for (const term of terms) {
        if (!term) continue;
        const needle = fold(term);
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
  }
  if (ranges.length) {
    CSS.highlights.set("search-match", new Highlight(...ranges));
  }
})();
