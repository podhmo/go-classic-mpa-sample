# TODO — Go classic MPA Issue Tracker

> **Note on updating this file:** upkeep rules live in
> [`.agents/skills/todo-md/SKILL.md`](.agents/skills/todo-md/SKILL.md) —
> follow them whenever you add or complete a task. Short version: this file
> lists actionable work only (`[ ]` / `[-]`); mark finished work `[x]` — a
> periodic GC sweep (`make todo-gc`) appends such entries to `TODO-archive.md`.

Source spec: `docs/ja/prompts.md` (Go 1.27+, `net/http`, `html/template`, `database/sql`, SQLite via `modernc.org/sqlite`, no JavaScript, no web frameworks, no ORM).

Module path: `github.com/podhmo/go-classic-mpa-sample`

## To Be Implemented

Guides referenced below are in the `modern-web-guidance` catalog (`.agents/skills/modern-web-guidance/`): run `npx -y modern-web-guidance@latest retrieve "<id>"` before implementing. Prefer declarative HTML/CSS; JavaScript is allowed only when its behavior can be verified via the storybook (`docs/ja/decisions.md` "JavaScript 方針") — entries marked **JS** need a `/static/` external file (CSP `script-src 'self'`; inline `<script>` stays blocked). When the first JS file lands, update the "No JavaScript." claim in `layout.html`'s footer.

- [ ] **Breadcrumb の構造化（issue・discussion 詳細共通）**: `breadcrumbs` ガイド準拠にする。現行はフラット `<nav>` + `aria-hidden` な `/` テキスト + `<span>` の現在地。`<ol><li><a>` の構造に変え、現在地は `aria-current="page"` のリンク（`#` 自己参照）にし、区切りは CSS の `li + li::before { content: "/" / ""; }` で生成してマークアップから取り除く。対象: `issue_detail.html` / `discussion_detail.html` の `.breadcrumb`。
- [ ] **コメント行ごとの同名コントロールを一意化（discussion 詳細、issue 側も再点検）**: `accessibility` ガイド「見た目が同じで動作が違うコントロールは accessible name で区別」。discussion 詳細の各コメントにある "Reply" `<summary>` と "Mark/Unmark answer" ボタンは全て同名 — copy link ボタンの `aria-label="Copy link to comment {id}"` と同じパターンで `Reply to comment {id}` / `Mark comment {id} as answer` などにする。issue 側は copy link 以外に行ごとのコントロールが無いか確認（あれば同様に）。
- [ ] **コメント/返信の部分更新（手書き fetch + HTML fragment）**（**JS**）: `docs/sketch/ja/partial-updates.md` の 2026-11-15 結論に基づく。`X-Partial` 判定で POST が `<li>` fragment を返す + `/static/partial-submit.js` で submit 奪取→fetch→`setHTML`/`parseHTML` 挿入→polite 通知。前提として CSP に `connect-src 'self'` を追加、`{{define "comment"}}` の切り出し、`enter-submit.js` の delegation 化。answer マーク・status toggle は Phase 2 で同機構に乗せる（`moveBefore` 活用）。関連ガイド: `sanitize-untrusted-html`, `move-dom-element-without-losing-state`, `persistent-toast-notifications`, `spinner`。
