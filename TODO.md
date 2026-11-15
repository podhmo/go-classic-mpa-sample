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

- [ ] **fake/stub の契約乖離を全コードから探索し整理する**: no-comments パスで web の fakeStore/storyStore に未宣言の意味論乖離を発見（`docs/sketch/ja/no-comments.md`「Same contract as the real store」項。`storyStore.SetCommentAnswer` は reply マークで `ErrNotFound`〈本物は `ErrNotAnswerable`〉+ 非 qa 拒否なし、`storyStore.ListIssues` は `ListFilter` 無視、`fakeStore.SessionUser` は ttl 無視、`APITokenUser` は常時 `ErrNotFound`）。全コード探索でさらに `*client.Client`（cli.Backend の HTTP 実装）が store sentinel エラーに写像しない潜在乖離も発見済み。同種の乖離が他パッケージの fake・スタブ・fixture 実装にも潜む可能性がある。
  - **探索の仕方**: (1) repo 内の interface 定義を列挙（`rg 'type \w+ interface'` → web.Store・api.Store・cli.Backend・cli.TokenStore など）。(2) 各 interface の実装を列挙（`var _ X =` アサーション、コンストラクタ呼び出し、`rg 'func \(.*\) メソッド名'`）— シグネチャ乖離はコンパイルで防がれるので対象は**意味論のみ**。(3) 本物実装の契約をメソッドごとに特定（doc コメント・返すエラー種別・副作用〈updated_at bump・CASCADE〉・デフォルト値・フィルタ/ソート/ページング意味論）。(4) fake/stub 実装を同じ観点で読み、返り値・エラー種別・副作用を突き合わせる。(5) `rg 'Same contract|same contract|contract as the real'` で自己申告コメントを洗い、申告と実装の一致も確認。(6) `git log -S '<契約の変わった処理>'` で契約変更コミットに fake 側の同梱更新があったか履歴確認（欠けていたら乖離の疑い）。(7) 見つけた乖離は「意図的スタブ（宣言で済む）」と「実バグ（修正要）」に分類。
  - **整理の方向**: 意図的スタブは doc コメントで省略範囲を宣言、実バグは本物に揃える（`storyStore.SetCommentAnswer` の reply→`ErrNotAnswerable`・非 qa 拒否は storybook の swap フラグメント挙動に関わるので揃える）。`*client.Client` は 404→`ErrNotFound` の写像追加 or API 側の機械可読エラーコード化を検討（現状 remote で sentinel 判定不能な潜在乖離、即実害なし）。根治は `*store.Store` / `*fakeStore` / `storyStore` に同一振る舞いスイート（bump 規則・エラー種別・フィルタ意味論）を流す共有 contract test。
