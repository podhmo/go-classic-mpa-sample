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
- `ListFilter` デフォルト: `Status:""` → `"open"`（`"all"` で無フィルタ）、`Sort` → `"newest"`（`created_at DESC, id DESC`）、`Page` 1-based（<1 または `MaxListPage` 超過は1へフォールバック）、`PerPage<=0` → 20、`PerPage>MaxListPerPage` → `MaxListPerPage` にクランプ。
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
- **remote client timeout**: `client.HTTPClient` は 30s の `http.Client.Timeout` を持つ。これは response body 読み取りまで含む全体 deadline で、応答を開始した長大な diff/markdown も 30s で打ち切る。CLI が無応答サーバーへ固まらないことを優先し、長時間転送が必要なら用途別 client を分ける。
- **merge 後の pin 更新失敗**: git 側 merge は既に確定しているため API/CLI/web は merge 自体を成功として扱い、pin 更新失敗だけを観測可能にする。web/API はサーバーログ、CLI は stderr warning に出し、close 時の pin 更新失敗は archived SHA が壊れるため従来通りエラーにする。
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
- **セキュリティヘッダ**: `X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`Referrer-Policy: same-origin` を middleware で全レスポンスに付与。CSP は当初 inline style（ラベル色）と衝突するため見送ったが、後日 `style-src-attr` で解消して追加（下記「CSP・セキュリティヘッダ」参照）。

## 今後の予定（backlog で確定済みの方向）

- **DB 初期化の明示コマンド**: `go run . init`（または `-init` フラグ）でデータベースファイルを作成し、index 付きの全テーブルを揃える。ユーザー指示による決定 — index 設定を「暗黙の起動時スキーマ初期化」に留めず、明示的な ops 手順として提供する。**実装済み**（main.go `initDB` + `store.Analyze` + `store.SchemaObjects`）:
  - 通常起動 `go run .` の自動 init は維持（ゼロ設定性を優先）— `init` は冪等な「作成/検証+stats 更新」ステップ。
  - DDL の単一ソースは `internal/store` の `schemaDDL` のみ — `init` は `store.Open`（=schema 保証）に乗る。`sqlite_master` 経由の `SchemaObjects` で実際の状態をレポート。
  - `ANALYZE` は init のたびに実行（= populated DB で再実行すれば stats 更新、ops の一環として意味を持つ）。
  - seed は `init` では実行しない（決定性・ops 純粋性 — デモデータ投入は起動時の `SeedDemoData` に留める）。
  - `flag` が最初の位置引数で解析を止めるため `-db` 等は subcommand より前に書く必要あり（README に記載）。
  - レポートは created/already existed + tables + indexes + analyze の4行、exit 0、再実行可（`sqlite_stat%`/`sqlite_autoindex%` はフィルタして決定的な出力にする）。

## 意図的に実装しないもの

- ~~認証・認可（ローカル単一ユーザー前提。公開ネットワーク向けではない）~~ → 2026-11-15 に authentication（投稿者識別）は実装済み（下記「Accounts Phase 1」参照）。authorization と外部公開前提は引き続き実装しない
- ~~JavaScript 全般（HTMX 等も含む）~~ → 2026-11-15 に緩和（下記「JavaScript 方針」参照）。SPA 化や HTMX/Turbo のようなクライアント駆動の枠組みは引き続き入れない
- ~~ラベル管理画面（シードの固定ラベルのみ）~~ → 2026-11-17 に DB 管理画面として実装済み（下記「ラベルを定数から DB 管理へ」参照）
- Docker

## JavaScript 方針（2026-11-15 緩和）

- **経緯**: 元々 JS を使わなかった理由は「動作検証（イベント発火含む）が面倒になる」ためで、JS 自体を否定していたわけではない。自作 storybook（`cmd/storybook`、PR #24）がページを偽データで描画し `StoryHttp*` が実際のルーティング・ヘッダ（CSP 含む）をリプレイするようになったため、イベントの発火を含む挙動を storybook 経由で問題なく確認できるなら JS を書くことは問題ない、という方針に緩和した。
- **優先順位は変えない**: 宣言的な HTML/CSS（Invoker Commands、View Transitions、`speculationrules` 等）で実現できるものは引き続きそちらを優先する。JS は宣言的機能で届かない場合の手段。
- **使うときの条件**: (1) storybook で挙動を検証できること（イベント発火を含む）。検証経路が無い JS は書かない。(2) JS は `/static/` の外部ファイルに置く — CSP は本緩和と同時に `script-src 'none'` → `'self'` へ変更済みで、同一生成元のファイルは読み込めるが inline `<script>` は引き続きブロックされる。`StoryHttp*` が本番ヘッダをそのままリプレイするため、CSP とのズレはプレビュー段階で表面化する。
- **このセクション以前の記述について**: 各機能の実装ノートで「no-JS 方針」を理由に JS を採らなかった記述は、緩和前の実装経緯として残す（当時は宣言的実装が可能だったため JS を選ばなかった、という事実は変わらない）。

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

## 削除確認ダイアログ（2026-11-14、modern-web-guidance `declarative-dialog-popover-control` / `dialog` / `light-dismiss-a-dialog`）

- **`[popover]` ではなくモーダル `<dialog>` + `command="show-modal"`**: 取り消せない破壊的操作の確認なので `dialog` ガイドの Rule 1（ページを inert にするモーダル）に当たる。`popovertarget` で開く `<dialog popover>` は非モーダル（ページ操作可能）で確認用途に合わない。開閉は Invoker Commands（`commandfor` + `command="show-modal"` / `command="close"`）のみで JS なし。フォーカス復帰は UA 任せ（Esc・Cancel・背景クリックいずれでもトリガーの Delete ボタンに戻ることを Chromium 156 で確認）。
- **安全側に `autofocus`**: 開いた直後のフォーカスは Cancel。Delete issue は POST フォーム（既存の `POST /issues/{id}/delete`、CSRF 保護もそのまま）の submit ボタン。Cancel は同じ form 内なので `type="button"` を明示（無いと submit になり command が走らない）。
- **`closedby="any"` で背景クリックでも閉じる**: Safari は `closedby` 未対応だが Esc・Cancel で閉じられるので劣化は「背景クリック無効」だけ。ガイドが示す JS フォールバックは no-JS 方針で入れない。
- **非対応ブラウザは GET 確認ページへのリンクを出す（CSS で出し分け）**: Delete ボタン（dialog 用）と従来の `<a href="/issues/{id}/delete">` を両方出力し、`@supports` で片方だけ表示する。HTML の機能（Invoker Commands）は CSS から検出できず、またトリガーを GET フォームに入れて「対応なら dialog、非対応なら GET 送信」にする手も使えない（form owner を持つボタンは `type="submit"` なら command より送信が優先され、`type` 省略だと Chrome 156 では送信も command もせず "require a type=button attribute. No action will be taken." と警告するだけ）。そこで代理として `sibling-index()` を使う: MDN browser-compat-data 上、Chrome 138 / Firefox 154 / Safari 26.2 で、Invoker Commands（135 / 144 / 26.2）より後か同時にすべてのエンジンで出荷されているため、`@supports (order: sibling-index())` が真なら dialog を開ける。誤判定の方向は「対応しているのにリンク側が出る」（Chrome 135–137・Firefox 144–153）だけで、その場合も GET 確認ページで削除できる。CSS が読めない環境では両方表示される。polyfill（`invokers-polyfill`）は JS なので入れない。
- **Baseline**: Invoker Commands は Baseline 2025-12-12（Newly available）、`<dialog>` は Widely available、`closedby` は Limited availability（Safari なし）。

## フォームのエラー表示（2026-11-14、modern-web-guidance `required-field-feedback` / `accessible-error-announcement` / `style-parent-with-has` / `forms`）

- **invalid の出どころは 2 つ、見た目は 1 つ**: (a) サーバー検証の 422 再表示（`aria-invalid="true"` + エラー文 + `aria-describedby`）、(b) 送信前のブラウザ制約（`:user-invalid`。編集して離れた後か送信試行後にだけマッチし、ロード直後やタブで通過しただけでは出ない）。CSS は `:is(input, select, textarea):is(:user-invalid, [aria-invalid="true"])` で両方に同じ枠線・背景を当て、親の `.field:has(...)` でラベル/legend も赤くする。`:is()` で包むのはガイドのフォールバック手法どおり（素のセレクタリストだと `:user-invalid` 非対応ブラウザでルールごと落ち、サーバー側エラーまで無装飾になる）。
- **ガイドの「aria-invalid を :user-invalid に同期する JS」は入れない**: no-JS 方針のため。代わりに (1) 未エラーの欄は `aria-invalid="false"` を明示（ガイドの "Set aria-invalid="false" until :user-invalid matches"。Firefox/Safari が空の必須欄をロード直後から invalid と読むのを防ぐ）、(2) サーバー側エラー時だけ `aria-invalid="true"` とエラー id を `aria-describedby` に入れる。つまり送信前のクライアント側フィードバックは**視覚のみ**で、支援技術には `required` のネイティブ通知と送信後のサーバー検証で伝える。この非対称は受け入れる。
- **送信失敗後は最初のエラー欄に `autofocus`**: ガイドの "After a failed submit, move focus to the first invalid field" を JS なしで実現する。フォームは `novalidate`（サーバー側 422 UI を活かすため。pitfalls.md 参照）なのでブラウザはフォーカスを動かさない。エラー文の `role="alert"` は外した（ロード時から存在する live region は確実に読まれない — ガイド "Don't make each error message a live region"）。読み上げはフォーカス時の `aria-describedby` に任せる。
- **サーバー側エラーは次の送信まで消さない**: 直した後に `:user-valid` でエラー文を隠す CSS も書けるが、`aria-invalid="true"` は JS なしでは外せず見た目と ARIA がずれる。また本文の長さ超過のようにクライアント側制約が無い欄は、どんな編集でも `:user-valid` になり「直ったか」を判定できない。サーバーが真実の源という一貫性を優先した。
- **クライアント側メッセージは常にマークアップに置き CSS で出す**: タイトル・コメントには `class="field-error field-error-pending"` の「… is required.」を常に出力し、`.field:has(> :user-invalid) > .field-error-pending` のときだけ表示。色だけに頼らないよう文言 + 装飾アイコン（`::before { content: "!" / "" }`、代替テキスト空で読み上げ対象外。代替テキスト構文非対応ブラウザでは宣言ごと落ちてアイコンなし）。この要素は `aria-describedby` から参照しない（ガイド: 参照された文は非表示でも読まれる）。サーバー側エラーがある欄は同じ要素がサーバーの文言を持ち pending クラスなし。`:has()`/`:user-invalid` 非対応なら表示ルールが落ちて pending は出ないだけ（サーバー側エラーは常に表示）。
- **クライアント制約はサーバー検証に揃える**: タイトルに `pattern=".*\S.*"` を追加（空白のみはサーバーで trim されて "Title is required." になるため）。既存の `maxlength` と合わせタイトルの全サーバールールを事前に表現。本文・コメントの長さ上限は `maxlength` にしない（`maxlength` は UTF-16 コード単位、サーバーは rune 数で数えるため絵文字等で食い違う）。空白のみのコメントは textarea に `pattern` が無く事前検出できない（サーバーで弾く）。
- **必須マークの説明**: ガイドに従いフォーム冒頭に「Fields marked * are required.」を 1 回置く。ラベル内の `*` は従来どおり `aria-hidden`（`required` が読み上げられるため）。ヒント文は `aria-describedby` で欄に紐付けた（エラーがあればエラー id を先に）。
- **checkbox 群・select への適用可否（確認結果）**: セレクタは `input, select, textarea` 共通で、`fieldset.field:has(:is(:user-invalid, [aria-invalid="true"]))` が枠線と legend も赤くする。実ブラウザ（Chromium 156）で、ラベルの checkbox に一時的に `required` を付けて操作すると fieldset 枠・legend が赤くなり、フィルタの `<select>` に `required` を付けてキーボードで選び直すと `:user-invalid` で枠が赤くなることを確認。ただし現状どちらにも検証ルールが無い（ラベルは任意、select は常に有効値）ので属性は付けていない。なお「少なくとも 1 つ選択」は HTML 制約で表現できず（各 checkbox の `required` は「それぞれ必須」の意味）、JS なしでは実現できない。必要になったらサーバー側検証 + `aria-invalid` を fieldset 内の欄に出すパターンで足りる。

## レスポンシブ・タイポグラフィ（2026-11-14、modern-web-guidance `responsive-design` / `respect-os-text-scale` / `fluid-scaling` / `single-line-overflow` / `typography`）

- **本文は `0.9375rem`（既定設定で従来と同じ 15px）、root の `font-size` は触らない**: px 指定はブラウザの文字サイズ設定を無視する。`<meta name="text-scale" content="scale">` を layout に追加し、対応ブラウザでは OS のテキストスケールが root の既定サイズに反映される（ガイド: root を上書きするとこれを打ち消す）。`app.css` に px の `font-size` と `:root`/`html` の `font-size` が無いことをテスト（`TestStaticCSS`）で固定。フォーム部品は `max(1rem, 1em)` で既定設定時 16px 以上（iOS Safari のフォーカス時ズーム回避）。余白・枠線などの px は文字サイズと連動させる必要がないので残した。
- **640px 単一ブレークポイントの監査結果と置き換え**: 旧 `@media (max-width: 640px)` は (1) 本文を 14px に縮めていた（ガイド: 小画面で文字を縮めない）→ 廃止、(2) filter-bar を縦積み → コンポーネント側で解決、(3) issue-row を縦積み → コンテナクエリ、(4) page-head / issue-actions の縦積み → ページ単位なので media query のまま残し、閾値を `40em` にした（media query の em はブラウザ既定文字サイズ基準なので、文字を大きくした利用者ほど早く縦積みになる）。
- **filter-bar は `grid-template-columns: repeat(auto-fit, minmax(min(100%, 10rem), 1fr))`**: ブレークポイントなしで 4→3→2→1 列に折り返す。`min(100%, …)` で 320px 未満でもはみ出さない。640px 幅では 3 列 + Apply/Clear が 2 行目になる（4 列に収めるには最小幅を下げる必要があり、select が窮屈になるので受け入れた）。
- **issue-row はコンテナクエリ `@container issue-list (inline-size >= 48em)`**: 既定は縦積み（タイトル → 状態/ラベル/時刻）、リスト自体が十分広いときだけ横並び。閾値を em にしたのでテキストを拡大すると自動的に縦積みに戻る。
- **長いタイトルは一覧で 2 行 clamp（`line-clamp: 2` + `-webkit-` 版）、詳細ページは全文**: ガイドの single-line-overflow は 1 行 ellipsis だが、issue タイトルは識別に情報量が要るので 2 行にした。全文は DOM に残り（リンクのアクセシブルネーム・ページ内検索）、詳細ページの h1 で全文表示。`#番号` はタイトルの後ろの独立した flex item（`flex: none`）なので、タイトルが clamp されても見える。区切りのない長い文字列は `overflow-wrap: anywhere` で折る。
- **ラベルバッジは 1 行 ellipsis（最大 `min(100%, 12rem)`）、hover / キーボードフォーカスで全幅**: ガイドの「切り詰めた全文を見る手段を用意する」を `title` 属性（タッチ・キーボードで使えない）ではなく `:is(:hover, :focus-visible)` で実現。`prefers-reduced-motion: no-preference` のときだけ幅をトランジション。
- **h1 は `clamp(1.25rem, 1rem + 2cqi, 1.5rem)`**: `.main-content` を inline-size コンテナにし、ビューポートではなくコンテンツ幅に流体追従。上下限が rem なのでズーム・文字サイズ設定に従う（vw のみの指定はズームで大きくならない — ガイドの WCAG 1.4.4 注意）。
- **タッチ端末は幅に関係なく `@media (pointer: coarse)` でボタン・入力を 44px 以上**: 旧来は「狭い = タッチ」とみなしていたのを入力方式で分けた（`form-factor-adaptation`）。
- **本文段落は `max-inline-size: 75ch` + `text-wrap: pretty`、見出しは `text-wrap: balance`**: 1 行の長さを読みやすい範囲に抑える。`text-wrap` 非対応ブラウザは通常の折り返し。

## 長いリストのレンダリング（2026-11-14、modern-web-guidance `defer-rendering-heavy-content` / `performance`）

- **コメントだけ、しかも 50 件以上のスレッドだけに `content-visibility: auto`**: `.comment-list:has(> .comment:nth-child(50)) > .comment:nth-child(n + 3)`。実ブラウザ（Chromium 156、CPU 4x スロットリング、1280px、表・コード・アラート入りの重めのコメント）で読み込み時の style+layout を計測すると、11 件 37→43ms（悪化）、26 件 47→39ms、51 件 71→39ms、101 件 93→38ms、200 件 171→51ms（FCP 232→132ms）。損益分岐はおよそ 25 件、はっきり効くのは 50 件から。ガイドの「小さなページには使わない」に従い、通常のスレッドは何も変えない。
- **一覧の `.issue-row` には付けない**: 1 ページ 20 件固定（`perPage = 20`）で行の DOM も軽く、計測でも 1280px で layout 22.8→24.4ms・style 8.1→10.3ms と僅かに悪化した。先頭の数行は必ず初期表示内にあり、ガイドの「above-the-fold には付けない」にも反する。
- **先頭 2 件は常にレンダリング**: 本文が短い issue では最初のコメントが 800px の初期表示内（top ≈ 600px）に入る。どこまでが初期表示かは本文の長さ次第で CSS からは分からないので、確実に入りうる 2 件だけ除外した。
- **`contain-intrinsic-block-size: auto 6lh`**: 仮サイズは「短いコメント」（padding + 日時行 + 本文 3 行ほど ≈ 140px）。`auto` で一度描画した実寸を覚えるので、スクロールで戻ってもジャンプしない。重いコメントばかりのスレッドでは読み込み時の `scrollHeight` が実寸より小さく（200 件で 38,248px → 全部描画後 72,126px）、スクロールバーのつまみが進むにつれ縮むが、スクロール位置自体はスクロールアンカリングで飛ばない。インライン方向は親に引き伸ばされるので `contain-intrinsic-size` ではなく block 方向だけ指定した。`content-visibility: auto` に仮サイズが必ず付くことは `TestStaticCSS` で固定。
- **確認したこと（Chromium 156、ライト・ダーク両方）**: ホイールで最後までスクロールして各コメントが描画されること、`#comment-N`（150 件目）へのリンクで読み込み直後〜1 秒後まで対象が top=0 のまま `:target` の枠が付くこと、`window.find()`（ページ内検索相当）がスキップ中のコメントの文字列を見つけて描画させること、Tab でスキップ中のコメント内のリンクへ順に移動しフォーカス先が表示域に入ること、コメントフォームの `:has(:user-invalid)` エラー表示・空送信の 422 + `autofocus`、削除ダイアログの開閉とフォーカス復帰。
- **トレードオフ → 部分緩和（2026-11-14、bugs.md #23）: スキップ対象を `<li>` から `.comment-body` に移した**: 元はスキップ中の `<li>` が子なし listitem になり中身全体が `ignored`（`notRendered`）だった。ルールを `> .comment:nth-child(n + 3) .comment-body` に変えると、スキップ中でも `<li>` は `listitem ignored=false children=2`（パーマリンク入りヘッダ + スキップされた本文）として残り、欠落は**本文テキストだけ**に限定されることを CDP `Accessibility.getPartialAXTree` で確認。スクリーンリーダーは各コメントの存在・番号・リンクを列挙でき、本文だけが近くまでスクロールするまで見えない。perf の確認（CPU 4x・3 回中央値、style+layout 合計）: 101 件 none 75ms / li 19ms / body 28ms、200 件 none 169ms / li 39ms / body 45ms — body 版は li 版とほぼ同等の勝ちを維持（単発の外れ値は VM ノイズ）。閾値は 50 のまま（残存リスクは本文のみに縮小したため引き上げの必要性が減った）。`#comment-N` は `<li>` が常に実レイアウトを持つので以前よりむしろ確実。プレースホルダは `contain-intrinsic-block-size: auto 5lh`（li の padding+ヘッダは実寸なので 6lh→5lh に微調整）。`TestStaticCSS` に「CV ルールは `.comment-body` を対象とする」のガードを追加。find-in-page・スクロール描画・ライト/ダークを実ブラウザで再確認済み。

