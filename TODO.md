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

- [x] **zatu の --help 整備と agent 向け zatu-cli skill**: 全階層（top/group/leaf）で `-h/--help` を stdout・exit 0 にし、leaf の flag 一覧は `flag.PrintDefaults` から生成して正を flag 定義に一元化。skill `.agents/skills/zatu-cli/` は「--help を引け」に絞り、リプレイ例は `tools/demo-history.sh` が担う。
- [ ] **fake/stub 向けの共有 contract test を書く**: `*store.Store` / `fakeStore` / `storyStore` に同一振る舞いスイート（bump 規則・エラー種別・フィルタ意味論）を流す。fake のポインタ共有は answer-swap ハンドラが依存しているので本物に揃えず「fake の仕様として宣言」する方向（詳細: PR #71）
- [ ] **`*client.Client` の sentinel 非写像の方針を決める**: remote モードでは `errors.Is(err, store.ErrNotFound)` が成立しない潜在乖離（現状 sentinel 判定する消費者は local 限定で実害なし）。404→`ErrNotFound` 写像 or API の機械可読エラーコード化を検討（bugs.md #27）
