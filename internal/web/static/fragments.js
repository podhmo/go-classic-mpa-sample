// Shared helpers for client-side fragment swaps — extracted from
// partial-submit.js so live-updates.js (SSE-driven refetch + swap) runs
// the exact same parse/transition rules as form-driven swaps.

// Scoped view transitions (guide scoped-component-transitions): each
// DOM swap runs inside element.startViewTransition() on the smallest
// element containing every affected node, so the component cross-fades
// without blocking pointer events on the rest of the page or obscuring
// toasts/popovers in the top layer (a document-scoped transition
// would). Element-scoped transitions are Chrome 147+; elsewhere the
// update runs immediately with no animation — same DOM, same behavior.
// Reduced motion skips the transition too (the CSS kill-switch below
// covers other callers).
const supportsScopedTransition =
  "startViewTransition" in Element.prototype &&
  !matchMedia("(prefers-reduced-motion: reduce)").matches;

export function runScopedTransition(scopeEl, updateDOM) {
  if (!supportsScopedTransition || !scopeEl) {
    updateDOM();
    return Promise.resolve();
  }
  const transition = scopeEl.startViewTransition(updateDOM);
  // A second submit reusing the same scope skips this transition and
  // rejects ready — swallow it so it doesn't surface as an error.
  transition.ready.catch(() => {});
  // Callers await this before post-swap work (scroll/highlight/toast):
  // the update callback runs ASYNC after the old state is captured, so
  // without awaiting, code that follows reads the pre-swap DOM.
  return transition.updateCallbackDone;
}

// Smallest element containing every affected node: for an append that
// also carries data-swap meta nodes (which live outside the list), or a
// replace touching several spots at once, the scope is their common
// ancestor — containing both old and new states of everything that
// changes.
export function commonScope(elements) {
  let scope = elements[0];
  while (
    scope &&
    !elements.every((el) => scope === el || scope.contains(el))
  ) {
    scope = scope.parentElement;
  }
  return scope ?? document.documentElement;
}

// Server-rendered same-origin fragment, parsed via <template> so the
// markup stays inert (<script> never executes) until insertion. Note:
// Document.parseHTML is NOT used — it runs the default Sanitizer, which
// strips the class/id attributes this fragment depends on.
export function parseFragment(html) {
  const tpl = document.createElement("template");
  tpl.innerHTML = html;
  return [...tpl.content.childNodes];
}
