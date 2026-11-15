# ロードマップ（Roadmap）

このアプリのバージョン計画。現在地と今後の方向を記録する。

## Version 1 — Issue Tracker MPA（現行・達成済み）

`docs/ja/prompts.md` の仕様を満たす classic MPA の issue tracker として完成した状態。
基盤の設計・実装は v1 の一部として達成済み。

### 核となる機能（初期仕様）

- Issue 一覧（Open/Closed タブ、キーワード検索、ラベル絞り込み、ソート、ページネーション）
- Issue 詳細・作成・編集・削除（2段階確認）・open/closed 切替
- コメント投稿（フラット時系列、パーマリンク `#comment-N`）
- ラベル（複数付与・色付きバッジ・シード固定）
- SQLite 永続化（`modernc.org/sqlite`）・デモデータシード・`init` サブコマンド
- PRG・CSRF（`http.NewCrossOriginProtection`）・サーバ側バリデーション・styled 404/405
- シングルバイナリ（`embed.FS` でテンプレート/CSS/静的ファイルを埋め込み）

### v1 の改善群（decisions.md に記録済みの追加分）

- Markdown レンダリング（goldmark + GFM + GitHub 風アラート）
- ダークモード（OS 設定追従、トークン三段構え）
- セキュリティヘッダ・CSP（`default-src 'none'` ベース、`script-src 'self'`）
- ページ遷移アニメーション（cross-document View Transitions、方向付きページネーション遷移）
- フォームエラー表現（`:user-invalid` ↔ `aria-invalid` 同期、pending エラー、a11y）
- レスポンシブ・タイポグラフィ（コンテナクエリ、テキストスケール、44px タッチターゲット）
- 長いスレッドの `content-visibility` 最適化
- 宣言的 UI パーツ（Invoker Commands の削除ダイアログ、sticky ヘッダ、back-to-top、
  Speculation Rules プリフェッチ、スキップリンク）
- JS 方針緩和後の機能（検索語ハイライト、コメント permalink コピー、
  Ctrl/Cmd+Enter 送信、aria-invalid 同期）
- 自作 storybook（`cmd/storybook`、minigo 由来）による UI 検証基盤

### v1 の性格

- 認証・認可なしのローカル単一ユーザー前提。投稿者は「固定のローカルユーザー」。
- 宣言的 HTML/CSS 優先、JS は検証可能な範囲でのみ補助的に使用。

## Version 2 — 複数人の場として使えるように（計画中）

v1 の「単一ユーザーの issue 管理」に「誰が書いたか」の識別と「対話の場」を加える。
設計メモは `docs/sketch/ja/` にある（実装順に記載）。

### 2.1 アカウント（`docs/sketch/ja/accounts.md`）

- パスワード + サーバーセッションのログイン（argon2id、`users`/`sessions` テーブル）
- 自己登録 `/register`、demo ユーザー seed
- 投稿への `author_id` 記録と表示（未ログイン=anonymous、全機能は引き続き無ログインで使用可）
- `PRAGMA user_version` によるスキーマ移行機構（初の本マイグレーション）
- 識別のみで権限制御は変えない（authorization は future work）

### 2.2 Discussions タブ（`docs/sketch/ja/discussions.md`）

- トップナビに Discussions タブを追加
- `discussions` / `discussion_categories` / `discussion_comments` の並列テーブル
  （`author_id` を初めから持つ）
- カテゴリ（単一選択・シード固定、open/qa の2形式）・1段スレッド返信・
  Q&A の answer マーク・独自採番
- discussion は閉じない（status 遷移なし）。answered は `is_answer` + 部分一意 index

## Version 3 以降の候補（検討メモ済み・未着手）

- **JSON Web API + gh 風 CLI**（`docs/sketch/ja/cli-and-web-api.md`）:
  `/api/v1` + Bearer token、CLI は `Backend` interface の HTTP 実装。
  ユーザー概念がある v2 後なら token → user_id で actor を扱える。
- **コメントの部分更新**（`docs/sketch/ja/partial-updates.md`）:
  anchor 徹底 → 手書き fetch or htmx の HTML fragment 方式。
- reactions / 投票（accounts が前提条件）
- 権限制御（author 以外の編集・削除・answer mark 禁止）
- issue→discussion 変換、pin、カテゴリ管理 UI、poll 形式
- コメント個別削除、投稿の編集者記録（`edited_by`）

## 運用メモ

- 各 version の区切りは「機能のまとまり」であってリリースタグを切る運用ではない
  （このリポジトリは main への逐次マージで進む）。
- 設計メモ（`docs/sketch/ja/`）で決まったことは実装時に `docs/ja/decisions.md` へ
  確定事項として移す。
