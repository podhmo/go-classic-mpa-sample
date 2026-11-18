// Copy the whole issue/discussion as markdown (Invoker Commands + the
// Async Clipboard API, modern-web-guidance copy-button). The "Copy as
// markdown" button lives in the article action row with
// command="--copy-markdown"; its data-url points at the /markdown
// endpoint, whose response body is what gets copied. Progressive
// enhancement: browsers without CommandEvent get a click-listener
// fallback — without JS the button is inert (it is type="button" in a
// JS-only affordance, matching copy link).
import { reportCopy, writeFetched } from "./clipboard.js";

(() => {
  async function copyMarkdown(button) {
    const url = button.getAttribute("data-url");
    if (!url) { reportCopy(button, false); return; }
    // Memoized promise factory: the ClipboardItem path and the
    // write-after-fetch fallback inside writeFetched share one request.
    let p;
    const text = () => (p ??= fetch(url, { headers: { Accept: "text/markdown" } })
      .then((resp) => {
        if (!resp.ok) throw new Error(`GET ${url}: ${resp.status}`);
        return resp.text();
      }));
    reportCopy(button, await writeFetched(text));
  }

  // command events do not bubble: capture phase on document.
  // event.source is the clicked button.
  document.addEventListener("command", (event) => {
    if (event.command === "--copy-markdown") copyMarkdown(event.source);
  }, true);

  // Browsers without Invoker Commands: same markup, click fallback.
  if (!("CommandEvent" in window)) {
    document.addEventListener("click", (event) => {
      const button = event.target.closest('button[command="--copy-markdown"]');
      if (button) copyMarkdown(button);
    });
  }
})();
