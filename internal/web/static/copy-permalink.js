// Copy a comment's #comment-N permalink via Invoker Commands + the Async
// Clipboard API (modern-web-guidance copy-button). The "copy link" buttons
// live in each comment header with command="--copy-permalink"
// commandfor="comment-N". Progressive enhancement: browsers without
// CommandEvent get a click-listener fallback; browsers without
// navigator.clipboard get the deprecated execCommand path (clipboard.js).
import { reportCopy, writeText } from "./clipboard.js";

(() => {
  async function copyPermalink(button, target) {
    if (!target || !target.id) { reportCopy(button, false); return; }
    // Copy the canonical detail-page URL from data-url, not location.href:
    // after a rejected comment POST the page is re-rendered at
    // /issues/{id}/comments, a POST-only path that 404s on GET.
    const rel = button.getAttribute("data-url");
    const text = rel ? new URL(rel, location.href).href : location.href.split("#")[0] + "#" + target.id;
    reportCopy(button, await writeText(text));
  }

  // command events do not bubble: capture phase on document.
  // event.source is the clicked button; event.target is commandfor's element.
  document.addEventListener("command", (event) => {
    if (event.command === "--copy-permalink") copyPermalink(event.source, event.target);
  }, true);

  // Browsers without Invoker Commands: same markup, click fallback.
  if (!("CommandEvent" in window)) {
    document.addEventListener("click", (event) => {
      const button = event.target.closest('button[command="--copy-permalink"]');
      if (!button) return;
      copyPermalink(button, document.getElementById(button.getAttribute("commandfor")));
    });
  }
})();
