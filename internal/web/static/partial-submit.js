// Partial submit: forms marked data-partial are posted via fetch and the
// returned HTML fragment is swapped in — comment forms (default mode)
// append the new comment's <li> to the form's data-partial-target, except
// fragment nodes marked data-swap, which replace their same-id element
// instead (e.g. the header meta re-rendered for the bumped updated_at);
// data-partial-mode="replace" forms (status toggle, answer mark) swap
// existing elements out: each fragment node self-targets by its own id,
// and a node without an id falls back to data-partial-target. On 422 the
// re-rendered form replaces itself. Without this module the same forms do
// a classic POST → 303 → GET round-trip (progressive enhancement).
// Design: docs/sketch/ja/partial-updates.md.
//
// The submit listener is delegated on document so it also catches forms
// this script itself swapped in (and future injected forms).
import { toast } from "./toast.js";
import { syncCommentSubmit } from "./comment-submit-disabled.js";
import { parseFragment, runScopedTransition, commonScope } from "./fragments.js";

document.addEventListener("submit", (event) => {
  const form = event.target;
  if (!(form instanceof HTMLFormElement) || !form.hasAttribute("data-partial")) return;
  event.preventDefault();
  if (form.dataset.busy === "1") return; // in-flight: ignore re-submits
  postPartial(form);
});

// The collapsed reply entry is a <summary> that hides once its <details>
// opens; without this the click would leave focus on the now-hidden
// summary (or drop it to <body>), forcing a second click before typing.
// toggle does not bubble — capture on document. Progressive enhancement:
// with JS off the details still opens, the user just clicks the field.
document.addEventListener(
  "toggle",
  (event) => {
    const details = event.target;
    if (!(details instanceof HTMLDetailsElement) || !details.open ||
        !details.classList.contains("reply-details")) return;
    details.querySelector("textarea")?.focus();
  },
  true
);

