// Live updates (docs/sketch/ja/live-updates.md): an EventSource listens
// on the detail page's per-resource SSE channel; each pushed event is
// only a hint ({kind, target, parent}) — the client refetches the
// matching server-rendered fragment and upserts it, so markdown stays
// server-side truth. Events are debounced/coalesced (a burst like an AI
// posting several comments becomes one fetch), and any lost connection
// triggers a whole-list resync on reconnect — missed hints are expected,
// never replayed.
//
// Progressive enhancement: with JS off (or EventSource missing) the page
// is a normal static detail page; nothing here is required for posting.
import { announce } from "./announcer.js";
import { markDirty } from "./leave-guard.js";
import { parseFragment, runScopedTransition, commonScope } from "./fragments.js";

const section = document.querySelector("[data-live-events]");
if (section && typeof EventSource === "function") start(section);

function start(section) {
  const kind = section.dataset.liveKind; // "issue" | "discussion"
  const eventsURL = section.dataset.liveEvents;
  const fragBase = section.dataset.liveFragments;
  const listURL = section.dataset.liveList;
  const list = section.querySelector(".comment-list");
  let lost = false;

  // Events accumulate into one pending batch; flush() runs ~250ms after
  // the last event so bursts collapse into single fragment fetches.
  const pending = {
    ids: new Set(),        // issue comments / discussion threads to upsert
    replies: new Map(),    // thread id -> Set(reply ids)
    answerIds: new Set(),  // threads re-rendered by an answer move
    removes: [],           // deleted comment-level rows {target, parent}
    article: false,
    deleted: false,
    resync: false,
  };
  let timer = 0;
  let flushing = false;
  let queued = false;

  const es = new EventSource(eventsURL);
  es.addEventListener("comment", (e) => add(pending.ids, e));
  es.addEventListener("thread", (e) => add(pending.ids, e));
  es.addEventListener("reply", (e) => {
    const { target, parent } = JSON.parse(e.data);
    if (!pending.replies.has(parent)) pending.replies.set(parent, new Set());
    pending.replies.get(parent).add(target);
    poke();
  });
  es.addEventListener("answer", (e) => {
    const { target, parent } = JSON.parse(e.data);
    pending.answerIds.add(target);
    if (parent) pending.answerIds.add(parent);
    poke();
  });
  es.addEventListener("status", () => mark("article"));
  es.addEventListener("edit", () => mark("article"));
  es.addEventListener("new", () => {});
  es.addEventListener("delete", () => mark("deleted"));
  // A comment-level row (issue comment / thread / reply) was deleted:
  // drop the node directly — issue comments live under #comment-N,
  // threads under #thread-N, replies share the #comment-N namespace.
  es.addEventListener("remove", (e) => {
    pending.removes.push(JSON.parse(e.data));
    poke();
  });
  es.addEventListener("resync", () => mark("resync"));
  // EventSource fires onerror while retrying (its own backoff honors our
  // `retry:` directive); onopen covers the successful reconnect.
  es.onerror = () => {
    lost = true;
  };
  es.onopen = () => {
    if (lost) mark("resync");
    lost = false;
  };

  function add(set, e) {
    set.add(JSON.parse(e.data).target);
    poke();
  }
  function mark(key) {
    pending[key] = true;
    poke();
  }
  function poke() {
    clearTimeout(timer);
    timer = setTimeout(flush, 250);
  }

  // A replaced element may hold a reply draft (details.reply-details on
  // a thread <li>) — open or merely closed-with-text: stash both state
  // and value and restore onto the freshly rendered node so an answer
  // mark never eats a half-typed reply.
  function stashDrafts(root) {
    const drafts = new Map();
    for (const d of root.querySelectorAll("details.reply-details")) {
      const ta = d.querySelector("textarea");
      const li = d.closest("li[id]");
      if (li && ta && (d.open || ta.value !== "")) {
        drafts.set(li.id, { open: d.open, text: ta.value });
      }
    }
    return drafts;
  }
  function restoreDrafts(root, drafts) {
    for (const [liId, draft] of drafts) {
      const li = root.querySelector(`li#${CSS.escape(liId)}`);
      const d = li?.querySelector("details.reply-details");
      const ta = d?.querySelector("textarea");
      if (d && ta) {
        d.open = draft.open;
        ta.value = draft.text;
        // Programmatic assignment raises no input event, so the restored
        // draft would match the baseline leave-guard snapshots on attach
        // — mark the form dirty or leaving drops the draft silently.
        if (draft.text !== "") markDirty(ta.closest("form"));
      }
    }
  }

  // Upsert one fragment node: data-swap nodes (the header meta, bumped
  // updated_at) and id-carrying nodes that already exist replace their
  // stale counterpart; genuinely new <li>s append to container and are
  // highlighted like a self-posted comment.
  function upsertNode(node, container) {
    const existing = node.id ? document.getElementById(node.id) : null;
    if (node.hasAttribute?.("data-swap") || existing) {
      const drafts = stashDrafts(existing ?? node);
      existing?.replaceWith(node);
      restoreDrafts(node, drafts);
      return null;
    }
    container.append(node);
    return node;
  }

  // Fetch a fragment, then inside one scoped transition swap every node.
  // Returns the appended (never-updated) nodes for highlight + announce.
  async function applyFragment(url, pickContainer) {
    const res = await fetch(url);
    if (!res.ok) return null;
    const nodes = parseFragment(await res.text()).filter(
      (n) => n.nodeType === Node.ELEMENT_NODE,
    );
    if (!nodes.length) return null;
    const appended = [];
    const affected = nodes
      .map((n) => (n.id ? document.getElementById(n.id) : null))
      .filter(Boolean);
    const drafts = stashDrafts(section);
    await runScopedTransition(
      commonScope([list, ...affected].filter(Boolean)),
      () => {
        for (const node of nodes) {
          const container = node.hasAttribute?.("data-swap")
            ? null
            : pickContainer(node);
          if (!container && !node.hasAttribute?.("data-swap")) continue;
          const added = upsertNode(node, container);
          if (added) appended.push(added);
        }
        restoreDrafts(section, drafts);
        if (appended.length) {
          list.hidden = false;
          const note = section.querySelector("[data-empty-note]");
          if (note) note.hidden = true;
          const count = section.querySelector("[data-comment-count]");
          if (count) {
            count.textContent = String(
              Number.parseInt(count.textContent, 10) + appended.length,
            );
            count.parentElement.hidden = false;
          }
        }
      },
    );
    return appended;
  }

  function highlight(nodes) {
    for (const n of nodes) {
      n.classList.add("comment-new");
      setTimeout(() => n.classList.remove("comment-new"), 4000);
    }
    nodes.at(-1)?.scrollIntoView({ block: "nearest" });
  }

  // Whole-list rebuild: drafts preserved across the rebuild; nodes whose
  // id already exists get replaced, everything else appended, and stale
  // leftovers (deleted rows) are dropped — replaceChildren owns the list.
  async function resync() {
    const url = `${fragBase}/${kind === "issue" ? "comments" : "threads"}`;
    const res = await fetch(url);
    if (!res.ok) return;
    const nodes = parseFragment(await res.text()).filter(
      (n) => n.nodeType === Node.ELEMENT_NODE,
    );
    const drafts = stashDrafts(section);
    const keep = new Set(nodes.map((n) => n.id).filter(Boolean));
    const stale = [...list.children].filter((c) => c.id && !keep.has(c.id));
    await runScopedTransition(commonScope([list]), () => {
      for (const node of nodes) {
        if (node.hasAttribute?.("data-swap")) {
          upsertNode(node, null);
          continue;
        }
        const existing = node.id ? document.getElementById(node.id) : null;
        if (existing) {
          existing.replaceWith(node);
        } else {
          list.append(node);
        }
      }
      for (const s of stale) s.remove();
      restoreDrafts(section, drafts);
      list.hidden = nodes.length <= 1; // meta node only => empty
      const note = section.querySelector("[data-empty-note]");
      if (note) note.hidden = nodes.length > 1;
      const count = section.querySelector("[data-comment-count]");
      if (count) {
        const n = list.querySelectorAll(":scope > li").length;
        count.textContent = String(n);
        count.parentElement.hidden = n === 0;
      }
    });
  }

  async function flush() {
    if (flushing) {
      queued = true;
      return;
    }
    flushing = true;
    try {
      if (pending.deleted) {
        es.close();
        // The watched resource is gone: a persistent notice beats a
        // transient toast — the user may be mid-read.
        if (!section.querySelector("[data-live-deleted]")) {
          const p = document.createElement("p");
          p.className = "card";
          p.setAttribute("role", "alert");
          p.dataset.liveDeleted = "";
          const a = document.createElement("a");
          a.href = listURL;
          a.textContent =
            kind === "issue" ? "Back to issues" : "Back to discussions";
          p.append(
            document.createTextNode("This " + kind + " was deleted. "),
            a,
          );
          section.before(p);
        }
        announce(
          kind === "issue"
            ? "This issue was deleted."
            : "This discussion was deleted.",
        );
        return;
      }
      if (pending.resync) {
        await resync();
      } else {
        if (pending.article) {
          const res = await fetch(`${fragBase}/article`);
          if (res.ok) {
            const nodes = parseFragment(await res.text()).filter(
              (n) => n.nodeType === Node.ELEMENT_NODE,
            );
            const old = document.querySelector(".issue-detail");
            if (old && nodes[0]) {
              await runScopedTransition(commonScope([old]), () =>
                old.replaceWith(nodes[0]),
              );
              announce(
                kind === "issue" ? "Issue updated." : "Discussion updated.",
              );
            }
          }
        }
        if (pending.answerIds.size) {
          const ids = [...pending.answerIds].join(",");
          const appended = await applyFragment(
            `${fragBase}/threads?ids=${ids}`,
            () => list,
          );
          if (appended?.length) highlight(appended);
          announce("Answer updated.");
        }
        if (pending.ids.size) {
          const ids = [...pending.ids].join(",");
          const path = kind === "issue" ? "comments" : "threads";
          const appended = await applyFragment(
            `${fragBase}/${path}?ids=${ids}`,
            () => list,
          );
          if (appended?.length) {
            highlight(appended);
            announce(
              kind === "issue" ? "New comment." : "New thread.",
            );
          }
        }
        for (const [threadId, ids] of pending.replies) {
          const ol = section.querySelector(
            `#thread-${CSS.escape(String(threadId))} .comment-replies`,
          );
          if (!ol) continue;
          const appended = await applyFragment(
            `${fragBase}/replies?thread=${threadId}&ids=${[...ids].join(",")}`,
            () => ol,
          );
          if (appended?.length) {
            ol.hidden = false;
            highlight(appended);
            announce("New reply.");
          }
        }
        for (const { target, parent } of pending.removes) {
          const node = document.getElementById(
            parent || kind === "issue" ? `comment-${target}` : `thread-${target}`,
          );
          if (!node) continue;
          const ol = node.parentElement;
          node.remove();
          // An emptied replies <ol> returns to its initial hidden state.
          if (ol?.classList.contains("comment-replies") && !ol.children.length) {
            ol.hidden = true;
          }
        }
        if (pending.removes.length) {
          // Recount top-level items like resync does (reply removals
          // leave it untouched — replies are nested, not :scope > li).
          const n = list.querySelectorAll(":scope > li").length;
          const count = section.querySelector("[data-comment-count]");
          if (count) {
            count.textContent = String(n);
            count.parentElement.hidden = n === 0;
          }
          list.hidden = n === 0;
          const note = section.querySelector("[data-empty-note]");
          if (note) note.hidden = n !== 0;
        }
      }
    } finally {
      flushing = false;
      pending.ids.clear();
      pending.replies.clear();
      pending.answerIds.clear();
      pending.removes.length = 0;
      pending.article = pending.deleted = pending.resync = false;
      if (queued) {
        queued = false;
        poke();
      }
    }
  }
}