## CSP・セキュリティヘッダ（2026-11-14、modern-web-guidance `security` / `privacy`）

- **CSP を `default-src 'none'` ベースで有効化**: `style-src 'self'; style-src-attr 'unsafe-inline'; img-src 'self' data:; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`。当初は JS ゼロだったため `script-src 'none'` を張っていたが、2026-11-15 の JS 方針緩和にあわせ `'self'` へ変更（`/static/` の外部ファイルは許可、inline は引き続きブロック）。bugs.md #13' で「inline style と相性を考えると過剰」として見送った判断を `style-src-attr` で解消し取り下げた（#24 で解消済み）。
- **inline `style=` は `style-src-attr 'unsafe-inline'` でのみ許可**: ラベルバッジの `style="--label-color: {{.Color}}"` は DB 由来の動的値で、nonce/hash は style 属性値には使えない（nonce は `<style>`/`<script>` 要素のみに効く）。`-attr` を分けて指定することで「属性だけ通して `<style>` 要素は塞ぐ」最小の穴にした — 実ブラウザで `<style>` 要素注入が `style-src 'self'` にブロックされることを確認済み。`style-src-attr` 未対応の古いブラウザは `style-src` にフォールバックしてバッジ色だけ muted に落ちる graceful degradation（レイアウトは壊れない）。
- **`img-src 'self' data:`**: `data:` は自前の SVG favicon 用。goldmark はユーザー投稿の `![i](https://…)` を `<img>` で出力しうるが、外部画像は tracking pixel になるため意図的に遮断（privacy ガイドの「サードパーティリソースはプライバシー漏洩源」）。実ブラウザでブロックされることを確認。CSS の `url()` 経由の外部送信もこれで塞がる。もしユーザー画像を許したくなったら `https:` を足す判断ポイント。
- **追加ヘッダ**: `Cross-Origin-Opener-Policy: same-origin-allow-popups`（ポップアップ利用なし、ガイド既定値）、`Cross-Origin-Resource-Policy: same-origin`（全レスポンスがアプリ内部向け）、`Permissions-Policy` で未使用の強力機能を拒否（`accelerometer/camera/geolocation/gyroscope/magnetometer/microphone/payment/usb`）、`Vary: Sec-Fetch-Site, Origin`（`CrossOriginProtection` が参照するリクエストヘッダ — 共有キャッシュが 403/200 を別リクエストへ流用しないように）。

## スキップリンク・アクセシビリティ（2026-11-14、modern-web-guidance `accessibility`）

- **スキップリンクのターゲットは `<main id="main-content" tabindex="-1">`**: ガイドは「ターゲット内の最初の意味のある要素（例: h1）をターゲットにする」案も挙げるが、詳細ページでは h1 の手前にパンくずがあり、h1 起点だとそれが前方スキップの対象外になる。`<main>` 全体を `tabindex="-1"` でプログラム的にフォーカス可能にした（画面拡大鏡がフォーカスに追従するためのガイド推奨）。リンクは `<body>` 先頭（`<header>` より前）に置き最初の Tab ストップとし、非フォーカス時は clip-path で 1px に畳む。フォーカス時は `position: fixed` で左上に固定表示し、通常コンテンツを押し動かさない。表示トリガーは `:focus`（`:focus-visible` 非対応でも動く。スキップリンクはキーボードでしかフォーカスされない）。リングはアクセント背景上で見えない `--accent` ではなく `--on-emphasis`（両スキームで白）。
- **`role="list"` を `.issue-list` / `.comment-list` に付与**: ガイド caveat — Safari は `<nav>` 外の `<ul>`/`<ol>` に `list-style: none`（または flex/grid）を当てるとリスト意味を落とす。両リストとも `list-style: none`（comment-list は flex でもある）なので `role="list"` で復元。
- **フィルタフォームに `role="search"` + `aria-label="Filter issues"`**: 一覧ページの検索領域としてスクリーンリーダーのランドマークジャンプに載せる。
- **パンくずの現在地に `aria-current="page"`**: 最終項目はリンクでなく span だが、aria-current はコンテキスト位置を伝える。
- **コメントパーマリンクのアクセシブルネームを個別化**: 全コメントで可視テキストが同じ時刻表記になり得る（リンク一覧で「Nov 8, 2026 01:06」が連続）ため、`href="#comment-N"` ごとに別ターゲットへ向かうリンク名を `<span class="visually-hidden">(comment {{.ID}})</span>` で区別。`.visually-hidden` はガイドのユーティリティをそのまま追加（`:where(:not(:focus-within, :active))` でフォーカス可能な中身は自分で表示できる）。
- **監査結果（変更なしで済んだもの）**: ランドマークは全ページで banner(nav「Main」)/main/contentinfo が揃い、複数 nav はすべて aria-label で区別済み（Main / Breadcrumb / Filter by status / Pagination）。見出しは全テンプレートで h1 単一、詳細ページは h1 → h2（dialog・Comments）でレベル飛びなし。`:focus-visible` のリングは既存ルールが `a, button, input, select, textarea` を網羅済み（checkbox も `input` に含まれる）。`<dialog>` の aria-labelledby/describedby・`:user-invalid`/`aria-describedby` のフォーム連携も既に実装済み。
- **実ブラウザ検証（Chromium 156、ライト・ダーク両方）**: 1 回目の Tab でスキップリンクが左上に表示され、Enter でフォーカスが `<main>` に移り次の Tab が本文先頭の要素（タブ/パンくず）に行くこと、全対話要素に 2px のリングが見えること、削除ダイアログが Enter で開き autofocus の Cancel にフォーカス・Esc で Delete ボタンに戻ること、Playwright の AX スナップショットで各ランドマーク・`search`・`list`/`listitem`・リンク名「時刻 (comment N)」が出ることを確認。

## Speculation Rules による次ページ先読み（2026-11-14、modern-web-guidance `improve-next-page-load-performance`）

- **実装判断: wontfix ではなく採用**: speculation rules は宣言的 JSON のブラウザヒントで JS エンジンを走らせず、かつ `Speculation-Rules` レスポンスヘッダーによる外部 ruleset 配信なら `<script>` 要素自体も不要 — `@view-transition` や Invoker Commands と同じ「宣言的ブラウザ機能」のカテゴリとして no-JavaScript ポリシーに適合すると判断。
- **inline `<script type="speculationrules">` は `script-src 'none'` にブロックされる（実測）**: 「inert なので CSP の対象外」という予想に反し、Chromium 156 は inline speculation rules を `script-src` で検査して拒否（console: `Applying inline speculation rules violates ... 'script-src 'none''`）。`'unsafe-inline'` を足す・hash を列挙するのは CSP の緩和/保守コストになるため取らず、ヘッダー配信に切替。
- **`Speculation-Rules` ヘッダー + 外部 ruleset**: `listIssues` が `Speculation-Rules: "/static/speculation-rules.json"` を返す。ヘッダー値は sf-string で引用符必須（無引用だと "Cannot parse Speculation-Rules header value" 警告）。ruleset は `application/speculationrules+json` 必須（spec 規定、`application/json` では無視される）。ヘッダーは一覧ページのみ — 投機対象リンクが存在するのはそこだけ。
- **Chrome は CSP `prefetch-src` を未実装**: 書くと "Unrecognized Content-Security-Policy directive" と警告されるだけなので CSP には追加しない。投機 fetch を CSP で制限する術は現状無いため、`where.selector_matches` で対象を `.issue-list a.issue-title, .pagination a` に限定 — ページの主要な次遷移先のみを推測対象とし、フィルタ/ラベル/New issue 等は外す。状態変更は全て POST フォームなので GET リンクの prefetch に副作用は無い。
- **prefetch + `eagerness: "moderate"`**: ガイドが初回実装に勧める保守的な組合せ（"prefetch ... is a good choice for initial implementation, expanding to prerender later"）。moderate（~200ms hover）は一覧の等確率リンク群向き。conservative（pointerdown）では先読み時間がほぼ取れず、eager/immediate は 2 件制限を浪費する（指示範囲外でもある）。
- **鮮度リスク**: prefetch 文書は hover 時点のスナップショット。一覧・詳細は変わり得るが hover→click は秒オーダーで許容範囲と判断。閲覧者自身が変更した直後（コメント投稿等）はブラウザが関連投機を破棄する挙動に任せる。
- **検証方法の記録**: 投機 fetch は Playwright `requests`/Network ドメインに出ないため、標準ライブラリのロギングプロキシを `:8081` に立ててサーバー側ヘッダーで観測。hover → `GET /issues/2` `Sec-Purpose: 'prefetch'` `Sec-Speculation-Tags: '"issue-list"'`（`tag` がサーバー側識別に効く）、Next で `/issues?page=2` も発火、セレクタ外（New issue 等）は発火しない、クリック遷移正常、console エラー・警告ゼロを確認。
- **付けなかったもの**: HSTS（平 HTTP の localhost アプリであり、HTTP 経由ではブラウザが無視する）、`upgrade-insecure-requests`（全 URL を https に書き換えてアプリ自体が壊れる）、`require-trusted-types-for 'script'`（`script-src 'none'` で DOM シンクに届くスクリプト自体存在しないため無意味）、`report-to`（レポート endpoint がない）、`sandbox`（フォームまで塞がる）。`Referrer-Policy` は現行 `same-origin` 維持（ガイド既定の `strict-origin-when-cross-origin` より cross-origin への漏洩が少なく、外部参照ゼロなので実害なし）。

## textarea の自動フィット（2026-11-15、modern-web-guidance `form-fields-automatically-fit-contents`）

- **`field-sizing: content` を textarea に適用**: 入力量に追従して伸縮。Baseline 2026-06（Newly available）のプログレッシブエンハンスメント — 非対応ブラウザは `rows` 属性の固定高に落ちるだけなのでフォールバック JS（guide の grid-mirror 方式）は入れない。
- **インライン方向は `width: 100%` 据え置き**: ガイドの「長い placeholder で横方向が暴れる」対策どおり、textarea は水平 auto-size させず縦のみ伸縮。
- **上下限はフィールドごとに `lh` 単位で指定**: `#comment-body` は 4–16lh（旧 `rows=4` 相当を下限に）、`#f-body` は 10–32lh（旧 `rows=10` 相当）。`box-sizing: border-box` なので padding を含む。`lh` 単位は要素の line-height 基準で、文字サイズ設定に追従する。
- **実ブラウザ検証（Chromium 156、storybook）**: `#f-body` が空で 248px（min 10lh）、40 行入力で 794px（max 32lh、内部スクロール）にキャップされ、短文に戻すと min に縮むことを確認。

## 検索語ハイライト（2026-11-15、modern-web-guidance `highlight-text-ranges`）— 初の JS

- **CSS Custom Highlight API で `?q=` の語を issue タイトル上にマーキング**: `/static/search-highlight.js` が `#filter-q` の値を読み、`.issue-list .issue-title` 配下のテキストノードを TreeWalker で走査して `Range` を `search-match` ハイライトに登録。DOM 不変・要素追加なし。一覧がタイトルしか表示しないため、ボディ側のヒットは対象外。
- **初の JS ファイル**: JS 方針緩和後の最初の適用。CSP `script-src 'self'` で `/static/` の外部ファイルのみ許可、inline `<script>` は引き続きブロック — issues.html の `<script src defer>` で読み込み、一覧ページのみに限定。
- **一致判定はサーバーと同じ `instr(lower())` 相当**: 大文字小文字を無視した部分文字列（語分割しない）。オフセットは lowercase 化した文字列ではなく原文上で走査（`'İ'` 等のケースフォールドで長さが変わる文字への対策）。
- **`::highlight(search-match)` は `--accent-emphasis` 背景 + `--on-emphasis` 文字**: 既存の塗りボタン色ペアでライト/ダーク両方のコントラストを担保（文字 4.5:1+、背景 vs サーフェス 3:1+）。
- **非対応ブラウザはハイライト無しで終了**: `CSS.highlights` 不在なら早期 return。ガイドの `<mark>` フォールバックは「ハイライトが必須機能の場合のみ」推奨なので不採用 — 検索自体はサーバー側フィルタで機能する。
- **フッターの "No JavaScript." を更新**: 実態に合わせ "Declarative first; minimal JavaScript where it earns its place." に変更。
- **検証（Chromium 156、storybook StoryIssueListSearch）**: `CSS.highlights.get('search-match').size === 1`、"Improve responsive layout" の "layout" が視覚的にマーキングされることを確認。コンソールエラー・CSP 違反なし。旧 `TestSpeculationRules` の「`<script>` 全禁止」アサーションを「外部 /static スクリプトのみ許可、inline 禁止」に更新。

## コメントのパーマリンクコピーボタン（2026-11-15、`copy-permalink` ブランチ）

- **ガイド**: modern-web-guidance `copy-button`。「copy text that doesn't exist on the page — URL, code block」用途（コメントの `#comment-N` リンクはDOM上に文字列として存在しない）に該当するため、クリップボードへの書き込みは JS 必須。宣言的に書ける部分（ボタン、状態表示）は全てHTML/CSS。
- **Invoker Commands (`command`/`commandfor`)**: ガイド推奨の宣言的呼び出し。`command` イベントは bubble しないので `document` で capture phase に listen。`event.source` がボタン、`event.target` が `commandfor` の指すコメント要素。`CommandEvent` 非対応ブラウザは同じマークアップを click フォールバックで処理。
- **`navigator.clipboard.writeText` + `execCommand` フォールバック**: ガイド通り Async Clipboard API を優先し、失敗時（権限拒否・非対応）は非推奨だが依然動く `execCommand("copy")` に落とす。
- **フィードバックは `role="status"` の `<span>`**: ガイドの「Copy a tokenから離れ、可視の toast/inline 表示を SR にも届ける」に従い、ボタン隣に "Copied"/"Copy failed" を2秒表示。VoiceOver が同一メッセージの再読みをスキップする問題には NBSP 追加で対処。`aria-label` はボタン自体（コメント番号込み）に付け、`role=status` 側にエコーを書かない（重複読み上げ回避）。
- **hover だけに依存しない**: `(hover: hover)` 環境では opacity 0→hover/focus-within で表示、タッチは常時表示。コピー成功/失敗中は状態表示のため表示維持。
- **検証**: storybook の `StoryIssueDetail` で実ブラウザ確認 — クリックで `…#comment-1` が writeText に渡り、"Copied" 表示と `data-copied` 状態を確認。

## Ctrl/Cmd+Enter でのフォーム送信（2026-11-15、`ime-enter-submit` ブランチ）

- **ガイド**: modern-web-guidance `ime-safe-enter-submit`。要点は「裸の Enter で送信するカスタム keydown ハンドラは IME 確定 Enter と衝突する」こと。修飾キー併用については明示的に「Cmd+Enter や Ctrl+Enter などのショートカットは IME の確定キーと競合しないため IME 安全チェックは不要」とある。
- **Ctrl+Enter / Cmd+Enter を採用**: textarea で改行を潰さない（コメント/本文は改行必須）ことと、IME 安全を構造で担保するため裸 Enter ではなく修飾キー併用にした。`isComposing`/`keyCode === 229` のチェックはガイド上不要と判断し実装しない（将来裸 Enter 送信を追加する場合はその時点で必須）。
- **`requestSubmit()` を使う**: `form.submit()` ではなく `requestSubmit()` — submit イベントを発火させフォーム本来の送信経路を通す。`event.preventDefault()` で textarea への改行挿入を抑止。
- **対象は `form textarea` 全て**: コメント欄 (`#comment-body`) と issue 本文 (`#f-body`)。将来フォーム textarea が増えても追随不要なように個別 ID ではなく包括セレクタにした。
- **発見可能性**: 各 textarea に `aria-keyshortcuts="Control+Enter Meta+Enter"` を付与し、既存の `.field-hint`（aria-describedby 済み）に「Ctrl+Enter (Cmd+Enter on macOS) submits.」を追記。送信ボタン自体は従来通り `<button type="submit">`（ガイドの必須要件「explicit submit button」は既に満たしている）。
- **検証**: storybook の `StoryIssueDetail` で実ブラウザ確認 — Ctrl+Enter で submit イベント発火、裸 Enter では発火しないこと。

## ページネーションの方向付き遷移（2026-11-15、`pagination-transitions` ブランチ）

- **ガイド**: modern-web-guidance `directional-navigation-transitions`。ガイド本文は SPA 向け（`startViewTransition({types})`）だが、MPA では cross-document View Transitions + `pagereveal` の `viewTransition.types.add()` が同等手段。既存の cross-fade（`@view-transition { navigation: auto }`）はそのまま、ページ間移動時のみ forward/backward タイプを付けてスライドに置き換える。
- **方向検出は `page` パラメータの差分**: `navigation.activation.from.url`（旧URL）と `entry.url`（新URL）の `?page=` を比較。増→`forward`、減→`backward`。パスが同じ・page のみ異なる遷移に限定（issue 詳細への遷移等では何もしない→既定 cross-fade）。page パラメータ無しは 1 扱い。
- **タイミング**: `pagereveal` は新ドキュメント側で発火し、`vt.types` は mutable（Chrome 実測で add 確認）。bfcache restore 時は `viewTransition` が null なのでガード。`navigation.activation` 非対応ブラウザもガード — いずれも既定 cross-fade にフォールバック（ガイドの progressive enhancement 方針）。
- **CSS**: `html:active-view-transition-type(forward|backward)` で `::view-transition-old/new(root)` の animation-name を差し替え（旧ページ→進行方向へ100%、新ページ→逆方向から100%、transform のみ・非線形 ease-in-out）。`prefers-reduced-motion: no-preference` の `@view-transition` ゲートは既存のまま — reduce 環境では cross-doc VT 自体が無効なので追加対応不要。
- **検証**: storybook では実際の document 遷移を再現できないため verify-ui ルート — `go run .` の実アプリ + Playwright（`addInitScript` で pagereveal を記録）。Next クリック→`["forward"]`、Previous クリック→`["backward"]` を確認。なお計測は listener 内で `setTimeout(0)` して読む必要があった（addInitScript の listener は外部 JS の listener より先に走り、即時読みだと types 追加前の空配列が見える）。

## Sticky サイトヘッダー（2026-11-15、`sticky-header` ブランチ）

- **ガイド**: modern-web-guidance `state-aware-sticky-headers`。`position: sticky` + `@container scroll-state(stuck: block-start)` で JS なしに「stuck 時だけ見た目を変える」を実現（scroll-state queries は Chrome/Edge 144+。Firefox/Safari では影が出ないだけで sticky 自体は動く — ガイド推奨の progressive enhancement として受け入れ、IntersectionObserver フォールバックは入れない）。
- **ガイドの制約「クエリはコンテナの子孫にしか効かない」対応**: `.site-header`（=stickyコンテナ）に置いていた背景・下ボーダーを全幅の子 `.header-bar` に移し、`header-inner` をその内側に保つ構造に変更。stuck 状態では `.header-bar` に `box-shadow` を追加。
- **box-model を変えない**: stuck では影のみ変更。高さ・padding を触らないのでガイドの scroll anchoring 振動問題は起きず、`overflow-anchor: none` も不要（親は body で本来置けない）。
- **`scroll-padding-top: 5rem`**: sticky で隠れる分を anchor ジャンプ（`#comment-N` 等）とフォーカス可視性（WCAG 2.4.11）に配慮して html に設定。
- **検証**: storybook の `StoryIssueDetail` で実ブラウザ確認 — スクロール前 `box-shadow: none`、スクロール後に影が出て `site-header` が `top: 0` に留まることを確認。

