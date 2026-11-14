// Copy a comment's #comment-N permalink via Invoker Commands + the Async
// Clipboard API (modern-web-guidance copy-button). The "copy link" buttons
// live in each comment header with command="--copy-permalink"
// commandfor="comment-N". Progressive enhancement: browsers without
// CommandEvent get a click-listener fallback; browsers without
// navigator.clipboard get the deprecated execCommand path.
(() => {
  const timers = new WeakMap();
  let lastMessage = "";

  function report(button, ok) {
    const tip = button.parentElement && button.parentElement.querySelector(".copy-tip");
    if (!tip) return;
    clearTimeout(timers.get(button));
    button.removeAttribute("data-copied");
    button.removeAttribute("data-failed");
    let message = ok ? "Copied" : "Copy failed";
    // VoiceOver skips a message identical to the previous one: append a
    // no-break space so repeat announcements differ.
    if (message === lastMessage) message += " ";
    lastMessage = message;
    tip.textContent = "";
    void button.offsetWidth; // restart the CSS transition
    button.setAttribute(ok ? "data-copied" : "data-failed", "");
    requestAnimationFrame(() => { tip.textContent = message; });
    timers.set(button, setTimeout(() => {
      button.removeAttribute("data-copied");
      button.removeAttribute("data-failed");
      tip.textContent = "";
    }, 2000));
  }

  function legacyCopy(text) {
    const field = Object.assign(document.createElement("textarea"), { value: text });
    field.setAttribute("readonly", "");
    field.style.cssText = "position:fixed;inset-block-start:0;opacity:0";
    document.body.append(field);
    field.select();
    try { return document.execCommand("copy"); }
    finally { field.remove(); }
  }

  async function copyPermalink(button, target) {
    if (!target || !target.id) { report(button, false); return; }
    // Copy the canonical detail-page URL from data-url, not location.href:
    // after a rejected comment POST the page is re-rendered at
    // /issues/{id}/comments, a POST-only path that 404s on GET.
    const rel = button.getAttribute("data-url");
    const text = rel ? new URL(rel, location.href).href : location.href.split("#")[0] + "#" + target.id;
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      if (!legacyCopy(text)) { report(button, false); return; }
    }
    report(button, true);
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
