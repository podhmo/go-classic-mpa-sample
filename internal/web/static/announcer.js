// Shared polite live region (aria-live="polite") for form-errors.js and
// partial-submit.js. One announcement channel per page: the latest message
// wins instead of two competing regions (guides
// accessible-error-announcement, persistent-toast-notifications). A module
// — imported by the scripts that announce, never loaded as a <script> tag.
let region;
let announceTimer;
let lastMessage = "";

export function politeRegion() {
  if (!region) {
    region = document.createElement("div");
    region.setAttribute("aria-live", "polite");
    region.classList.add("visually-hidden");
    document.body.append(region);
  }
  return region;
}

export function announce(text) {
  const el = politeRegion();
  // VoiceOver skips a message identical to the previous one: append a
  // no-break space so repeat announcements differ.
  if (text === lastMessage) text += " ";
  lastMessage = text;
  clearTimeout(announceTimer);
  el.textContent = "";
  // A beat later so the announcement isn't swallowed by other updates.
  announceTimer = setTimeout(() => {
    el.textContent = text;
  }, 100);
}