## Back to Top ボタン（2026-11-15、`back-to-top` ブランチ）

- **ガイド**: modern-web-guidance `scroll-position-aware-elements`。スクロール量で要素の表示を切り替える container scroll-state queries（`scrollable: top`）を使用 — JS scroll listener 不要。
- **スクロールコンテナは `html`**: アプリのスクローラは document viewport なので `html { container-type: scroll-state }`。ガイドの `.scroller`（中身が div）相当をルートに適用した形。sticky-header の `container-name: site-header` とは別コンテナで衝突しない。
- **ボタンは `position: fixed`**: ガイドは scroller 内 `sticky` を使うが、document スクローラに対応するのは fixed。issue detail ページのみに配置（TODO「長いスレッドで」）。リンク先は `#main-content` — skip-link と同じ既存アンカーを再利用、focus も移る。
- **フォールバックはガイドの Basic Fallback**: 未対応ブラウザでは常時表示（ボタンが消えたままになる方が悪い）。`@supports (container-type: scroll-state)` で対応ブラウザだけ hidden + scroll 連動に切替。IntersectionObserver の Advanced Fallback は入れない（常時表示で十分）。
- **検証**: 新規 story `StoryIssueDetailLong`（24件コメントの長いスレッド）で実ブラウザ確認 — 初期 `visibility:hidden/opacity:0`、スクロール後 `visible/1`、クリックで `#main-content` に遷移。

## aria-invalid ↔ :user-invalid 同期 JS（2026-11-15、`user-invalid-sync` ブランチ）

- **ガイド**: modern-web-guidance `accessible-error-announcement`。JS 解禁により「見送り」と記録していた同期を実装。送信前のクライアント側エラーがこれまで視覚のみだったのを、支援技術にも同じタイミングで届くようにした。
- **同期の中核はガイドのスクリプトそのまま**: blur/change は `syncAria(el, true)`（表示+読み上げ）、input は消去側、invalid（novalidate なので将来向け保険）、reset は `setTimeout` 後に全フィールド。`flagged`/`announced` の WeakSet 管理も踏襲。
- **`data-error` で欄→エラー要素を結ぶ**: `#comment-body`→`comment-error`、`#f-title`→`f-title-error`。`aria-describedby` には表示中だけエラー id を入れる（ガイド: 参照先は非表示でも読まれる）。pending エラー要素は既存のものを再利用 — `.field:has(:user-invalid)` で CSS が出すのと同じ瞬間に describedby に載るので、見た目と読み上げが一致する。
- **サーバー側エラーは従来どおり次の送信まで残す**: スクリプトが立てていない `aria-invalid="true"` は触らない（ガイドの設計と一致）。`#f-body` はクライアント制約なし（optional）なので `data-error` 不付与 — サーバー側のみ。
- **読み上げはページに1つの polite live region**: `visually-hidden` div を動的生成し、エラー出現時に「ラベル: エラー文」を 500ms 遅延で設定（次フィールドの読み上げと被らないように）。送信失敗後の focus 移動は既存（サーバー再描画の `autofocus`）で担保。
- **検証**: storybook で実ブラウザ確認 — `#comment-body` に実入力→消去→Tab で `aria-invalid=true` + `aria-describedby="comment-hint comment-error"` + announcer「Add a comment: Comment is required.」、再入力で `false` に戻る、`StoryIssueDetailCommentError`（サーバーエラー状態）では blur しても `aria-invalid="true"` が維持されること。

（随時追記）

## minigo storybook 環境の構成（2026-11-14）

- **minigo は `third_party/minigo` への plain clone + `cmd/storybook` のネスト module + `replace` で取り込む**: submodule/go.work/vendor は採用せず。ルート module を汚染せず、clone 無しでも `go build ./...`/`go test ./...` が通る opt-in 構成にした。詳細は `docs/sketch/ja/storybook-with-minigo.md`。
- **sqlite 非実行は `modernc.org/sqlite` の空 bind で実現**: story 実行時は `import _ "modernc.org/sqlite"` が init されず driver 登録も走らない。`store.Open` を呼ぶ story は書かない。
- **`internal/store` は source 解釈、goldmark 系は host bind**: bound host struct の script リテラルは bound html/template からフィールド評価できないため、view model を生産するパッケージは source 解釈に寄せる（minigo の制限、issue としてフィードバック予定）。
- **`parsePages` を `ParseFS` から `ReadFile`+`Parse` に変更**: bound html/template に ParseFS が無く、script 側 `fs.FS` を bound 関数に渡せないため。挙動は同一。

## storybook: ビューポート切替は iframe 幅の固定で実装（2026-11-14）

モバイル確認用に `?w=320|360|768` で preview iframe の幅を固定する方式を選んだ。理由: JS なし・既存の index テンプレートだけで実現でき、デバイスエミュレーションより単純。全 story を 320px まで確認した結果、アプリ側 CSS の追修正は不要と判断（先行の responsive 対応で済んでいた）。

## minigo 新版 (#1068 対応) に合わせた storybook 機構の整理（2026-11-15）

- **自前の探索・描画機構を廃止し `github.com/podhmo/minigo/storybook` に移行**: minigo 側で共有化された `Book{Dir, Tag, Src, Setup}` + `Discover` + `Render`（`NewSession` fork 型）を使い、ローカルの `Book` は `*storybook.Book` の埋め込み + `StaticDir` の薄いラッパに縮小。重複コードを捨てて upstream 追従コストを下げる。
- **`parsePages` を `ParseFS` に復帰**: minigo#1074 で bound `ParseFS` + script `fs.FS` ブリッジが入ったため、回避用の `ReadFile`+`Parse` は不要になった（2026-11-14 の暫定決定を打ち消し）。
- **bind は plain Go 値 + `minigo.BindAs`**: `WrapFunc` の明示呼出しは不要になった（plain func は自動変換）。`goldmark-alert` の declared name 問題は `BoundOf().Name` 修正から `BindAs("alert")` へ。
- **story に renderXxx helper を噛ませて `Story.Group` を活用**: storybook の group 判定は「story 本体が最後に呼ぶ無修飾関数」を見る。従来は全 story が `printRec` で終わり 1 グループに潰れたため、`renderIssueList`/`renderIssueDetail`/… の薄い helper を story 側ファイルに追加し、index の variants grid（`/grid/<name>`）が render メソッド単位で出るようにした。アプリ側コードは不変。
- **internal/store は引き続き source 解釈**: `minigo.HostTypeOf` で bind に移す道はできたが、現状の source 解釈がゼロコストで supported のままなので移行しない（必要になれば gen-intrinsics で生成）。

## storybook: CSP 緩和を upstream (minigo#1081) に移譲（2026-11-15）

- **`cmd/storybook` 側の `connect-src 'self'` 追記ワークアラウンドを撤去**: `-watch` 時、`StoryHttp*` が replay する実 CSP（`default-src 'none'`、connect-src 無し）が reload script の EventSource を塞ぐため、consumer 側で `; connect-src 'self'` を足していた。minigo#1081 で `AppendLiveReload` 自身が CSP を緩めるようになった（`relaxConnectSrc` — `default-src` のソースリストを引き継いだ上で `'self'` を足し、default-src 無しの policy は無制限としてそのまま残す）ため、下流側の対応は不要になった。`page.go` は upstream の `examples/storybook/cmd/storybook/page.go` と再び同一内容に戻した。

## Accounts Phase 1: ログイン + 投稿者識別（2026-11-15、`devin/1794740040-accounts-phase1` ブランチ）

設計メモ `docs/sketch/ja/accounts.md` の Phase 1 を実装。authentication のみで authorization は入れず、anonymous 投稿は引き続き全機能で有効。

- **マイグレーションは `PRAGMA user_version`（案B・決定済み）**: `migrations` スライスの i 番目が v i → i+1 を担当し、各マイグレーションは自身の tx で適用後 `user_version = i+1` を刻む。新規 DB は全リストを走査するので init と upgrade が同じ経路。DB の version がコード側より新しい場合は起動エラーにする（サイレント降格を防ぐ）。`ALTER TABLE ADD COLUMN` のような非冪等 DDL を素直に書けるのが利点。
- **パスワードは argon2id PHC 文字列**: `golang.org/x/crypto/argon2`（m=64MiB, t=3, p=2, salt 16B, key 32B）で `$argon2id$v=19$m=...,t=...,p=...$...` 形式を保存。検証は PHC パース後 `subtle.ConstantTimeCompare`。パラメータ変更時も旧ハッシュは埋め込みパラメータで検証できる。
- **セッションは opaque token + DB 保存**: cookie は 32B `crypto/rand` の base64url、DB には SHA-256 hex のみ保存（DB 流出時にトークンを再生できない）。`HttpOnly; Path=/; SameSite=Lax`。**`Secure` は付けない** — plain-HTTP localhost で動かす用途のため。`MaxAge` も付けずブラウザセッション cookie とし、30日の有効期限はサーバー側 `expires_at` で担保（残り半分未満でスライド更新）。
- **middleware 構成**: `securityHeaders` → `http.NewCrossOriginProtection`（CSRF）→ `withUser` → mux。`withUser` は cookie → `SessionUser` → context へ載せるだけでリクエストを拒否しない（anonymous 許可）。POST は CrossOriginProtection が Sec-Fetch-Site/Origin で弾く。
- **ログイン失敗は汎用メッセージ + 固定遅延**: 「Name or password is incorrect.」で 422 再表示（ユーザー存在の oracle 化を避ける）+ 200ms sleep。`next` は `/` 始まりかつ `//` 不可に制限（open redirect 対策）。セッション fixation は「匿名セッションを発行しない・ログイン時に新 token」をもって回避。
- **name は UNIQUE + COLLATE NOCASE**: `alice`/`ALICE` の取り合い・ログインを同一視。登録エラーは modernc `*sqlite.Error.Code() == SQLITE_CONSTRAINT_UNIQUE` を検出して 422 フィールドエラーに。
- **author_id は NULL = anonymous**: issues/comments に `INTEGER REFERENCES users(id)` を追加。表示側は LEFT JOIN で `Author *User` を読み、nil は `anonymous` と描画。`CreateIssue`/`CreateComment` は authorID（0=anonymous）を取る署名に変更。
- **demo ユーザーは issue シードと独立して投入**: `SeedDemoData` は issues が空のときだけ issue/label を入れるが、`demo`/`demo` ユーザーは「demo が存在しなければ」の条件で常時投入 — 既存 DB を v2 に上げた場合もログインを試せるように。ハッシュは起動時生成（ソースに認証情報リテラルを置かない）。

## Discussions タブ実装（2026-11-15、`devin/1794741925-discussions` ブランチ）

設計メモ `docs/sketch/ja/discussions.md` の案B（並列テーブル）で実装。issues とは別の `discussion_categories` / `discussions` / `discussion_comments` を追加し、既存テーブルへの ALTER はなし（マイグレーションは追加のみ）。

- **discussion に status を持たせない**: discussions は close しないため status カラム自体を置かない。「回答あり」はカラムではなく `discussion_comments.is_answer` から EXISTS 副問合せで導出し、list/detail の両方と `?answered=yes|no` フィルタで同じ条件を使い回す。
- **answer の一意性は部分 UNIQUE index**: `UNIQUE(discussion_id) WHERE is_answer=1` で「1 discussion に answer は高々1つ」を DB 制約で担保。mark 移動は「全コメント unmark → 対象 mark」の2更新を1 tx で行う（constraint を一時違反しない順序）。
- **is_answer カラムを選んだ理由**: `discussions.answer_comment_id` 参照型は dangling pointer の心配（コメント削除・discussion 削除の cascade 順）があり、mark 移動も2箇所更新になるため却下。フラグ型ならコメント削除だけで整合する。
- **返信は1段のみ・`parent_id IS NULL` 制約で表現**: 返信への返信は `CreateDiscussionComment` 側が `parent_id` 対象の `parent_id IS NULL` を検証して `ErrInvalidParent`。深さは保存モデルが担保するので表示側は2段を描くだけ。
- **answer マークは top-level コメントかつ `format='qa'` カテゴリのみ**: reply を answer にすると親が残ると不自然なため reply は `ErrNotAnswerable`。`format` は `discussion_categories.format`（open|qa）で決め、open カテゴリでは UI 側もボタンを出さない。
- **カテゴリは seed 固定 taxonomy**: announcements/general/ideas(open) + qa。管理 UI はスコープ外（メモ通り）。フィルタは `?category={slug}` で slug 指定なので URL から id 解決不要。
- **nav タブは `Section` フィールド駆動**: 各ページ view-model に `Section string` を持たせ、layout が `aria-current="page"` + accent underline で現在地を示す。「New ○」ボタンも Section で切替。
- **返信フォームは `<details>`**: JS なし宣言型（メモ通り）。エラー時は `ReplyDraftID` で該当 `<details>` を `open` にして下書きを保持。親がトップレベル以外（返信への返信・他 discussion のコメント）の場合はどの `<details>` にも置けないため、下段コメントフォームへドラフトをフォールバックさせて入力文を失わない。
- **一覧は `.issue-list` / `.issue-title` クラスを再利用**: search-highlight.js が無修正で効く。`.discussion-list` を併記して今後の分岐余地を残す。speculation-rules のセレクタは `.discussion-list a.issue-title` も対象に追加。
- **storyStore を現行 Store インターフェースへ追従**: auth 系メソッドと `authorID` 引数が抜けて `-tags story` が壊れていた既存 bug を修正し、discussion fixture/story（list/empty/detail/form/errors/delete）を追加。

## コメント/返信の部分更新方針（2026-11-15、`devin/1794743974-partial-updates-revisited` ブランチ）

設計メモ `docs/sketch/ja/partial-updates.md` の再検討結果を決定として記録。実装は未着手（TODO.md の「コメント/返信の部分更新」エントリ参照）。

- **採用は手書き fetch + HTML fragment（案B）、スコープはコメント/返信投稿のみ**: `X-Partial` 判定ヘッダで POST が `<li>` fragment を返し、`/static/partial-submit.js`（~100 行）が submit を奪って fetch→挿入→通知する小機構。answer マーク・status toggle は Phase 2 で同機構に乗せる。
- **CSP に `connect-src 'self'` の追加が必須前提**: 現行 `default-src 'none'` では fetch が全ブロックされる。部分更新のどの案でも必要。
- **htmx / Datastar は採用しない（再評価条件つき）**: ネイティブ API（`setHTML()` Sanitizer、`moveBefore()`、`ariaNotify()`、`<progress>`）が vendored 依存の売りを相当埋めたため。機構が増えて管理が面倒になった時点で htmx、push/SSE 需要が出た時点で Datastar を再評価する。JSON API 経由は「ページ内更新の道具としては」不採用のまま。
- **volatile 機能は採用不可・ウォッチのみ**: `html-setters` / html-streaming-setters（宣言的部分更新）は単一エンジンの volatile 機能で opt-in ゲート付きのため選択肢から除外。

## JS 資産の構成: ESM 採用・バンドル不採用（2026-11-15、`devin/1794743974-partial-updates-revisited` ブランチ）

設計メモ `docs/sketch/ja/js-modules.md` の結論を決定として記録。

- **`type="module"` への移行は `partial-submit.js` 実装と同時に開始**: 動機は polite announcer 等の共有コードを import で切り出せること。module は defer 相当なので読み込みセマンティクスは不変。classic/module の混在は許容し漸進移行。
- **import map / bare specifier は使わない**: `<script type="importmap">` はインライン script で CSP `script-src 'self'` にブロックされる。全て自前ファイルなので相対パス import のみ。
- **バンドル・minify・fingerprint は現時点不採用**: シングル Go バイナリ + embed.FS + node ツールチェーンなしの方針と、合計 ~12KB の規模では効果が小さいため。再評価トリガー（~50KB 超 / ページあたり 5 ファイル超 / 外部依存 vendored 化）をメモに記録。
- **条件付き依存は `conditional-async-dependencies` の単一エントリパターン**: TLA + 兄弟 static import 回避（Safari 15–26.6 の TLA 同時 import bug 対策）。

## 部分更新 Phase 1 の実装決定（2026-11-15、`devin/1794745928-partial-updates` ブランチ）

「コメント/返信の部分更新方針」の実装で固めた細部。

- **fetch body は `URLSearchParams`（urlencoded）で送る**: `FormData` を渡すと multipart/form-data になり、通常フォーム送信と別のパーサ経路を通ることになる。`new URLSearchParams(new FormData(form))` で JS/非JS が同一 wire format（`application/x-www-form-urlencoded`）を共有する。
- **fragment のパースは `<template>` 一本**: `Document.parseHTML` は既定 Sanitizer を通し `class`/`id` を削る（実測）。同一オリジンのサーバー出力なのでサニタイズ不要、`<template>` innerHTML で属性を保持する（pitfalls.md 参照）。
- **422 時はフォーム fragment ごと差し替え・live region は鳴らさない**: エラーフォーム差し替えで `<details open>` 等の状態もサーバー再現でき、フォーカス移動と競合しないため announce はしない（persistent-toast-notifications ガイドの原則どおり）。
- **件数表示と空ノートは data 属性駆動**: サーバー再描画なしで `data-comment-count` を +1、`data-empty-note` を hidden にする。常時描画の `ol`（空なら `hidden`）を挿入先にして「0件→1件」の DOM 構造差を消した。
- **新 `<li>` のハイライトは `.comment-new` 一時クラス**: `:target` は挿入ノードに効かないため、`:target` と同じ見た目のクラスを4秒だけ付ける方式。
- **submit 奪取は document への delegated listener**: 差し替えられたフォーム（422 後や今後の注入）にも自動で効く。`enter-submit.js` も同理由で document delegation 化。

## JSON Web API（`/api/v1`）の実装決定（2026-11-16、`devin/1794764986-web-api` ブランチ）

`docs/sketch/ja/cli-and-web-api.md` Phase 2 の実装で固めた細部。

