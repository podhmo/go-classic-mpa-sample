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
- ラベル管理画面（シードの固定ラベルのみ）
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

- **ディスパッチ**: main.go は `os.Args[1]` が `internal/cli` のサブコマンド名（issue/comment/label/auth/api/help/discussion）なら CLI モードへ `os.Exit(cli.Run(...))`。`init` も含めた対称の分岐は「init だけ flag パス」の非対称が既存と整合するためこの形。usage は `issues <command>` 表記。
- **Backend interface は consumer 側宣言**: `cli.Backend` は store ドメイン型（`store.Issue`/`ListFilter`/`IssueInput` 等）のまま定義し、ローカルは `*store.Store` がそのまま構造的に満たし、リモートは `*client.Client` が api DTO ↔ store 型を往復変換して満たす。`web.Store`/`api.Store` と同じ「同一契約の 2 実装」パターン。
- **フラグは leaf command 単位**: flag.Parse は位置引数で止まるため `--db/--server/--token` は各 leaf の FlagSet に定義し、位置引数を挟んだ再 Parse で `issue view 1 --json title` 等どこに書いても効く形にした（gh と同じ自由度）。環境変数は `ISSUES_DB/ISSUES_SERVER/ISSUES_TOKEN`。
- **`auth token` はローカル専用**: api_tokens への INSERT は HTTP を通さず DB 直書き（パスワード検証を API に載せない設計メモ「認証」節の通り）。`--server` 指定時は usage エラーで拒否。
- **`issues api` はリモート専用・GET のみ**: パスは `/api/v1` 配下に正規化（`/issues` も `issues` も同じ）。非 2xx は body を stderr + exit 1。`--method/--field` は Phase 4 へ送った。
- **--json のキー名は api DTO 準拠**: `issue list --json id,title` と `issues api /issues` が同じ JSON 語彙を話すよう、cli 側で store 型→api DTO 変換してからフィールド選択する。
- **未実装は明示**: `discussion` は `IsSubcommand` に含めるが Run では「未実装（TODO.md 管理）」の usage エラー。discussion コマンド群と Phase 4（completion、--method/--field、NO_COLOR/TTY、token revoke/list）は TODO.md のオープン項目として残す。
- **E2E は実バイナリで**: `internal/e2etests`（`-tags=e2e`、`make test-e2e`）で `go build` → `init` → serve(`-api-token` 付き) → `/register` POST → `auth token` → `--server/--token` と `ISSUES_SERVER/ISSUES_TOKEN` の両経路で issue CRUD + `api` + author がユーザー別トークン名になることまで確認した。
