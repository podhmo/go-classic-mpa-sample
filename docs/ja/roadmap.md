# ロードマップ（Roadmap）

バージョンごとの機能一覧。各項目は「機能名 — 使い方 — 詳細ドキュメント」。

## v1: Issue tracker（現行・達成済み）

- Issue 一覧 — `GET /issues`（検索・ラベル絞り込み・ソート・ページネーション） — `docs/ja/prompts.md`
- Issue 詳細・作成・編集・削除・open/closed — `/issues/{id}` 系 — `docs/ja/prompts.md`
- コメント — issue 詳細の下部フォーム（`#comment-N` パーマリンク付き） — `docs/ja/prompts.md`
- ラベル — フォームで選択、一覧のバッジをクリックで絞り込み（管理画面なし） — `docs/ja/prompts.md`
- Markdown 記法 — issue 本文・コメント欄（GFM + `> [!NOTE]` 等のアラート） — `docs/ja/decisions.md`「Markdown レンダリング」
- ダークモード — OS 設定に自動追従（トグルなし） — `docs/ja/decisions.md`「ダークモード」
- ページ遷移アニメーション — 通常遷移はフェード、Prev/Next は方向スライド（自動） — `docs/ja/decisions.md`「ページ遷移アニメーション」「ページネーションの方向付き遷移」
- 検索語ハイライト — `?q=` 使用時に一覧タイトルを自動マーキング — `docs/ja/decisions.md`「検索語ハイライト」
- コメント permalink コピー — 各コメントの "copy link" ボタン — `docs/ja/decisions.md`「コメントのパーマリンクコピーボタン」
- Ctrl/Cmd+Enter 送信 — 各フォームの textarea — `docs/ja/decisions.md`「Ctrl/Cmd+Enter でのフォーム送信」
- 削除確認ダイアログ — 詳細ページの Delete ボタン（非対応ブラウザは確認ページへ） — `docs/ja/decisions.md`「削除確認ダイアログ」
- 自作 storybook — `make storybook` → http://localhost:6006 — `docs/ja/for-ui-designer.md`
- DB 初期化コマンド — `go run . -db app.db init` — `docs/ja/decisions.md`「今後の予定」

## v2: アカウント + Discussions + 部分更新（全て実装済み）

- アカウント（ログイン + 投稿者識別） — `/register` `/login` `/logout`。**Phase 1 実装済み**（ログイン + 投稿者識別、authorization は未導入）。未ログインでも全機能は使え、投稿に投稿者名が付く — `docs/sketch/ja/accounts.md`
- Discussions タブ — `/discussions` 系。カテゴリ（seed 固定 taxonomy）・1段スレッド返信・Q&A の answer マーク — `docs/sketch/ja/discussions.md`。reactions・poll・issue→discussion 変換・pin・カテゴリ管理 UI は未実装（v3 候補）
- コメント/返信の部分更新 — 手書き fetch + HTML fragment（`X-Partial` 判定ヘッダ）で全体再描画を回避。`partial-submit.js` は最初の ES module とし共有コード（announcer 等）を module 化。answer マーク・status toggle も同機構の replace モードで実装済み — `docs/sketch/ja/partial-updates.md` `docs/sketch/ja/js-modules.md`

## v3 候補（検討メモあり・未着手）

- JSON Web API + gh 風 CLI — `/api/v1` + `issues issue list` 風サブコマンド — `docs/sketch/ja/cli-and-web-api.md`（2026-11-16 再検討済み。actor の前提は accounts Phase 1 で解消、残る論点は API credential の粒度）
- reactions・権限制御・issue→discussion 変換・pin・カテゴリ管理 UI — 各設計メモの「見送り」節を参照