- **API credential は単一 env トークン（`ISSUES_API_TOKEN` / `-api-token`）を採用**: 設計メモの二択（env 単一 vs `api_tokens` テーブル）のうち最小構成側。`api.New` は Bearer 一致のみ見て書き込みは `author_id=0`（anonymous）扱い。`api_tokens`（token→user_id、author が立つ側）は TODO.md の別タスクとして残す。
- **fail-closed は mount レベルで担保**: トークン未設定なら `/api/v1` を mux に登録しない（存在しない = HTML の汎用 404）。`web.WithAPIToken` option で mount、中の mux は `Authorization: Bearer` を `crypto/subtle` 定数時間比較で検証し不一致・欠落は 401 JSON。
- **バリデーションは `internal/store` の `ValidateIssueInput`/`ValidateDiscussionInput`/`ValidateCommentBody` に集約**: 「`*Input` 値を検証する関数」を web/api 両方から呼ぶ形（メモの分解案どおり）。上限値 `MaxTitleRunes=200`/`MaxBodyRunes=10000` も store に移し web の同名定数は廃止。
- **ステータス変更は PATCH でなく `POST /api/v1/issues/{id}/status` を維持**: HTML 側のアクションエンドポイントと線を揃え、JSON も `{status}` 明示。update 系は `PATCH /issues/{id}`（送ったフィールドだけ更新）、削除は `DELETE` → 204。
- **`GET /api/v1/issues/{id}` は labels+comments を同梱**: 詳細取得を 1 call にする（HTML detail ページと同じ情報単位）。一覧は `{items, total, page, per_page}` エンベロープ、`per_page` 上限 100。
- **label/category の参照は名前/スラグも受ける**: `label_ids`/`category_id`（数値）に加え `labels: ["Bug"]`/`category: "qa"` を許容し、store の List* で名前→id を解決。不明な名前は 422 `fields` エラー（gh CLI の `--label` が名前を取るのに揃えた）。
- **`/api/v1` の mount はメソッド付き subtree パターン（GET/POST/PATCH/DELETE）**: Go ServeMux の制約で、メソッド無指定の `/api/v1/` は catch-all `GET /` と衝突（パスは狭いがメソッド集合は広い）するため。PUT 等は 405 に落ちる。
- **タイムスタンプは RFC 3339 文字列**: DTO 側で `time.Time.Format` して出す（Unix 秒ではなく人間可読寄せ）。
- **web のテストは fakeStore、api のテストは実 `*store.Store`** を使い分け: API は store 直上の薄い層なので、実 DB で wire contract（認証・エンベロープ・422 fields）を見る方が意味が大きい。

## Go ソースのファイル配置規則（2026-11-16、`devin/1794766503-file-symbol-alignment` ブランチ）

ファイル名とシンボル名の対応を取るリファクタリングで固めた配置規則。

- **トピックごとのファイルに、そのトピックの定義を集める**: `issues.go` / `discussions.go` / `accounts.go` の3本柱。`handlers.go`（issue 定義の寄せ集め）は `issues.go` に改名して廃止。
- **web パッケージ**: `issues.go` = issue のページ view model + ハンドラ + `parseListFilter`/`issuesURL` 系、`auth.go` = アカウントルート + セッション/cookie/context 解決（`withUser`/`userFromContext`）、`web.go` = ルーティング (`New`)・middleware・横断 helper のみ。
- **store パッケージ**: `store.go` = `Store`・スキーマ/migration・`Open` 等の DB 管理のみ。`Issue`/`Comment`/`Label` 型と CRUD は `issues.go`、共有 scan/null helper は `store.go` に残す。
- **api パッケージ**: `api.go` = ルーティング・認証・JSON helper・`User` DTO のみ。DTO は所有者のファイルへ（`Issue`/`Comment`/`Label` → `issues.go`、`Discussion*` → `discussions.go`）。
- **テストも同規則**: `*_test.go` は対象ファイル名に合わせる（`issues_test.go`/`auth_test.go`/`seed_test.go`）。パッケージ共有の fake/harness（`fakeStore`, `newTestStore`, `newServer`）は親玉ファイル（`web_test.go`/`store_test.go`/`api_test.go`）に残す。

## ユーザー別 API トークン（api_tokens）の実装決定（2026-11-16、`devin/1794767572-api-tokens` ブランチ）

`docs/sketch/ja/cli-and-web-api.md`「認証」節の後者案（actor を立てる側）を実装したときの細部。

- **credential は env 単一トークン + api_tokens の併用**: env token（`ISSUES_API_TOKEN`）は従来どおり anonymous（author_id=0）扱い。一致しなかった Bearer は `api_tokens` テーブルを引き、行があればその user_id が actor になる — API 書き込みに author_id が立ち gh parity の投稿者識別に届く。両者は排他でなく共存する。
- **api_tokens は平文を保存しない・期限なし**: PK は `auth.HashToken`（SHA-256 hex、auth.NewToken も再利用 — sessions と同じプリミティブ）。expires_at は持たせず revoke は行削除のみ。寿命管理が要る段階で列を足す（TODO の「Phase 4 仕上げ」に管理導線として残す）。
- **mount 判定は起動時の1回**: `env token 非空 OR api_tokens 非空` で `/api/v1` を mount（`web.WithAPIEnabled` を追加。WithAPIToken は非空で enabled を兼ねるので既存呼び出しはそのまま）。稼働中に初めて token を発行しても remount はされないため再起動が要る — 一度 mount されれば発行/revoke の反映はリクエスト毎の解決で即効く（fail-closed と即時 revoke の折衷点としてここに置いた）。
- **actor は request context 経由で渡す**: bearerAuth が解決した user_id（0=anonymous）を context に入れ、4つの create ハンドラ（issue/comment/discussion/discussion_comment）が authorID に使う。ハンドラ内の anonymous 直値は残さない。
- **発行はローカルモード CLI（`issues auth token --user NAME`）**: パスワード検証を HTTP に出さないため、発行は DB 直結のサブコマンドに限定（メモどおり）。user 解決は `store.UserByName`（COLLATE NOCASE の unique index で大文字小文字無視）。

## gh ライクな CLI（issues サブコマンド群）の実装決定（2026-11-16、`devin/1794767751-gh-cli` ブランチ）

- **バイナリ構成: `cmd/issues` 別コマンド（案2）**: 初版は設計メモ推奨どおり単一バイナリ+サブコマンド（main.go が `os.Args[1]` 先回り dispatch）で実装したが、「web api を呼ぶ CLI はサーバーとは別のコマンドと定義場所であるべき（gh CLI 利用者に GitHub サーバ同梱のものを渡すようなもの）」との指摘で `cmd/issues/main.go` に分離。サーババイナリは `init` だけが残る（従来どおり）。storybook とは違い別 module にしない（依存隔離の理由がない）。
- **Backend interface は consumer 側宣言**: `cli.Backend` は store ドメイン型（`store.Issue`/`ListFilter`/`IssueInput` 等）のまま定義し、ローカルは `*store.Store` がそのまま構造的に満たし、リモートは `*client.Client` が api DTO ↔ store 型を往復変換して満たす。`web.Store`/`api.Store` と同じ「同一契約の 2 実装」パターン。
- **フラグは leaf command 単位**: flag.Parse は位置引数で止まるため `--db/--server/--token` は各 leaf の FlagSet に定義し、位置引数を挟んだ再 Parse で `issue view 1 --json title` 等どこに書いても効く形にした（gh と同じ自由度）。環境変数は `ISSUES_DB/ISSUES_SERVER/ISSUES_TOKEN`。
- **`auth token` はローカル専用**: api_tokens への INSERT は HTTP を通さず DB 直書き（パスワード検証を API に載せない設計メモ「認証」節の通り）。`--server` 指定時は usage エラーで拒否。
- **`issues api` はリモート専用・GET のみ**: パスは `/api/v1` 配下に正規化（`/issues` も `issues` も同じ）。非 2xx は body を stderr + exit 1。`--method/--field` は Phase 4 へ送った。
- **--json のキー名は api DTO 準拠**: `issue list --json id,title` と `issues api /issues` が同じ JSON 語彙を話すよう、cli 側で store 型→api DTO 変換してからフィールド選択する。
- **未実装は明示**: `discussion` は `IsSubcommand` に含めるが Run では「未実装（TODO.md 管理）」の usage エラー。discussion コマンド群と Phase 4（completion、--method/--field、NO_COLOR/TTY、token revoke/list）は TODO.md のオープン項目として残す。
- **E2E は実バイナリで**: `internal/e2etests`（`-tags=e2e`、`make test-e2e`）で `go build` → `init` → serve(`-api-token` 付き) → `/register` POST → `auth token` → `--server/--token` と `ISSUES_SERVER/ISSUES_TOKEN` の両経路で issue CRUD + `api` + author がユーザー別トークン名になることまで確認した。

## CLI discussion サブコマンドの実装決定（2026-11-16、`devin/1794769701-discussion-cmds` ブランチ）

設計メモの未決項「まず issues のみに絞るか」は「絞る」（含める）方で実装。issue 側と同じ構造の写し。

- **Backend interface を discussion 9 メソッドで拡張**: `*store.Store` がそのまま満たし、`internal/client/discussions.go` が `/api/v1/discussions*` に写す。上限超えのページング縫合は issues 側にあったロジックをジェネリックな `listWindow`（client.go）に抽出して共用 — listWindow 以外の既存動作は変更なし。
- **`--category` は slug 指定**: `?category=` URL フィルタと同じ語彙。`--label`（name → id 解決）と同じパターンで `ListDiscussionCategories` 経由で CategoryID に解決し、未知の slug は有効値を列挙した実行エラーにする。
- **`discussion answer N --comment C [--unmark]`**: qa カテゴリの answer マークを `SetCommentAnswer` に直通。API が `{"answer": bool}` を取るので unmark も同じサブコマンドのフラグで表現（`unanswer` 別コマンドは増やさない）。
- **view の comments はネスト出力**: top-level コメントの下に reply を一段深く表示し、answer コメントは `[answer]` を付記。`--comments --json` は issue 側と同じく comments をルートキーに添える。
- **init は固定カテゴリもシードする（Devin Review 指摘対応）**: 「デモデータを蒔かない」方針は維持しつつ、discussion_categories は参照データ（作成経路が他に存在しない）として `store.SeedCategories`（テーブル空のときのみ挿入、冪等）を `init` でも呼ぶ。これがないと init 直後のスキーマのみ DB でローカルモード `discussion create` が必ず失敗する。

## CLI Phase 4 仕上げの実装決定（2026-11-16、`devin/1794769881-cli-phase4` ブランチ）

- **`api` のフィールドは `-f`（文字列）と `-F`（型付き）の 2 系統**: gh api 準拠。`-F` は `null|true|false|整数|浮動小数` を JSON 型に変換し、それ以外は文字列（gh の `@file` 展開は実装しない）。GET/HEAD ではクエリパラメータ、その他では JSON body（Content-Type: application/json）。`-X/--method` は大文字正規化。
- **`--template` は Go text/template**: `--jq`（jq 言語の再実装）は持たず Go 標準のテンプレート出力とした。対象は `--json` を持つ全コマンド（issue/discussion/label の list・view、`auth token list`、`api`）。テンプレートが見るのはデコード後の JSON 語彙（`--json` と同じキー名）。`--json` とは排他（同時指定は usage エラー）。`api` ではレスポンス body を JSON デコードしてテンプレートに渡す。
- **色付けは NO_COLOR/TTY 判定**: stdout がキャラクタデバイス かつ `NO_COLOR` 未設定のときだけ ANSI 色を付ける（https://no-color.org）。対象は issue の status（open=緑/closed=赤）、discussion の `answered` マークと `[answer]` タグ（緑）に限定 — 他の出力はプレーンのまま。パイプ・リダイレクト・テストでは常に無色。
- **`auth token` の管理は `list`/`revoke` サブコマンド**: `list` はハッシュ先頭 8 文字の id + ユーザー + 発行時刻を出す（平文は永続化されていないので出しようもない）。`revoke` は位置引数の id（list が出すハッシュ prefix。曖昧ならエラー、該当なしもエラー）か、コマンドライン明示の `--token` 平文（`fs.Visit` で明示判定 — `ISSUES_TOKEN` env 既定値では誤 revoke させない。未知の平文は無害な no-op）。
- **`issues completion` は静的スクリプト**: コマンド文法が固定なので生成器は持たず bash/zsh/fish のスクリプトを定数として埋め込んだ（`issues completion bash` 等で標準出力）。
- **init は固定カテゴリもシード（PR #59 レビュー対応、前ブランチ）**: `store.SeedCategories` を公開して init から呼ぶ — 参照データ（他の作成経路なし）とデモデータを分け、init 直後のスキーマのみ DB でもローカル `discussion create` が動くようにした。

## 部分更新 append パスの stale メタ解消（2026-11-16、`devin/1794771092-partial-meta-swap` ブランチ）

- **`data-swap` プロトコル拡張**: append モードの fragment 内で `data-swap` 属性を持つノードはリストへの append ではなく同 id の既存要素を `replaceWith` で差し替える。`partial-submit.js` の append ループが先にこの印を見る。id が消えている stale な swap ノードは append せず捨てる（replace モードの self-target と同じ防御）。
- **ヘッダの "Last updated" を fragment に同梱**: コメント/返信の部分 POST は store の `updated_at` を進めるが、fragment が新 `<li>` だけだったためヘッダ行がリロードまで古いままだった。`issue_meta` / `discussion_meta` を `{{define}}` に切り出し（issue 側は新規に `id="issue-meta"` を付与、`article` 内のインライン `<p>` からの抽出）、`*_append` fragment が先頭に載せる。answer マークの replace モードが `discussion_meta` を同梱していたのと同じ発想。
- **POST 側は `GetIssue`/`GetDiscussion` で bump 後の値を取る**: status toggle の swap と同じく store の真の事後状態を描く。discussion の返信経路はこれまで discussion を再取得していなかったが、meta 同梱のために統一して取得する。
- **fake / storyStore の comment 作成も updated_at を bump**: 実 store の契約（comment = activity）に揃え、fragment の meta が実際に新しい値を返すことを `TestPartialCommentContract` で検証できるようにした。
- **moveBefore は使わない**: 既存の実機検証どおり `moveBefore` は接続済みノード専用で detach 済み fragment を拒否する（HierarchyRequestError）。swap ノードも `replaceWith`。
- **検証**: `go test ./...` + `cmd/storybook` の全テスト。実ブラウザ（playwright-cli）で実アプリの issue 詳細にコメント投稿 → URL が `/issues/1` のまま（非遷移）、`#issue-meta` の "Last updated" が新しい値に差し替わり新 `<li>` が append、コンソールエラー 0 を確認。

## 投稿成功の視覚トースト（2026-11-16、`devin/1794771371-partial-toast` ブランチ）

- **`/static/toast.js` 新設、`popover="manual"` で top layer 表示**: guide persistent-toast-notifications の MANDATORY。ライト解除なし・複数共存。Popover 非対応ブラウザでは属性が無意味なだけで、position:fixed の普通のボックスとして同じ見た目に落ちる（top layer なし）。ポリフィルは CSP `script-src 'self'` と "zero-JS 寄り" の方針から見送り。
- **スタックは 1 個ずつ個別配置**: top layer の子は祖先レイアウトを無視するので `.toast-region`（`role="region" aria-label="Notifications"`、AT が toast を安定して見つけるための器）内の各 toast を JS の `--toast-lift` で積み上げる。
- **SR 通知は ariaNotify() → announcer.js fallback**: ariaNotify() は Baseline 2026-09（Chrome 141+）。未対応では既存の polite live region に流す。toast 自身は live region にしないので二重読み上げにならない。
- **タイマー解除（8s）+ Close ボタン**: コメント投稿は低重要度の確認なので自動消去を許容（投稿物は一覧に見える＝「toast なしでも識別可能」の MANDATORY を満たす）。ただし toast 内にフォーカスがある間は消さず、focusout 後に再アーム。Close は autofocus しない（トーストはフォーカス遷移と競合してはいけない）。フォーカス中の toast を手動で閉じたら次の toast の Close、なければ toast 出現前の要素へフォーカスを返す。
- **入退場アニメーション**: `@starting-style` + `transition-behavior: allow-discrete`（shorthand に入れず別プロパティ宣言 — guide の DO）。display/overlay も transition 対象にして top layer 退場を拾う。`prefers-reduced-motion` で全無効。
- **partial-submit.js の成功 announce() を toast() に置換**: 422 差し替え・エラー経路は従来どおり（role="alert" インライン表示 + フォーカス遷移 — toast を出さない、guide の DON'T）。
- **検証**: `go test ./...`（`TestStaticToastJS` 追加 — module import なので `<script>` タグ検査では漏れる）。実ブラウザでコメント投稿 → 右下に "Comment posted. [Close]" が popover-open で出る、Close で消える、8s 自動消去、コンソールエラー 0 を確認。

## 部分更新の scoped view transition（2026-11-16、`devin/1794771565-partial-vt` ブランチ）

- **`element.startViewTransition()`（scoped）で swap を包む**: guide scoped-component-transitions の MANDATORY — document スコープだとページ全体のポインタイベントを止め、top layer の toast/popover を隠す。スコープは「影響を受ける全ノードの最小共通祖先」を動的に計算（`commonScope`）：append は挿入リスト＋ `data-swap` メタ行、replace は全 swap 対象、422 はフォームの親（フォーム自身は差し替えられるのでスコープにできない）。
- **feature detect は `Element.prototype.startViewTransition`**: document 版ではなく element 版の有無で判定（Chrome 147+）。非対応・`prefers-reduced-motion` はコールバック即実行の素の swap にフォールバック。`document.startViewTransition` への fallback は guide 非推奨（top layer を隠す・並行不可）なので採用しない。
- **`transition.ready.catch(() => {})`**: 同一スコープ上の連続 submit は遷移をスキップし ready を reject するので握りつぶす（guide DO）。DOM 更新自体は必ず走る。
- **子要素に view-transition-name を付けない**: `match-element` を各 `<li>` に付けると cross-document ナビ遷移にも per-item morph が漏れる。スコープ要素は UA により `view-transition-name: root` が付き自身が group として参加するので、コンポーネント単位の cross-fade は名前なしで得られる。
- **reduced-motion の CSS 保険**: `::view-transition-group(*)/old(*)/new(*) { animation: none !important }` を `@media (prefers-reduced-motion: reduce)` に追加（guide MANDATORY）。JS 側でもスキップする二重化。
- **検証**: Chromium 156 で `Element.prototype.startViewTransition` を計装して append（コメント投稿）・replace（status toggle）・422（空投稿）の 3 経路すべてで scoped VT が 1 回走り、DOM 差し替え・toast・エラー表示が正常、コンソールエラー 0 を確認。

## `<details>` 開閉アニメーション（2026-11-16、`devin/1794771977-details-anim` ブランチ）

- **`interpolate-size: allow-keywords` を :root に opt-in**: guide animate-to-intrinsic-sizes の MANDATORY。`block-size: 0 ↔ auto` の遷移を有効化する。対象は現状 `.reply-details`（discussion の返信フォーム）のみ。
- **`::details-content` 疑似要素に transition**: `<summary>` 以外の内容を包むブラウザ生成ラッパー。`block-size: 0; overflow: clip` + `transition: block-size, content-visibility` — content-visibility を遷移対象に入れないと閉じる途中で内容が先に消えて高さだけが動く。`transition-behavior: allow-discrete` は shorthand に入れず別プロパティ宣言（guide DO）。
- **非対応はルールごと落ちるだけ**: `::details-content` / `interpolate-size` 非対応ブラウザ（Firefox・Safari 現行）はセレクタ/プロパティを無視して従来どおり瞬時表示 — 別の fallback は書かない（guide の graceful 方針どおり）。`prefers-reduced-motion` で `transition: none`。
- **検証**: Chromium 156 で `details.open` 切替時に `offsetHeight` を rAF サンプリング → 195px→20px が ~15 フレームかけて滑らかに縮むことを確認（瞬時ではない）。

## タブの移動アンダーライン（2026-11-16、`devin/1794772097-tab-anchor-underline` ブランチ）

- **`.tabs::before` + anchor positioning**: guide anchor-positioning-tab-underline の推奨パターン — `.tab-active` に `anchor-name: --active-tab`、疑似要素に `position-anchor` + `anchor(left/right/bottom)` でインジケータを係留。装飾用の DOM 追加なし。
- **MPA ゆえ遷移は cross-document VT 側に載せる**: タブは通常リンク → ページ遷移。guide の「ページ遷移では動かない」制約は、疑似要素に `view-transition-name: tab-underline` を付けて既存の `@view-transition navigation: auto` に乗せることで解消 — 旧ページの下線が新ページの位置へ morph する（TODO の「view-transition 中の要素追従に注意」の対応）。同一ドキュメント内の scoped VT（partial-submit）とは無関係なので衝突なし。
- **fallback は既存の `.tab-active` border-bottom**: `@supports not (position-anchor)` 側は従来のスタイルのまま。対応ブラウザでは `.tab-active` の border を透明化して ::before に一本化（二本下線を防ぐ）。ARIA 側は既存の `aria-current="page"` を維持（guide MANDATORY、変更不要）。
- **検証**: Chromium 156 で `anchorName`/`view-transition-name`/`inset-*` の computed を確認、Open→Closed 遷移で下線が新タブ位置に配置されることをスクリーンショット確認。

