// Ctrl+Enter / Cmd+Enter submits the enclosing form from any textarea
// (modern-web-guidance ime-safe-enter-submit). Chosen over bare Enter
// deliberately: modifier combos do not conflict with IME composition, so
// no isComposing/keyCode-229 checks are needed. Delegated on document so
// it also covers textareas inside forms partial-submit.js swaps in.
document.addEventListener("keydown", (event) => {
  if (event.key !== "Enter" || !(event.metaKey || event.ctrlKey)) return;
  const textarea = event.target;
  if (!(textarea instanceof HTMLTextAreaElement) || !textarea.form) return;
  event.preventDefault();
  textarea.form.requestSubmit();
});
