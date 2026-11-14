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
- **コメント投稿後**: `303` → `/issues/{id}#comment-{comment_id}`（新規コメントのパーマリンクへ着地）。各コメントは `<li id="comment-N">` アンカーを持ち、タイムスタンプが `#comment-N` へのリンク（GitHub 式）。`:target` 疑似クラスでリンク先コメントをアクセント色枠でハイライト（JS 不使用）。
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

- **DB 初期化の明示コマンド**: `go run . init`（または `-init` フラグ）でデータベースファイルを作成し、index 付きの全テーブルを揃える。ユーザー指示による決定 — index 設定を「暗黙の起動時スキーマ初期化」に留めず、明示的な ops 手順として提供する。**実装済み**（main.go `initDB` + `store.Analyze` + `store.SchemaObjects`）:
  - 通常起動 `go run .` の自動 init は維持（ゼロ設定性を優先）— `init` は冪等な「作成/検証+stats 更新」ステップ。
  - DDL の単一ソースは `internal/store` の `schemaDDL` のみ — `init` は `store.Open`（=schema 保証）に乗る。`sqlite_master` 経由の `SchemaObjects` で実際の状態をレポート。
  - `ANALYZE` は init のたびに実行（= populated DB で再実行すれば stats 更新、ops の一環として意味を持つ）。
  - seed は `init` では実行しない（決定性・ops 純粋性 — デモデータ投入は起動時の `SeedDemoData` に留める）。
  - `flag` が最初の位置引数で解析を止めるため `-db` 等は subcommand より前に書く必要あり（README に記載）。
  - レポートは created/already existed + tables + indexes + analyze の4行、exit 0、再実行可（`sqlite_stat%`/`sqlite_autoindex%` はフィルタして決定的な出力にする）。

## 意図的に実装しないもの

- 認証・認可（ローカル単一ユーザー前提。公開ネットワーク向けではない）
- JavaScript 全般（HTMX 等も含む）
- ラベル管理画面（シードの固定ラベルのみ）
- Docker

## Markdown レンダリング（2026-11-14 ユーザー要望で追加）

- **ライブラリ**: `github.com/yuin/goldmark/v2` v2.1.15 + `github.com/yuin/goldmark-alert` v1.0.2（GitHub 風アラート `> [!NOTE]` 等、作者公式拡張）。当初の「実装しない」決定はユーザー指示により撤回。初回は v1.8.7 で実装したが、alert 拡張が v2 専用のため v2 へ移行（v1 の `goldmark.New().Convert()` facade は v2 では `parser.New()` + `html.New()` の手動構成に変更）。
- **レンダリング方式**: テンプレート関数 `markdown`（`funcMap` 経由）で表示時に変換し `template.HTML` を返す。DB には生 markdown を保存し、編集フォームの textarea も生テキストのまま。
- **拡張**: GFM（テーブル・打ち消し線・タスクリスト・URL 自動リンク）+ alert（NOTE/TIP/IMPORTANT/WARNING/CAUTION、カスタムタイトル可、アイコンは拡張内蔵 SVG で `fill: currentColor`）。アラートの色は既存 CSS 変数に寄せた（note→accent、tip→open、important→closed、warning→新規 --warn、caution→danger）。
- **安全性**: `html.WithUnsafe` なしがデフォルト — 生 HTML は `<!-- raw HTML omitted -->` に置換され、`javascript:`/`data:` 等の危険スキームは `href=""`/`src=""` に無効化される（`IsDangerousURL`、v2 でも同様）。別途 sanitizer（bluemonday 等）は不要と判断。
- **改行**: `html.WithHardWraps()` で単一改行を `<br>` に変換。旧 `white-space: pre-wrap` の見た目を維持するため。あわせて `.issue-body`/`.comment-body` の `pre-wrap` を外し、`.markdown-body` クラスに markdown 要素用 CSS を集約。
- **レンダリング失敗時**: `Render` のエラーはフォールバックとしてエスケープ済みテキストを返す（実際にはほぼ発生しない）。

## ダークモード（2026-11-14、modern-web-guidance `dark-mode` / `contrast-color`）