## ナビゲーション方向に応じた遷移（2026-11-16、`devin/1794773401-directional-nav-vt` ブランチ）

- **一覧↔詳細を forward/backward に分類**: `pagination-transition.js` を拡張 — 同一路径の `?page=N` は従来どおりページ番号比較、パスが違う場合は `/issues|discussions(/<id>)?` で「list→detail=forward / detail→list=backward」。その他の遷移（タブ・フィルタ・セクション跨ぎ）はタグなし＝従来の cross-fade 維持（modern-web-guidance directional-navigation-transitions）。
- **タグ付けは `pageswap` と `pagereveal` 両方**: `pageswap` は旧ページ側で `event.activation.from/entry.url` を比較、`pagereveal` は新ページ側で `navigation.activation` を比較。Chrome 156 実測で pageswap 側に add した type が pagereveal の `vt.types` に引き継がれないため、`:active-view-transition-type()` が確実に効くよう新ページ側でも add する。direction() は同一路径かどうかで分岐するため、パンくず戻り・ページ送りどちらの方向でも同一結果になる。
- **`pagereveal` が `viewTransition=null` だった不具合の解消（本 PR の一番の発見）**: Chrome は opt-in（`@view-transition { navigation: auto }`）の検出を「新ページの pagereveal 直前」に行うが、長大な外部 stylesheet の末尾にあった opt-in が検出に間に合わず遷移がスキップされていた（vtbag.dev の既知の落とし穴）。対策として `@view-transition` だけを `layout.html` `<head>` の **inline `<style>`** に移動し一本化（app.css 側は `::view-transition-*` アニメーション群のみ残し、遷移開始までには確実に読み込まれている）。
- **inline `<style>` 解禁のため CSP に `style-src-elem` を追加**: 既存方針（`style-src 'self'` + 属性だけ `style-src-attr 'unsafe-inline'`、`<style>` 要素は遮断）を維持するため `style-src-elem 'self' 'unsafe-inline'` を追加 — 外部 stylesheet の `self` に加えて `<style>` 要素のみ許可。`unsafe-inline` が必要なのは opt-in 要素だけでなく、Chrome が遷移の capture スタイルを実行時に `<style>` 要素として挿入するため（`style-src 'self'` 単独では `Applying inline style violates ... style-src 'self'` で遷移自体が abort される — 実測）。
- **スクリプトは `<head>` の同期 script に移動**: `defer`/`module` では遷移保留中の pagereveal より後に実行されることがありリスナー未登録のままパリティを取りこぼす（実測で `pagereveal` 未捕捉を確認）。`<head>` の通常 `<script src>` なら解析中に必ず登録される。全ページで load しても副作用は pagereveal/pageswap への no-op リスナーのみ。
- **speculation rules との関係**: 一覧の `prefetch` は再現中の一因と誤認して一時無効化して試したが、inline opt-in 後は prefetch 有りでも `vt.types` 付きで遷移が走ることを確認し、`Speculation-Rules` ヘッダーは維持（両立する）。
- **検証**: Chromium 156 headed で一覧→詳細 `pv:forward` / 詳細→一覧 `pv:backward` の type 付与と `:active-view-transition-type(forward|backward)` 一致を確認、アニメーション 8s 化の一時改修でスライド中の両ページ同居をスクリーンショット確認（前進: 旧リスト左→新詳細右から / 後退: 逆）。最終形は 0.22s ＋計装なしに戻してあり console エラー 0。

## discussion_comments を threads/comments 2 テーブルへ分割（2026-11-16、discussion-thread-tables ブランチ）

- **GitHub Discussions のモデルに合わせて `discussions` 1—N `discussion_threads` 1—N `discussion_comments` に変更**: 従来は `discussion_comments.parent_id` の self-FK で「top-level=スレッド先頭」を暗黙に表していたが、thread を明示した `discussion_threads` テーブルを新設。スレッド自体が開始投稿（body/author/is_answer を持つ）で、`discussion_comments` は返信のみ `thread_id` でぶら下げる。`ErrInvalidParent` → `ErrInvalidThread`、`SetCommentAnswer` → `SetThreadAnswer`。
- **migrateV5 はデータを保持して移行**: `parent_id IS NULL` の行を id そのまま `discussion_threads` にコピー、コメント表は `discussion_comments_new`（`thread_id` 列）を作って `parent_id`→`thread_id` に写し替え DROP/RENAME。孤立した返信（親のない行）は捨てられる — ここまでは integrity 破損した行だけ。is_answer 部分 UNIQUE index も `discussion_threads` 側へ移動。
- **ID 空間が分かれた副作用**: 返信の id と同じ値の thread id が別 discussion 内に存在し得るので「返信への返信」を ID だけで誤検出/拒否できない。API 面ではスレッド id しか渡せない経路だけ残すので問題にならない。
- **API**: `GET /discussions/{id}` は `"threads"` を返し、新規に `POST .../threads`、`POST .../threads/{tid}/comments`、`POST .../threads/{tid}/answer`。HTML ルートは据え置き（`POST /discussions/{id}/comments`、同 `{cid}/answer` で cid=thread id）だが hidden フィールドは `parent_id` → `thread_id`。CLI も `--parent`/`--comment` → `--thread`。
- **検証**: `go test ./...`（store に `TestMigrationV5` で v4→v5 の id 保持・返信付け替え・is_answer 継承を確認済み）、`go vet`、`gofmt` 全パス。

## 返信フォームをスレッド末尾の常時表示に（2026-11-16、`devin/reply-form-bottom` ブランチ）

- **`<details>` 畳み込みを廃止**: 従来は各スレッドに `<details class="reply-details"><summary>Reply</summary>` で返信フォームを畳んでいたが、新規コメントの入口が「Reply」表記＋コメント群の上部という形で分かりにくかった。返信群の直下に常時表示のフォームを置き、ラベルは "Add a comment"（送信ボタンは "Comment"）。
- **`<details>` 関連 CSS を全削除**: `.reply-details` summary スタイルと `::details-content` アニメーション（`:root { interpolate-size: allow-keywords }` 含む）は対象要素が消えたので丸ごと撤去。`.reply-form` に `margin-top`/`border-radius` を追加してスレッドカード内で返信群と同じ inset 見た目に揃えた。
- **`ThreadDraftID` は残す**: reopen する `<details>` は無くなったが、失敗した返信のドラフトとエラー表示を「そのスレッドのフォーム」に戻すために引き続き使う。
- **"Comments (N)" 見出しも削除**: スレッド＋返信の合計カウントは「何を数えているか」が分かりにくく、GitHub Discussions にも見出し自体がない。`discussionDetailPage.CommentCount` と `countDiscussionComments` を撤去（issues 側の `data-comment-count` は残す）。
- **スレッドのヘッダは `<name>` のみ、返信は "commented"**: 「スレッドを立てた投稿」に動詞を付けるのは不自然なのでスレッドは投稿者名だけ。返信は GitHub と同じく "commented" に統一（"replied" 表記は廃止）。
- **accessible name は従来パターンで一意化**: textarea は `aria-label="Add a comment on thread N"`（可視ラベル "Add a comment" はその部分文字列 = WCAG 2.5.3 を満たす）、ボタンは `aria-label="Post comment on thread N"`。
- **検証**: `go test ./...`、`go vet`、`gofmt` 全パス。実画面で返信フォームが各スレッド末尾に常時表示されることを確認。

## 返信フォームを折り畳みインプットに（2026-11-16、`devin/reply-accordion` ブランチ）

- **背景**: 常時表示の返信フォームは最初から強調され過ぎている（GitHub の "Write a reply" と同じ問題意識）。初期状態では畳み、クリックで開く形に戻す — ただし前回の「Reply」トグル表記はやめ、GitHub と同じく畳まれた1行インプット風にする。
- **no-JS で実現**: `<details class="reply-details"><summary class="reply-collapsed">Add a comment</summary>` を返信群の直下に置き、summary を枠線つき単一行（`list-style: none` + `::marker`/`::-webkit-details-marker` 消去 + `cursor: text` + muted 色）に整形して「クリックすると開く入力欄」に見せる。`[open] > summary { display:none }` で開いた後は summary を隠し、本物のフォームが入れ替わる。開閉自体は JavaScript 不要（フォーカス移行だけ下記の小さな JS 拡張を使う）。
- **ヒント行は開いたフォーム内に残す**: "Markdown (GFM) supported. Ctrl+Enter …" は畳まれた状態では見えず、開いた状態では見える — この両方を満たすのでテキスト自体は削除しない。
- **送信ボタンは右寄せ**: Comment / Start thread の両フォームのボタンを `.form-actions { display:flex; justify-content:flex-end }` で右寄せ。
- **`ThreadDraftID` が再び活きる**: `<details>` が復活したので、失敗した返信は `open` 属性で当該スレッドのフォームを開いた状態で再描画する（ドラフト＋エラー表示を保持）。
- **底部の "Start a thread" フォームは畳まない**: GitHub の Write ボックスに相当する新規スレッド作成は常時表示のまま（ボタン右寄せのみ適用）。
- **開いたとき textarea にフォーカスを移す**: `[open] > summary` は `display:none` で消えるため、クリック後フォーカスが隠れた summary（→body）に残り、もう一回クリックが必要になる。`partial-submit.js` に `toggle`（capture）リスナーを追加し、`.reply-details` が開いたら内部 textarea を `focus()` する — no-JS 時も開閉自体は動く（プログレッシブ強化）。
- **検証**: `go test ./...`、`go vet`、`gofmt` 全パス。実画面で折り畳み表示・開閉・エラー時の open 復元を確認。

## 返信フォームに Cancel 収納ボタン（2026-11-16、`devin/reply-cancel` ブランチ）

- **背景**: GitHub の reply box のように、一度開いたコメント入力を Cancel で畳み戻せるようにする。`<details>` を内部から畳む HTML 原生手段は存在しないため、フォーム内に `<button type="button" data-reply-cancel>Cancel</button>` を置き、`partial-submit.js` の委譲クリックリスナーが `details.open=false` にして summary（折り畳みバー）にフォーカスを戻す。無 JS 時はボタンが無反応でフォームは開いたまま — 従来動作と同じで機能損失なし。
- **右寄せの適用範囲を修正**: 前 PR で `.form-actions { justify-content:flex-end }` をグローバルに足しており、ログイン・削除確認・issue/discussion 編集フォームなど共有 `.form-actions` のボタンも右寄せになっていた。`.comment-form .form-actions` に限定して他画面へ影響しないように戻す。
- **検証**: `go test ./internal/web/`、`go vet`、`gofmt` 全パス。実画面で Cancel クリック→折り畳みバーに戻ることを確認。

## UI ルールの集約と監査由来の確定事項（2026-11-16、`devin/1794805434-ui-rules-doc` ブランチ）

外部デザインシステムチェックリスト（全41項目）との監査を実施し、結果を新設の `docs/ja/ui-rules.md` に「正 / 正（代案）/ 変更予定 / N/A」で集約した。監査中にユーザーへ確認して確定した方針:

- **日時は絶対時刻（`fmtTime`）のみ**: 相対時刻表示とホバーツールチップは採用しない。
- **返信フォームの排他制御は設けない**: 複数スレッドの返信欄を同時に開いたままにしてよい（空の開いた欄を自動で閉じる挙動は入れない）。
- **余白は偶数 px 基準 + 近接の原則**: 4 の倍数グリッドへの丸めは採用しない。
- **ボタン配置は「左:キャンセル / 右:確定・右寄せ」を正とする**: dialog と返信フォームは準拠済み。未適用の作成/編集フォーム・GET 削除確認ページは TODO で追従。
- **グローバルヘッダの New ボタンは枠線化**: `btn-primary` を外してページ内 Primary を1つにする（TODO で対応）。
- **リンクの外観ルールは「本文テキストと見分けがつくこと」まで**: 古典的 web リンク（青・紫・常時下線）の強制は採用しない。現行の `--accent` 色・ボタン/バッジ/tab 化した `<a>` は許容。判別不能な `.comment-permalink`（本文色・無下線）は TODO で修正。

## UI ルール監査の「変更予定」一括対応（2026-11-16、`devin/1794806699-ui-rule-audit-fixes` ブランチ）

ui-rules.md の監査 TODO 全7件を実装。個別の判断:

- **`.form-actions` 自体を `flex-end` に**: 「左:キャンセル/右:確定・右寄せ」が全フォームの規約なので、`.comment-form`/`.confirm-dialog` の個別 flex-end 指定を撤去してベースに一本化。あわせてフォーム/削除確認ページの DOM 順を Cancel→確定 に入れ替え。例外（左寄せが正しい場所）は存在しない。
- **ヘッダ New ボタンは `btn`（枠線）に**: `btn-primary` を外し、ページ内 Primary（空状態 CTA やフォーム送信）との重複を解消。
- **空欄 disabled は GitHub 式の採用判断**: modern-web-guidance `forms` の「バリデーションを塞ぐための submit 無効化は不可」とは別物（空投稿は 422 しか返らず表示すべきエラーもない）。`comment-submit-disabled.js` は delegated `input` + MutationObserver でスワップ投入フォームにも対応し、`syncCommentSubmit` を partial-submit.js が import して二重送信防止の再活性化と競合しないよう最終状態で再評価する。
- **返信投稿後の畳み**: append 成功パスで `details.open=false`＋summary へフォーカス（Cancel ボタンと同じ着地）。トップレベルフォームは従来どおり textarea にフォーカスを残す。
- **離脱ガード（`leave-guard.js`）**: フィールド値を描画時値と比較して dirty 判定（編集フォームが「最初から dirty」にならないよう値比較式）。`reset` イベント（部分投稿成功時のみ発火）で microtask 遅延に再ベースラインして解除。`beforeunload` の文言はブラウザ固定。auth フォームは対象外。
- **モバイル残件**: `viewport-fit=cover` を付けて `env(safe-area-inset-*)` が効くようにしたうえで toast/back-to-top に加算。装飾 `:hover` を全て `@media (hover: hover)` ガードへ（`:is(:hover,:focus-visible)` は分離して focus 側を維持）。狭画面は back-to-top を左下に移動し `.main-content` の下端余白を拡大して送信ボタンがスクロールで固定要素帯から外れるようにした。
- **permalink は常時下線**: muted 色のまま `text-decoration-color: var(--text-muted)` の下線で「押せる」と判別できる形に。
- **検証**: `go test ./...`、`go vet`、`gofmt`、storybook（新規 StoryDiscussionDetailReplyOpen / StoryDiscussionDetailLong / StoryIssueDetailEmpty 含む全ストーリー描画）全パス。JS 挙動（disabled 切替・details 畳み・beforeunload）は実サーバ + Playwright で確認。

## クライアント JS のライフサイクル統合（2026-11-16、`devin/dom-hooks-js-consolidation` ブランチ）

監査対応で増えたクライアント JS（計10ファイル約930行）の「散在」を整理するリファクタリング。議論の結論: 痛いのはファイル数ではなく「fragment 差し替えに各ファイルが独自に追従する」構造だった（422 swap で dirty 残留・checkbox 未追跡の2バグはこれが原因）。vdom 系は不採用 — DOM の所有権はサーバ（X-Partial fragment swap）にあり、クライアント reconcile は宣言的 SSR と衝突するため。

- **`dom-hooks.js`（新規）**: 共有 MutationObserver 1本 + `bind(selector, {init, teardown})` の attach/detach 契約。既存要素には即時 init、注入要素は addedNodes で init、removedNodes で teardown。`seen` は WeakSet で差し替えられた要素を自然に捨てる。
- **leave-guard.js**: 自前の MutationObserver と初回スナップショットループを廃止し `bind("form", {init: snapshot, teardown: unmark})` に一本化。dirtyCount のリークは契約上起こり得ない構造になった。
- **comment-submit-disabled.js**: 同様に `bind("form.comment-form", {init: syncCommentSubmit})`。無状態なので teardown は不要。
- **form-errors.js**: 初回の全フィールド `syncAria` を bind の init に置き換え、スワップ投入されたフィールド（422 再描画）にも aria-invalid="false" の初期状態が付くようにした。
- **partial-submit.js は無変更**: delegated submit/click/toggle リスナーで注入フォームをカバーできており、要素 attach 時の作業を持たないため。
- **ページ種別エントリ1本化**: detail ページの script タグ 6本を `page-detail.js` 1本に、form ページの 3本を `page-form.js` 1本に集約。テンプレ側で振る舞いの組み合わせが drift しない。各振る舞いファイルは残り、ui-rules.md のファイル名参照はそのまま有効。
- **検証**: `go test ./...`、`go vet`、`gofmt` 全パス。

## ライブ更新（SSE）の実装（2026-11-16、`devin/*-live-updates-impl` ブランチ）

`docs/sketch/ja/live-updates.md` の実装。設計どおり SSE + in-process hub + store 層 emitter + invalidation-only イベント。実装時に決めた細部:

- **`internal/events` は契約だけを持つ**: `Change`/`Hub` を置き、store が import する一方向依存。store 側は `SetListener` で emit 関数の注入を受けるので store→web の依存は発生しない。`New(st, opts...)` の呼び出し（storybook・テスト）が増えるのを避けるため hub は `WithHub` Option で注入し、未指定時は静かな private hub（endpoints は立つが emit されない）。
- **イベントは全種類 emit する**: 設計では Phase 1 を comment/thread/reply に絞っていたが、status/answer/edit/delete も同じ仕組みで済むため揃えた。client は status/edit→`/fragments/article`、answer→`threads?ids=`、delete→恒常 notice、resync→全リスト再構築を実装済み。一覧ページ（`issues`/`discussions` topic）への emit も済んでおり、list 側クライアントは未実装のまま将来対応とする。
- **answer イベントは Parent に前の answer thread を持つ**: `SetThreadAnswer` が tx 内でクリア前に前の marked thread id を読み、クライアントは2枚の `<li>` を1回の fragment GET で差し替える。
- **hub のバッファ溢れは resync に畳む**: 購読者が詰まったとき writer を止めない代わりに backlog を捨て `resync` マーカー1件を残す（設計メモの「取りこぼしは resync で回復」の具体化）。購読バッファは32。
- **reply は thread 全体ではなく reply list だけ差し替える**: `GET /discussions/{id}/fragments/replies?thread=T(&ids=)` — 開いている返信フォームを守る。answer/resync で thread `<li>` 自体が差し替わるときは、開いている `details.reply-details` と textarea 値を client が退避・復元する。
- **WriteTimeout は per-response で解除**: `http.NewResponseController(w).SetWriteDeadline(time.Time{})` — サーバ全体の 15s 制限は通常レスポンスにはそのまま残す（設計メモの落とし穴1）。
- **`fragments.js` 新設**: `parseFragment`/`runScopedTransition`/`commonScope` を partial-submit.js から抽出し、live-updates.js と共有 — fragment の解釈（`<template>` parse・scoped transition・`data-swap` 自己対象化）を drift させない。
- **検証**: `go test ./...`・`go vet`・`gofmt` 全パスに加え、実サーバ + playwright で issue comment・discussion thread・reply の3パスを実ブラウザ確認（他クライアントの投稿が `.comment-new` ハイライト付きで live append される）。

