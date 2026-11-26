// Progressive markdown writing enhancement for every body textarea:
// client-created Write/Preview tabs, server-rendered preview fragments, and
// crash-safe localStorage drafts. Without JavaScript the plain textarea and
// POST flow remain unchanged.
import { bind } from "./dom-hooks.js";
import { t } from "./i18n.js";
import { markDirty } from "./leave-guard.js";

let nextID = 0;

function previewURL() {
  const match = location.pathname.match(/^\/w\/([^/]+)(?:\/|$)/);
  return match ? `/w/${encodeURIComponent(match[1])}/preview` : "/preview";
}

function draftKey(textarea) {
  const id = textarea.id || textarea.closest("form")?.action || "body";
  return `draft:${location.pathname}:${id}`;
}

function readDraft(key) {
  try { return localStorage.getItem(key); } catch { return null; }
}

function writeDraft(key, value) {
  try {
    if (value === "") localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    // Private-mode quota failures must not break typing.
  }
}

function clearPendingDrafts() {
  let raw = null;
  try { raw = sessionStorage.getItem("draft:pending"); } catch { return; }
  if (!raw) return;
  try {
    const pending = JSON.parse(raw);
    if (location.pathname !== pending.from) {
      try { localStorage.removeItem(pending.key); } catch {}
      sessionStorage.removeItem("draft:pending");
    }
  } catch {
    sessionStorage.removeItem("draft:pending");
  }
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
  textarea.addEventListener("input", () => saveState(textarea, state));
  textarea.addEventListener("change", () => saveState(textarea, state));
}

function trackSubmit(form) {
  if (!(form instanceof HTMLFormElement)) return;
  form.addEventListener("submit", () => {
    if (form.hasAttribute("data-partial")) return;
    const textarea = form.querySelector('textarea[name="body"]');
    if (!textarea) return;
    try {
      sessionStorage.setItem("draft:pending", JSON.stringify({
        key: draftKey(textarea), from: location.pathname,
      }));
    } catch {}
  });
  form.addEventListener("reset", () => {
    // partial-submit.js fires reset only after a successful partial post.
    queueMicrotask(() => {
      const textarea = form.querySelector('textarea[name="body"]');
      if (textarea) {
        try { localStorage.removeItem(draftKey(textarea)); } catch {}
      }
    });
  });
}

clearPendingDrafts();
bind('textarea[name="body"]', { init: enhance });
bind("form", { init: trackSubmit });
