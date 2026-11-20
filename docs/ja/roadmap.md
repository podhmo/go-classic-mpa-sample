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
- Markdown エクスポート — 詳細ページの "Copy as markdown" ボタン、`GET /w/{ws}/issues|discussions/{id}/markdown`、`GET /api/v1/w/{ws}/.../{id}/markdown`、CLI `zatu issue|discussion view --markdown`。serializer は `internal/export` で全 surface 共有 — `docs/ja/decisions.md`「Markdown エクスポート」

## v3 候補（検討メモあり・未着手）

- Pull requests — git コマンドで再現。**実装済み**: `internal/gitrepo` + `pull_requests`/`pr_comments` + `/pulls` web + `/api/v1/pulls*` + `zatu pr` + `export.PullRequestMarkdown`。repo 未検出時は `-repo`/`ZATUDAN_REPO`/auto-detect いずれも見つからない場合に限り機能ごと非表示 — `docs/sketch/ja/pull-requests.md`、`docs/ja/decisions.md`「pull requests 機能」
- i18n（en/ja）— **実装済み**: `internal/web/i18n.go` の `messages` struct カタログ + ロケール別テンプレートセット + `?lang=`/cookie/`GET /locale` スイッチャ + `<html data-i18n>` → `i18n.js` で静的 JS 文字列。UGC は未翻訳（`lang`/`dir` 付けは今後） — `docs/sketch/ja/i18n.md`、`docs/ja/decisions.md`「i18n 全面適用」

- JSON Web API + gh 風 CLI — `/api/v1` + `issues issue list` 風サブコマンド — `docs/sketch/ja/cli-and-web-api.md`（2026-11-16 再検討済み。actor の前提は accounts Phase 1 で解消、残る論点は API credential の粒度）
- インフラ適用例 — Cloud Run + Litestream + GCS + IAP の最小構成（デプロイ・SQLite 永続化・authorization 未導入の補完としての認証境界。terraform スケッチ程度） — [issue #91](https://github.com/podhmo/go-classic-mpa-sample/issues/91)
- reactions・権限制御・issue→discussion 変換・pin・カテゴリ管理 UI — 各設計メモの「見送り」節を参照
