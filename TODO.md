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

- [x] **Breadcrumb の構造化（issue・discussion 詳細共通）**: `breadcrumbs` ガイド準拠にする。現行はフラット `<nav>` + `aria-hidden` な `/` テキスト + `<span>` の現在地。`<ol><li><a>` の構造に変え、現在地は `aria-current="page"` のリンク（`#` 自己参照）にし、区切りは CSS の `li + li::before { content: "/" / ""; }` で生成してマークアップから取り除く。対象: `issue_detail.html` / `discussion_detail.html` の `.breadcrumb`。
- [ ] **コメント行ごとの同名コントロールを一意化（discussion 詳細、issue 側も再点検）**: `accessibility` ガイド「見た目が同じで動作が違うコントロールは accessible name で区別」。discussion 詳細の各コメントにある "Reply" `<summary>` と "Mark/Unmark answer" ボタンは全て同名 — copy link ボタンの `aria-label="Copy link to comment {id}"` と同じパターンで `Reply to comment {id}` / `Mark comment {id} as answer` などにする。issue 側は copy link 以外に行ごとのコントロールが無いか確認（あれば同様に）。
- [ ] **コメント/返信の部分更新（手書き fetch + HTML fragment）**（**JS**）: `docs/sketch/ja/partial-updates.md` の 2026-11-15 結論（案B）に基づく。関連ガイド: `sanitize-untrusted-html`, `move-dom-element-without-losing-state`, `persistent-toast-notifications`, `spinner`。
  - [x] **サーバ側**: CSP に `connect-src 'self'` 追加。`templates/fragments.html` に `{{define "issue_comment"/"discussion_comment"/"discussion_reply"}}` + `{{define "issue_comment_form"/"discussion_comment_form"/"discussion_reply_form"}}` を切り出し、renderer が block 単位で描画。`createComment`・`createDiscussionComment` は `X-Partial: 1` なら新コメントの `<li>` fragment（`text/html`）、バリデーション失敗なら 422 + エラーフォーム fragment を返す。通常 submit は従来どおり 303 PRG（progressive enhancement）。
  - [x] **クライアント側**: `/static/partial-submit.js`（最初の ES module、`docs/sketch/ja/js-modules.md`）で `form[data-partial]` の submit を奪取→`fetch`→`<template>` パース（`Document.parseHTML` は Sanitizer が class/id を削るため不採用、pitfalls.md）→`data-partial-target` へ append。送信中は submit `disabled`+`aria-busy`+`<progress>`（indeterminate）、二重送信は in-flight フラグ。完了でフォームリセット・フォーカスは textarea に残す・共有 announcer（`/static/announcer.js`、form-errors.js も import に移行し `type="module"` 化）で polite 通知。422 は form fragment を差し替え。`enter-submit.js` は document delegation 化。テンプレート側は `data-partial`/`data-partial-target` 属性、空でも `<ol>` を `hidden` で常時描画、`data-empty-note`・`data-comment-count` を追加。新 `<li>` は一時 class でハイライト（`:target` は部分挿入では効かない）。
  - [x] **storybook 検証**: fragment 見た目の print story + `StoryHttp` story（X-Partial POST→fragment、通常 POST→303、X-Partial 空 POST→422）を `story_pages.go` に追加。`cmd/storybook` に catch-all mount（実リクエストを `StoryHttpApp` story に渡し `New(storyStore)` を実パスで動かす）を実装し、`fetch`→fragment→DOM swap まで実ブラウザで確認可能にする（storyStore は stateless fake — POST→GET の持続は実アプリ側 verify-ui で確認）。
  - [ ] **Phase 2（別タスク）**: answer マーク・status toggle を同機構に乗せる（`<li>`/ピル差し替え、`moveBefore` 活用・Safari は `insertBefore` fallback）。一覧フィルタ・削除・編集・認証フォームは navigation のまま。
