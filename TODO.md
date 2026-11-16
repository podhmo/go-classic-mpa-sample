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

- [x] **fake/stub の契約乖離を全コードから探索し整理する**: 全 5 interface × 全実装の意味論突き合わせを実施（記録は PR #71）。実バグ寄りは全て修正、意図的スタブは doc コメントで宣言済み
- [ ] **fake/stub 向けの共有 contract test を書く**: `*store.Store` / `fakeStore` / `storyStore` に同一振る舞いスイート（bump 規則・エラー種別・フィルタ意味論）を流す。fake のポインタ共有は answer-swap ハンドラが依存しているので本物に揃えず「fake の仕様として宣言」する方向（詳細: PR #71）
- [ ] **`*client.Client` の sentinel 非写像の方針を決める**: remote モードでは `errors.Is(err, store.ErrNotFound)` が成立しない潜在乖離（現状 sentinel 判定する消費者は local 限定で実害なし）。404→`ErrNotFound` 写像 or API の機械可読エラーコード化を検討（bugs.md #27）
- [x] **UI ルール監査で検出した不統一の修正**: `docs/ja/ui-rules.md` の「変更予定」項目（外部デザインシステムチェックリスト監査 2026-11-16 の結果）
  - [x] **ボタン配置の統一**: 作成/編集フォーム（issue_form/discussion_form）と GET 削除確認ページ（issue_delete/discussion_delete）を「左:キャンセル / 右:確定・右寄せ」に揃える。dialog・返信フォームは準拠済み
  - [x] **ヘッダ New ボタンの枠線化**: layout.html の "New issue"/"New discussion" から `btn-primary` を外しページ内 Primary を1つにする
  - [x] **コメント欄が空のとき送信ボタンを Disabled に**: **JS**（textarea の input 監視。トップレベル・返信両方）
  - [x] **返信の部分投稿後に `<details>` を畳む**: **JS**（partial-submit.js append パスで `.reply-form` 成功時に `details.open=false`）
  - [x] **入力中の離脱ガード**: **JS**（dirty 時に beforeunload 確認を出す static/ スクリプト。送信成功で解除）
  - [x] **モバイル対応の残件**: 固定要素（toast/back-to-top）に `env(safe-area-inset-*)` を加味、装飾的 `:hover` を `@media (hover:hover)` でガード統一、狭画面で固定要素が送信ボタン等を覆わない配置の見直し
  - [x] **storybook ストーリーの拡充**: 変更予定項目（ボタン配置・狭画面レイアウト等）を目視確認できるよう、不足している状態ストーリーを `internal/web/story_pages.go` に追加する。JS 挙動は verify-ui 側で確認
  - [x] **コメント permalink を判別可能にする**: `.comment-permalink` が `color: inherit; text-decoration: none` で本文と同色無下線（hover でしか判別できない）→ リンクと分かる表現に直す（ui-rules.md §9、2026-11-16 決定）
- [x] **クライアント JS のライフサイクル統合**: 共有 registry `dom-hooks.js`（1本の MutationObserver + `bind(selector, {init, teardown})`）に attach/detach 追従を集約し、detail/form ページの script タグをページ種別エントリ1本に畳む（decisions.md 2026-11-16 参照）
- [ ] **他者コメントのリアルタイム反映（SSE push）**: `GET /{issues,discussions}/{id}/events` + store 層 emitter + `live-updates.js` + fragment GET エンドポイント。設計は `docs/sketch/ja/live-updates.md`（`WriteTimeout` 解除・static ルート登録が前提）。Phase 1 は comment/thread/reply の live append のみ
