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
- **トレードオフ: スキップ中のコメントはアクセシビリティツリーから外れる**: Chromium 156 の CDP `Accessibility.getFullAXTree` では、スキップ中の `<li>` は子なしの listitem になり、中身は `ignored`（`notRendered`）。スクリーンリーダーのブラウズモードのリンク一覧・見出しジャンプ等では、近くまでスクロールするまでその内容が見えない可能性がある（VM では実際の支援技術で確かめられない）。50 件未満のスレッドでは発生しない — 閾値を設けた最大の理由。bugs.md #23 に記録。

## CSP・セキュリティヘッダ（2026-11-14、modern-web-guidance `security` / `privacy`）

- **CSP を `default-src 'none'` ベースで有効化**: `script-src 'none'; style-src 'self'; style-src-attr 'unsafe-inline'; img-src 'self' data:; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`。JS ゼロなので `script-src 'none'` が張れるのが本アプリの最大の利点。bugs.md #13' で「inline style と相性を考えると過剰」として見送った判断を `style-src-attr` で解消し取り下げた（#24 で解消済み）。
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
- **付けなかったもの**: HSTS（平 HTTP の localhost アプリであり、HTTP 経由ではブラウザが無視する）、`upgrade-insecure-requests`（全 URL を https に書き換えてアプリ自体が壊れる）、`require-trusted-types-for 'script'`（`script-src 'none'` で DOM シンクに届くスクリプト自体存在しないため無意味）、`report-to`（レポート endpoint がない）、`sandbox`（フォームまで塞がる）。`Referrer-Policy` は現行 `same-origin` 維持（ガイド既定の `strict-origin-when-cross-origin` より cross-origin への漏洩が少なく、外部参照ゼロなので実害なし）。

（随時追記）
