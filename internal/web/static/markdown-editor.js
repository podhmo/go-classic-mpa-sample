// Progressive markdown writing enhancement for every body textarea:
// client-created Write/Preview tabs, server-rendered preview fragments, and
// crash-safe localStorage drafts. Without JavaScript the plain textarea and
// POST flow remain unchanged.
import { bind } from "./dom-hooks.js";
import { t } from "./i18n.js";
import { markDirty } from "./leave-guard.js";

let nextID = 0;
const editorStates = new WeakMap();

function previewURL() {
  const match = location.pathname.match(/^\/w\/([^/]+)(?:\/|$)/);
  return match ? `/w/${encodeURIComponent(match[1])}/preview` : "/preview";
}

function draftKey(textarea) {
  const form = textarea.closest("form");
  let identity = "";
  if (form) {
    const action = form.getAttribute("action") || "body";
    const hidden = [...form.querySelectorAll('input[type="hidden"]')];
    const lineAnchor = hidden
      .filter((input) => ["path", "line", "side"].includes(input.name))
      .map((input) => `${input.name}=${input.value}`);
    const index = [...form.querySelectorAll('textarea[name="body"]')].indexOf(textarea);
    if (lineAnchor.length) {
      identity = [action, ...lineAnchor].join(":");
    } else {
      identity = textarea.id;
    }
    if (!identity) {
      const anchor = hidden
        .filter((input) => ["thread_id"].includes(input.name))
        .map((input) => `${input.name}=${input.value}`);
      identity = [action, ...anchor, `textarea${Math.max(index, 0)}`].join(":");
    }
  }
  const account = document.documentElement.dataset.userId || "anonymous";
  // Use the form action rather than location.pathname: a rejected create
  // POST is re-rendered on the collection URL, and the draft must keep the
  // same key across that validation round-trip.
  const path = form ? new URL(form.action, location.origin).pathname : location.pathname;
  return `draft:${account}:${path}:${identity || "body"}`;
}

function readDraft(key) {
  try { return localStorage.getItem(key); } catch { return null; }
}

function writeDraft(key, value) {
  try {
    // Empty is a meaningful draft on edit forms: clearing the published
    // body must survive a reload just like any other unsent edit.
    localStorage.setItem(key, value);
  } catch {
    // Private-mode quota failures must not break typing.
  }
}

function clearPostedDraft() {
  const prefix = "zatudan_draft_posted=";
  const entry = document.cookie.split("; ").find((part) => part.startsWith(prefix));
  if (!entry) return;
  let key = entry.slice(prefix.length);
  try {
    key = decodeURIComponent(key.replace(/\+/g, "%20"));
    localStorage.removeItem(key);
  } catch {
    // A malformed or unavailable store must not block the page.
  }
  document.cookie = "zatudan_draft_posted=; Path=/; Max-Age=0";
}

function saveState(textarea, state) {
  writeDraft(state.key, textarea.value);
  state.dirty = true;
}

async function loadPreview(textarea, panel, state) {
  panel.textContent = "";
  const progress = document.createElement("progress");
  progress.setAttribute("aria-label", t("preview"));
  panel.append(progress);
  try {
    const body = new URLSearchParams({ body: textarea.value });
    const res = await fetch(state.previewURL, { method: "POST", body });
    if (!res.ok) throw new Error(String(res.status));
    const html = await res.text();
    if (textarea.value.trim() === "") {
      panel.textContent = t("preview_empty");
      panel.classList.add("markdown-preview-empty");
    } else {
      panel.innerHTML = html;
      panel.classList.remove("markdown-preview-empty");
    }
    state.dirty = false;
  } catch {
    panel.textContent = t("preview_failed");
    panel.classList.add("markdown-preview-empty");
  }
}

function activate(textarea, panel, tabs, which, state) {
  const writing = which === "write";
  textarea.hidden = !writing;
  panel.hidden = writing;
  tabs[0].setAttribute("aria-selected", String(writing));
  tabs[1].setAttribute("aria-selected", String(!writing));
  tabs[0].tabIndex = writing ? 0 : -1;
  tabs[1].tabIndex = writing ? -1 : 0;
  if (!writing && state.dirty) loadPreview(textarea, panel, state);
}