## storybook index/grid の UI 再設計（2026-11-16、`devin/*-storybook-ui` ブランチ）

podhmo からの改善要望（grid と story のリンク誤押・見た目・story を探す手間）に対する chrome の作り直し。`cmd/storybook` は upstream 例の port だが chrome はすでに独自拡張済みのため、このリポジトリ側で改修した。

- **選択モデルを「プレビュー領域に何を出すか」に統一**: story は `?s=`、group の variants grid は `?g=`。grid pill を押しても index のナビは残り、iframe 内に grid が開く。`/grid/<name>` ページ自体は全幅比較用・共有リンク用に残す。
- **grid 導線と story リンクの視覚的分離**: グループ名はリンクをやめて sticky なセクション見出し化（件数バッジ付き）。grid への入口は形の違う `⊞ grid` ピルボタンのみ — 一覧内で story 行と押し間違えないように、というのが目的。
- **`?q=` フィルタ**: name/doc/group の小文字 haystack へのトークン AND 部分一致。JS では input 毎にライブ絞り込み（`?q` は replaceState）、JS なしでは `<search>` の GET form でサーバ側フィルタにフォールバック。`/` でフォーカス、Esc でクリア、Enter で先頭候補を開く、↑↓ で候補間移動。
- **プレビューツールバー**: iframe 上のバーに group / story 名 / `story_pages.go:行` / `open ↗`（raw を新規タブ）/ `⊞ grid` / 幅切替を集約。viewport 切替はナビからここへ移動 — 幅はプレビューの属性なので。
- **story 選択はページリロードしない**: `data-src`（preview 用 URL）を各行に持たせ、クリックは iframe.src 差替え + active 付け替え + pushState。フィルタ語・ナビのスクロール位置が維持される（スクロール位置は sessionStorage にも退避）。JS が無くても `?s`/`?g`/`?w`/`?q` のサーバレンダリングで全機能が動く。
- **export も同じ UX**: 静的 index.html ではサーバがいない代わりに、同じ inline script が URLSearchParams から `?s/?g/?w/?q` を読んで選択を適用する。`data-src` が serve（`/story/X`）と export（`X.html`）で差を吸収する。
- **story ページへの chrome 注入はしない**: プレビュー中の story 名・アクションは index 側ツールバーに置く方針 — story ドキュメントを汚すとスクリーンショット用途（PR 添付・ドキュメント用スナップショット）に chrome が写り込むため。export の story html はバイト単位で素のまま。
- **検証**: `cd cmd/storybook && go test ./...` パス（index 構造・`?g` プレビュー・`?q` フィルタのサーバ側検査を追加）。served（:6006）と静的 export（python http.server）の両方で Playwright により story/grid 切替・フィルタ・キーボード・幅切替・popstate を実ブラウザ確認。

## workspace の実装（2026-11-17、`devin/*-workspaces-impl` ブランチ）

`docs/sketch/ja/workspaces.md`（PR #85）の実装。issues/discussions は必ず workspace（GitHub リポジトリ相当）に所属する。設計からの変更点・決め事を記す。

- **非 GET の workspace 不一致は 404**: スケッチでは「`/w/{別ws}/issues/{id}` への POST は別 ws へ 303/302 で誘導」案だったが、PUT/PATCH/DELETE を勝手に別 workspace に再送するのは危険（別 workspace 上で mutation を再生してしまう）。GET/HEAD は canonical パスへ 302、それ以外のメソッドは素直に 404 に倒した。
- **既存 URL 保持は 307/302 リダイレクト**: `/issues`, `/issues/{id}` 等は方法ごとに登録して canonical `/w/{slug}/...` へ。`GET`/`HEAD` → 302、それ以外 → 307 でメソッド・body 維持。これらの legacy ルートは「メソッド付き」で登録する必要がある — メソッドなし `/issues/{rest...}` は catch-all `GET /` とコンフリクトする（ServeMux の「より一般的なパスでより少ないメソッド」判定）。GET/POST/PUT/PATCH/DELETE を列挙。
- **CLI 側の `--workspace` はバックエンド組み立て時に注入**: `cli.Backend` の既存メソッドシグネチャは変えず、`store.In(ctx, slug)` が返す `ScopedStore` / `client.Client.In(slug)`（URL に `/w/{slug}` を埋める薄いラッパ）でスコープを作る。env は `ISSUES_WORKSPACE`、既定 `"default"`。
- **migration v6 は親テーブルの再構築なので FK 一時停止が要る**: `DROP TABLE issues/discussions` は暗黙 DELETE で子（comments, issue_labels, discussion_threads, discussion_comments）の `ON DELETE CASCADE` が発火し中身を消す。`PRAGMA foreign_keys=OFF` はトランザクション内では効かないため、専用 conn 上で OFF → tx → `pragma_foreign_key_check` で検証 → ON、という `migrateWithFKOff` を `migrate()` から版指定で呼ぶ（`rebuildNeedsFKOff`）。
- **workspace 管理の web ルートは2階層**: `GET|POST /workspaces` + `GET /workspaces/new`（管理一覧・作成）は workspace 非依存、`GET|POST /w/{slug}/delete`（削除）は `scoped()` 経由で対象 ws を解決。`{ws}` は「削除される側」。
- **そのまま採用した設計**: 採番はグローバル維持（issue #N は全 workspace で一意）、`workspaces(slug UNIQUE)` + `issues/discussions.workspace_id NOT NULL ON DELETE CASCADE`、`EnsureDefaultWorkspace` は「workspaces が空のときだけ default を INSERT」（init/起動時の冪等な保証）、最後の1件は `ErrLastWorkspace` → 422、label/category/user はグローバルのまま、ws 間移動・per-ws 採番・per-ws label は future work。
- **検証**: `go test ./...` / `go vet` / `gofmt` 全パス + 実サーバ手動確認（`/`→`/w/default/issues` 302、`/issues` legacy 302、ws 作成→削除→最後の1件拒否 422、`/w/beta/issues/1` で他 ws の issue に GET→canonical 302、`issues workspace list|create|delete` と `--workspace` スコープの CLI、旧 `/api/v1/issues` は 404）。

## migration の squash（2026-11-17、`devin/*-squash-migrations` ブランチ）

リリース前で DB は捨ててよいので、v1〜v6 の累積 migration を1本に潰し、`schemaDDL` を現行スキーマ（workspaces 含む全テーブル）そのものにした。`init` は一発で現行状態を作る。

- `migrations` / `PRAGMA user_version` の仕組み自体は残す（将来リリース後に versioned migration が必要になったらここに追加していく）。今は `migrateV1` 1本で `schemaVersion = 1`。
- 'default' workspace の作成は DDL 内の `INSERT ... WHERE NOT EXISTS`（init 時の1件保証）+ `EnsureDefaultWorkspace`（空になった場合の再作成）の二段で担保。
- v6 のテーブル再構築（issues_new/discussions_new + `migrateWithFKOff`）は旧 DB のデータ移行のためのものだったので不要になり削除。DROP TABLE 時の ON DELETE CASCADE 問題自体は pitfalls.md の知見として残す。
- **旧フォーマットの DB は fail fast**: 事前に issues テーブルが存在する DB では `CREATE TABLE IF NOT EXISTS` が触らないためカラム欠落のまま動いてしまう。migrateV1 後に `workspace_id` カラムを検査し、なければ「DB を消して init し直せ」というエラーにする。既存の dev DB は削除対象。
- **検証**: `go test ./...` / `go vet` / `gofmt` 全パス。アップグレード経路を検証していた TestMigrationFromVersionZero / TestMigrationV5 は経路ごと消えたため削除し、新規スキーマ検査は workspace/discussion_threads 系カラムを含む形に更新。

## ローカル CLI からの書き込みを SSE に届ける（2026-11-17、`devin/*-cli-local-sse-changes` ブランチ）

`issues` CLI の local mode（`--db` 直結）はサーバーとは**別プロセス**で同じ SQLite を開くため、`SetListener → hub.Publish` の in-process 通知では拾えなかった — web/API 経由の書き込みだけが SSE に流れていた。`docs/sketch/ja/live-updates.md` が「複数プロセスが同一 DB を共有したら」として挙げていた逃げ道まさにそのケースなので、案 (a) の **changes テーブル + poll** に切り替えた。

- **`changes` テーブルが唯一の通知経路**: 全 mutator が mutation と**同じトランザクション**内に hint 行を `INSERT`（`commitTx` が `tx.Commit()` の代わり — 記録忘れを構造的に防ぐ）。hint と本体は必ず同時にコミット/ロールバックされる。
- **`SetListener`/`notify` は撤去**: in-proc 速報経路とテーブル経路を二重に持つと重複 publish のための origin 管理が要る。ポーリング一本化で +≤250ms の遅延になるが、クライアント側の 300ms debounce の中に収まる。
- **サーバーは `WatchChanges` で tail**: main.go が `go st.WatchChanges(ctx, watermark, hub.Publish)` を起動（250ms 間隔）。別プロセスの書き込み（local CLI、もう1台のサーバー）も同じ経路で SSE に届く。起動時は `MAX(seq)` から開始してバックログを再生しない（hint は揮発、取りこぼしはクライアント resync で回復）。
- **watermark は呼び出し側が同期的に採取**: `WatchChanges(ctx, after, fn)` は起点 seq を引数で受け取り、main.go が `LatestChangeSeq` を goroutine 起動前に読む。goroutine 内でサンプルすると、起動〜初回読みの間にコミットされた書き込みが抜ける（review 指摘）。
- **既存 v1 DB 向けに `migrateV2` を追加**: `changes` テーブルの定義は `changesDDL` const に切り出し、`schemaDDL`（新規 DB 用、migrateV1 経由）と `migrateV2` の両方から実行。squash 済みの `user_version=1` DB は migrateV1 を再実行しないため、テーブル追加は versioned migration 側に置かないと既存 DB で全 `commitTx` が失敗していた（review 指摘）。
- **保持は5分**: `PurgeChanges` が `created_at` の古い行を捨てる（watcher ループ内で1分に1度）。長期停止後の再起動ではバックログは読まないため消費未済の古行は意味を持たない。
- **tx を持たなかった3 mutator**（`SetIssueStatus`/`DeleteIssue`/`DeleteDiscussion`）は `BeginTx` で包んで `commitTx` に揃えた。
- **検証**: `go test ./...` / `go vet` / `gofmt` 全パス。`changes_test.go` は同一ファイルを開く2本の `*Store` で CLI↔server 分割を再現して配達を確認し、`TestMigrateV1DatabaseGainsChangesTable` が v1 DB への migrateV2 を再現。e2e（`make test-e2e`）は実バイナリで SSE 購読 → local-mode `comment create --db` → `event: comment` 受信を確認。ついでに `discussion comment`（thread 化で出力が変わっていた）と `api` パス（workspace スコープ化で `/w/{slug}/` 必須になっていた）の stale な e2e 期待値も修正した。

## バイナリ・コマンド名の改称（2026-11-17、`devin/*-rename-zatudan-zatu` ブランチ）

`docs/sketch/ja/naming.md`（PR #90）の調査をもとに命名を決定。サーバ/app は **`zatudan`**、CLI は **`zatu`**、env prefix は **`ZATUDAN_*`**。開発中のため breaking changes は移行措置なしでそのまま付け替え。

- **サーバの entrypoint は `cmd/zatudan` のみ**: naming.md の案 S2。`go build` のバイナリ名が import path 末端由来のため、`-o` 運用ではなく cmd/ 移設で名前を確定させる。当初は `go run .` 維持のため root main.go → internal/app への委譲構造にしたが、「動かなくて良い・委譲も不要」との判断で廃止し、bootstrap は `cmd/zatudan/main.go` に直接置く（dev 時は `go run ./cmd/zatudan`）。
- **CLI は `cmd/issues` → `cmd/zatu`**: `git mv` で移動し usage 文字列・doc comment・completion スクリプト（`_zatu`, `zatu.fish`）・エラーメッセージを一式更新。
- **env は app 名に揃える**: `ISSUES_*` → `ZATUDAN_*`（server: `ZATUDAN_ADDR`/`ZATUDAN_DB`/`ZATUDAN_API_TOKEN`、CLI: `ZATUDAN_DB`/`ZATUDAN_SERVER`/`ZATUDAN_TOKEN`/`ZATUDAN_WORKSPACE`）。CLI 短名 `zatu` ではなく app 名 prefix を選んだ — env は「アプリの設定」でありコマンドごとの修飾ではないため。
- **既定 DB 名は `zatudan.db`**（旧 `issues.db`）。旧 DB は移行対象にしない。
- **変えないもの**: `issues` は引き続きドメイン語 — URL パス（`/w/{slug}/issues` 等）、`/api/v1`、JSON フィールド名、テーブル名、CLI サブコマンド語彙（`zatu issue list` 等）は据え置き。リポジトリ名・module path も意図どおり据え置き。
- **検証**: `go test ./...` / `go vet` / `gofmt` 全パス。`make test-e2e`（実バイナリ init→serve→local/remote CLI）パス。`go build ./cmd/zatudan`・`./cmd/zatu` のバイナリ名確認。

## ラベルを定数から DB 管理へ（2026-11-17、`devin/*-labels-db-managed` ブランチ）

ラベル選択肢はこれまで `seedLabels` の定数データを `SeedDemoData` で投入するだけで、追加・削除の術がなかった。ユーザー要望により「issue/discussion で別種」「ワークスペース毎に設定可能（`workspace_id IS NULL` はマスターデータ）」「削除は可視性の変更」という3点を含めて DB 管理化した。

- **スキーマ（migrateV3）**: `labels` に `workspace_id`（NULL=マスター、全ワークスペースで可視）・`kind`（`'issue'|'discussion'`、picker を分離するため同名を用途別に持てる）・`archived` を追加。一意制約は `UNIQUE(name)` から式インデックス `idx_labels_kind_name_ws ON (kind, name, COALESCE(workspace_id, 0))` へ — マスターとワークスペースで同名ラベルを共存させる union 方式のため。`discussion_labels` join テーブルを新設。
- **既定ラベルはデータマイグレーション**: `defaultLabelsDDL` を `schemaDDL`（新規 DB）と `migrateV3`（既存 DB）の両方から `INSERT ... WHERE NOT EXISTS` で実行し、Bug/Documentation/Enhancement/Feature をマスターの issue ラベルとして投入。`seedLabels` 定数は廃止し、`SeedDemoData` はマスター issue ラベルを名前解決して付ける形に。
- **削除は archived フラグ**: picker・フィルタ・管理一覧からは消えるが、付与済みの issue/discussion には残り続ける（attach 系クエリは archived を絞らない）。`setIssueLabels`/`setDiscussionLabels` は `kind`+`archived=0` でガードし、別 kind・削除済み id の混入を防ぐ。復帰は管理画面の Restore または同名再作成（`CreateLabel` が archived 行を色更新して復活させる）の2経路。
- **API は `/api/v1/w/{ws}/labels` へ移動**: labels は issue と同じワークスペーススコープの資源になったのでグローバル `/api/v1/labels` は廃止（discussion-categories のみグローバル継続）。GET `?kind=`、POST `{name,color,kind,shared}`、PATCH/DELETE `{id}`（DELETE=アーカイブ）。
- **CLI は `zatu label list|create|delete`**: `--kind` で issue/discussion を切替え、`--shared` でマスター作成。unarchive は同名 `label create`（store の restore セマンティクスそのまま）。issue 側の `--label` 解決は `kind='issue'`、discussion create/edit の `--label` は `kind='discussion'` を見る。
- **FK-off rebuild の復活**: v2→v3 で `labels` を再構築する際、DROP TABLE が `issue_labels` の `ON DELETE CASCADE` で紐付けを全消しするため、以前削除した `migrateWithFKOff`（migrateV2 用）を migrations[2] 向けに復活させた（pitfalls の知見どおり）。
- **検証**: `go test ./...` / `go vet` / `gofmt` 全パス。`internal/store/labels_test.go` が CRUD・union・discussion 付与・v2→v3 移行を検証。実サーバで管理画面の作成→重複 422→アーカイブ→復帰、discussion picker、local-mode `zatu label` を手動確認済み。

追記（レビュー指摘対応）: 添付ガードに `(workspace_id IS NULL OR workspace_id = ?)` を加え他ワークスペースのラベル id を拒否。replace 時の DELETE は `archived=1` の添付を温存する（picker に出ないため編集で消えてしまうのを防ぐ）。API の label フィルタは `label_id` パラメータを優先（同名 union の曖昧さ回避）。ラベル管理フォームも `novalidate`/`page-form.js`/aria 関連付けを他フォームと揃えた。

## issue/discussion の Markdown エクスポート（2026-11-18、main ブランチ）

「特定の issue / discussion(thread) を Markdown としてコピーしたい、API としても露出したい」という要望。serializer を `internal/export` に切り出し、HTML アプリ・`/api/v1`・CLI の 3 面が同一実装を共有する形にした。

- **`internal/export` に serializer を集約**: `IssueMarkdown(issue, comments, wsSlug)` / `DiscussionMarkdown(discussion, threads, wsSlug)`。本文は既に Markdown なのでエスケープせず verbatim で埋め込み、メタデータは `- **Key**: value` の箇条書き。コメント/スレッドは `###`、返信は `####` の見出しで畳み込み、answer マークはスレッド見出しに `· answer` 接尾辞。`Answered` メタ行は qa カテゴリのみ（open カテゴリではマーク自体を持たないため）。
- **エンドポイントは web と API の双子**: `GET /w/{ws}/issues|discussions/{id}/markdown` と `GET /api/v1/w/{ws}/.../{id}/markdown` が同じ文書を返す。`/api/v1` は Bearer 認証なのでブラウザの fetch からは叩けず、HTML 側にも同一路由を置いた（cookie セッション版）。成功は `text/markdown; charset=utf-8` + `Content-Disposition: inline` — 本文はクリップボード入力なので `attachment` の強制ダウンロードはしない。エラーは各層の既存挙動（API は JSON 404、web は styled 404 / GET なら canonical workspace へ 302）。
- **UI はアクション行の "Copy as markdown" ボタン**: `command="--copy-markdown"` + `commandfor`（article id）の Invoker Command で、copy link と同型の進行形強化。クリックで `/markdown` を fetch して clipboard へ書き込む。`static/clipboard.js` に report bubble（`role="status"` の copy-tip）と write path を集約し、`copy-permalink.js` も import する形にリファクタ。`ClipboardItem` に promise を渡す方式で fetch 中もユーザーアクティベーションを保持（Safari は await 越しの `writeText` を拒否するため）。ClipboardItem 非対応は fetch→`writeText`→`execCommand` の順にフォールバック。
- **CLI は `zatu issue|discussion view N --markdown`**: 同じ export 関数を使うため local/remote backend で差異なし（remote も既存の Get+List で文書を構成）。`--json`/`--template`/`--web` とは排他、`--markdown` 指定時は常に comments/threads 込み。
- **検証**: `go test ./...` / `go vet` / `gofmt` 全パス。export 単体・API・web・CLI に各テスト追加。storybook でアクション行の描画（広幅・狭幅）を確認。実ブラウザでのコピー動作は verify-ui フローで別途確認。

## seed の分離 — schema / master data / mock data（2026-11-19、`devin/*-split-seed-migrations` ブランチ）

起動時の自動シードを3層に分けた。ユーザーの要求は「空っぽな状態で起動できるようにしたい。ラベル等のマスターデータだけは DB に残したい」。

