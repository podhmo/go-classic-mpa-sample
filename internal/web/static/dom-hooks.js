// Shared element-lifecycle registry: one MutationObserver for every
// behavior that must attach to elements now and when they enter the DOM
// later (a partial-submit.js fragment swap, a re-rendered 422 form).
//
// bind(selector, {init, teardown}) runs init(el) for each matching
// element already on the page and each one inserted afterwards, and
// teardown(el) when a matching element leaves — teardown is where
// per-element state is released, so state can never outlive its element
// across a swap (a detached 422 form must drop its dirty flag, etc.).
//
// This replaces the per-file observers each behavior used to carry: the
// contract is "init on attach, teardown on detach", not "watch the DOM
// yourself". A module — never loaded as a <script> tag.
const bindings = [];

function matches(node, selector) {
  if (!(node instanceof Element)) return [];
  const out = node.matches(selector) ? [node] : [];
  for (const el of node.querySelectorAll(selector)) out.push(el);
  return out;
}

export function bind(selector, { init, teardown } = {}) {
  const b = { selector, init, teardown, seen: new WeakSet() };
  bindings.push(b);
  // Module scripts run after parsing, so the full document is here —
  // init everything already present exactly like an injected node would.
  for (const el of document.querySelectorAll(selector)) {
    b.seen.add(el);
    init?.(el);
  }
  return b;
}

new MutationObserver((records) => {
  for (const rec of records) {
    for (const node of rec.addedNodes) {
      for (const b of bindings) {
        for (const el of matches(node, b.selector)) {
          if (b.seen.has(el)) continue;
          b.seen.add(el);
          b.init?.(el);
        }
      }
    }
    for (const node of rec.removedNodes) {
      for (const b of bindings) {
        if (!b.teardown) continue;
        for (const el of matches(node, b.selector)) {
          if (!b.seen.delete(el)) continue;
          b.teardown(el);
        }
      }
    }
  }
}).observe(document.body, { childList: true, subtree: true });
