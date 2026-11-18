// Shared clipboard helpers for the copy-* command modules: the .copy-tip
// bubble feedback and the write paths (Async Clipboard API, then the
// deprecated execCommand fallback). An import-only module — the detail
// page reaches it through copy-permalink.js / copy-markdown.js.
const timers = new WeakMap();
let lastMessage = "";

// reportCopy drives the .copy-tip sibling of the button: data-copied /
// data-failed on the button reveals the tip (the CSS keeps it out of the
// accessibility tree only via opacity), and the tip text is the live
// region sighted and screen-reader users share.
export function reportCopy(button, ok) {
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

// execCommand("copy") is the fallback of last resort for browsers without
// navigator.clipboard; it must run inside the user gesture's task.
function legacyCopy(text) {
  const field = Object.assign(document.createElement("textarea"), { value: text });
  field.setAttribute("readonly", "");
  field.style.cssText = "position:fixed;inset-block-start:0;opacity:0";
  document.body.append(field);
  field.select();
  try { return document.execCommand("copy"); }
  finally { field.remove(); }
}

// writeText copies an already-known string. Returns success.
export async function writeText(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return legacyCopy(text);
  }
}

// writeFetched copies text still being produced by text() (a promise
// factory). ClipboardItem accepts a promise of the data, which keeps the
// click's user activation alive across the fetch — Safari rejects a
// writeText issued only after an await. Browsers without ClipboardItem
// fetch first; the gesture is spent by then, so writeText's execCommand
// path is the only clipboard left. Returns success.
export async function writeFetched(text) {
  if ("ClipboardItem" in window && navigator.clipboard && navigator.clipboard.write) {
    try {
      await navigator.clipboard.write([new ClipboardItem({
        "text/plain": text().then((t) => new Blob([t], { type: "text/plain" })),
      })]);
      return true;
    } catch {
      // The fetch failed (offline/404) or the write was denied — the
      // plain path below reports the same failure either way.
    }
  }
  try {
    return await writeText(await text());
  } catch {
    return false;
  }
}