- **3層の分類**: schema migration（`migrate`/`schemaDDL`、テーブル+index。master ラベルと default workspace は DDL 内 `INSERT ... WHERE NOT EXISTS` でここに含まれる）/ master data（`SeedMasterData` = `EnsureDefaultWorkspace` + `SeedCategories`、app が機能するための参照データ）/ mock data（`SeedDemoData` = demo issues/discussions/comments + `demo` ユーザー）。
- **`-seed` フラグ（env `ZATUDAN_SEED`）で opt-in**: ユーザー選択「b の逆」。run は `-seed` なしでは demo を一切投入しない（従来は `issues` が空なら毎回自動投入していた）。`-seed` 付きの `init` でも demo が入る —「init で全部投入してあとで空にしたい」ワークフローも `-seed` なし init（master のみ）で代替できる。
- **master data は常に保証**: `-seed` の有無に関わらず起動時に `SeedMasterData` を呼ぶ。categories を失うと local CLI で discussion を作れず、workspace を失うと scoped store が死ぬため「空っぽ」は「ユーザー生成データが空」であって参照データ込みではない。
- **`SeedDemoData` 内の `ensureCategories` は残す**: demo discussions が category_id を参照するため、テスト等で `SeedDemoData` を直接呼んでも自足するように（冪等）。
- **e2e は demo 非依存**: `internal/e2etests` の fixture は全て自作なので `-seed` なしのまま通る。
- **検証**: `go test ./...` / `go vet` / `gofmt` 全パス。`TestSeedMasterData` 追加（workspace・labels・categories 有・issues/discussions/users 無）。実バイナリで `init`（master のみ）→ 起動（issues 空・200）→ 同 DB に `-seed` 起動（demo 投入）を確認。

## zatu CLI の --help 整備とエージェント向け skill（2026-11-19、`devin/zatu-help-skill` ブランチ）

「開発中の記録を issue/discussion にリプレイする」用途に向けて、新しいエージェントが `gh` のように zatu を独学できる形にした。要点は skill を薄く保ち、使い方の正を `--help` に集約すること。

- **3 階層の --help**: top（`zatu [--help|-h|help]`）= 全コマンド面 + backend 選択、group（`zatu issue --help` 等）= そのグループの action 一覧、leaf（`zatu issue list --help`）= usage 行 + `flag.PrintDefaults` の flag 一覧。いずれも stdout・exit 0。`isHelpArg` が `help`/`-h`/`--help` を各 dispatcher で処理する。
- **flag 定義を leaf help の single source に**: leaf の flag 一覧はヘルプ文字列に複写せず `PrintDefaults` で生成する（flag 追加時のドリフト防止）。usage 行の引数ヒントだけ `parseLeaf` の `argHint` 引数で渡す。誤用時の flag パッケージ出力は従来どおり io.Discard に潰し、明示的な help 要求時のみ stdout へ出す。
- **`flag.ErrHelp` を `errHelp` sentinel に写像**: flagset が -h/--help を検出したら leaf usage を stdout に出して `errHelp` を返し、Run は exit 0 に写像。誤用（不明 flag・引数不足）は従来どおり usageError → exit 2 + usage を stderr。
- **コメント編集は未実装のまま**: store/web/API いずれもコメント update を持たないため、訂正は追記コメントで行う（gh でも他人のコメントは編集できず自然な型）。issue/discussion 本体の title/body は `edit` で訂正可能。
- **`tools/demo-history.sh`**: zatu のみでリアルな開発履歴（7 issues・4 discussions・answer マーク・label 作成・edit による訂正）をリプレイする実行可能なリファレンス。`zatudan -db X init` で master data（discussion categories）を入れてから流す — 新規 DB は categories が無く `discussion create` が通らないため。
- **検証**: `go test ./internal/cli` の `TestHelp`（全階層の exit 0 + usage 内容）、`go test ./...` / `go vet` / `gofmt` 全パス。実バイナリで全 --help 経路とスクリプトのリプレイを確認済み。

追記（同日、2エージェント実使用テスト）: 環境変数と「`/tmp/zatu --help` で自分で学べ」という指示のみを与えた2つの新規エージェント（alice/bob、それぞれ別ユーザーの API token で投稿）が、コマンド不教示のまま issue への再現報告→typo の訂正追記→qa discussion 作成→回答→answer マークまで自律的に完遂した。つまずき点は「`comment edit` が存在しない（追記コメントで代替 — 想定どおりの訂正導線）」「`view` は `--comments` が要る」「category slug を列挙するコマンドがない（TODO.md へ記録）」の3点のみで、いずれも `--help` とエラーメッセージから自力復帰。skill を薄く保ち使い方の正を `--help` に集約する設計は、この条件で成立することが確認できた。

追記2（同日、クリーンルーム再検証）: リポジトリ参照の混入を除くため、バイナリのみを置いた `/tmp/zatu-room-<name>` を pwd・「それ以外のファイルを読まない」明示禁止の条件で carol/dave に再実施。結果: 外部ファイルアクセス 0・全コマンド一発成功で、carol は category slug を `discussion list` 出力列から自力発見（fallback の起票は不要と判断）、dave は answer マーク・issue 起票誘導まで完遂。1 回目の傍証（AGENTS.md 注入・ファイル到達可能だった点）はあったが、2 回目で非隔離への懸念は払拭 — `--help` 集約はリポジトリ知識なしでも成立する。新たな観察: `discussion comment --help` は create への誘導のみで一手間増える、削除済み番号（#8）の `view` が 404 になるのは意外感あり、`--thread` の ID 依存は自明でない。

## `zatu discussion categories` の追加（2026-11-19、main ブランチ）

TODO.md の項目。「`discussion create --category SLUG` が必須なのに slug を列挙する術がない」という、2エージェント実使用テストで発覚した発見性の穴を埋める。

- **動詞は `discussion` グループの flat action**: `zatu discussion categories`。`category list` のような入れ子にしなかった — categories は discussion 専用の参照データで、リソースグループを分けるほどの動詞面を持たない（作成・削除は CLI に出さず master data のみ）。`label list` 等の「resource verb」形に揃え、help テキスト・completion（bash/zsh/fish）を一式更新。
- **backend は unscoped で開く**: `o.backend(ctx, bf)`（= `--workspace` の scoped backend）ではなく `backendIn(ctx, bf, "")` を使う。categories は workspace 非依存の master data で、remote も `/api/v1/discussion-categories` のグローバル経路（`client.In` でも ws 不変）— scoped で開くと local だけが「存在しない workspace で ErrNotFound」になりモード間で非対称になるため、workspace コマンドと同型の unscoped open にした。`--workspace` は無視される。
- **出力は `SLUG\tNAME\tFORMAT\tDESCRIPTION`**: `--category` に渡す slug を先頭列に（`label list` が参照キーの name を先頭に置くのと同じ約束）。`--json`/`--template` は `categoryViews`（api.DiscussionCategory DTO）経由で他コマンドと同じ語彙。
- **検証**: `go test ./internal/cli`（`TestDiscussionCategories` 追加: plain 出力の slug 網羅・行フォーマット・`--json` フィールド選択・`--workspace` 無視）。実バイナリで local（`init` した DB）・remote（`-api-token` サーバ）両モードの列挙と、列挙した slug での `discussion create` を確認。

## コメントの編集・削除・hide（モデレーション）（2026-11-19、main ブランチ）

issue コメント・discussion スレッド（開設ポスト）・返信の3種に対して、edit / delete / hide / unhide を全層（store・web・API・client・CLI・export・SSE・storybook）で実装した。これまでの「コメントは編集できない、訂正は追記で」（zatu-help skill 節の記述）を置き換える決定。

- **moderation state は `store.Moderation` に集約**: `EditedAt` / `Hidden` / `HiddenReason` の3項目を3種の投稿型に embed。DB は `edited_at` / `hidden_at` / `hidden_reason` カラム（schema v4 migration）。hidden は `hidden_at` 非NULLで表現し、reason を別カラムに保持 — 「解除後も理由を履歴として残す」設計ではなく、unhide（reason="" 書き込み）で両方 NULL に戻るシンプルな状態機械。
- **hide 理由は固定7値**: `store.HideReasons`（spam / abuse / off-topic / outdated / duplicate / resolved / low-quality）を唯一の真実にし、store・API・HTML select・CLI flag validation が同じセットを参照。理由は必須 — 「理由なし hide」は API でも HTML でも 422/400。
- **edit は body が実際に変わったときだけ `edited_at` を打つ**: 同値 PATCH は no-op（updated_at も触らない）。GitHub の "edited" 印と同様に履歴は持たず、最新時刻のみ。
- **delete はハードデリート**: 復元 UI・ゴミ箱は持たない。スレッド削除は既存の `ON DELETE CASCADE` で返信ごと消える（web の確認ページに「and its N replies」と警告を出す）。
- **親の `updated_at` は触らない**: moderation 操作は「新しいアクティビティ」ではないため issue/discussion の updated_at を更新しない。新規コメント投稿・answer マークは従来どおり bump する。
- **スコープはパスの全階層で検証**: `/discussions/{id}/threads/{tid}/comments/{cid}` のように、別 discussion / 別スレッド配下の id を指定した場合は 404（404 は情報漏洩も防ぐ）。
- **SSE は新イベント種別 `remove` を追加**: 既存 kind は全て「追加 or 更新」だったため、行の物理削除を表す `KindRemove` を新設（payload は target=li id + parent=一覧 ul id）。live-updates.js は pending map で remove を追跡し、empty-state の復帰とカウント減算まで面倒を見る。edit/hide/unhide は既存の更新イベント（KindEdit 系）に乗せ、部分再描画は既存 fragment refresh に委譲。
- **web UI の操作分割**: Edit は専用ページ（ui-rules §1-7 の「インライン編集なし」方針に合わせ GET フォーム + POST）、Delete は GET 確認ページ→POST（§7 の確認経由義務）、hide/unhide はカード内 POST + partial 差し替え。hide の理由選択は `<details>` 収納の必須 `<select>` — 7 値単一選択なので radio 群より 1 コントロールが適切（modern-web-guidance 調査済み）。hidden カードは `<details>` で本文を畳み、ページ内検索・支援技術から残す。
- **API 形状**: PATCH `{"body"}` / DELETE → 204 / POST `.../hide` `{"reason"}` / `.../unhide` `{}` → 200 + 最新 DTO。DTO は `edited_at`（omitempty）・`hidden`・`hidden_reason`（omitempty）を inline embed（`api.Moderation` — CLI の --json view も同型を再利用）。
- **CLI 構文**: issue 側は `zatu comment edit|delete|hide|unhide N C`、discussion 側は `zatu discussion comment <action> N --thread T [--comment C]`（`--comment` 省略でスレッド自体が対象）。
- **検証**: `go test ./...` / `go vet` / `gofmt` 全パス。store（moderation_test.go）、web handler（comments_test.go: edit/delete/hide/unhide/404/partial 契約）、API（comments_test.go: 3種の PATCH/DELETE/hide + スコープ 404 + フィールド 422）にテスト追加。storybook に edited/hidden fixture と edit/delete/hide/unhide partial story を追加。

## pull requests 機能（2026-11-19、`docs/sketch/ja/pull-requests.md` §7 の実装）

issue tracker に git の pull request 相当を再現する機能。実体は DB の `pull_requests` 行（base_ref/head_ref + 記録済み SHA）で、コミット列・diff・conflict は open 中 git で都度計算する。

- **repo の不在は機能の不在**: `zatudan` は `-repo`/`ZATUDAN_REPO`/`-db` 起点の `.git` 上方探索で解決（`off` で明示無効化）。未検出なら /pulls・/api/v1/pulls*・nav リンク・storybook 登録を全てマウントしない — 404 と機能消失は同じ扱い。解決は起動時1回で、後から現れた repo は再起動まで見えない（-api-token と同じポリシー）。
- **nav の pulls 表示は Renderer ごとの状態**: repo-backed と repo-less の handler/storybook が同一プロセスに同居しても、layout の nav リンク状態が global variable 経由で漏れないようにする。
- **pin `refs/zatudan/pr/<id>/head` が履歴保全の役**: open 中は head ref 追従、close/merge 時は記録済み `head_sha` で pin を更新し、アーカイブ済みコミットが gc されないことを保証。pin 失敗は操作自体を中断する。
- **merge は web/CLI 2経路・同じ gitrepo.Merge**: `merge|squash|rebase` の3メソッド。squash の既定メッセージは `タイトル (#N)`。merge 後の pin は res.HeadSHA を使う（解決と merge の間に head が進みうるため）。
- **diff 意味論の分割**: コミット列は two-dot `base..head`、files ビューは three-dot（open は merge-base→head の GitHub 同等、closed/merged は記録 SHA で固定）。rename コメントは旧/新パス両方へグルーピングする。
- **API は unscoped**: `/api/v1/pulls*` は workspace スコープを持たない（client の `wsPath` を使わない）。repo 無効時はルート自体が未登録（404）。`/diff`（text/plain）と `/markdown`（export.PrMarkdown）を専用エンドポイントにした。
- **`zatu pr` は Backend iface の13メソッドで両モードを吸収**: local は gitrepo.Resolve で repo を自前解決し git 作業（pin・merge・snippet 採取）を CLI 層が行い store 動詞を呼ぶ。remote は `/api/v1/pulls*` を叩き `--repo` は警告して無視。`--head` 省略は local のみ current branch 解決。
- **form errors は構造体アダプタ**: ページモデルの `Errors` は `pullFormErrors{Title,Body,Ref}` + `any()`（store.FieldErrors ではない既存規約に準拠）。
- **export は純粋関数**: `export.PullRequestMarkdown` は収集済みの metadata+commits+comments+snippet を受けて描画だけ行う。review comment は `path:L<n> (side)` 見出し + locator 行 + 行番号なし fence（エージェント誤カウント対策）。
- **検証**: `go test ./...` 全パス（api: 新規 pulls_test.go、export: pulls_test.go、cli: fake 拡張）。実サーバで create→detail→files→comment→SSE→merge→close を curl 検証済み。`zatu pr` 実バイナリで list/create/view/markdown/diff/comment/merge を local mode 検証済み。

## 描画計測と視覚差分検知（2026-11-20、`docs/sketch/ja/rendering-and-visual-diff.md`）

chrome-devtools の trace insight で計測した結果、CSS はボトルネックではなく（style+layout ~10ms、LCP 70ms）、実害は `/static/` の無キャッシュ（全ナビゲーションで 59.4kB 再取得）だった。

- **`/static/` に `Cache-Control: public, max-age=3600`**: embed.FS は modtime を持たず validator を出せないため、再検証ではなく短い max-age に倒した（hash 入り URL + immutable は TODO 送り）。ハンドラは `serveStatic` helper に集約し、全静的資産に一律適用。
- **storybook fixture の時刻は `storyNow` に pin**: Pull 系 story が `time.Now()` を使っており、撮影ごとに 263–1687px の偽差分が出た。`storyTime*` と同列の固定時刻 `storyNow`（2026-11-19T12:00Z）に全置換 — 「現在っぽい時刻」を持つ fixture の正はこれ。
- **視覚回帰は `tools/visual-diff.sh`（update/check）**: playwright-cli の fullPage スクリーンショット（`animations:'disabled'` + `document.fonts.ready` 待機）を ImageMagick `compare -metric AE` で閾値判定（既定 100px、AA ノイズは sub-10px）。baseline は `.visual-diff/`（gitignore）に撮り直す運用 — 固定 fixture があるから成立する。
- **CSS coverage（CDP rule usage tracking）の解釈**: 未到達ルールは「storybook では検証不能な pseudo-state/JS 駆動」と「fixture gap」を含むため dead code 判定には使わない。storybook の検証盲点の定量化に使う。

## i18n プロトタイプ（2026-11-20、`docs/sketch/ja/i18n.md`）

- **ロケール解決は `withLocale` ミドルウェア**（`?lang=` → cookie → `Accept-Language` → `en`）。`?lang=` は cookie を Set して `lang=` を除いた URL へ 303 リダイレクト。URL に言語を残さない方針（SEO 不要の localhost アプリ）。Accept-Language は ja/en の q 値を比較し、同値なら先出を優先する簡易パースとする。
- **カタログは自作の薄い層**: `internal/web/i18n.go` の `messages` struct（フィールド = メッセージ）に `messagesEN`/`messagesJA` を置く。初版は JSON+`{{t "key"}}` だったが、キーの typo が両面で検出不能とのレビュー指摘で struct + フィールド関数名化に変更（typo がコンパイル/パースエラーになる）。空フィールドは en にフォールバック（部分適用が壊れない）。go-i18n / x/text/message は ja/en のみ・複数形ほぼ不要の現要件では過剰と判断。JS 側でカタログが要るなら JSON + `{{t "key"}}` 突合 lint（minigo）が代替。
- **テンプレートはロケール別にパース**: `parsePages(locale)` が `messageFuncs`（struct フィールド → 同名関数）と `{{lang}}` をそのロケールに束縛し、`pagesByLang[locale][page]` で保持。ビューモデル 15+ にフィールドを足さず `layout.html` の `<html lang>` まで届くのが採用理由。
- **プロトタイプ範囲は issues 一覧 + layout のみ**。validation 文言・エラーページ・静的 JS 文字列・`fmtTime` 書式・UGC の `lang` は未対応（TODO.md の「i18n の全面適用」参照）。

## i18n 全面適用（2026-11-20、TODO「i18n の全面適用」）

- **カタログを全画面へ拡張**: `messages` struct を ~290 フィールド（テンプレート文言 + ハンドラ側 Title/Heading/SubmitLabel + エラーページ + validation）へ。ハンドラ内の英語リテラルは `msgFor(r)` 経由でカタログ参照に置き換え、ビューモデルの error フィールドは `string` のまま境界で訳す（`{{if .Errors.Title}}` の空判定を保つため）。
- **store の FieldErrors をコード化**: `store.FieldError{Code, Limit, Message}` + `ErrCodeRequired/TooLong/SlugPattern/HexColor`。web 層は `fieldErrText(lang, field, fe)` で訳し、API/CLI は従来どおり英語 `Message` を返す（機械可読性を落とさず web だけ訳せる形）。
- **スイッチャは `GET /locale?lang=`**: cookie を Set して Referer のパス+クエリへ 303（フィルタ保持）。`?lang=` 直リンクは `withLocale` の既存処理が引き続き担う（`/locale` パス自身は除外して二重処理を回避）。クロスオリジン Referer は `/` フォールバック。
- **静的 JS の文字列は `<html data-i18n>`**: ロケール別 `jsDictFor(lang)` の JSON を属性に埋め、`static/i18n.js` の `t(key, ...args)` が読む。CSP `script-src 'self'` のまま inline `<script>` 不要。
- **`fmtTime`/`authorName`/`hideReasonLabel`/`noun`/`repliesClause`/`labelKind`/`labelScope` はロケール別 funcs 束縛**（`parsePages(lang)` 内）。package 級の同名 funcs は削除せず funcMap の既定として残し、`parsePages` がロケール別で上書き。
- **文中リンク・HTML を含むメッセージは分割方式**: `*Pre`/`Link`/`Tail` や `*HTML` サフィックスのフィールドを使い、HTML を返すフィールドは `template.HTML` を返し各 arg を `HTMLEscaper` 経由。
- **UGC は未翻訳**: issue 本文・コメント等のユーザ生成コンテンツは `lang`/`dir` 属性も付けずそのまま（TODO 残課題）。

## storybook: UI を `minigo/x/storybook` のライブラリに移譲（2026-11-23、minigo#1418 対応）

