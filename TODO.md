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

- [x] **Discussions タブ（GitHub Discussions 風）**: `/discussions` 系の追加。カテゴリ（単一選択・シード定義、format: open/qa）・1段スレッド返信・qa の answer マークを採用。reactions・poll・issue→discussion 変換・pin・カテゴリ管理 UI は見送り（設計メモ「見送り」節）。設計: `docs/sketch/ja/discussions.md`（案B: 並列テーブル）
  - [x] **Phase 1: 骨格** — migration V3（`discussion_categories`/`discussions`/`discussion_comments` + 部分一意 index）・store メソッド・一覧/詳細/作成/編集/削除/コメント投稿のルート・ナビの Issues/Discussions タブ化（view model に `Section` を追加し New ボタンを文脈分岐）・カテゴリ＋デモ discussion のシード・store/web テスト。コメント表示は時系列フラット（スレッド化は Phase 2）。
  - [x] **Phase 2: 対話構造** — 1段ネスト表示と `<details>` 内返信フォーム（hidden `parent_id`、store 層で「同一 discussion の top-level」検証）、qa のみ answer マーク/解除（`POST .../comments/{cid}/answer`、2行更新を1トランザクション＋部分一意 index）、詳細・一覧の Answered バッジと `?answered=yes|no` フィルタ、answer/返信で `updated_at` bump。
  - [x] **Phase 3: 仕上げ** — speculation-rules の discussions 対応、search-highlight.js の selector 一般化（**JS**）、storybook story 充実、`docs/ja/roadmap.md` の v2 欄更新。
