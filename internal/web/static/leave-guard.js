// Warn before leaving the page while a form still holds unsent input.
// Each tracked field is compared against the value it was rendered with,
// so an edit form is clean until its content actually changes, and
// typing-then-erasing leaves no false warning. A classic POST navigates
// away anyway; a partial post (data-partial) clears through the form's
// `reset` event, which only fires on success. Loaded on the form and
// detail pages; auth forms are short enough to skip the guard.
import { bind } from "./dom-hooks.js";

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

// Attach via the shared lifecycle registry: every present and injected
// form gets a baseline at attach time — a 422 re-render carries the
// draft as the new rendered value — and detach releases its dirty
// count, so a swapped-out dirty form cannot leak a warning. The initial
// attach pass is what stops the first `input` from snapshotting the
// just-typed value and marking the form clean when it is not.
bind("form", { init: snapshot, teardown: unmark });

window.addEventListener("beforeunload", (event) => {
  if (dirtyCount === 0) return;
  event.preventDefault();
  event.returnValue = ""; // legacy trigger; browsers show their own text
});