- **`page.go` 削除**: index(ナビ+grid+iframe)・`/story`・`/grid`・`/static` のハンドラと `-o` export が minigo 側に吸収され、example が持っていた UI は `Book.Handler()` / `Book.Export(ctx, dir, stories)` として `x/storybook` パッケージに入った。パッケージ自体も `minigo/storybook` → `minigo/x/storybook` へ移動（minigo から浮いた存在との判断）。
- **アプリ mux の mount は `Book.Fallback`**: chrome が握るのは GET の `/{$}`, `/story/`, `/grid/`, `/static/` と live-reload だけ。それ以外のパスと非 GET が `Fallback http.Handler` に流れるので、`web.New(storyStore)` 相当の appHandler をそのまま差し、catch-all mux を自前で噛ませる必要がなくなった。
- **残る差分は `bindApp` + `appHandler` + flags + `errorPage` のみ**: cmd/storybook は ~1,950 行から ~390 行へ。story 探索・描画・UI の回帰は upstream のテストに任せ、ローカルは `TestAppMount`（app mount の疎通）と `TestWatchRequiresHTTP` のみ保持。
- **`Setup` hook は据え置き**: goldmark 系・sqlite 空 bind の注入点は変わらない。

## 部分投稿フォームの入力ロックと遅延読み上げ（2026-11-25、go-spa-sample#23 の対照）

go-spa-sample#23 で修正された SPA 側の comment mutation regression を MPA 側に対照し、部分更新固有の3件を修正した。

- **送信中は入力コントロールも lock する**: 従来は submit ボタンのみ無効化していたため、fetch 中に追記できてしまい、成功時の `form.reset()` がその追記を消していた。FormData を先に捕捉し（disabled control は送信対象外）、input/select/textarea を元の disabled 状態を記録して lock。成功 reset と失敗復帰の直前に unlock する。
- **遅延エラー読み上げはフィールドの存亡と可視性を見る**: blur/change の 500ms 後に live region へ書く間に partial swap でフィールドが消えたり、reply cancel で `<details>` が畳まれたりすると stale なエラーを読んでいた。timer 発火時に同一フィールドかつ `isConnected` / `checkVisibility()` の場合だけ読み上げる。`checkVisibility()` が無い環境でも closed `<details>` 内なら読まない。
- **invalid な返信 Cancel は pointer capture で押せるようにする**: blur で `:user-invalid` のエラー行が出ると Cancel が mouseup 前に押し下げられ、マウスクリックが外れていた。`pointerdown` 時に Cancel ボタンへ pointer capture を設定し、開始したボタンへ activation を届ける。pointer を掴んだまま外へ drag して離した場合は cancel しない。
- **検証**: 実サーバ + playwright-cli で、遅延 POST 中の textarea lock・成功後の reset/focus 復帰・network failure のドラフト保持・422 の draft/error 復元・reply POST・valid/invalid cancel・cancel 後の stale announcement 抑止を確認。

## open questions の回答（2026-11-26、podhmo）

- **一覧のライブ化は行わない**: SSE による passive banner や行の自動差し替えは一覧へ入れない。
- **空状態導線と一括操作は今は入れない**: 「作成する」「フィルタを解除」リンク追加も、close/label 一括操作も行わない。
- **writing experience は3機能を1PRにまとめる**: Markdown Preview、localStorage 下書き、`#12` リンク化を同じ PR で扱う。下書きは投稿成功時だけ自動削除し、保持期限も手動クリア導線も設けない。
- **モバイル nav は `<details>` メニューに畳む**: キーボード shortcuts は今は入れない。最後に見た workspace の cookie 復帰は後回し。
- **検索は FTS5 より先にクエリパーサだけ入れる**: `is:open label:bug author:demo` 形式を web/CLI/API 共通の文法にし、条件はすべて AND とする。
- **楽観的排他の `If-Match` は任意**: 指定された更新だけ `version` を照合し、web フォームは常に hidden version を送る。不一致時の差分表示は実装しやすい最小範囲に留める。
- **API 拡張は今は行わない**: ページング拡張、コメント取得 endpoint、token scope/expiration はいずれも先送り。
- **PR files は局所修正に留める**: 不正マークアップと N+1 git 起動を直し、行コメント form は `?line=` 付き GET でサーバー描画する。大規模再設計はしない。
- **remote CLI の 404 写像は endpoint 文脈付き**: API 変更なしで、record missing と分かる endpoint だけ `store.ErrNotFound` に写像する。
- **PR merge は最小 CAS を先に入れる**: service 抽出は後で行い、lease/復旧も実装しやすい最小範囲に留める。
- **PR の pin 失敗は許容して修復可能にする**: pin 欠損を検出・再作成できる運用を優先し、厳密な transaction 結合は求めない。

## SQLite WAL + immediate transaction（2026-11-26、owner improvements 対応）

- **file-backed DB は WAL + synchronous(NORMAL)**: reader/writer の並行性を上げる。WAL は DB ファイル側の永続設定だが、DSN にも `_pragma=journal_mode(WAL)` を付けて新規 DB から有効にする。in-memory DB には付けない。
- **transaction は `_txlock=immediate`**: read-before-write の mutator が deferred transaction の upgrade で `SQLITE_BUSY` / snapshot conflict にならないよう、BEGIN 時点で write lock を取る。busy_timeout(5000) と foreign_keys(1) は従来どおり全接続へ付与する。

## /static/ の認証外配信・gzip・ETag（2026-11-26、owner improvements 対応）

- **既知の静的ファイルだけ exact path で認証 middleware の外に出す**: session cookie 付きでも stylesheet/module ごとに DB 照会しない。未知の `/static/` 配下は従来どおり動的 mux の styled 404 に落とす。
- **embed 資産は起動後の初回アクセスで byte・gzip・sha256 ETag をキャッシュする**: deploy まで内容が変わらないため process lifetime で有効。ETag conditional request は 304、gzip 対応 client には事前圧縮 body を返し `Vary: Accept-Encoding` を付ける。versioned URL + immutable 化は別 TODO に残す。

## workspace 解決の count-free lookup（2026-11-26、owner improvements 対応）

- **scoped route は `WorkspaceRefBySlug` を使う**: `/w/{slug}/...` の定常解決では id/slug/name/description/created_at だけを読み、issues/discussions の `COUNT(*)` を実行しない。返り値の count field は zero value のまま。
- **件数を表示する画面だけ従来の counted lookup を使う**: workspace delete 確認と API workspace detail/create response は `WorkspaceBySlug`、workspace 一覧は `ListWorkspaces` の counted query を使う。redirect 用の `WorkspaceByID` は稀な経路なので現状維持。

## markdown 表示変換の LRU cache（2026-11-26、owner improvements 対応）

- **cache key は `sha256(body)`**: DB には生 markdown を残し、goldmark の表示 HTML だけを process-local にキャッシュする。LRU は128件まで、64KiBを超える source は cache しない（出力まで含めたメモリ上限を緩やかに抑える）。
- **storybook 互換のため key は binary digest の string**: `sha256.Sum256` の `[32]byte` 返り値は minigo 経路で扱いにくいため、`sha256.New().Sum(nil)` の bytes を string key にする。衝突耐性の意味論は同じ。

## /static/ versioned URL + immutable（2026-11-26、owner improvements 対応）

- **HTML が参照する静的資産は `/static/<name>?v=<content hash>`**: 全 embedded asset の名前と内容から作る deployment hash を query に載せ、一致する URL だけ `Cache-Control: public, max-age=31536000, immutable` にする。`v` なしの旧 URL は互換 fallback として従来の `max-age=3600`、stale な `v` は 404。
- **storybook 互換のため path ではなく query に hash を入れる**: storybook は `/static/` をアプリより先に live overlay へ振るため、`/static/v/<hash>/...` だと export/preview の資産解決を壊す。query なら path が変わらず、overlay は従来どおりファイルを供給できる。
- **module import は serve 時に同じ `v` 付きへ書き換える**: `<script type="module">` の entrypoint だけ versioned にしても、`./clipboard.js` などの relative import から query は継承されない。embedded JS の `./<name>.js` 文字列を process-local に `?v=` 付きへ変換し、module graph 全体が deployment ごとの cache key を持つようにする。

## ES module dependency の modulepreload（2026-11-26、owner improvements 対応）

- **page detail/form の依存 module を `<link rel="modulepreload">` で並列化**: bundler なしの ES module 構成は維持しつつ、entrypoint が import 解決で段階的に依存を発見する waterfall を避ける。`modulePreloads` は embedded JS の静的 `import`/`export ... from` 行から direct+transitive dependency を導くため、新しい依存を足したときにテンプレート側の手動一覧を更新しない。
- **page ごとの script は `<head>` へ移す**: `<link rel="modulepreload">` が body 末尾近くにあると entrypoint 発見とほぼ同時になり効果が薄い。layout に空の `head` block を用意し、page template 側で override する。`type="module"` と `defer` はもともと parser blocking ではないため、body 先頭から head への移動で実行タイミングの意味論は変えない。
- **versioned URL と同じ `staticURL` を使う**: preload も entrypoint も同じ content hash query を持ち、immutable cache の対象になる。

## テンプレート parse の clone 化（2026-11-26、owner improvements 対応）

- **layout はロケールごとに 1 回だけ parse する**: 各 page はその template set を `Clone()` してから自分の `content` / `head` override を足す。page ごとに layout を再 parse しないため起動時の CPU を減らす。
- **fragment は必要な detail page だけで parse する**: `html/template.Clone` は parse tree をコピーするため、全 fragment を shared set に入れると使わない page にも複製が残る。issue/discussion/pull detail にだけ従来どおり必要な fragment file を足す。
- **`jsDictFor` の JSON は locale ごとに cache する**: catalog は起動後に変わらないため、描画や Renderer 構築のたびに marshal しない。

## PR detail の git query batching/cache（2026-11-26、owner improvements 対応）

- **base/head の commit SHA は `rev-parse` 1 回で解決する**: `RevParsePair` / `RevParseMany` は複数の `<ref>^{commit}` を同じ process に渡す。`--verify` を複数回指定すると git が単一 revision として扱えないため使わず、exit status・行数・hex 形式で batch 全体を検証する。1 件でも不正なら従来どおり `ErrBadRef`。
- **open PR detail は先に SHA 解決し、その同じ commit pair で log と merge check を実行する**: ref 名を個別コマンドへ渡さないため、表示する commit 列と merge 判定が途中で動いた ref をまたがない。web/API/local CLI の detail 経路で共有する。
- **`(baseSHA, headSHA)` を key にした process-local LRU は 32 件**: object id は immutable なので、complete clone の同じ pair の `git log` と clean な `merge-tree` 結果を SSE fragment や再描画で再実行しない。ref 名そのものは movable なので cache key にしない。shallow clone は deepening で同じ tip pair の log が増えうるため cache しない。conflict/unknown な merge 結果も cache せず、merge 系 config 変更後に stale conflict が form を隠すことを避ける（実際の merge は常に実行前に conflict を pre-flight する）。cache は git 失敗を保存せず、読み出し・格納時に slice を copy して caller mutation を遮断する。

## API/store の roundtrip 削減（2026-11-26、owner improvements 対応）

- **issue PATCH は事前読み出ししない**: `PatchIssueInWorkspace` が `WHERE id=? AND workspace_id=?` の scoped update で title/body を `COALESCE(?, column)` し、label 更新も同じ transaction に入れる。既存 issue を読まずに部分更新できる代わりに、missing id と validation error が同時にある request は validation を先に返す（従来の 404 優先ではなく 422 優先）。response 用の再読み出しも `GetIssueInWorkspace` で workspace scope 済みにする。
- **comment の再取得は単行 lookup**: `GetComment` / `GetPRComment` を `(parent id, comment id)` で scope し、mutation response の DTO 組み立てで全件 `List*Comments` + 線形探索しない。API endpoint は増やさず store API の追加に留める。
- **label 名解決は `LabelByName`**: 単一名なら全 label を読まず、kind/name/workspace visibility で 1 行だけ引く。複数 distinct 名の write では per-name query を避けて従来どおり catalog を 1 回だけ読む。shared master と workspace label が同名なら workspace 側を優先し、picker で workspace-specific に見える方と一致させる。

## hidden タブの SSE 切断（2026-11-26、owner improvements 対応）

- **`EventSource` は visible な間だけ保持する**: detail page の `live-updates.js` は初期表示が hidden なら接続せず、`visibilitychange` で hidden になった時点で close する。localhost/HTTP/1.1 の同一オリジン 6 接続上限を休眠タブが占有しないようにする。
- **visible 復帰時は hint 再構成を試みず whole-detail resync する**: hidden 中の SSE event は欠けるが、もともと invalidation-only で missed hint の replay もない設計なので、article probe で削除を確認したら `/events` へ接続し、その最初の open 後に article card と timeline fragment を再取得する。snapshot が subscription より後になるため、snapshot 取得と接続確立の間の書き込みを取りこぼさない。offline のままの復帰でも接続だけは開始し、EventSource の retry 後の open が同じ resync 経路へ入る。
- **delete 済み resource では再接続も timeline resync もしない**: 接続中に delete event を受けた場合はその時点で `gone` にし、hidden 中に消えた場合は visible 復帰時の article fragment 404 を削除として恒久通知へ写す。`gone` 後は追加の fragment 再取得や `/events` 再接続を skip し、削除検出は article probe 1回に留める。
- **row 再描画では reply draft に加えて hide reason picker も保持する**: visible 復帰 resync は unchanged row も replace するため、open state と選択中の reason を既存の draft restore 経路に載せる。
- **flush 中に届いた hint は次回 batch へ残す**: fragment fetch 中の書き込みはその fetch 結果へ反映されないことがあるため、flush 開始時に pending を snapshot してから clear する。待機中に届いた event を finally で消すと、subscription 済みでも更新漏れが起きる。

## web handler の定型整理（2026-11-26、owner improvements 対応）

- **detail 参照は `requireIssue` / `requireDiscussion` に寄せる**: path id 検証、store 404 写像、workspace scope 確認を1箇所にし、handler 側は返された model の immutable な ID を使う。comment 作成など form parsing を先に行う必要がある経路だけ、順序を変えない範囲で helper を後置する。
- **fragment の `?ids=` は nil map を unfiltered sentinel にする**: パラメータ無しは nil、空指定は空 map なので、従来の `filtered` bool を別に持たない。絞り込みは generic な `filterByID` に集約し、issue comments/discussion threads/replies/PR comments で共有する。
- **form page は builder で共通フィールドを埋める**: `Section`/`Workspace`/`User` と title==heading の既定を `newIssueFormPage` / `newDiscussionFormPage` に寄せ、validation 再描画と通常表示で構造をずらさない。
- **Renderer の dead code は削除する**: typed render method 経由になった `execute` と handler 側の styled 404 に置き換わった `Renderer.notFound` を残さない。

## 一覧テンプレートの partial 化（2026-11-26、owner improvements 対応）

- **pagination は `pager` partial + `pagerData`**: issue/discussion/pull の3コピーをやめ、URL だけ各 handler が `newPager` へ渡す。pulls URL は空の `q=` を出さず、最終ページクランプは既存の `listPulls` 側の挙動を維持する。
- **issue/discussion delete confirm は `delete_confirm` partial**: 見出し・確認文・警告・action/cancel/submit label だけ各ページから `dict` で渡し、form markup を共有する。
- **一覧行は `record_row` が外側の `<li>` と main/meta の2カラム構造だけ持つ**: 各行種別のリンク先や badge は page-local な `record_row_main` / `record_row_meta` に残し、URL 設計や status 表示の差分を無理に一般化しない。

## i18n catalog の分割（2026-11-26、owner improvements 対応）

- **catalog struct・en・ja・locale runtime を分ける**: `messages.go` / `messages_en.go` / `messages_ja.go` / `locale.go` に分割し、テンプレートへ bind する関数名や fallback 規則は変えない。
- **ja の空フィールドはテストで落とす**: `messageFuncs` は空文字列を en fallback として扱うため、未翻訳が黙って混入しないよう `messagesJA` を reflection で全件検査する。

## pull CLI/client の単体テスト（2026-11-26、owner improvements 対応）

- **remote PR の状態遷移・markdown/diff・エラー写像を httptest で固定する**: CLI は wire path と出力文言、client は DTO→store 変換と text endpoint の `*Error` 写像を見る。git 実体が必要な merge 自体はここでは扱わない。
- **`reopen` の成功出力 typo は修正する**: 従来の `action+"d"` は `reopend` になるため、reopen だけ `reopened` を明示する。

## 古いドキュメント記述の同期（2026-11-26、owner improvements 対応）

- **ラベル管理未実装の記述を除去**: decisions の「実装しないもの」と workspaces sketch の seed 固定 taxonomy 記述を、2026-11-17 の DB 管理化（master/shared + workspace-specific label）へ揃えた。
- **view-transition inline `<style>` は hash 化しない**: Chrome が遷移 capture 用の `<style>` を実行時に挿入するため、静的な opt-in style だけを hash 固定しても遷移が CSP で abort される。既存の実ブラウザ計測（「inline `<style>` 解禁のため CSP に `style-src-elem` を追加」）を正として、layout コメントを実装中の `style-src-elem 'self' 'unsafe-inline'` に合わせた。

## Write/Preview・下書き保存・#参照リンク化（2026-11-26、owner improvements 対応）

- markdown 入力欄は JS で Write/Preview タブへ拡張し、Preview は `POST /preview` / `POST /w/{ws}/preview` が返すサーバー描画 fragment を使う。CSP を緩めず、no-JS では従来の textarea のままにする。
- 下書きは `draft:<path>:<form>` 系キーの localStorage に保存する。partial post は成功後の reset で削除し、classic post は hidden `draft_key` を成功 redirect 時の短命 cookie で返して exact key だけを削除する。validation 失敗や別ページへの手動遷移では消えない。期限・手動クリア UI は設けない。
- `#12` は markdown AST の Text node だけを `/w/{workspace}/issues/12` へリンク化する。code span/block と既存 link の中は書き換えず、cache key に workspace slug を含める。

## モバイル nav の `<details>` 化（2026-11-26、owner improvements 対応）

- **nav tree は1本のまま `<details>` で包む**: 大幅では summary を隠して inline nav、40em 以下では summary "Menu" から absolute dropdown を開く。リンクは別 tree に複製しない。
- **no-JS では open のまま機能させる**: markup は `open` 開始にし、`nav-menu.js` は narrow 判定が分かった初期だけ close、wide では常に open へ戻す。narrow 内の resize でユーザーが開いた状態を勝手に閉じない。
- **closed 時は明示的に nav を display:none にする**: `.site-nav` 自体の author display が closed `<details>` の UA hiding を上書きするブラウザがあるため、窄 layout では `.nav-menu:not([open]) .site-nav` を明示する。

## issue 検索クエリ構文（2026-11-26、owner improvements 対応）

- **FTS5 の前段として issue 検索だけに qualifier parser を入れる**: `is:open` / `is:closed`、`label:NAME`、`author:NAME`、残りの空白区切り語をすべて AND で title/body substring 条件にする。discussions/pulls の検索と FTS5 化は後段。
- **parser と解釈は store 側に置く**: web/API/local CLI/remote CLI が同じ `q` 文字列を store 層へ渡すので、3画面で文法実装が分岐しない。`label:` は workspace/shared label 名を SQL で直接照合し、未知 label は0件になる（既存の `label=` パラメータの422とは別仕様）。
- **`is:` は暗黙 open default を上書きするが明示フィルタとは AND になる**: `status` 未指定なら `is:closed` で closed を出す。web/CLI は「status 未指定」を store へ残し、明示 `--state open` や `?status=open` と `is:closed` が衝突した場合は0件にする。
- **検索語ハイライトも qualifier を外す**: client側 JS は Go parser と同じ quoted token 解釈で `is:/label:/author:` を除き、残り語を個別にハイライトする。
