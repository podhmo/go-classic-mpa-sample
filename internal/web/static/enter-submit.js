// Ctrl+Enter / Cmd+Enter submits the enclosing form from any textarea
// (modern-web-guidance ime-safe-enter-submit). Chosen over bare Enter
// deliberately: modifier combos do not conflict with IME composition, so no
// isComposing/keyCode-229 checks are needed ("Other custom submission
// shortcuts (such as Cmd+Enter or Ctrl+Enter) do not conflict with IME
// confirmation keys and do not require IME safety checks").
(() => {
  for (const textarea of document.querySelectorAll("form textarea")) {
    textarea.addEventListener("keydown", (event) => {
      if (event.key !== "Enter" || !(event.metaKey || event.ctrlKey)) return;
      event.preventDefault(); // don't insert a newline
      textarea.form?.requestSubmit(); // runs the form's submit path
    });
  }
})();
