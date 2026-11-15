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
- [x] **コメント行ごとの同名コントロールを一意化（discussion 詳細、issue 側も再点検）**: `accessibility` ガイド「見た目が同じで動作が違うコントロールは accessible name で区別」。discussion 詳細の各コメントにある "Reply" `<summary>` と "Mark/Unmark answer" ボタンは全て同名 — copy link ボタンの `aria-label="Copy link to comment {id}"` と同じパターンで `Reply to comment {id}` / `Mark comment {id} as answer` などにする。issue 側は copy link 以外に行ごとのコントロールが無いか確認（あれば同様に）。
- [x] **コメント/返信の部分更新（手書き fetch + HTML fragment）**（**JS**）: `docs/sketch/ja/partial-updates.md` の 2026-11-15 結論（案B）に基づく。関連ガイド: `sanitize-untrusted-html`, `move-dom-element-without-losing-state`, `persistent-toast-notifications`, `spinner`。
  - [x] **サーバ側**: CSP に `connect-src 'self'` 追加。`templates/fragments.html` に `{{define "issue_comment"/"discussion_comment"/"discussion_reply"}}` + `{{define "issue_comment_form"/"discussion_comment_form"/"discussion_reply_form"}}` を切り出し、renderer が block 単位で描画。`createComment`・`createDiscussionComment` は `X-Partial: 1` なら新コメントの `<li>` fragment（`text/html`）、バリデーション失敗なら 422 + エラーフォーム fragment を返す。通常 submit は従来どおり 303 PRG（progressive enhancement）。
  - [x] **クライアント側**: `/static/partial-submit.js`（最初の ES module、`docs/sketch/ja/js-modules.md`）で `form[data-partial]` の submit を奪取→`fetch`→`<template>` パース（`Document.parseHTML` は Sanitizer が class/id を削るため不採用、pitfalls.md）→`data-partial-target` へ append。送信中は submit `disabled`+`aria-busy`+`<progress>`（indeterminate）、二重送信は in-flight フラグ。完了でフォームリセット・フォーカスは textarea に残す・共有 announcer（`/static/announcer.js`、form-errors.js も import に移行し `type="module"` 化）で polite 通知。422 は form fragment を差し替え。`enter-submit.js` は document delegation 化。テンプレート側は `data-partial`/`data-partial-target` 属性、空でも `<ol>` を `hidden` で常時描画、`data-empty-note`・`data-comment-count` を追加。新 `<li>` は一時 class でハイライト（`:target` は部分挿入では効かない）。
  - [x] **storybook 検証**: fragment 見た目の print story + `StoryHttp` story（X-Partial POST→fragment、通常 POST→303、X-Partial 空 POST→422）を `story_pages.go` に追加。`cmd/storybook` に catch-all mount（実リクエストを `StoryHttpApp` story に渡し `New(storyStore)` を実パスで動かす）を実装し、`fetch`→fragment→DOM swap まで実ブラウザで確認可能にする（storyStore は stateless fake — POST→GET の持続は実アプリ側 verify-ui で確認）。
  - [x] **Phase 2（別タスク）**: answer マーク・status toggle を同機構に乗せる（`<li>`/ピル差し替え、`moveBefore` 活用・Safari は `insertBefore` fallback）。一覧フィルタ・削除・編集・認証フォームは navigation のまま。実装メモ: `moveBefore` は接続済みノード専用の state-preserving move で detach 済み fragment を受け付けない（HierarchyRequestError、実機検証済み）ため、差し替えは `replaceWith` で実装。
