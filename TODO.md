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

- [-] **fake/stub の契約乖離を全コードから探索し整理する**: 探索は完了 — 全 5 interface × 全実装の意味論突き合わせ結果を `docs/sketch/ja/no-comments.md`「契約乖離の全コード sweep」節に記録（2026-11-16）。残りは整理作業:
  - **修正**: `storyStore.SetCommentAnswer`（reply の mark/unmark → `ErrNotFound` を本物同様 `ErrNotAnswerable` に + 非 qa 拒否 — storybook の swap フラグメント挙動に関わる）、`fakeStore.SetIssueStatus` の値検証、`fakeStore.ListIssues` の `LabelID != 0`→`> 0`、LabelIDs dedup・ラベル name 順ソート、storyStore の `Answered` 再計算、`fakeStore.UpdateDiscussion` のチェック順序
  - **宣言**: storyStore/fakeStore の意図的スタブ（フィルタ無視・auth 常時成功・ttl/期限なし・`APITokenUser` stub・create/update/delete の no-op など）に省略範囲の doc コメント。`*client.Client` の sentinel 未写像も併せて宣言 or 404→`ErrNotFound` 写像 / API 側機械可読エラーコード化を検討（現状 remote で sentinel 判定不能な潜在乖離、即実害なし）
  - **根治**: `*store.Store` / `fakeStore` / `storyStore` に同一振る舞いスイート（bump 規則・エラー種別・フィルタ意味論）を流す共有 contract test。fake のポインタ共有は answer-swap ハンドラが依存しているので本物に揃えず「fake の仕様として宣言」する方向
  - **副産物**: `web_test.go` 末尾の宙吊りバナー `/* ---------- accounts ---------- */` は no-comments パスの削除漏れ