- **OS 設定に追従するだけ（トグルなし）**: `<meta name="color-scheme" content="light dark">` + `:root { color-scheme: light dark }`。手動トグルは JS か cookie+サーバー往復が必要になり、ガイドも "Most sites do not need a persistent toggle" としているため作らない。
- **トークンの三段構え**: 生の値は `--x-light` / `--x-dark` に置き、`--x` は (1) 既定で `var(--x-light)`、(2) `@media (prefers-color-scheme: dark)` で `var(--x-dark)`（`light-dark()` 非対応ブラウザ向けフォールバック）、(3) `@supports (color: light-dark(#000, #fff))` 内で `light-dark(var(--x-light), var(--x-dark))`。(3) はページ全体では (2) と同値だが、将来サブツリーに `color-scheme` を当てたときにも追従する。ガイド例の `:root` 内ネスト `@media` は使わずトップレベルに書いた（`light-dark()` 非対応の古いブラウザは CSS ネストも怪しいため）。
- **新トークン**: ハードコードされていた色をすべてトークン化。`--accent-emphasis`（塗りボタン背景。リンク用 `--accent` はダークで明るい `#4493f8` だが白文字は 3.2:1 しか出ないため、塗りは `#1f6feb` で 4.6:1）、`--danger-emphasis`（同理由、ダーク `#b62324` で白文字 6.5:1）、`--on-emphasis`、`--surface-hover`、`--open-border` / `--closed-border`、`--label-border`。ダークのパレット値は GitHub のダークテーマに寄せ、本文/補助文字/リンク/状態色はいずれも surface・bg に対して 4.5:1 以上を計算で確認。
- **ラベルバッジの文字色**: DB 由来の色を `style="--label-color: {{.Color}}"` で渡し（html/template の CSS フィルタは custom property 値にも効き、不正値は `ZgotmplZ` になることを確認）、CSS で `contrast-color(var(--label-bg))` → 非対応なら Relative Color Syntax の XYZ-y 閾値（0.17913）計算 → それも非対応なら従来の固定 `#1f2328`。各段は `@supports` で囲う（`var()` を含む宣言は未対応関数でも parse を通り、計算値時点で無効 → `unset` になって前の宣言にフォールバックしないため）。バッジ背景は DB の色そのままでスキームに依存しないため、文字色もスキーム非依存。
- **favicon**: data-URI SVG 内に `<style>@media (prefers-color-scheme:dark)…</style>` を埋め、ダーク時は四角を `#58a6ff`・リングを `#0d1117` に切り替え。従来の `#2563eb` はダークのタブバー（`#35363a` 想定）に対して 2.3:1 で 3:1 を割っていた（変更後 4.8:1）。SVG favicon 内の media query を解さないブラウザは従来どおりの見た目。

## ページ遷移アニメーション（2026-11-14、modern-web-guidance `cross-document-transitions`）

- **`@view-transition { navigation: auto }` を共有 `app.css` に置くだけ**: 遷移元・遷移先の両ページが opt-in している必要があるが、全ページが同じ `app.css` を読むので 1 箇所で済む。テンプレートは変更なし。非対応ブラウザ（2026-11 時点で Firefox）は未知の at-rule として無視し、通常の即時遷移になる。
- **`prefers-reduced-motion: no-preference` のときだけ opt-in**: ガイドの例どおり `@view-transition` 自体を media query で囲う。`reduce` では遷移が作られない（`pagereveal` の `viewTransition` が `null`）ことを実ブラウザで確認。
- **root のクロスフェードのみ、0.18s に短縮**: UA 既定のフェード（0.25s）を `::view-transition-group(root)` の `animation-duration` で短くした（old/new の image は duration を group から継承する）。ヘッダー等に `view-transition-name` を付けて個別に固定・モーフィングする演出は「控えめ」の範囲を超えるので入れない。
- **POST → 303 リダイレクト後も遷移する**: 新規作成・編集・コメント投稿は PRG で同一オリジンにリダイレクトするため、フォーム送信後の表示にも同じクロスフェードがかかる（Chrome 156 で確認）。
- **ページネーションの方向アニメーションは入れない**: `@view-transition` の `types` は遷移先ドキュメントに静的に付く値で、Prev/Next は同じテンプレートの同じページ種別なので「どちらから来たか」を表せない。方向を付けるにはガイドの `pagereveal` + Navigation API（JS）か、サーバーが Referer 等を見てページごとに `<style>` を出し分ける必要があり、前者は no-JS 方針、後者は将来の CSP（`style-src`）対応と衝突するので見送り。

（随時追記）
