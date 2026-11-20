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

- [ ] **fake/stub 向けの共有 contract test を書く**: `*store.Store` / `fakeStore` / `storyStore` に同一振る舞いスイート（bump 規則・エラー種別・フィルタ意味論）を流す。fake のポインタ共有は answer-swap ハンドラが依存しているので本物に揃えず「fake の仕様として宣言」する方向（詳細: PR #71）
- [ ] **`*client.Client` の sentinel 非写像の方針を決める**: remote モードでは `errors.Is(err, store.ErrNotFound)` が成立しない潜在乖離（現状 sentinel 判定する消費者は local 限定で実害なし）。404→`ErrNotFound` 写像 or API の機械可読エラーコード化を検討（bugs.md #27）
- [x] **Pull requests を git コマンドで再現する**: `docs/sketch/ja/pull-requests.md` の設計どおりに実装。実体は DB 行 + (base_ref, head_ref)、コミット列・diff・conflict は open 中 git 都度計算、close/merge 時に `refs/zatudan/pr/<id>/head` を pin。repo 未検出時は機能ごと非表示（`-repo`/`ZATUDAN_REPO`、auto は -db 起点の `.git` 上方探索）
  - [x] **api: /api/v1/pulls\***: pulls GET/POST、{id} GET/PATCH/DELETE、comments GET/POST、comments/{cid} PATCH/DELETE + hide/unhide、status POST、merge POST、diff GET（text/plain）、markdown GET。repo 無効 server では未マウント（404）（§7）
  - [x] **cli: zatu pr**: list/view/diff/create/edit/close/reopen/merge/comment 動詞（create は `--path P --line L [--side left]`）。local モードは `--db` 起点の repo 解決（`--repo` 上書き、未検出は `not a git repository` で exit 1）、remote は /api/v1/pulls\* 経由（`--repo` は警告して無視）。completion.go と printUsage の機械的更新（§7）
  - [x] **export: PrMarkdown**: review comment を `path:L42` (side) + `path L<開始>-<終了>` + プレーン fenced スニペット（DB の diff_snippet、行番号プレフィックスなし）+ 本文の形で出力。呼び出し側が集めた構造体を受ける純粋関数。`--json` は同構造の struct（§7）
  - [x] **seed/storybook/demo**: storybook story は gitrepo の fake + 偽データ（PR #103）。実動作確認用スクリプトは `tools/demo-pr-history.sh`（fixture repo + `zatu pr` リプレイ）
  - [x] **docs 更新**: decisions.md（PR の実体・ref pin・merge 2 経路・auto-detect の解決規則）、roadmap.md、ui-rules.md（/pulls の UI 規約）
