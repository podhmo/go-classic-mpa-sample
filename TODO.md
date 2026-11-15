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

- [ ] **fake/storyStore と実 store の契約乖離を整理する**: no-comments パスの追検証で未宣言の意味論乖離を発見（詳細: `docs/sketch/ja/no-comments.md`「Same contract as the real store」項）。`storyStore.SetCommentAnswer` は reply マークで `ErrNotFound`（本物は `ErrNotAnswerable`）を返し非 qa カテゴリの拒否チェックも無い、`storyStore.ListIssues` は `ListFilter` 無視、`fakeStore.SessionUser` は ttl 無視、`APITokenUser` は常に `ErrNotFound`。やり方: (1) 乖離が意図的なスタブなら storyStore/fakeStore の doc コメントに「契約のどこを省いているか」を明記して宣言に留める、(2) `SetCommentAnswer` の reply→`ErrNotAnswerable` と非 qa 拒否は storybook の swap フラグメント挙動に関わるので本物に揃える、(3) 根治として `*store.Store` / `*fakeStore` / `storyStore` に同一の振る舞いスイート（bump 規則・エラー種別・フィルタ意味論）を流す共有 contract test を `internal/web` か `internal/store` に置く。
