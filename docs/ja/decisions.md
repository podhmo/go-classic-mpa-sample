# 決定事項（Decisions）

このドキュメントは実装中に行った設計・作業上の決定を記録する。

## 作業分担・プロセス

- **タスク分解**: `docs/ja/prompts.md` を `TODO.md` に分解。データ層（Track A）とWeb層（Track B）の2トラックに分割し、サブエージェント2個で並行実装。
- **共有契約の先固定**: 並行作業の衝突を避けるため、`internal/store` のAPI（型・メソッドシグネチャ・`ErrNotFound`・デフォルト値の意味論）をスケルトンコミットで先に固定した。両エージェントはこの契約に対して実装する。
- **worktree**: `git worktree` を `.worktrees/store`（ブランチ `impl/store`）と `.worktrees/web`（ブランチ `impl/web`）に作成。`.gitignore` に `.worktrees/` を追加。統合は main へのマージで行う。
- **ファイル所有権の分離**: エージェント間のコンフリクト防止のため、Track A は `internal/store/**` のみ、Track B は `main.go` / `internal/web/**` / `README.md` のみを触るルールとした。`go.mod`/`go.sum` はスケルトン時点で固定し両者とも変更禁止。

## 技術スタック

- **モジュールパス**: `github.com/podhmo/go-classic-mpa-sample`
- **SQLite ドライバ**: `modernc.org/sqlite` v1.62.0（CGO 不要）。ドライバ名は `"sqlite"`。
- **パッケージ構成**: `main.go`（package main, 起動処理）+ `internal/store`（データアクセス）+ `internal/web`（ハンドラ・テンプレート・静的ファイル）。過剰なレイヤー分割を避けるため3分割に留めた。
- **テンプレート/CSS の配布**: `embed.FS` でバイナリ埋め込み（シングルバイナリ動作）。

## store API の意味論（契約として固定）

- Issue番号 = `issues.id`。`INTEGER PRIMARY KEY AUTOINCREMENT` で採番し、削除後も再利用しない。
- `ListFilter` デフォルト: `Status:""` → `"open"`（`"all"` で無フィルタ）、`Sort` → `"newest"`（`created_at DESC, id DESC`）、`Page` 1-based（<1 は1にクランプ）、`PerPage<=0` → 20。
- `ListIssues` は `[]*Issue`（各 Labels 設定済み）とページング前の total count を返す。
- 存在しない行へのアクセスは `ErrNotFound` を返し、ハンドラ側で `errors.Is` → 404。
- `SeedDemoData` は issues テーブルが空のときだけ投入（冪等）。初回起動時のみデモデータ。
- タイムスタンプは UTC で保持。

## Web 層

- **Store の抽象化**: `internal/web` 側でコンシューマ側インターフェース `type Store interface {...}` を定義（Go 慣例）。実装は `*store.Store` が自動的に満たし、テストでは map ベースの fake を使う。これにより store 実装前でもweb層のテストが可能。
- **CSRF**: `http.NewCrossOriginProtection()`（Go 1.25+ の標準ライブラリ機能）をミドルウェアとして適用。ローカル利用のデモなので Fetch Metadata/Origin ベースで十分と判断。
- **PRG**: 成功POSTはすべて `303 SeeOther`。削除は `GET` 確認ページ + `POST` 実行の2段階（GETで破壊的操作をしない）。
- **バリデーション**: サーバー側のみ。title 必須（trim 後、上限 ~200）、body 任意（上限 ~10000）、コメント本文必須。失敗時は入力保持でフォーム再表示（422 または 400）。
- **ボディサイズ**: `http.MaxBytesReader` で ~1MB に制限。
- **id パース**: `r.PathValue("id")` + `strconv.ParseInt`。非数値 → 404。
- **起動オプション**: `-addr`（デフォルト `127.0.0.1:8080`、localhost のみバインド）、`-db`（デフォルト `issues.db`）。認証なし前提のため外部公開しない設計。
- **サーバー**: `http.Server` に `ReadHeaderTimeout` 等のタイムアウト設定、`signal.NotifyContext` で graceful shutdown。
- **エンドポイント構成**: `/` →303→ `/issues`。`GET /issues/new`, `POST /issues`, `GET /issues/{id}`, `GET|POST /issues/{id}/edit`, `POST /issues/{id}/status`, `GET|POST /issues/{id}/delete`, `POST /issues/{id}/comments`, `GET /static/app.css`。PUT/PATCH/DELETE は HTML フォームで扱えないため使わない。

## 実装詳細（マージ時点で確定）

