// Warn before leaving the page while a form still holds unsent input.
// Each tracked field is compared against the value it was rendered with,
// so an edit form is clean until its content actually changes, and
// typing-then-erasing leaves no false warning. A classic POST navigates
// away anyway; a partial post (data-partial) clears through the form's
// `reset` event, which only fires on success. Loaded on the form and
// detail pages; auth forms are short enough to skip the guard.
const TRACKED = 'textarea, input[type="text"], input[type="checkbox"], input[type="radio"], select';

// Checkboxes/radios keep their rendered `value`; their dirty signal is
// `checked` (e.g. label edits on the issue form).
function fieldState(el) {
  return el.type === "checkbox" || el.type === "radio" ? el.checked : el.value;
}

// form -> Map<field, rendered value>; WeakMap so forms swapped out by
// partial-submit.js drop out with their state.
const baselines = new WeakMap();
const dirtyForms = new WeakSet();
let dirtyCount = 0;

function mark(form) {
  if (dirtyForms.has(form)) return;
  dirtyForms.add(form);
  dirtyCount++;
}

function unmark(form) {
  if (!dirtyForms.has(form)) return;
  dirtyForms.delete(form);
  dirtyCount--;
}

function snapshot(form) {
  const values = new Map();
  for (const el of form.querySelectorAll(TRACKED)) values.set(el, fieldState(el));
  baselines.set(form, values);
}

function resync(form) {
  if (!(form instanceof HTMLFormElement)) return;
  if (!baselines.has(form)) snapshot(form);
  const values = baselines.get(form);
  let dirty = false;
  for (const [el, rendered] of values) {
    if (fieldState(el) !== rendered) {
      dirty = true;
      break;
    }
  }
  if (dirty) mark(form);
  else unmark(form);
}

document.addEventListener("input", (event) => {
  resync(event.target instanceof Element ? event.target.closest("form") : null);
});
document.addEventListener("change", (event) => {
  resync(event.target instanceof Element ? event.target.closest("form") : null);
});

document.addEventListener("reset", (event) => {
  const form = event.target;
  if (!(form instanceof HTMLFormElement)) return;
  // The reset event fires before the values are restored, and
  // partial-submit.js blanks the fields right after: re-baseline once the
  // whole synchronous block has settled, so the post-clear state counts
  // as clean.
  queueMicrotask(() => {
    baselines.delete(form);
    snapshot(form);
    unmark(form);
  });
});

document.addEventListener("submit", (event) => {
  const form = event.target;
  if (!(form instanceof HTMLFormElement)) return;
  // Classic submits navigate away; a data-partial form clears on the
  // reset that follows a successful post, not here — a failed fetch keeps
  // the input and the guard.
  if (form.hasAttribute("data-partial")) return;
  snapshot(form);
  unmark(form);
});

// Baseline everything already on the page (the script is deferred, so
// the DOM is complete) — without this the first `input` would snapshot
// the just-typed value and mark the form clean when it is not.
for (const form of document.querySelectorAll("form")) snapshot(form);

// Forms injected later (a 422 re-render carries the draft as the new
// rendered value) get their own baseline.
new MutationObserver((records) => {
  for (const rec of records) {
    for (const node of rec.addedNodes) {
      if (!(node instanceof Element)) continue;
      if (node instanceof HTMLFormElement) snapshot(node);
      for (const form of node.querySelectorAll("form")) snapshot(form);
    }
    // Forms detached by a fragment swap (e.g. the 422 re-render replaces
    // a dirty form with a fresh baseline) must release their dirty count;
    // otherwise the guard keeps warning after the state is gone.
    for (const node of rec.removedNodes) {
      if (!(node instanceof Element)) continue;
      if (node instanceof HTMLFormElement) unmark(node);
      for (const form of node.querySelectorAll("form")) unmark(form);
    }
  }
}).observe(document.body, { childList: true, subtree: true });

window.addEventListener("beforeunload", (event) => {
  if (dirtyCount === 0) return;
  event.preventDefault();
  event.returnValue = ""; // legacy trigger; browsers show their own text
});
