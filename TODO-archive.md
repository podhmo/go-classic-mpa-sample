# TODO archive

> **What this file is:** append-only history of completed work moved out of
> `TODO.md` by a periodic GC sweep (see `.agents/skills/todo-md/SKILL.md` —
> feature PRs never write here, so this file never conflicts). Each item
> lands at the end of the file as one line, prefixed with its ancestor
> chain (`- [x] A > B > **C**: ...`), so a grep hit names the whole path.
> Wording and links are kept verbatim; only the layout is normalized.
>
> **Reading this file:** do not read it whole — `grep` for a symbol or
> feature name; the matching line is the whole entry.

- [x] A1. Implement `Open`/`Close`: modernc.org/sqlite driver ("sqlite"), schema init, `PRAGMA foreign_keys=ON`, busy_timeout; support `:memory:` safely (shared-cache DSN or MaxOpenConns(1))
- [x] A2. Schema: `issues(id AUTOINCREMENT, title NOT NULL, body, status CHECK open/closed, created_at, updated_at)`, `comments(id, issue_id FK ON DELETE CASCADE, body NOT NULL, created_at)`, `labels(id, name UNIQUE, color)`, `issue_labels(issue_id, label_id, PK pair, FKs cascade)`, indexes for list filtering (status, created_at)
- [x] A3. All methods above with parameterized queries only; `Scan` into types; label population via single grouped query (avoid N+1 if simple)
- [x] A4. `SeedDemoData`: labels (Bug/Feature/Documentation/Enhancement + colors), ~6+ issues with variety: open/closed, multiple labels, comments, one long body, staggered created_at; only when issues table empty
- [x] A5. `internal/store/store_test.go`: `:memory:` DB per test — CRUD, status toggle, comments, list filters (status/query/label/sort/pagination+total), ErrNotFound, seed idempotency
- [x] A6. `go test ./internal/store/` passes; `go vet` clean. Do NOT touch files outside `internal/store/`, do NOT modify `go.mod`/`go.sum`. Commit to branch `impl/store` in the worktree.
- [x] B1. `internal/web`: define narrow `Store` interface (consumer-side) listing exactly the contract methods; `func New(st Store) http.Handler` building `http.ServeMux` with method+path patterns
- [x] B2. Routes: `GET /`→303 `/issues`; `GET /issues` (list: status toggle, `q` keyword, `label` filter, `sort` newest/oldest, `page` pagination, query params preserved in links, empty-state message); `GET /issues/new`; `POST /issues`; `GET /issues/{id}` (detail+comments+comment form); `GET /issues/{id}/edit`; `POST /issues/{id}/edit`; `POST /issues/{id}/status` (toggle); `GET /issues/{id}/delete` (confirm page); `POST /issues/{id}/delete`; `POST /issues/{id}/comments`; `GET /static/app.css`
- [x] B3. PRG: all successful POSTs → `303 SeeOther`. 404 for missing issue (`errors.Is(err, store.ErrNotFound)`), 405 handled by mux patterns
- [x] B4. Validation server-side: title required (trim, ≤200), body optional (reasonable cap), comment body required; on error re-render form preserving input + `aria-describedby` error messages
- [x] B5. CSRF: wrap mux with `http.NewCrossOriginProtection()` (Go 1.25+ stdlib); also `http.MaxBytesReader` (~1MB) on POST bodies
- [x] B6. Templates in `internal/web/templates/` via `embed.FS`: shared `layout.html` (header/nav/footer), `issues.html`, `issue_detail.html`, `issue_form.html` (new+edit), `issue_delete.html`, `error.html`; `html/template` auto-escaping; template funcs for date formatting + query building; plaintext bodies with `white-space: pre-wrap`
- [x] B7. `internal/web/static/app.css` via `embed.FS`: light theme, neutral palette, consistent buttons/forms/badges/links, label colors, open/closed visual status (not color-only), responsive (desktop+mobile), visible `:focus` styles, semantic HTML, `label`↔input association, no JS/emoji icons
- [x] B8. `main.go`: flags `-addr` (default `127.0.0.1:8080`), `-db` (default `issues.db`, also allow env override); `store.Open` + `SeedDemoData`; `http.Server` with sensible timeouts (ReadHeaderTimeout etc.); graceful shutdown on SIGINT
- [x] B9. `internal/web/web_test.go`: `httptest` + in-memory fake Store — list renders, 404 on missing issue, validation error re-renders form, POST→303 redirect location, HTML escaping of `<script>`, comment post, status toggle, delete flow. (Real-SQLite integration is verified post-merge, so tests MUST use the fake.)
- [x] B10. `README.md` rewrite: overview, tech stack, `go run .` startup, `go test ./...`, flags/env vars, routes, DB location, architecture, intentionally-not-implemented list (auth, markdown, JS)
- [x] B11. `go build ./...`, `go test ./internal/web/`, `go vet` clean. Do NOT touch `internal/store/**`, `go.mod`, `go.sum`. Commit to branch `impl/web` in the worktree.
- [x] I1. Merge `impl/store` + `impl/web` into `main`; `go mod tidy`; fix conflicts/compile errors
- [x] I2. `go build ./...`, `go vet ./...`, `go test ./...` (34 tests pass, -race clean)
- [x] I3. Smoke test: `go run .` → curl `/issues`, create/comment/toggle/delete flows, confirm SQLite file persists
- [x] I4. Dedicated QA pass (fresh subagent) → findings recorded in `docs/ja/bugs.md`
- [x] F1. Unknown/duplicate `label_id` → 500: `setIssueLabels` → `INSERT OR IGNORE ... SELECT id FROM labels` (silently skip, consistent with web dropping non-numeric ids)
- [x] F2. `SetIssueStatus` bumps `updated_at` only when status actually changes (CASE WHEN)
- [x] F3. `CreateComment` bumps parent issue `updated_at` in same tx
- [x] F4. Tests: unknown+dup label ids on create/update, no-op status keeps updated_at, comment bumps updated_at
- [x] F5. Out-of-range `page`: parse bound (≤1e6, kill int64 overflow) + clamp to `TotalPages` & re-query so out-of-range shows last page with pagination
- [x] F6. Catch-all `"/"` → styled 404; explicit `GET /static/app.css` (kills `/static/` dir listing)
- [x] F7. `updateIssue`: existence check before validation re-render (missing issue → 404 not 422)
- [x] F8. Detail-page label links → `?status=all&label=N`
- [x] F9. `issuesURL`: omit `status` when open (default)
- [x] F10. main.go: reject `-db ""`
- [x] F11. Security headers middleware: nosniff + frame DENY + referrer-policy
- [x] F12. Regression tests per bug (see docs/ja/bugs.md)
- [x] P1. Merge `fix/store` + `fix/web`; `go mod tidy`; build+vet+test+race
- [x] P2. Re-run smoke test incl. the fixed paths (bogus label_id, page=99999, /nonexistent styled 404)
- [x] P3. Update bugs.md statuses → fixed; update decisions/pitfalls; final checklist §11; cleanup worktrees; final report
- [x] R1. Introduce `Renderer` struct owning the parsed page templates; typed methods `renderIssueList` / `renderIssueDetail` / `renderIssueForm` / `renderIssueDelete` / `renderError` (+ `notFound` convenience) taking concrete page-data types instead of `data any`
- [x] R2. `handler` uses the Renderer for all render call sites; `New(st Store) http.Handler` signature unchanged; keep buffer-then-write behavior and error-page fallback
- [x] R3. Existing tests still pass untouched; `go vet`/`gofmt` clean
- [x] T1. Issue lifecycle: POST create → appears in `GET /issues` → `GET /issues/{id}` shows title/body/labels → POST edit changes title → POST status closed → GET shows Closed + absent from default list → POST delete → 303 → GET 404
- [x] T2. Comments & validation: POST comment → renders on detail with timestamp; empty/whitespace comment → 422 page containing error text + preserved draft; comment on missing issue → 404
- [x] T3. List interactions on seeded data: `?status=closed` shows only closed; `?q=` matches title AND body; `?label=` filters; `?sort=oldest` reverses order; pagination `?page=2` + `?page=999` clamps
- [x] T4. CSRF through the real middleware: cross-site `Sec-Fetch-Site` POST → 403 with no mutation; same-origin POST → 303
- [x] T5. Seed sanity: `SeedDemoData` → list renders seeded issues with label badges; closed seed issues appear only under `?status=closed|all`
- [x] M1. `github.com/yuin/goldmark/v2` v2.1.15 dependency (GFM extension, `html.WithHardWraps()`); raw HTML omitted, dangerous URL schemes neutralized by default renderer. Started on v1.8.7, then migrated to v2 for the official alert extension
- [x] M2. `internal/web/markdown.go`: `renderMarkdown` → `template.HTML` via `parser.New` + `html.New`; template func `markdown` in funcMap; detail template renders issue body + comments via it
- [x] M3. CSS: `.markdown-body` content styles (pre/code/blockquote/headings/lists/tables/hr/img); drop `white-space: pre-wrap` from `.issue-body`/`.comment-body`
- [x] M4. Tests: `markdown_test.go` unit tests + handler-level markdown test; fix E2E comment-body assertion for `<p>` wrapping
- [x] M5. Docs: decisions.md (new section), README (notes + tech stack), pitfalls.md; verified live via curl + playwright screenshot
- [x] M6. GitHub alerts `> [!NOTE|TIP|IMPORTANT|WARNING|CAUTION]` via `github.com/yuin/goldmark-alert` (official, v2-only); `.markdown-alert-*` CSS with per-kind colors + currentColor icons; unit test added
- [x] O1. `init` subcommand / `-init` flag: `go run . init` (or `go run . -init`) creates the database file and lays down the complete schema — all tables WITH their indexes (`issues`, `comments`, `labels`, `issue_labels`, `issues(status, created_at)`-style indexes, any extra indexes deemed useful for list/search/label queries). Decided shape (user): explicit init step rather than a loose SQL script. Design questions to resolve at implementation time:
- O1. `init` subcommand / `-init` flag > Does normal startup (`go run .`) still auto-init an empty DB (keep zero-config) or require `init` first (explicit)? Prefer keeping auto-init for `go run .` simplicity — `init` then acts as an explicit idempotent "create/verify schema" step usable for ops/`ANALYZE`.
- O1. `init` subcommand / `-init` flag > Factor schema DDL so `init` and `store.Open` share one source of truth (e.g. exported `store.EnsureSchema`/`store.Init` or a `cmd` path that opens + migrates + exits).
- O1. `init` subcommand / `-init` flag > `init` should print what it did (created file, applied N tables/indexes) and exit 0; safe to run twice.
- O1. `init` subcommand / `-init` flag > Consider `ANALYZE` after init/seed for the sqlite query planner.