- **タイムスタンプ**: `INTEGER` unix秒（UTC）で保持。インデックス効きがよくドライバ依存の変換も不要。
- **DSN**: `Open` はパスを `file:` DSN に包み `_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)` を付与。`_pragma` はプール内の**全接続**に適用される（接続後に `Exec("PRAGMA ...")` すると他接続で無効になる罠を回避）。`:memory:` → `file::memory:` + `SetMaxOpenConns(1)`/`SetMaxIdleConns(1)`。
- **検索**: `instr(lower(title), lower(?))` / `instr(lower(body), lower(?))` の OR 条件。`LIKE` より単純で `%`/`_` のエスケープ考慮が不要。
- **ラベルフィルタ**: `EXISTS (SELECT 1 FROM issue_labels ...)` サブクエリ。`JOIN`+`DISTINCT` を避け `COUNT(*)` と WHERE を完全一致させやすい。
- **ラベル取得**: issue一覧に対し `IN (?,?,...)` のグループ化クエリ1本で付与（N+1 回避）。
- **web.Store**: `internal/web` が9メソッドのインターフェースを定義し `*store.Store` が構造的に充足。テストは map ベース fake。
- **環境変数**: `ISSUES_ADDR` / `ISSUES_DB`（フラグ `-addr`/`-db` が優先）。
- **静的ファイル**: `internal/web/static/` を `embed.FS` + `http.FileServerFS` で `/static/` 配信。
- **サーバータイムアウト**: ReadHeader 10s / Read 15s / Write 15s / Idle 60s。
- **コメント投稿後**: `303` → `/issues/{id}#comments`（アンカー付き）。
- **シード**: ラベル4種（Bug `#d73a4a`, Feature `#0075ca`, Documentation `#1f6feb`, Enhancement `#a2eeef`）+ issue 7件（open/closed混在、複数ラベル、コメント0〜3件、長文あり、作成日時ばらつき）。issues テーブル非空なら何もしない（tx内 `COUNT(*)` チェック）。
- **`status` フィルタ**: web 側は `open|closed|all` に正規化してから `ListIssues` に渡す。
- **テスト規模**: store 12件 / web 22件（fake使用）。マージ後 `go test ./...` 全パス、実サーバーでのスモークテスト済み（CRUD・コメント・状態切替・削除・検索・CSRF・XSS・永続化）。

## バグ修正ラウンドで確定した意味論

- **未知・重複 `label_id` は silently drop**: 非数値/非正数（web層でdrop）と存在しないID（store層で `INSERT OR IGNORE ... SELECT id FROM labels` でdrop）を同じ方針に揃えた。500にしない・バリデーションエラーにもしない（通常UIでは発生しないため）。
- **`updated_at` の意味**: 「内容の実質的な変化」。変化のない status POST では bump しない（`CASE WHEN` ガード）。コメント投稿はアクティビティとして bump する（GitHub 式）。`UpdateIssue` は送信=更新意思として無条件 bump（変更有無は見ない）。
- **ページング**: `page` は `1..1_000_000` のみ受理（int64オーバーフロー防止）。`Page > TotalPages` は最終ページにクランプして再クエリ（空表示+復帰不能を防ぐ）。
- **404/405 の分担**: GET/POST の未知パスは `GET /`・`POST /` キャッチオール → styled 404。PUT/DELETE 等のブラウザが出さないメソッドは mux 自動 405（素の text/plain、`Allow` 付き）をそのまま使う。
- **静的配信**: ファイル単位の明示登録（`GET /static/app.css` → `ServeFileFS`）。ディレクトリ mount によるリスティング露出を防ぐ。
- **セキュリティヘッダ**: `X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`Referrer-Policy: same-origin` を middleware で全レスポンスに付与。CSP は inline style（ラベル色）と衝突するため見送り。

## 今後の予定（backlog で確定済みの方向）

- **DB 初期化の明示コマンド**: `go run . init`（または `-init` フラグ）でデータベースファイルを作成し、index 付きの全テーブルを揃える。ユーザー指示による決定 — index 設定を「暗黙の起動時スキーマ初期化」に留めず、明示的な ops 手順として提供する。詳細は `TODO.md` O1。実装時に検討: 通常起動の自動 init 併存可否、DDL の単一ソース化、`ANALYZE` 実施、冪等性。

## 意図的に実装しないもの

- 認証・認可（ローカル単一ユーザー前提。公開ネットワーク向けではない）
- Markdown レンダリング（本文・コメントはプレーンテキスト、`white-space: pre-wrap` で改行保持）
- JavaScript 全般（HTMX 等も含む）
- ラベル管理画面（シードの固定ラベルのみ）
- Docker

（随時追記）