// Cancel collapses the open reply box back to the input bar and returns
// focus to it (the bar reappears once open is removed). Without JS the
// button is inert and the box simply stays open — same as before it
// existed.
document.addEventListener("click", (event) => {
  const btn = event.target.closest?.("[data-reply-cancel]");
  if (!btn) return;
  const details = btn.closest("details.reply-details");
  if (!details) return;
  details.open = false;
  details.querySelector("summary")?.focus();
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
// form.reset() is required — nothing else clears the submission-attempted
// flag behind :user-invalid — but it also restores the rendered defaults
// (after a 422 swap that IS the rejected draft), so blank the fields
// *after* resetting.
function resetPostedForm(form) {
  form.reset();
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

// Replace mode: each element node in the fragment self-targets —
// id="comment-3" replaces #comment-3 — so one response can refresh
// several spots at once (an answer mark re-renders both the newly marked
// comment and the one losing the badge). A node without an id falls back
// to the form's data-partial-target, which is consumed once; a stale
// self-targeting node whose id is already gone is skipped rather than
// replacing the wrong element.
//
// replaceWith, not moveBefore: moveBefore is a state-preserving move for
// an element already connected in the document — it rejects a detached
// node with HierarchyRequestError (Chrome 133+), which every freshly
// parsed fragment node is, so it can never apply to a fragment swap.
// (TODO's moveBefore note assumed it was a drop-in insertBefore
// replacement; verified against the live browser: it is not.)
function swapTargets(form, nodes) {
  let fallback = form.dataset.partialTarget
    ? document.querySelector(form.dataset.partialTarget)
    : null;
  let swapped = 0;
  for (const node of nodes) {
    let target = node.id ? document.getElementById(node.id) : null;
    if (!target && fallback) {
      target = fallback;
      fallback = null;
    }
    if (!target || !target.parentNode) continue;
    target.replaceWith(node);
    swapped++;
  }
  if (swapped === 0) throw new Error("fragment matched no element on the page");
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
      // The scope must be the form's parent: the form itself is
      // replaced, and the scope has to persist through the swap.
      runScopedTransition(form.parentElement, () => {
        form.replaceWith(replacement);
        (replacement.querySelector("[autofocus]") ?? replacement.querySelector("textarea"))?.focus();
      });
      return;
    }
    if (!res.ok || nodes.length === 0) throw new Error(`post failed: ${res.status}`);

    if (form.dataset.partialMode === "replace") {
      const targets = nodes
        .map((n) => (n.id ? document.getElementById(n.id) : null))
        .filter(Boolean);
      await runScopedTransition(commonScope(targets.length ? targets : [form]), () => {
        swapTargets(form, nodes);
        // The submitted form went away with its host element: move focus to
        // its re-rendered counterpart (same action ⇒ same control). This is
        // focus continuity, not a jump — no announcement conflicts.
        // hide/unhide swap the action URL itself (…/hide ⇄ …/unhide), so
        // the action lookup misses there — fall back to the swapped-in
        // post's first moderation control instead of dropping to <body>.
        const action = form.getAttribute("action");
        const refocus = (action &&
          document.querySelector(`form[action="${CSS.escape(action)}"] button[type="submit"]`)) ||
          (form.dataset.partialTarget &&
            document.querySelector(form.dataset.partialTarget)?.querySelector(".comment-action"));
        refocus?.focus();
      });
      toast(form.dataset.partialAnnounce || "Updated.");
      return;
    }

    const target = document.querySelector(form.dataset.partialTarget);
    if (!target) throw new Error(`no insertion target: ${form.dataset.partialTarget}`);
    // The data-swap nodes (e.g. the bumped header meta) live outside the
    // insertion target, so the scope is their common ancestor.
    const swapEls = nodes
      .filter((n) => n.hasAttribute("data-swap"))
      .map((n) => (n.id ? document.getElementById(n.id) : null))
      .filter(Boolean);
    let appended = null;
    await runScopedTransition(commonScope([target, ...swapEls]), () => {
      for (const node of nodes) {
        // A node marked data-swap (e.g. the header meta re-rendered because
        // the post bumped updated_at) replaces its stale counterpart by id
        // instead of appending to the list. A swap node whose id is already
        // gone is skipped rather than appended in the wrong place.
        if (node.hasAttribute("data-swap")) {
          const existing = node.id ? document.getElementById(node.id) : null;
          existing?.replaceWith(node);
          continue;
        }
        target.append(node);
        appended = node;
      }
      if (target.hidden) target.hidden = false;
      // The "No comments yet." note lives next to the top-level list.
      const note = target.parentElement?.querySelector("[data-empty-note]");
      if (note) note.hidden = true;
      const count = document.querySelector("[data-comment-count]");
      if (count) {
        count.textContent = String(Number.parseInt(count.textContent, 10) + 1);
        count.parentElement.hidden = false; // unwrap "Comments (N)"
      }
    });

    const added = appended;
    resetPostedForm(form);
    // A reply box collapses back to its input bar once the post lands;
    // focus moves to the bar like the Cancel button does. Top-level
    // forms stay put and keep focus in the cleared textarea.
    const details = form.closest("details.reply-details");
    if (details) {
      details.open = false;
      details.querySelector("summary")?.focus();
    } else {
      form.querySelector("textarea")?.focus();
    }
    if (added) {
      // Same highlight a #comment-N anchor would give via :target —
      // DOM-inserted nodes can't be :target, hence the class.
      added.classList.add("comment-new");
      added.scrollIntoView({ block: "nearest" });
      setTimeout(() => added.classList.remove("comment-new"), 4000);
    }
    toast(form.dataset.partialAnnounce || "Comment posted.");
  } catch {
    showSubmitError(form, "Comment could not be posted. Please try again.");
  } finally {
    progress.remove();
    delete form.dataset.busy;
    form.removeAttribute("aria-busy");
    for (const btn of form.querySelectorAll('button[type="submit"]')) {
      btn.disabled = false;
    }
    // Re-enabling above would also un-disable an emptied comment form —
    // let the empty-submit guard re-evaluate against the final state.
    syncCommentSubmit(form);
  }
}
