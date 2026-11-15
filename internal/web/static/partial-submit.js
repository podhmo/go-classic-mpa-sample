// Partial submit: forms marked data-partial are posted via fetch and the
// returned HTML fragment is swapped in — the new comment's <li> appended
// to the form's data-partial-target, or on 422 the re-rendered form
// replacing itself. Without this module the same forms do a classic
// POST → 303 → GET round-trip (progressive enhancement). Design:
// docs/sketch/ja/partial-updates.md.
//
// The submit listener is delegated on document so it also catches forms
// this script itself swapped in (and future injected forms).
import { announce } from "./announcer.js";

// Server-rendered same-origin fragment, parsed via <template> so the
// markup stays inert (<script> never executes) until insertion. Note:
// Document.parseHTML is NOT used — it runs the default Sanitizer, which
// strips the class/id attributes this fragment depends on.
function parseFragment(html) {
  const tpl = document.createElement("template");
  tpl.innerHTML = html;
  return [...tpl.content.childNodes];
}

document.addEventListener("submit", (event) => {
  const form = event.target;
  if (!(form instanceof HTMLFormElement) || !form.hasAttribute("data-partial")) return;
  event.preventDefault();
  if (form.dataset.busy === "1") return; // in-flight: ignore re-submits
  postPartial(form);
});

// A failed POST gets a visible note, not just a live-region announcement
// — sighted users must see that nothing was saved. role="alert" makes it
// its own live region, so announce() is not needed for this path.
function showSubmitError(form, message) {
  let el = form.querySelector("[data-submit-error]");
  if (!el) {
    el = document.createElement("p");
    el.className = "field-error";
    el.setAttribute("role", "alert");
    el.dataset.submitError = "";
    (form.querySelector('button[type="submit"]') ?? form.lastElementChild).before(el);
  }
  el.textContent = message;
}

// A successful post clears the fields and any server error markings.
// form.reset() would restore the rendered defaults — after a 422 swap
// those ARE the rejected draft and its visible error.
function resetPostedForm(form) {
  for (const el of form.elements) {
    if (!(el instanceof HTMLTextAreaElement)) continue;
    el.value = "";
    el.setAttribute("aria-invalid", "false");
    el.removeAttribute("autofocus");
    const err = el.dataset.error && document.getElementById(el.dataset.error);
    if (err) err.classList.add("field-error-pending");
    const keep = (el.getAttribute("aria-describedby") || "")
      .split(/\s+/)
      .filter((id) => id && id !== el.dataset.error);
    if (keep.length) el.setAttribute("aria-describedby", keep.join(" "));
    else el.removeAttribute("aria-describedby");
  }
}

async function postPartial(form) {
  form.dataset.busy = "1";
  form.setAttribute("aria-busy", "true");
  for (const btn of form.querySelectorAll('button[type="submit"]')) {
    btn.disabled = true;
  }
  form.querySelector("[data-submit-error]")?.remove();
  // Indeterminate <progress>: correct AT semantics for free (guide
  // spinner) and no value attribute needed.
  const progress = document.createElement("progress");
  progress.setAttribute("aria-label", "Posting");
  form.append(progress);
  try {
    const res = await fetch(form.action, {
      method: "POST",
      // URLSearchParams keeps the wire format identical to a plain form
      // submit (application/x-www-form-urlencoded) — a FormData body
      // would switch it to multipart, a second parser path to break.
      body: new URLSearchParams(new FormData(form)),
      headers: { "X-Partial": "1" },
    });
    const nodes = parseFragment(await res.text()).filter(
      (n) => n.nodeType === Node.ELEMENT_NODE,
    );
    if (res.status === 422) {
      // Validation failed: the server re-rendered just this form with the
      // error marked. Swap it in and move focus to the flagged field —
      // no announcement: a live region must never fight focus
      // redirection (guide persistent-toast-notifications).
      const replacement = nodes[0];
      if (!(replacement instanceof HTMLFormElement)) throw new Error("unexpected 422 fragment");
      form.replaceWith(replacement);
      (replacement.querySelector("[autofocus]") ?? replacement.querySelector("textarea"))?.focus();
      return;
    }
    if (!res.ok || nodes.length === 0) throw new Error(`post failed: ${res.status}`);

    const target = document.querySelector(form.dataset.partialTarget);
    if (!target) throw new Error(`no insertion target: ${form.dataset.partialTarget}`);
    for (const node of nodes) target.append(node);
    if (target.hidden) target.hidden = false;
    // The "No comments yet." note lives next to the top-level list.
    const note = target.parentElement?.querySelector("[data-empty-note]");
    if (note) note.hidden = true;
    const count = document.querySelector("[data-comment-count]");
    if (count) {
      count.textContent = String(Number.parseInt(count.textContent, 10) + 1);
      count.parentElement.hidden = false; // unwrap "Comments (N)"
    }

    const added = target.lastElementChild;
    resetPostedForm(form);
    form.querySelector("textarea")?.focus();
    if (added) {
      // Same highlight a #comment-N anchor would give via :target —
      // DOM-inserted nodes can't be :target, hence the class.
      added.classList.add("comment-new");
      added.scrollIntoView({ block: "nearest" });
      setTimeout(() => added.classList.remove("comment-new"), 4000);
    }
    announce("Comment posted.");
  } catch {
    showSubmitError(form, "Comment could not be posted. Please try again.");
  } finally {
    progress.remove();
    delete form.dataset.busy;
    form.removeAttribute("aria-busy");
    for (const btn of form.querySelectorAll('button[type="submit"]')) {
      btn.disabled = false;
    }
  }
}
