# TODO — Go classic MPA Issue Tracker

> **Note on updating this file:** upkeep rules live in
> [`.agents/skills/todo-md/SKILL.md`](.agents/skills/todo-md/SKILL.md) —
> follow them whenever you add or complete a task. Short version: this file
> lists actionable work only (`[ ]` / `[-]`); mark finished work `[x]` — a
> periodic GC sweep (`make todo-gc`) appends such entries to `TODO-archive.md`.

Source spec: `docs/ja/prompts.md` (Go 1.27+, `net/http`, `html/template`, `database/sql`, SQLite via `modernc.org/sqlite`, no JavaScript, no web frameworks, no ORM).

Module path: `github.com/podhmo/go-classic-mpa-sample`

## To Be Implemented

Guides referenced below are in the `modern-web-guidance` catalog (`.agents/skills/modern-web-guidance/`): run `npx -y modern-web-guidance@latest retrieve "<id>"` before implementing. Everything must stay JavaScript-free per `docs/ja/prompts.md`; all picks below are declarative HTML/CSS only.

- [x] **Dark mode support** (guides: `dark-mode`, `contrast-color`): `:root` に `color-scheme: light dark` と `<meta name="color-scheme">` を追加し、固定ライトパレットを `light-dark()` トークン化 + `@media (prefers-color-scheme: dark)` フォールバック。ラベルバッジ（DB 由来の動的色）の文字色コントラストと favicon のダーク時視認性も確認する。
- [x] **Cross-document view transitions** (guide: `cross-document-transitions`): `@view-transition { navigation: auto }` で MPA 遷移に控えめなクロスフェードを入れる。純粋な CSS のプログレッシブエンハンスメント。ページネーション等は transition `types` で方向アニメーションも検討。
- [x] **Declarative delete confirmation** (guides: `declarative-dialog-popover-control`, `dialog`, `light-dismiss-a-dialog`): 詳細ページの削除確認を `popovertarget`/Invoker Commands で動く `[popover]`/`<dialog>` にし、JS なしで開閉できるようにする。既存の GET 確認ページは非対応ブラウザ向けフォールバックとして残す。Baseline サポートと dismiss 時のフォーカス復帰を確認。
- [x] **Form feedback polish** (guides: `required-field-feedback`, `accessible-error-announcement`, `style-parent-with-has`, `forms`): 視覚的なエラー表示を `aria-invalid` と `:user-invalid`/`:has()` で同期させ、インタラクション後にのみエラーが出るよう統一。ラベルの checkbox 群・select にも同じパターンを適用できるか確認。
- [x] **Responsive & typography pass** (guides: `responsive-design`, `respect-os-text-scale`, `fluid-scaling`, `single-line-overflow`, `typography`): `body { font-size: 15px }` が px 指定なので rem 化して OS テキストスケールに追従させる。640px 単一ブレークポイントの現状を監査し、filter-bar / issue-row の狭幅表示と長いタイトルの clamp/ellipsis を整える。
- [x] **Rendering performance for long lists** (guides: `defer-rendering-heavy-content`, `performance`): `.issue-row` / コメントに `content-visibility: auto` + `contain-intrinsic-size` を検討し、画面外レンダリングを省く。CSS のみ。
- [x] **CSP & header hardening** (guides: `security`, `privacy`): JS ゼロなので `script-src 'none'` を含む Content-Security-Policy を付けられる（`object-src 'none'`, `base-uri 'none'`, `frame-ancestors 'deny'` 等）。ラベル色の inline `style=` と `style-src` の両立方法を決める（bugs.md #13' で CSP はこの理由で見送り済み）。
- [x] **Skip link & a11y sweep** (guide: `accessibility`): "Skip to content" リンク追加、`:focus-visible`、ランドマーク・見出し階層の監査を全テンプレートで実施。
- [x] **Speculation rules prefetching** (guide: `improve-next-page-load-performance`): `<script type="speculationrules">` で Next ページや issue リンクを prefetch/prerender。実行 JS ではなく宣言的 JSON だが、no-JavaScript ポリシーに合うか判断してから実装する（合わないと判断したら wontfix で記録）。
  - 実装: inline `<script>` は CSP `script-src 'none'` にブロックされるため、`Speculation-Rules` レスポンスヘッダー + `/static/speculation-rules.json` で配信（decisions.md 参照）。
