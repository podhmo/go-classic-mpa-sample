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

## 意図的に実装しないもの

- 認証・認可（ローカル単一ユーザー前提。公開ネットワーク向けではない）
- Markdown レンダリング（本文・コメントはプレーンテキスト、`white-space: pre-wrap` で改行保持）
- JavaScript 全般（HTMX 等も含む）
- ラベル管理画面（シードの固定ラベルのみ）
- Docker

（随時追記）