- [ ] **部分更新 append パスのメタ行 stale**: コメント/返信の部分投稿で store は `updated_at` を進めるが、fragment は新 `<li>` だけを返すためヘッダの "Last updated" がリロードまで古いまま（Phase 2 の answer swap は `discussion_meta` ノード同梱で解決済み、PR #49 の review finding 由来）。append モードは全ノードを target へ挿入するので、自己ターゲットする swap ノードを混ぜるにはプロトコル拡張が要る（例: fragment 内で `data-swap` 印のノードは `replaceWith`、残りを append）。
- [ ] **`<details>` の開閉アニメーション**: 返信フォーム等の details 展開を `interpolate-size: allow-keywords` + `::details-content` で高さアニメーションに（modern-web-guidance `animate-to-intrinsic-sizes`）。非対応ブラウザは従来どおり瞬時表示で graceful。
- [ ] **部分更新の swap を scoped view transition で滑らかに**: `partial-submit.js` の DOM 差し替え（コメント挿入・422 フォーム差し替え・answer/status pill）を `document.startViewTransition` で囲み視覚遷移に（modern-web-guidance `scoped-component-transitions`）。`prefers-reduced-motion` と非対応ブラウザは素の swap にフォールバック。
- [ ] **投稿成功の視覚トースト**: 部分更新の成功フィードバックは現在 announcer（SR 専用）のみ。`aria-live` toast で「コメントを投稿しました」等を視覚的にも出す（modern-web-guidance `persistent-toast-notifications`）。
- [ ] **タブの移動アンダーライン**: issues 一覧の open/closed/all タブで選択中インジケータを anchor positioning で遷移させる（modern-web-guidance `anchor-positioning-tab-underline`）。view-transition 中の要素追従に注意。
- [ ] **ナビゲーション方向に応じた遷移**: cross-document view transition を前進/後退で方向づけ（一覧→詳細はスライドイン、戻るは逆方向）に（modern-web-guidance `directional-navigation-transitions`）。現行の一律 cross-fade を拡張。
- [ ] **長ページ用 back-to-top**: issue/discussion 詳細で下部までスクロールしたとき浮遊ボタンを scroll-driven animation で出現させる（modern-web-guidance `scroll-position-aware-elements`）。非対応ブラウザは常時表示または非表示で graceful。
- [-] **JSON Web API (`/api/v1`)** — `docs/sketch/ja/cli-and-web-api.md` の Phase 2。`internal/api` に consumer 側 Store interface + DTO + ハンドラ。Bearer は最小の単一 env トークン（`ISSUES_API_TOKEN`、未設定なら mount しない fail-closed。API 書き込みは anonymous 扱い）。エラー形式 `{"error":{"message","fields"}}`、ステータスは HTML 側と同じ意味論。
  - [x] **骨格 + Bearer fail-closed**: `internal/api` 新設、mux に `/api/v1` サブツリー mount（token 未設定時は mount 自体しない）。`Authorization: Bearer` を `crypto/subtle` 定数時間比較で検証、不一致・欠落は 401 JSON。
  - [x] **issues/comments/labels endpoints**: `GET /api/v1/issues`（status/q/label/sort/page/per_page）、`GET /api/v1/issues/{id}`（labels+comments 同梱）、`POST /api/v1/issues`、`PATCH /api/v1/issues/{id}`、`POST /api/v1/issues/{id}/status`、`DELETE /api/v1/issues/{id}`、`POST /api/v1/issues/{id}/comments`、`GET /api/v1/labels`。
  - [x] **バリデーションの共有**: `validateIssueInput`/`validateDiscussionInput` を「`*Input` 値を検証する関数」に分解して web/api 両方から使う（`internal/store` の `ValidateIssueInput`/`ValidateDiscussionInput`/`ValidateCommentBody` + `MaxTitleRunes`/`MaxBodyRunes` に集約）。
  - [x] **discussions endpoints**: `GET|POST /api/v1/discussions`、`GET|PATCH|DELETE /api/v1/discussions/{id}`、`POST /api/v1/discussions/{id}/comments`（`parent_id` で返信）、`POST /api/v1/discussions/{id}/comments/{cid}/answer`、`GET /api/v1/discussion-categories`。
  - [ ] **api_tokens（ユーザー別トークン）**: token→user_id の `api_tokens` テーブル（migrateV4）+ `issues auth token` 発行サブコマンド（gh parity の actor 識別。cli-and-web-api.md「認証」節）。単一 token の次の段階として検討。
