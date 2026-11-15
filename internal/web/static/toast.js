// Visual toast notifications for partial-post successes (and anywhere a
// transient confirmation helps). One region container stacks the toasts;
// each toast is a popover="manual" element so it sits in the top layer,
// never light-dismisses, and stays until the close button or the timer
// removes it. Guide: persistent-toast-notifications.
//
// Screen readers get the same message through ariaNotify() where it
// exists (Baseline 2026-09), falling back to the shared polite live
// region in announcer.js — the toast itself is not a live region, so the
// two channels never double-announce.
//
// Fallback when Popover is missing (older browsers): the popover
// attribute is inert, showPopover/hidePopover don't exist, and the toast
// behaves as a plain fixed-position box — same visuals, no top layer.
import { announce } from "./announcer.js";

const DISMISS_MS = 8000;
const STACK_GAP_PX = 8;
// Longer than the longest .toast transition so the exit animation
// finishes before the node leaves the DOM.
const REMOVE_DELAY_MS = 220;

let region;
let restoreFocus;

function toastRegion() {
  if (!region) {
    region = document.createElement("div");
    // A named region so AT users can reliably locate the toasts even
    // though popover children render in the top layer, outside its box.
    region.setAttribute("role", "region");
    region.setAttribute("aria-label", "Notifications");
    region.className = "toast-region";
    document.body.append(region);
  }
  return region;
}

// Stack the newest toast at the bottom and lift the older ones. Each
// toast is positioned individually (top-layer children ignore ancestor
// layout), via --toast-lift.
function restack() {
  let lift = 0;
  for (const toast of region.querySelectorAll(".toast")) {
    toast.style.setProperty("--toast-lift", `${lift}px`);
    lift += toast.offsetHeight + STACK_GAP_PX;
  }
}

function dismiss(toast) {
  const next = [...region.querySelectorAll(".toast")].find((t) => t !== toast);
  const hadFocus = toast.contains(document.activeElement);
  const drop = () => {
    toast.remove();
    restack();
    if (hadFocus) {
      // Focus continuity: another toast's close button, else the element
      // that was focused before the first toast appeared.
      (next?.querySelector(".toast-close") ?? restoreFocus)?.focus();
    }
  };
  if (typeof toast.hidePopover === "function" && toast.matches(":popover-open")) {
    // The toggle event fires on close; give the exit transition a beat,
    // then remove the node.
    toast.addEventListener("toggle", () => setTimeout(drop, REMOVE_DELAY_MS), {
      once: true,
    });
    toast.hidePopover();
  } else {
    drop();
  }
}

export function toast(message) {
  if (typeof document.ariaNotify === "function") {
    document.ariaNotify(message);
  } else {
    announce(message);
  }

  // Refresh the restore target per toast — capturing it only once leaves
  // a stale (often detached, after a replace-mode swap) element. Keep the
  // previous target when focus already sits inside the region, so a
  // toast shown while another toast's Close has focus doesn't restore
  // into a node that is about to be removed.
  if (!region || !region.contains(document.activeElement)) {
    restoreFocus = document.activeElement;
  }
  const el = document.createElement("div");
  el.className = "toast";
  el.setAttribute("popover", "manual");

  const text = document.createElement("p");
  text.className = "toast-message";
  text.textContent = message;

  // Keyboard-focusable dismiss, but never autofocused — showing a toast
  // must not move focus (guide: toasts must not fight focus direction).
  const close = document.createElement("button");
  close.type = "button";
  close.className = "toast-close";
  close.textContent = "Close";
  close.addEventListener("click", () => dismiss(el));

  el.append(text, close);
  toastRegion().append(el);
  if (typeof el.showPopover === "function") el.showPopover();
  restack();

  // Timed dismissal is fine for this low-importance confirmation (the
  // posted content is visible in the list anyway), but never while focus
  // sits inside the toast — wait for it to leave first.
  const expire = () => {
    if (el.contains(document.activeElement)) {
      el.addEventListener("focusout", () => setTimeout(expire, 500), {
        once: true,
      });
      return;
    }
    dismiss(el);
  };
  setTimeout(expire, DISMISS_MS);
}
