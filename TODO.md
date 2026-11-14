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

- [x] **Textarea auto-fit** (guide: `form-fields-automatically-fit-contents`): `field-sizing: content`（min/max の上下限つき）で issue body・comment の textarea を入力量に追従させる。宣言的 CSS のみ。
- [x] **Sticky site header with stuck state** (guide: `state-aware-sticky-headers`): サイトヘッダーを `position: sticky` 化し、stuck 時に影や縮小などの視覚変化を付ける。scroll-state クエリ等の宣言的手段を優先。
- [x] **Back-to-top on long threads** (guide: `scroll-position-aware-elements`): 長いコメントスレッドでスクロール位置連動の back-to-top ボタンを出す。CSS scroll-driven animation を優先し、非対応ブラウザは常時表示 or 非表示で graceful に。長いスレッドの story を `story_pages.go` に追加して確認する。
- [x] **Search-term highlighting** (guide: `highlight-text-ranges`): `?q=` の語を一覧の issue タイトル上で CSS Custom Highlight API によりハイライトし、どこがヒットしたか視認できるようにする。**JS**。
- [x] **Copy comment permalink** (guide: `copy-button`): 各コメントのパーマリンク横にコピーボタンを置き、`#comment-N` の URL を `navigator.clipboard` でコピー。非対応・権限なし時のフォールバックも用意。**JS**。
- [x] **IME-safe Cmd/Ctrl+Enter submit** (guide: `ime-safe-enter-submit`): コメント・issue フォームを Cmd/Ctrl+Enter で送信できるようにし、IME 変換中の Enter で誤送信しない。**JS**。
- [x] **Directional pagination transitions** (guide: `directional-navigation-transitions`): `pagereveal` イベント + view-transition types で Prev/Next の方向アニメーション。no-JS 方針時代に見送った項目の解禁（decisions.md「ページ遷移アニメーション」参照）。**JS**。storybook で片方向の再現が難しければ verify-ui（実サーバー + playwright）で検証する。
- [x] **aria-invalid ↔ :user-invalid sync** (guide: `accessible-error-announcement`): 以前 no-JS で見送った「`aria-invalid` を `:user-invalid` に同期させる JS」を追加し、送信前のクライアント側エラーも支援技術に伝える（decisions.md「フォームのエラー表示」の非対称を解消）。**JS**。