function targetName(textarea) {
  const explicit = textarea.getAttribute("aria-label");
  if (explicit) return explicit.trim();
  if (textarea.id) {
    const label = document.querySelector(`label[for="${CSS.escape(textarea.id)}"]`);
    const text = (label?.textContent || "").replace(/\s+/g, " ").replace(/\s*\*\s*$/, "").trim();
    if (text) return text;
  }
  // Review-line textareas have no id/label; their localized placeholder is
  // the only line-specific name the server rendered ("Comment on line 42").
  return (textarea.getAttribute("placeholder") || "").replace(/\s+/g, " ").trim();
}

function enhance(textarea) {
  if (!(textarea instanceof HTMLTextAreaElement) || textarea.dataset.markdownEditor) return;
  textarea.dataset.markdownEditor = "1";
  const state = { key: draftKey(textarea), dirty: true, previewURL: previewURL() };

  const saved = readDraft(state.key);
  if (saved !== null && saved !== textarea.value) {
    textarea.value = saved;
    textarea.dispatchEvent(new Event("input", { bubbles: true }));
    markDirty(textarea.closest("form"));
  }

  const base = textarea.id || `markdown-body-${++nextID}`;
  const target = targetName(textarea);
  const tablist = document.createElement("div");
  tablist.className = "markdown-tabs";
  tablist.setAttribute("role", "tablist");
  const write = document.createElement("button");
  const preview = document.createElement("button");
  for (const [button, label, mode] of [[write, t("write"), "write"], [preview, t("preview"), "preview"]]) {
    button.type = "button";
    button.className = "markdown-tab";
    button.setAttribute("role", "tab");
    button.id = `${base}-${mode}-tab`;
    button.textContent = label;
    if (target) button.setAttribute("aria-label", t(`${mode}_for`, target));
    button.addEventListener("click", () => activate(textarea, panel, [write, preview], mode, state));
    tablist.append(button);
  }
  const panel = document.createElement("div");
  panel.className = "markdown-preview markdown-body";
  panel.id = `${base}-preview-panel`;
  panel.setAttribute("role", "tabpanel");
  panel.setAttribute("aria-labelledby", preview.id);
  panel.hidden = true;
  write.setAttribute("aria-controls", base);
  write.setAttribute("aria-selected", "true");
  preview.setAttribute("aria-controls", panel.id);
  preview.setAttribute("aria-selected", "false");
  preview.tabIndex = -1;

  tablist.addEventListener("keydown", (event) => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const mode = event.key === "ArrowRight" ? "preview" : "write";
    activate(textarea, panel, [write, preview], mode, state);
    (mode === "write" ? write : preview).focus();
  });

  textarea.before(tablist);
  textarea.after(panel);
  editorStates.set(textarea, { panel, tabs: [write, preview], state });
  textarea.addEventListener("input", () => saveState(textarea, state));
  textarea.addEventListener("change", () => saveState(textarea, state));
}

function trackSubmit(form) {
  if (!(form instanceof HTMLFormElement)) return;
  form.addEventListener("submit", () => {
    if (form.hasAttribute("data-partial")) return;
    const textarea = form.querySelector('textarea[name="body"]');
    if (!textarea) return;
    let marker = form.querySelector('input[name="draft_key"]');
    if (!marker) {
      marker = document.createElement("input");
      marker.type = "hidden";
      marker.name = "draft_key";
      form.append(marker);
    }
    marker.value = draftKey(textarea);
  });
  form.addEventListener("reset", () => {
    // partial-submit.js fires reset only after a successful partial post.
    queueMicrotask(() => {
      const textarea = form.querySelector('textarea[name="body"]');
      if (textarea) {
        try { localStorage.removeItem(draftKey(textarea)); } catch {}
        const editor = editorStates.get(textarea);
        if (editor) {
          activate(textarea, editor.panel, editor.tabs, "write", editor.state);
          editor.panel.textContent = "";
          editor.state.dirty = true;
        }
      }
    });
  });
}

clearPostedDraft();
bind('textarea[name="body"]', { init: enhance });
bind("form", { init: trackSubmit });
