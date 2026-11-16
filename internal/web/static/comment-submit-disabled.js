// Disable the comment form's submit button while its textarea is empty —
// top-level "Comment"/"Start thread" and every reply form. An empty post
// can only ever 422, so the button stays inert until there is text.
// Progressive enhancement: without this module the same forms validate
// and post normally.
//
// Reacting to delegated `input` covers every keystroke, and a
// MutationObserver syncs the initial state of forms partial-submit.js
// swaps in (a 422 re-render) — a fresh form starts disabled only if it
// is actually empty, so a swapped-in form carrying the rejected draft
// stays submittable.
const COMMENT_FORM = "form.comment-form";

export function syncCommentSubmit(form) {
  if (!form?.matches?.(COMMENT_FORM)) return;
  const textarea = form.querySelector("textarea");
  const submit = form.querySelector('button[type="submit"]');
  if (!textarea || !submit) return;
  submit.disabled = textarea.value.trim() === "";
}

document.addEventListener("input", (event) => {
  const form =
    event.target instanceof Element ? event.target.closest(COMMENT_FORM) : null;
  if (form) syncCommentSubmit(form);
});

for (const form of document.querySelectorAll(COMMENT_FORM)) {
  syncCommentSubmit(form);
}

new MutationObserver((records) => {
  for (const rec of records) {
    for (const node of rec.addedNodes) {
      if (!(node instanceof Element)) continue;
      if (node.matches(COMMENT_FORM)) syncCommentSubmit(node);
      for (const form of node.querySelectorAll(COMMENT_FORM)) {
        syncCommentSubmit(form);
      }
    }
  }
}).observe(document.body, { childList: true, subtree: true });
