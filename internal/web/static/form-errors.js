// Sync aria-invalid with the CSS :user-invalid state, so the programmatic
// state lands at the same moment the visual error styling does
// (modern-web-guidance accessible-error-announcement). Fields link their
// error element via data-error="<id>"; its id is listed in
// aria-describedby only while the error is shown — describedby text is
// announced even while hidden, so it must not be referenced up front.
// An aria-invalid="true" this script didn't set (a server-side error) is
// left alone: the server stays the source of truth until the next submit.
(() => {
  const linkError = (el, shown) => {
    const errorId = el.dataset.error;
    if (!errorId) return;
    const ids = (el.getAttribute("aria-describedby") || "")
      .split(/\s+/)
      .filter((id) => id && id !== errorId);
    if (shown) ids.push(errorId);
    if (ids.length) el.setAttribute("aria-describedby", ids.join(" "));
    else el.removeAttribute("aria-describedby");
  };

  // One polite live region for the page announces an error that appears
  // when the user leaves or changes a field — a moment later so it doesn't
  // talk over the next field.
  const announcer = document.createElement("div");
  announcer.setAttribute("aria-live", "polite");
  announcer.classList.add("visually-hidden");
  document.body.append(announcer);
  let announceTimer;
  let announcedField;

  // Text as a screen reader reads it: icons' text alternatives in,
  // aria-hidden text out.
  const spokenText = (node) => {
    const copy = node.cloneNode(true);
    copy.querySelectorAll('[aria-hidden="true"]').forEach((n) => n.remove());
    copy.querySelectorAll('[role="img"][aria-label]').forEach((n) =>
      n.replaceWith(`${n.getAttribute("aria-label")}: `));
    return copy.textContent.replace(/\s+/g, " ").trim();
  };
  const announceError = (el) => {
    const error = document.getElementById(el.dataset.error);
    if (!error) return;
    const label = el.labels?.[0] ? `${spokenText(el.labels[0])}: ` : "";
    clearTimeout(announceTimer);
    announcer.textContent = "";
    announcedField = el;
    // Wait until the screen reader has announced the field that receives
    // focus next.
    announceTimer = setTimeout(() => {
      announcer.textContent = label + spokenText(error);
    }, 500);
  };

  const flagged = new WeakSet();
  // Fields that arrived with a server-rendered aria-invalid="true". Once
  // the script flags such a field, clearing a client-side :user-invalid
  // must restore the server state, not wipe it: the server error element
  // stays visible until the next submit (it may report a constraint the
  // client cannot check, e.g. a server-side length limit).
  const serverFlagged = new WeakSet();
  // Fields whose current error has been announced, or read out by focus
  // after a failed submit.
  const announced = new WeakSet();
  const syncAria = (el, announce = false) => {
    if (!el.matches?.("input, select, textarea")) return;
    const invalid = el.matches(":user-invalid");
    if (invalid) {
      if (announce && !announced.has(el)) {
        announceError(el);
        announced.add(el);
      }
      if (el.getAttribute("aria-invalid") === "true" && !flagged.has(el)) {
        serverFlagged.add(el);
      }
      el.setAttribute("aria-invalid", "true");
      linkError(el, true);
      flagged.add(el);
    } else if (flagged.has(el)) {
      const serverErr = serverFlagged.has(el);
      el.setAttribute("aria-invalid", serverErr ? "true" : "false");
      linkError(el, serverErr);
      flagged.delete(el);
      announced.delete(el);
      // Clear a fixed field's announcement so it isn't found later when
      // reading the page.
      if (announcedField === el) {
        clearTimeout(announceTimer);
        announcer.textContent = "";
      }
    } else if (el.getAttribute("aria-invalid") !== "true") {
      el.setAttribute("aria-invalid", "false");
      linkError(el, false);
      announced.delete(el);
      if (announcedField === el) {
        clearTimeout(announceTimer);
        announcer.textContent = "";
      }
    }
  };

  // Start every field at aria-invalid="false" — Firefox and Safari expose
  // an empty required field as invalid from page load.
  document.querySelectorAll("input, select, textarea").forEach((el) => syncAria(el));

  // Update on blur (show + announce) and input (clear as the user types).
  document.addEventListener("blur", (e) => syncAria(e.target, true), true);
  document.addEventListener("input", (e) => {
    if (e.target.hasAttribute("aria-invalid")) syncAria(e.target);
  });
  // Controls that commit on 'change' can become :user-invalid without
  // losing focus.
  document.addEventListener("change", (e) => syncAria(e.target, true), true);
  // A failed native submit fires 'invalid' on each failing field (the app's
  // forms use novalidate today, so this is future-proofing).
  document.addEventListener("invalid", (e) => {
    announced.add(e.target);
    syncAria(e.target);
  }, true);
  // Reset clears :user-invalid after the 'reset' event.
  document.addEventListener("reset", (e) => {
    setTimeout(() => [...e.target.elements].forEach((el) => syncAria(el)));
  }, true);
})();
