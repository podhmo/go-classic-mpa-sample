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

- [x] **ラベルを DB 管理化（定数の廃止）**: `labels` に `workspace_id`（NULL=マスター）・`kind`（issue/discussion）・`archived` を追加し、既定ラベルは `defaultLabelsDDL` のデータマイグレーションで投入。管理画面 `/w/{ws}/labels`（追加/編集/削除=アーカイブ/復帰）と discussion のラベル選択 UI、API `/api/v1/w/{ws}/labels`、CLI `zatu label create|delete` を実装（PR 参照）
- [ ] **fake/stub 向けの共有 contract test を書く**: `*store.Store` / `fakeStore` / `storyStore` に同一振る舞いスイート（bump 規則・エラー種別・フィルタ意味論）を流す。fake のポインタ共有は answer-swap ハンドラが依存しているので本物に揃えず「fake の仕様として宣言」する方向（詳細: PR #71）
- [ ] **`*client.Client` の sentinel 非写像の方針を決める**: remote モードでは `errors.Is(err, store.ErrNotFound)` が成立しない潜在乖離（現状 sentinel 判定する消費者は local 限定で実害なし）。404→`ErrNotFound` 写像 or API の機械可読エラーコード化を検討（bugs.md #27）
