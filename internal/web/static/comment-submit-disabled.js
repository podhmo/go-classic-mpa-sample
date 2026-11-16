// Disable the comment form's submit button while its textarea is empty —
// top-level "Comment"/"Start thread" and every reply form. An empty post
// can only ever 422, so the button stays inert until there is text.
// Progressive enhancement: without this module the same forms validate
// and post normally.
//
// Reacting to delegated `input` covers every keystroke, and dom-hooks.js
// attach runs the initial sync of forms partial-submit.js swaps in (a
// 422 re-render) — a fresh form starts disabled only if it is actually
// empty, so a swapped-in form carrying the rejected draft stays
// submittable.
import { bind } from "./dom-hooks.js";

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

// Stateless sync: only attach is needed — a detached form's button
// leaves with it.
bind(COMMENT_FORM, { init: syncCommentSubmit });