- [x] **todo-gc のバグ修正** ([PR #4 Devin Review](https://github.com/podhmo/go-classic-mpa-sample/pull/4)): レビューで見つかった `tools/todo-gc` の3件。(a) archive 書き込み失敗時に TODO.md が先に書き換わり完了項目を喪失する → 書き込み順序の見直し。(b) 空行を挟むとネスト項目が root 扱いになり、[x] 親が open な子を残したままアーカイブされる・子の祖先チェーンが失われる → `parse` の修正。(c) note（`- 完了の条件:` 等）配下の完了項目が sweep されず TODO に残り続ける → `sweepNode` の修正。回帰テストを追加すること。同じコードが html-sandbox の `tools/todo-gc` にもあるので、直したら upstream へのフィードバックも検討する。

- [x] **minigo storybook preview**: minigo（third_party clone + cmd/storybook ネスト module）で sqlite 非実行の storybook 的プレビュー環境を構築。`make setup-minigo` + `make storybook` で起動。設計と発見は `docs/sketch/ja/storybook-with-minigo.md`、minigo への制限フィードバックは issue 化して minigo 側へ送る。
- [x] **Dark mode support** (guides: `dark-mode`, `contrast-color`): `:root` に `color-scheme: light dark` と `<meta name="color-scheme">` を追加し、固定ライトパレットを `light-dark()` トークン化 + `@media (prefers-color-scheme: dark)` フォールバック。ラベルバッジ（DB 由来の動的色）の文字色コントラストと favicon のダーク時視認性も確認する。
- [x] **Cross-document view transitions** (guide: `cross-document-transitions`): `@view-transition { navigation: auto }` で MPA 遷移に控えめなクロスフェードを入れる。純粋な CSS のプログレッシブエンハンスメント。ページネーション等は transition `types` で方向アニメーションも検討。
- [x] **Declarative delete confirmation** (guides: `declarative-dialog-popover-control`, `dialog`, `light-dismiss-a-dialog`): 詳細ページの削除確認を `popovertarget`/Invoker Commands で動く `[popover]`/`<dialog>` にし、JS なしで開閉できるようにする。既存の GET 確認ページは非対応ブラウザ向けフォールバックとして残す。Baseline サポートと dismiss 時のフォーカス復帰を確認。
- [x] **Form feedback polish** (guides: `required-field-feedback`, `accessible-error-announcement`, `style-parent-with-has`, `forms`): 視覚的なエラー表示を `aria-invalid` と `:user-invalid`/`:has()` で同期させ、インタラクション後にのみエラーが出るよう統一。ラベルの checkbox 群・select にも同じパターンを適用できるか確認。
- [x] **Responsive & typography pass** (guides: `responsive-design`, `respect-os-text-scale`, `fluid-scaling`, `single-line-overflow`, `typography`): `body { font-size: 15px }` が px 指定なので rem 化して OS テキストスケールに追従させる。640px 単一ブレークポイントの現状を監査し、filter-bar / issue-row の狭幅表示と長いタイトルの clamp/ellipsis を整える。
- [x] **Rendering performance for long lists** (guides: `defer-rendering-heavy-content`, `performance`): `.issue-row` / コメントに `content-visibility: auto` + `contain-intrinsic-size` を検討し、画面外レンダリングを省く。CSS のみ。
- [x] **CSP & header hardening** (guides: `security`, `privacy`): JS ゼロなので `script-src 'none'` を含む Content-Security-Policy を付けられる（`object-src 'none'`, `base-uri 'none'`, `frame-ancestors 'deny'` 等）。ラベル色の inline `style=` と `style-src` の両立方法を決める（bugs.md #13' で CSP はこの理由で見送り済み）。
- [x] **Skip link & a11y sweep** (guide: `accessibility`): "Skip to content" リンク追加、`:focus-visible`、ランドマーク・見出し階層の監査を全テンプレートで実施。
- [x] **Speculation rules prefetching** (guide: `improve-next-page-load-performance`): `<script type="speculationrules">` で Next ページや issue リンクを prefetch/prerender。実行 JS ではなく宣言的 JSON だが、no-JavaScript ポリシーに合うか判断してから実装する（合わないと判断したら wontfix で記録）。
- Speculation rules prefetching > 実装: inline `<script>` は CSP `script-src 'none'` にブロックされるため、`Speculation-Rules` レスポンスヘッダー + `/static/speculation-rules.json` で配信（decisions.md 参照）。
- [x] **bugs.md #23 mitigation**: 50件以上のスレッドでスキップ中コメントが AX ツリーから落ちる問題を、`content-visibility` の対象を `<li>` から `.comment-body` に移して部分緩和（listitem+パーマリンクは残り欠落は本文のみ）。bugs.md #23 のステータスを部分緩和に更新（decisions.md「長いリストのレンダリング」参照）。

- [x] **Textarea auto-fit** (guide: `form-fields-automatically-fit-contents`): `field-sizing: content`（min/max の上下限つき）で issue body・comment の textarea を入力量に追従させる。宣言的 CSS のみ。
- [x] **Sticky site header with stuck state** (guide: `state-aware-sticky-headers`): サイトヘッダーを `position: sticky` 化し、stuck 時に影や縮小などの視覚変化を付ける。scroll-state クエリ等の宣言的手段を優先。
- [x] **Back-to-top on long threads** (guide: `scroll-position-aware-elements`): 長いコメントスレッドでスクロール位置連動の back-to-top ボタンを出す。CSS scroll-driven animation を優先し、非対応ブラウザは常時表示 or 非表示で graceful に。長いスレッドの story を `story_pages.go` に追加して確認する。
- [x] **Search-term highlighting** (guide: `highlight-text-ranges`): `?q=` の語を一覧の issue タイトル上で CSS Custom Highlight API によりハイライトし、どこがヒットしたか視認できるようにする。**JS**。
- [x] **Copy comment permalink** (guide: `copy-button`): 各コメントのパーマリンク横にコピーボタンを置き、`#comment-N` の URL を `navigator.clipboard` でコピー。非対応・権限なし時のフォールバックも用意。**JS**。
- [x] **IME-safe Cmd/Ctrl+Enter submit** (guide: `ime-safe-enter-submit`): コメント・issue フォームを Cmd/Ctrl+Enter で送信できるようにし、IME 変換中の Enter で誤送信しない。**JS**。
- [x] **Directional pagination transitions** (guide: `directional-navigation-transitions`): `pagereveal` イベント + view-transition types で Prev/Next の方向アニメーション。no-JS 方針時代に見送った項目の解禁（decisions.md「ページ遷移アニメーション」参照）。**JS**。storybook で片方向の再現が難しければ verify-ui（実サーバー + playwright）で検証する。
- [x] **aria-invalid ↔ :user-invalid sync** (guide: `accessible-error-announcement`): 以前 no-JS で見送った「`aria-invalid` を `:user-invalid` に同期させる JS」を追加し、送信前のクライアント側エラーも支援技術に伝える（decisions.md「フォームのエラー表示」の非対称を解消）。**JS**。
- [x] **Accounts Phase 1: ログイン + 投稿者識別** (design: `docs/sketch/ja/accounts.md`): authentication（誰であるか）のみで authorization は入れない。未ログインでも全機能は使え、投稿に `author_id` を記録して投稿者名を表示する。anonymous は users 行を持たず `author_id IS NULL` で表現。
- [x] Accounts Phase 1: ログイン + 投稿者識別 > **`user_version` マイグレーション機構**: `schemaDDL` 一本立ちの `Open` を `PRAGMA user_version` 駆動の移行関数列に置き換える。v1=現行スキーマ、v2=users/sessions + `issues.author_id` + `comments.author_id`（ALTER）。新規 DB も同じ経路を辿るので `init` の冪等検証としての意味は維持。
- [x] Accounts Phase 1: ログイン + 投稿者識別 > **store: users/sessions 層 + argon2id**: `golang.org/x/crypto/argon2` で PHC 形式のハッシュ（`internal/auth`）。`CreateUser`（name は `UNIQUE COLLATE NOCASE`）、`AuthenticateUser`、`CreateSession`（32B 乱数 token、DB は SHA-256 のみ）、`SessionUser`（期限切れ除外 + sliding renewal）、`DeleteSession`、起動時の expired sweep。
- [x] Accounts Phase 1: ログイン + 投稿者識別 > **web: 認証 middleware + context**: session cookie → `SessionUser` → `context.Context` に `*store.User`（nil が anonymous）。`web.Store` インターフェース拡張 + fakeStore 対応。cookie は `HttpOnly; SameSite=Lax; Path=/`（平 HTTP localhost 前提で `Secure` は付けない、session cookie として Max-Age なし）。
- [x] Accounts Phase 1: ログイン + 投稿者識別 > **register/login/logout 画面・ハンドラ**: `GET/POST /register`、`GET/POST /login`、`POST /logout`（GET logout 不可）。認証失敗は汎用メッセージで 422 再表示 + 固定遅延。登録後は自動ログイン。`?next=` で元ページへ戻る。`autocomplete="current-password"/"new-password"`。
- [x] Accounts Phase 1: ログイン + 投稿者識別 > **ヘッダーのログイン状態表示**: 未ログインは `Log in`/`Register` リンク、ログイン中は `@name` + POST の Logout ボタン。全ページモデルに `User` を通す（error ページ含む）。
- [x] Accounts Phase 1: ログイン + 投稿者識別 > **投稿への author_id 記録 + 表示**: `CreateIssue`/`CreateComment` に author を渡し、詳細ページに `Opened by <name>` / `<name> commented` を表示。NULL は `anonymous`。
- [x] Accounts Phase 1: ログイン + 投稿者識別 > **seed デモユーザー**: `demo`/`demo` のユーザーを seed（ハッシュは実行時生成）して初回起動からログインできる形に。
- [x] Accounts Phase 1: ログイン + 投稿者識別 > **テスト + 実機検証 + docs**: store/web テスト追加、`go test ./... && go vet ./... && gofmt -l .`、実サーバで register→login→投稿→logout を確認。`docs/ja/decisions.md`・`docs/ja/roadmap.md` を更新。

- [x] **Discussions タブ（GitHub Discussions 風）**: `/discussions` 系の追加。カテゴリ（単一選択・シード定義、format: open/qa）・1段スレッド返信・qa の answer マークを採用。reactions・poll・issue→discussion 変換・pin・カテゴリ管理 UI は見送り（設計メモ「見送り」節）。設計: `docs/sketch/ja/discussions.md`（案B: 並列テーブル）
- [x] Discussions タブ（GitHub Discussions 風） > **Phase 1: 骨格** — migration V3（`discussion_categories`/`discussions`/`discussion_comments` + 部分一意 index）・store メソッド・一覧/詳細/作成/編集/削除/コメント投稿のルート・ナビの Issues/Discussions タブ化（view model に `Section` を追加し New ボタンを文脈分岐）・カテゴリ＋デモ discussion のシード・store/web テスト。コメント表示は時系列フラット（スレッド化は Phase 2）。
- [x] Discussions タブ（GitHub Discussions 風） > **Phase 2: 対話構造** — 1段ネスト表示と `<details>` 内返信フォーム（hidden `parent_id`、store 層で「同一 discussion の top-level」検証）、qa のみ answer マーク/解除（`POST .../comments/{cid}/answer`、2行更新を1トランザクション＋部分一意 index）、詳細・一覧の Answered バッジと `?answered=yes|no` フィルタ、answer/返信で `updated_at` bump。
- [x] Discussions タブ（GitHub Discussions 風） > **Phase 3: 仕上げ** — speculation-rules の discussions 対応、search-highlight.js の selector 一般化（**JS**）、storybook story 充実、`docs/ja/roadmap.md` の v2 欄更新。

- [x] **Breadcrumb の構造化（issue・discussion 詳細共通）**: `breadcrumbs` ガイド準拠にする。現行はフラット `<nav>` + `aria-hidden` な `/` テキスト + `<span>` の現在地。`<ol><li><a>` の構造に変え、現在地は `aria-current="page"` のリンク（`#` 自己参照）にし、区切りは CSS の `li + li::before { content: "/" / ""; }` で生成してマークアップから取り除く。対象: `issue_detail.html` / `discussion_detail.html` の `.breadcrumb`。
- [x] **コメント行ごとの同名コントロールを一意化（discussion 詳細、issue 側も再点検）**: `accessibility` ガイド「見た目が同じで動作が違うコントロールは accessible name で区別」。discussion 詳細の各コメントにある "Reply" `<summary>` と "Mark/Unmark answer" ボタンは全て同名 — copy link ボタンの `aria-label="Copy link to comment {id}"` と同じパターンで `Reply to comment {id}` / `Mark comment {id} as answer` などにする。issue 側は copy link 以外に行ごとのコントロールが無いか確認（あれば同様に）。
- [x] **コメント/返信の部分更新（手書き fetch + HTML fragment）**（**JS**）: `docs/sketch/ja/partial-updates.md` の 2026-11-15 結論（案B）に基づく。関連ガイド: `sanitize-untrusted-html`, `move-dom-element-without-losing-state`, `persistent-toast-notifications`, `spinner`。
- [x] コメント/返信の部分更新（手書き fetch + HTML fragment） > **サーバ側**: CSP に `connect-src 'self'` 追加。`templates/fragments.html` に `{{define "issue_comment"/"discussion_comment"/"discussion_reply"}}` + `{{define "issue_comment_form"/"discussion_comment_form"/"discussion_reply_form"}}` を切り出し、renderer が block 単位で描画。`createComment`・`createDiscussionComment` は `X-Partial: 1` なら新コメントの `<li>` fragment（`text/html`）、バリデーション失敗なら 422 + エラーフォーム fragment を返す。通常 submit は従来どおり 303 PRG（progressive enhancement）。
- [x] コメント/返信の部分更新（手書き fetch + HTML fragment） > **クライアント側**: `/static/partial-submit.js`（最初の ES module、`docs/sketch/ja/js-modules.md`）で `form[data-partial]` の submit を奪取→`fetch`→`<template>` パース（`Document.parseHTML` は Sanitizer が class/id を削るため不採用、pitfalls.md）→`data-partial-target` へ append。送信中は submit `disabled`+`aria-busy`+`<progress>`（indeterminate）、二重送信は in-flight フラグ。完了でフォームリセット・フォーカスは textarea に残す・共有 announcer（`/static/announcer.js`、form-errors.js も import に移行し `type="module"` 化）で polite 通知。422 は form fragment を差し替え。`enter-submit.js` は document delegation 化。テンプレート側は `data-partial`/`data-partial-target` 属性、空でも `<ol>` を `hidden` で常時描画、`data-empty-note`・`data-comment-count` を追加。新 `<li>` は一時 class でハイライト（`:target` は部分挿入では効かない）。
- [x] コメント/返信の部分更新（手書き fetch + HTML fragment） > **storybook 検証**: fragment 見た目の print story + `StoryHttp` story（X-Partial POST→fragment、通常 POST→303、X-Partial 空 POST→422）を `story_pages.go` に追加。`cmd/storybook` に catch-all mount（実リクエストを `StoryHttpApp` story に渡し `New(storyStore)` を実パスで動かす）を実装し、`fetch`→fragment→DOM swap まで実ブラウザで確認可能にする（storyStore は stateless fake — POST→GET の持続は実アプリ側 verify-ui で確認）。
- [x] コメント/返信の部分更新（手書き fetch + HTML fragment） > **Phase 2（別タスク）**: answer マーク・status toggle を同機構に乗せる（`<li>`/ピル差し替え、`moveBefore` 活用・Safari は `insertBefore` fallback）。一覧フィルタ・削除・編集・認証フォームは navigation のまま。実装メモ: `moveBefore` は接続済みノード専用の state-preserving move で detach 済み fragment を受け付けない（HierarchyRequestError、実機検証済み）ため、差し替えは `replaceWith` で実装。
- [x] **部分更新 append パスのメタ行 stale**: コメント/返信の部分投稿で store は `updated_at` を進めるが、fragment は新 `<li>` だけを返すためヘッダの "Last updated" がリロードまで古いまま（Phase 2 の answer swap は `discussion_meta` ノード同梱で解決済み、PR #49 の review finding 由来）。append モードは全ノードを target へ挿入するので、自己ターゲットする swap ノードを混ぜるにはプロトコル拡張が要る（例: fragment 内で `data-swap` 印のノードは `replaceWith`、残りを append）。
- [x] **`<details>` の開閉アニメーション**: 返信フォーム等の details 展開を `interpolate-size: allow-keywords` + `::details-content` で高さアニメーションに（modern-web-guidance `animate-to-intrinsic-sizes`）。非対応ブラウザは従来どおり瞬時表示で graceful。
- [x] **部分更新の swap を scoped view transition で滑らかに**: `partial-submit.js` の DOM 差し替え（コメント挿入・422 フォーム差し替え・answer/status pill）を `document.startViewTransition` で囲み視覚遷移に（modern-web-guidance `scoped-component-transitions`）。`prefers-reduced-motion` と非対応ブラウザは素の swap にフォールバック。
- [x] **投稿成功の視覚トースト**: 部分更新の成功フィードバックは現在 announcer（SR 専用）のみ。`aria-live` toast で「コメントを投稿しました」等を視覚的にも出す（modern-web-guidance `persistent-toast-notifications`）。
- [x] **タブの移動アンダーライン**: issues 一覧の open/closed/all タブで選択中インジケータを anchor positioning で遷移させる（modern-web-guidance `anchor-positioning-tab-underline`）。view-transition 中の要素追従に注意。
- [x] **ナビゲーション方向に応じた遷移**: cross-document view transition を前進/後退で方向づけ（一覧→詳細はスライドイン、戻るは逆方向）に（modern-web-guidance `directional-navigation-transitions`）。現行の一律 cross-fade を拡張。
- [x] **長ページ用 back-to-top**: issue/discussion 詳細で下部までスクロールしたとき浮遊ボタンを scroll-driven animation で出現させる（modern-web-guidance `scroll-position-aware-elements`）。非対応ブラウザは常時表示または非表示で graceful。
- [x] **gh ライクな CLI（`issues` サブコマンド群）** — `docs/sketch/ja/cli-and-web-api.md` の Phase 1/3/4。`internal/cli`（コマンド文法 + consumer 側 `Backend` interface）+ `internal/client`（`/api/v1` HTTP クライアント、api DTO ↔ store 型の往復）。`--server`/`ISSUES_SERVER` + `--token`/`ISSUES_TOKEN` 指定でリモートモード、無指定なら `-db` 直結のローカルモード。バイナリは `cmd/issues` 別コマンド（storybook 式・案2。単一バイナリ案1で一度実装したが「API を叩く CLI にサーバを同梱すべきでない」との指摘で分離）。
- [x] gh ライクな CLI（`issues` サブコマンド群） > **Phase 1: ローカル CLI**: `issue list|view|create|edit|close|reopen|delete`、`comment create`、`label list`。`--json FIELD,...`（フィールド選択、API の DTO キー名に揃える）。exit code 0 成功 / 1 実行エラー / 2 使い方エラー。
- [x] gh ライクな CLI（`issues` サブコマンド群） > **Phase 3: リモートモード**: `internal/client` で `Backend` の HTTP 実装。`-w/--web`（サーバ URL + ページパスをブラウザで開く、要 --server、`issue view|create`）、`issues api <path>`（生 API 抜け道・リモート専用、非 2xx は stderr + exit 1）。
- [x] gh ライクな CLI（`issues` サブコマンド群） > **`auth token`**: `issues auth token --user NAME` で api_tokens を発行（ローカルモード限定: `UserByName` + `CreateAPIToken`、平文は一度だけ表示）。
- [x] gh ライクな CLI（`issues` サブコマンド群） > **discussion サブコマンド群**: `discussion list|view|create|edit|delete`、`discussion comment create [--parent]`、`discussion answer`（メモの未決項「まず issues のみに絞るか」は絞る方で実装。`/api/v1/discussions*` は済みなので追加は機械的）。
- [x] gh ライクな CLI（`issues` サブコマンド群） > **Phase 4 仕上げ**: `api` の `--method/--field`、shell completion、`--template` 出力、NO_COLOR/TTY 判定、api_tokens の revoke/一覧導線（`auth token --revoke` / `auth token list` 等、期限なし方針のままなら管理面が要る）。
- [x] **JSON Web API (`/api/v1`)** — `docs/sketch/ja/cli-and-web-api.md` の Phase 2。`internal/api` に consumer 側 Store interface + DTO + ハンドラ。Bearer は単一 env トークン（`ISSUES_API_TOKEN`）またはユーザー別 `api_tokens`（行があれば actor が立つ）。認証情報が 1 つも無ければ mount しない fail-closed。エラー形式 `{"error":{"message","fields"}}`、ステータスは HTML 側と同じ意味論。
- [x] JSON Web API (`/api/v1`) > **骨格 + Bearer fail-closed**: `internal/api` 新設、mux に `/api/v1` サブツリー mount（token 未設定時は mount 自体しない）。`Authorization: Bearer` を `crypto/subtle` 定数時間比較で検証、不一致・欠落は 401 JSON。
- [x] JSON Web API (`/api/v1`) > **issues/comments/labels endpoints**: `GET /api/v1/issues`（status/q/label/sort/page/per_page）、`GET /api/v1/issues/{id}`（labels+comments 同梱）、`POST /api/v1/issues`、`PATCH /api/v1/issues/{id}`、`POST /api/v1/issues/{id}/status`、`DELETE /api/v1/issues/{id}`、`POST /api/v1/issues/{id}/comments`、`GET /api/v1/labels`。
- [x] JSON Web API (`/api/v1`) > **バリデーションの共有**: `validateIssueInput`/`validateDiscussionInput` を「`*Input` 値を検証する関数」に分解して web/api 両方から使う（`internal/store` の `ValidateIssueInput`/`ValidateDiscussionInput`/`ValidateCommentBody` + `MaxTitleRunes`/`MaxBodyRunes` に集約）。
- [x] JSON Web API (`/api/v1`) > **discussions endpoints**: `GET|POST /api/v1/discussions`、`GET|PATCH|DELETE /api/v1/discussions/{id}`、`POST /api/v1/discussions/{id}/comments`（`parent_id` で返信）、`POST /api/v1/discussions/{id}/comments/{cid}/answer`、`GET /api/v1/discussion-categories`。
- [x] JSON Web API (`/api/v1`) > **api_tokens（ユーザー別トークン）**: token→user_id の `api_tokens` テーブル（migrateV4、平文は保存せず SHA-256 のみ、revoke は行削除、期限なし）。Bearer は env token → anonymous / api_tokens 行 → その user_id が actor（API 書き込みに author_id が立つ）。mount 判定は起動時: env token 非空 OR `api_tokens` 非空で `/api/v1` を mount（`web.WithAPIEnabled`）。発行導線 `issues auth token` は「gh ライクな CLI」タスク側。

- [x] **fake/stub の契約乖離を全コードから探索し整理する**: 全 5 interface × 全実装の意味論突き合わせを実施（記録は PR #71）。実バグ寄りは全て修正、意図的スタブは doc コメントで宣言済み
- [x] **UI ルール監査で検出した不統一の修正**: `docs/ja/ui-rules.md` の「変更予定」項目（外部デザインシステムチェックリスト監査 2026-11-16 の結果）
- [x] UI ルール監査で検出した不統一の修正 > **ボタン配置の統一**: 作成/編集フォーム（issue_form/discussion_form）と GET 削除確認ページ（issue_delete/discussion_delete）を「左:キャンセル / 右:確定・右寄せ」に揃える。dialog・返信フォームは準拠済み
- [x] UI ルール監査で検出した不統一の修正 > **ヘッダ New ボタンの枠線化**: layout.html の "New issue"/"New discussion" から `btn-primary` を外しページ内 Primary を1つにする
- [x] UI ルール監査で検出した不統一の修正 > **コメント欄が空のとき送信ボタンを Disabled に**: **JS**（textarea の input 監視。トップレベル・返信両方）
- [x] UI ルール監査で検出した不統一の修正 > **返信の部分投稿後に `<details>` を畳む**: **JS**（partial-submit.js append パスで `.reply-form` 成功時に `details.open=false`）
- [x] UI ルール監査で検出した不統一の修正 > **入力中の離脱ガード**: **JS**（dirty 時に beforeunload 確認を出す static/ スクリプト。送信成功で解除）
- [x] UI ルール監査で検出した不統一の修正 > **モバイル対応の残件**: 固定要素（toast/back-to-top）に `env(safe-area-inset-*)` を加味、装飾的 `:hover` を `@media (hover:hover)` でガード統一、狭画面で固定要素が送信ボタン等を覆わない配置の見直し
- [x] UI ルール監査で検出した不統一の修正 > **storybook ストーリーの拡充**: 変更予定項目（ボタン配置・狭画面レイアウト等）を目視確認できるよう、不足している状態ストーリーを `internal/web/story_pages.go` に追加する。JS 挙動は verify-ui 側で確認
- [x] UI ルール監査で検出した不統一の修正 > **コメント permalink を判別可能にする**: `.comment-permalink` が `color: inherit; text-decoration: none` で本文と同色無下線（hover でしか判別できない）→ リンクと分かる表現に直す（ui-rules.md §9、2026-11-16 決定）
- [x] **クライアント JS のライフサイクル統合**: 共有 registry `dom-hooks.js`（1本の MutationObserver + `bind(selector, {init, teardown})`）に attach/detach 追従を集約し、detail/form ページの script タグをページ種別エントリ1本に畳む（decisions.md 2026-11-16 参照）
- [x] **他者コメントのリアルタイム反映（SSE push）**: `GET /{issues,discussions}/{id}/events` + store 層 emitter + `live-updates.js` + fragment GET エンドポイント。設計は `docs/sketch/ja/live-updates.md`。comment/thread/reply の live append に加え status/answer/edit/delete/resync も対応。一覧ページ側の未読表示・presence は未実装
- [x] **storybook の UI 改善**: index に検索フィルタ（`?q=`、name/doc/group、ライブ絞り込み+GET フォールバック）とプレビューツールバー（story 名・group・ソース位置・raw/grid・幅切替）を追加。grid への導線は story リンクと混同しない `⊞ grid` ピルに分離し、grid を index 内プレビュー（`?g=`）で開けるようにした。story 選択は iframe 差替えのみでページリロードせず、export した index.html でも JS が `?s/?g/?w/?q` を読んで動く
- [x] **workspace（GitHub リポジトリ相当のコンテナ）の導入**: issues/discussions を workspace 所属にする。設計は `docs/sketch/ja/workspaces.md`（採番はグローバル維持、正準 URL `/w/{slug}/...`、migration v6 で default 作成、最後の1件は削除不可）。workspace 間移動・per-workspace 採番・ws 別 label/category は future work

- [x] **ラベルを DB 管理化（定数の廃止）**: `labels` に `workspace_id`（NULL=マスター）・`kind`（issue/discussion）・`archived` を追加し、既定ラベルは `defaultLabelsDDL` のデータマイグレーションで投入。管理画面 `/w/{ws}/labels`（追加/編集/削除=アーカイブ/復帰）と discussion のラベル選択 UI、API `/api/v1/w/{ws}/labels`、CLI `zatu label create|delete` を実装（PR 参照）
- [x] **issue/discussion の Markdown エクスポート**: `internal/export` の共有 serializer を `GET /w/{ws}/issues|discussions/{id}/markdown`・`GET /api/v1/w/{ws}/.../{id}/markdown`（`text/markdown`）・詳細ページの "Copy as markdown" ボタン（`copy-markdown.js` + 共有 `clipboard.js`）・CLI `zatu issue|discussion view --markdown` の 4 surface に接線。設計は `docs/ja/decisions.md`「Markdown エクスポート」

- [x] **zatu の --help 整備と agent 向け zatu-cli skill**: 全階層（top/group/leaf）で `-h/--help` を stdout・exit 0 にし、leaf の flag 一覧は `flag.PrintDefaults` から生成して正を flag 定義に一元化。skill `.agents/skills/zatu-cli/` は「--help を引け」に絞り、リプレイ例は `tools/demo-history.sh` が担う。
- [x] **`zatu` に discussion categories の一覧コマンドがない**: `discussion create --category SLUG` が必須なのに slug を列挙する術がない。`discussion list` の2列目か、未知 slug を渡したときのエラー "valid: ..." からしか発見できない（`zatu discussion categories` のような listing verb を検討。2エージェントによる実使用テストで発覚）。→ `zatu discussion categories` を実装（slug/name/format/description のタブ区切り、`--json`/`--template` 対応）。
- [x] **コメントの編集・削除・hide（理由付き最小化）**: issue コメント・discussion スレッド・返信の3種すべてに edit（専用 /edit ページ）/delete（確認ページ）/hide・unhide（spam/abuse/off-topic/outdated/duplicate/resolved/low-quality の理由選択、`<details>` 折りたたみ表示）を web/api/cli で実装。schema v4 で `edited_at`/`hidden_at`/`hidden_reason` 追加、編集済みは "edited" マーカー表示

- [x] Pull requests を git コマンドで再現する > **internal/gitrepo**: repo 解決（auto-detect + `-repo` + `git --version` プローブ）＋ git 実行ラッパ（log/diff/name-status/merge-base/merge-tree/commit-tree/update-ref/worktree list/status/symbolic-full-name/check-ref-format）＋ merge 実行 2 経路（checked-out → in-worktree、非 checked-out → plumbing）。git <2.38 は conflict 判定 unknown + plumbing merge 不可。web/api/cli から呼べる interface seam（§1, §4, §5, §9）
- [x] Pull requests を git コマンドで再現する > **store: pull_requests / pr_comments**: schemaDDL + migrateV5 の両方に DDL（changesDDL/migrateV2 パターン）。status 遷移（open→closed/merged、reopen は closed のみ）、`(base_ref, head_ref) WHERE status='open'` の部分一意 index、anchor（path+line+side+anchor_sha+diff_snippet）付き review comment、moderation 3 列は既存と同型（§2, §3）

- [x] Pull requests を git コマンドで再現する > **web: /pulls 系**: 一覧・new・詳細（commits/conflict バッジ/コメント時系列）・edit・files（diff + anchored comment `<details>` フォーム）・comment 動詞（edit/delete/hide/unhide）・status（close/reopen）・merge 確認→POST。repo 無効時は nav 非表示 + ルート未マウント（404）。live-updates.js に `"pr"` kind 分岐、topic `pr:{id}`/`pulls`、fragment/SSE 追加（§6, §8）

- [x] **fake/stub 向けの共有 contract test を書く**: `*store.Store` / `fakeStore` / `storyStore` に同一振る舞いスイート（bump 規則・エラー種別・フィルタ意味論）を流す。fake のポインタ共有は answer-swap ハンドラが依存しているので本物に揃えず「fake の仕様として宣言」する方向（詳細: PR #71）
- [x] **CSS coverage 未到達ルールの仕分け**: 全 story の rule usage union で 68 ルール未到達 — pseudo-state/JS 駆動（toast, [data-copied], dialog :modal）と fixture gap（.issue-body-empty, .user-handle, .error-page, .badge-file-D/R）を分けて story 追加 or 実 app 検証に振る（詳細: [`docs/sketch/ja/rendering-and-visual-diff.md`](docs/sketch/ja/rendering-and-visual-diff.md)）
- [x] **Long story を content-visibility 閾値超にする**: StoryIssueDetailLong/StoryDiscussionDetailLong が 24 件で `.comment-list:has(> .comment:nth-child(50))` を発火しない → 50+ 件にして CV パスを視覚検証可能にする
- [x] **/static/ の versioned URL 化**: `max-age=3600` は妥協案。hash 入り URL + `immutable` にすると長期キャッシュが効く（modern-web-guidance performance の CacheFirst 方針、[`docs/sketch/ja/rendering-and-visual-diff.md`](docs/sketch/ja/rendering-and-visual-diff.md)）
- [x] **visual-diff の拡張**: (a) dark モードの baseline を別途撮る、(b) JS 駆動状態（dialog open / toast / copied）の story 化または実 app 撮影で `.toast` 等の視覚カバレッジを取る
- [x] **`createPull` の base/head 正規化エラーを分離する**: 片方の `NormalizeRef` エラーがもう片方で上書きされないようにし、不正 ref を 422 として返す（[bugs.md #37](docs/ja/bugs.md)、[owner-improvements §1.2](docs/sketch/ja/owner-improvements.md)）
- [x] **API と web の PR 一覧で page をクランプする**: 巨大な `page` で `(page-1)*perPage` がオーバーフローしないよう、store 層の共通クランプに寄せる。API・web の回帰テストを足す（[bugs.md #38](docs/ja/bugs.md)、[owner-improvements §1.4](docs/sketch/ja/owner-improvements.md)）
- [x] **PR diff の切り詰めをクライアントへ通知する**: API は `X-Truncated: true` を返し、remote CLI は不完全な diff であることを警告する（[bugs.md #41](docs/ja/bugs.md)、[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [x] **remote CLI の HTTP client に timeout を付ける**: 共有 `client.HTTPClient` に 30s 程度の timeout を設定し、無応答サーバーで CLI が永遠に待たないようにする（[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [x] **pin 更新エラーの扱いを揃える**: api/cli/web で `repo.UpdatePin` の失敗を少なくともログへ記録する。必要なら共通 helper へ寄せる（[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [x] **`CheckMerge` に解決済み SHA を渡す**: ref 名ではなく `RevParse` 済みの base/head SHA で `merge-tree` を実行し、報告 SHA と判定対象を一致させる（[bugs.md #39](docs/ja/bugs.md)、[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [x] **`pullsEnabled` を Renderer ごとの状態にする**: global variable をやめ、同一プロセス内の複数 handler/storybook が競合しないようにする（[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [x] **hide reason の Bad request 見出しを翻訳する**: `comments.go` / `pulls.go` の直書き `"Bad request"` を `msgFor(r).ErrBadRequestH` に置き換える（[bugs.md #40](docs/ja/bugs.md)、[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [x] **`Accept-Language` の q 値を解釈する**: `en, ja;q=0.1` で英語を選ぶよう簡易 q パースを実装し、`wantsJapanese` の回帰テストを足す（[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [x] **SQLite を WAL + immediate transaction にする**: `_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate` を検証し、並行 writer で `SQLITE_BUSY` が出ないテストを足す（[owner-improvements §1.3](docs/sketch/ja/owner-improvements.md)）
- [x] **`/static/` を `withUser` の外に出し gzip + ETag を付ける**: 静的ファイルごとのセッション照会をなくし、事前圧縮と 304 を入れる。versioned URL 化は既存タスクで後段とする（[owner-improvements §2.3](docs/sketch/ja/owner-improvements.md)）
- [x] **workspace 解決から COUNT(*) を外す**: slug→ID の軽量クエリを用意し、件数が必要な画面だけで数える。`/w/{ws}/...` 全リクエストの定常クエリを減らす（[owner-improvements §2.1](docs/sketch/ja/owner-improvements.md)）
- [x] **markdown レンダリング結果を LRU キャッシュする**: `sha256(body)` をキーに表示時変換の結果だけキャッシュし、DB に生 markdown を置く方針は維持する（[owner-improvements §2.4](docs/sketch/ja/owner-improvements.md)）
- [x] **PR 詳細の git 呼び出しをまとめる**: `rev-parse` の束ね方と `(baseSHA, headSHA)` キーの小さな cache を検討し、`Log` / `CheckMerge` の重複実行を減らす（[owner-improvements §2.6](docs/sketch/ja/owner-improvements.md)）
- [x] **テンプレート parse をロケールごとの clone に寄せる**: layout+fragments を 1 回 parse して `Clone()` し、`jsDictFor` の JSON 化もロケール単位にする（[owner-improvements §2.7](docs/sketch/ja/owner-improvements.md)）
- [x] **API のラウンドトリップを減らす**: workspace 解決を scoped update に寄せる、`GetComment` または `INSERT ... RETURNING` を足す、`LabelByName` を足す、の小さな store API 拡張を行う（[owner-improvements §2.8](docs/sketch/ja/owner-improvements.md)）
- [x] **hidden タブの SSE を閉じる**: `visibilitychange` で hidden 時に `EventSource` を閉じ、visible 時に再接続 + resync する。storybook/実 app で確認する（**JS**、[owner-improvements §2.9](docs/sketch/ja/owner-improvements.md)）
- [x] **web handler の小さな定型重複を削る**: `requireIssue` / `requireDiscussion`、generic な `filterByID`、フォーム page builder を入れ、未使用の `Renderer.execute` / `Renderer.notFound` を整理する（[owner-improvements §3.4](docs/sketch/ja/owner-improvements.md)）
- [x] **重複テンプレートを共通化する**: pager と delete confirm の双子テンプレートを整理し、pulls 側の空 `q=` 出力も直す。markup 差分は storybook で確認する（[owner-improvements §3.5](docs/sketch/ja/owner-improvements.md)）
- [x] **i18n catalog を分割し ja の空フィールドを検出する**: `messages.go` / `messages_en.go` / `messages_ja.go` / `locale.go` に分け、en fallback に黙って落ちるフィールドをテストで防ぐ（[owner-improvements §3.6](docs/sketch/ja/owner-improvements.md)）
- [x] **`internal/cli/pulls.go` と `internal/client/pulls.go` の単体テストを足す**: 状態遷移・エラー写像・markdown/diff 出力の最低限を固定する（[owner-improvements §3.7](docs/sketch/ja/owner-improvements.md)）
- [x] **古いドキュメント記述を現状に揃える**: labels 管理画面は実装済み、`style-src-elem` の CSP 記述は実装とコメントのどちらかに揃える。CSP hash 化の可否は実ブラウザで確認する（[owner-improvements §3.8](docs/sketch/ja/owner-improvements.md)）
- [x] **writing experience を1PRで実装する**: Write/Preview タブ + `POST /preview`、localStorage の下書き自動保存（投稿成功時だけ削除・保持期限/手動クリアなし）、server-side `#12` リンク化をまとめて入れる（[open questions](docs/ja/open-questions.md) 回答済み、[owner-improvements §5.1](docs/sketch/ja/owner-improvements.md)）
- [x] **モバイル nav を `<details>` メニューに畳む**: 小幅 viewport では layout のリンク群を `<details>`/`<summary>` に畳み、storybook の `?w=360` と keyboard-only 操作で確認する（[open questions](docs/ja/open-questions.md) 回答済み、[owner-improvements §5.2](docs/sketch/ja/owner-improvements.md)）
- [x] **検索クエリパーサを FTS より先に入れる**: `is:open label:bug author:demo` 形式を web/CLI/API 共通の文法として parse し、条件はすべて AND にする。FTS5 への置き換えは後段とする（[open questions](docs/ja/open-questions.md) 回答済み、[owner-improvements §2.2](docs/sketch/ja/owner-improvements.md)）
- [x] **remote CLI の 404 を endpoint 文脈付きで写像する**: API 変更は入れず、record missing と分かる endpoint だけ `store.ErrNotFound` に写像する。missing route を record missing に誤分類しない（[open questions](docs/ja/open-questions.md) 回答済み、[bugs.md #27](docs/ja/bugs.md)）
- [x] **PR files の不正マークアップと N+1 git 起動を直す**: 行コメント form は JS を増やさず `?line=` 付き GET でサーバー側に描画する。テーブル化や word-diff などの大規模再設計は今回入れない（[open questions](docs/ja/open-questions.md) 回答済み、[owner-improvements §2.5](docs/sketch/ja/owner-improvements.md)）
- [x] **更新 API に任意 `If-Match` と web hidden version を入れる**: `If-Match` 指定時だけ `version` を照合し、web フォームは常に hidden version を送る。不一致時の差分表示は実装しやすい最小範囲に留める（[open questions](docs/ja/open-questions.md) 回答済み、[owner-improvements §4.1](docs/sketch/ja/owner-improvements.md)）
- [x] **PR merge を最小 CAS で直列化する**: まず api/cli/web の3箇所に merging 予約を入れ、service 抽出は後で行う。lease/復旧は実装しやすい最小範囲に留める（[open questions](docs/ja/open-questions.md) 回答済み、[bugs.md #36](docs/ja/bugs.md)）
- [x] **PR の pin 欠損を修復可能にする**: pin 失敗で PR 作成自体を巻き戻す設計に寄せず、pin 欠損を検出して後で再作成できる導線を持たせる（[open questions](docs/ja/open-questions.md) 回答済み、[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [x] **ログイン後に元のページへ戻る**: login link に safe な `?next=` を付け、認証後に元の path+query へ戻す（[owner-improvements §5.2](docs/sketch/ja/owner-improvements.md)）
- [x] **ES module の waterfall を modulepreload で平らにする**: page detail 系の依存 module を `<link rel="modulepreload">` で並列化し、バンドルしない方針は維持する（[owner-improvements §2.3](docs/sketch/ja/owner-improvements.md)）
- [x] **i18n の全面適用**: `docs/sketch/ja/i18n.md` 案C（薄いカタログ + ロケール別テンプレートセット + `?lang=`/cookie スイッチャ）を全画面へ横展開。実施済み: store 層 `FieldError` のコード化→web 層で訳す（`fieldErrText`）、静的 JS の文字列は `<html data-i18n>` の JSON → `i18n.js` の `t()`、`fmtTime`/`authorName`/`hideReasonLabel`/`noun`/`repliesClause` はロケール別 funcs 束縛、文中リンクは Pre/Link/Tail 分割フィールド、スイッチャは `GET /locale?lang=` が Referer のパス+クエリへ 303。残課題: UGC（issue 本文・コメント）の `lang`/`dir` 付け
- [x] **Pull requests を git コマンドで再現する**: `docs/sketch/ja/pull-requests.md` の設計どおりに実装。実体は DB 行 + (base_ref, head_ref)、コミット列・diff・conflict は open 中 git 都度計算、close/merge 時に `refs/zatudan/pr/<id>/head` を pin。repo 未検出時は機能ごと非表示（`-repo`/`ZATUDAN_REPO`、auto は -db 起点の `.git` 上方探索）
- [x] Pull requests を git コマンドで再現する > **api: /api/v1/pulls\***: pulls GET/POST、{id} GET/PATCH/DELETE、comments GET/POST、comments/{cid} PATCH/DELETE + hide/unhide、status POST、merge POST、diff GET（text/plain）、markdown GET。repo 無効 server では未マウント（404）（§7）
- [x] Pull requests を git コマンドで再現する > **cli: zatu pr**: list/view/diff/create/edit/close/reopen/merge/comment 動詞（create は `--path P --line L [--side left]`）。local モードは `--db` 起点の repo 解決（`--repo` 上書き、未検出は `not a git repository` で exit 1）、remote は /api/v1/pulls\* 経由（`--repo` は警告して無視）。completion.go と printUsage の機械的更新（§7）
- [x] Pull requests を git コマンドで再現する > **export: PrMarkdown**: review comment を `path:L42` (side) + `path L<開始>-<終了>` + プレーン fenced スニペット（DB の diff_snippet、行番号プレフィックスなし）+ 本文の形で出力。呼び出し側が集めた構造体を受ける純粋関数。`--json` は同構造の struct（§7）
- [x] Pull requests を git コマンドで再現する > **seed/storybook/demo**: storybook story は gitrepo の fake + 偽データ（PR #103）。実動作確認用スクリプトは `tools/demo-pr-history.sh`（fixture repo + `zatu pr` リプレイ）
- [x] Pull requests を git コマンドで再現する > **docs 更新**: decisions.md（PR の実体・ref pin・merge 2 経路・auto-detect の解決規則）、roadmap.md、ui-rules.md（/pulls の UI 規約）
