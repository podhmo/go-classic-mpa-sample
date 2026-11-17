# 落とし穴・ハマりどころ（Pitfalls）

このドキュメントは実装中に遭遇した（あるいは事前に把握した）落とし穴を記録する。

## 環境・作業開始時

- **`prompts.md` はリポジトリルートではなく `docs/ja/prompts.md` にあった**。タスク仕様書の場所は先に `find` で確認する。
- **Go 1.27 の確認**: 要求は Go 1.27+。環境は go1.27.3 で問題なし。`http.ServeMux` のメソッド付きパターン（Go 1.22+）と `http.NewCrossOriginProtection`（Go 1.25+）が使える。

## SQLite / database/sql

- **`:memory:` DB は `database/sql` のコネクションプールで壊れる**: `sql.Open("sqlite", ":memory:")` では接続ごとに別の空DBが作られるため、スキーマ初期化した接続とクエリの接続が別物になり `no such table` になる。対策: memory DSN を `file:<name>?mode=memory&cache=shared` にする、または `db.SetMaxOpenConns(1)`（`SetMaxIdleConns` も併用してアイドル時にDBが破棄されないようにする）。store 側で検出して処理する方針。
- **外部キーは PRAGMA 必須**: SQLite はデフォルトで FK 無効。`PRAGMA foreign_keys = ON` を接続ごとに有効化しないと `ON DELETE CASCADE` が効かない。DSN の `_pragma` オプション（modernc.org/sqlite は `_pragma=foreign_keys(1)` 等が使える）か、接続後に PRAGMA を発行する。単に `db.Exec("PRAGMA foreign_keys=ON")` だけだとプール内の他接続に効かない点に注意 → DSN 指定が確実。
- **`AUTOINCREMENT` を付けないと id が再利用される**: 通常の `INTEGER PRIMARY KEY` は最大id削除後に再利用されうる。Issue番号の再利用禁止要件のため `AUTOINCREMENT` 必須。
- **`CreateComment` の FK 違反検知**: 存在しない issue へのコメント挿入は FK エラーになるが、ドライバのエラー型判定に依存するより先に `SELECT 1` で存在確認して `ErrNotFound` を返すほうが素直。
- **N+1 回避**: 一覧で各 issue のラベルを取る際、1 issue 1 クエリにしない。issue_id 単位のグループ化クエリ1本でまとめて取得する。

## net/http・Web

- **CSRF は標準 `http.NewCrossOriginProtection()` で**: Go 1.25+。Sec-Fetch-Site/Origin ヘッダベースのため、テストでは `Sec-Fetch-Site: cross-site` を付けたPOSTが403になることを確認すればよい。トークン方式を自前実装する必要はない（ローカルデモの範囲では）。
- **HTMLフォームは GET/POST しか出せない**: PUT/PATCH/DELETE は使わず、更新・削除・状態切替はすべて POST + PRG（303）に寄せる。
- **削除確認ページ**: `GET /issues/{id}/delete` は確認画面の表示のみ。GET で削除しない。
- **非数値の `{id}`**: `strconv.ParseInt` 失敗は 400 ではなく 404 寄りで扱う（仕様: 存在しない Issue は 404）。
- **ページングの total**: `LIMIT/OFFSET` の結果とは別に同じ WHERE で `COUNT(*)` を取らないと総ページ数が出せない。
- **`html/template` は自動エスケープされるが油断しない**: `<script>` を含むタイトルが `&lt;script&gt;` で出力されることをテストで担保する。
- **本文の改行**: プレーンテキストをそのまま出すと改行が潰れる。`white-space: pre-wrap` をCSSで指定する。

## 並行作業（サブエージェント分割）

- **契約未固定だと並行実装が破綻する**: web層がstore実装を待つと逐次になる。先にstore APIの型・シグネチャ・意味論をスケルトンとしてコミットし、web側は consumer-side interface + fake でテストする構成にした。
- **worktree はリポジトリ内 `.worktrees/` に置く**: サンドボックスの権限範囲をまたがないため。`.gitignore` への追加を忘れないこと。

## Markdown（goldmark）

- **v1 と v2 で API が別物**: v1 は `goldmark.New(WithExtensions(...)).Convert()` の facade、v2 は `parser.New(parser.WithExtensions(...))` と `html.New(html.WithExtensions(...))` を個別に組み、`p.Parse(src)` → `r.Render(&buf, src, doc)` と手動で繋ぐ。拡張も `extension.NewGFMParser()` / `NewGFMHTMLRenderer()` のように parser 側・renderer 側に分離している。サードパーティ拡張を選ぶときは対応メジャーを確認すること（`yuin/goldmark-alert` は v2 専用、`zmtcreative/gm-alert-callouts` は v1 用）。
- **生 HTML は「エスケープ表示」ではなく「コメントに置換」される**: `html.WithUnsafe` なしのデフォルトでは `<script>` などの生 HTML が出力上 `<!-- raw HTML omitted -->` になる。ユーザーが `<details>` などを書いても表示されず消える — 「見えない」挙動なのでテストでは「生タグが出ない」ことだけを assert する。`<b>` のようなインライン HTML でもタグだけが omitted コメントになり中身のテキストは残る。
- **危険スキームは goldmark が潰す**: `[x](javascript:...)` → `href=""`、`![i](javascript:...)` → `src=""`。`data:` も同様。別 sanitizer は不要だった（v1.8.7 と v2.1.15 で実測確認）。
- **`template.HTML` を返す箇所は最小化**: `renderMarkdown` だけが HTML を生成し、テンプレート側は `{{markdown .Body}}` と書くだけ。フォーム再表示用の生テキスト（`CommentDraft`・`Input.Body`）は markdown を通さないこと — 二重変換・エスケープ崩れのもと。
- **`pre-wrap` を外し忘れると段落間が二重に空く**: markdown 出力は `<p>` 等のブロック要素なので、旧来の `white-space: pre-wrap` を残すと `<!-- raw HTML omitted -->` コメント由来の空行含め見た目が崩れる。`.markdown-body` にスタイルを集約した。
- **goldmark-alert の SVG アイコンは `fill` 未指定**: `GithubAlertIcons` の SVG に fill 属性がないため `fill: currentColor` を CSS で与えないとアイコンだけ黒になる。タイトル文字色と揃えるのが GitHub 流。

## 実際にハマったところ

- **`go mod tidy` は未使用依存を落とす**: スケルトン時点で `go get modernc.org/sqlite` しても、スタブが import していないため `tidy` で require から消える。実装マージ後に再度 `go mod tidy` すれば戻るので実害はないが、「依存をピン留めしたつもり」にならないよう注意。
- **`exec` の `&` + `kill %1` は呼び出しをまたぐと効かない**: バックグラウンド起動したサーバーを別コマンド呼び出しで `kill %1` しようとしてもジョブ参照が切れ、プロセスが残ってポートバインド失敗（`bind: address already in use`）になった。対策: `pkill -f <pattern>` や `lsof -ti:<port> | xargs kill` で確実に殺す。
- **検証結果の見誤り**: 上記の残存プロセスが応答し続けたため「再起動後の永続化確認」のつもりが旧プロセスへのリクエストになっていた。再起動検証では必ず `pgrep`/`lsof` で旧プロセスの死を確認してから結果を信用する。
- **`mux.HandleFunc("/", h)` は全メソッドの405を飲み込む**: Go 1.22+ mux で、メソッド無指定の `"/"` キャッチオールを登録すると PUT/DELETE 等の「パスは存在するがメソッドが違う」リクエストまで catch-all に流れ、自動405（`Allow` ヘッダ付き）が出なくなる。styled 404 を返したい場合は `GET /` と `POST /` を別々に登録し、メソッド不一致は mux の 405 に任せるのが正解。実装時は「404 で返っても安全」に見えるが仕様（不正メソッドは405）を崩すので要注意。
- **`UPDATE ... SET col=CASE WHEN...` の句順**: `SetIssueStatus` で `status=?` を先に書くと、SQLite は代入前の値で CASE を評価するが、DB/文法依存の心配を避けるため `updated_at=CASE WHEN status != ?` を `status` 代入より先に置く書き方にした（防御的記述）。
- **`git add -A` は無関係な大きなファイルを拾う**: 作業ディレクトリに残っていた 129MB の `playwright-cli` バイナリを誤ってコミットしかけた（amend で除去）。`.gitignore` されていない実行バイナリ・スクラッチはコミット前に `git status` で目視確認する。
- **HTML5 `required` はサーバー側バリデーションを無効化する**: 「バリデーションはサーバー側で」という要件のフォームに素の `required` を付けると、ブラウザが送信自体をブロックしてネイティブバブルを出し、設計した 422+インラインエラーの UI に到達しない。curl/httptest では絶対に見えないバグ — **実ブラウザでの手動・自動検証が必須**。対策は `<form novalidate>`（`required` 属性は aria 的な意味として残してよい）。
- **`go run` を pkill しても子プロセスが残る**: `go run ./cmd/zatudan` は親が go toolchain、実際にポートを握るのは `go-build/.../exe/` 配下の子バイナリ。`pkill -f 'go run'` は親しか殺さず、旧バージョンのサーバーが応答し続ける。`lsof -ti:<port>` で実際の PID を殺すのが確実（二度目の遭遇）。
- **embed.FS の変更はプロセス再起動が必要**: テンプレート/CSS を embed しているため、`go run` 中に HTML を直しても反映されない。ブラウザ検証では「今動いているバイナリが最新か」をまず確認する（今回は旧サーバーが `required` 付きテンプレートを返し続けていた）。
- **`html/template` は `+` を `&#43;` にエスケープする**: UTF-7 XSS 対策のため。`"10,000+ records"` のような文字列はレンダリング後に verbatim で現れない — テストでタイトル文字列をそのまま assert すると落ちる。テストではエスケープされない部分文字列で assert する。
- **`var()` を含む CSS 宣言は「後勝ちフォールバック」にならない**: `color: #000; color: contrast-color(var(--bg));` と書いても、`var()` を含む宣言は parse 時には常に有効扱いされ、未対応関数だと計算値時点で無効（IACVT）→ `unset`（`color` なら親から継承）になり、前の `#000` には戻らない。新しい色関数を custom property と組み合わせるときは `@supports (color: contrast-color(red))` のように宣言ごと囲う。
- **出荷済みの Blink 機能は `--disable-blink-features` で無効化できない**: Invoker Commands / `sibling-index()` 非対応ブラウザを Chromium で再現しようとフラグを渡しても、ランタイムフラグが既に削除されていて効かない（Chrome 156）。CSS の `@supports` 分岐を確かめるなら、Playwright の `page.route` で `app.css` を差し替え、条件の関数名を未知の名前に書き換えるほうが確実。
- **form 内の `<button commandfor>` は `type="button"` を明示する**: form owner があると `type="submit"` は command より送信が優先され、`type` 省略は Chrome 156 では送信も command もしない（コンソールに "Buttons associated with forms that include command or commandfor attributes are ambiguous, and require a type=button attribute. No action will be taken."）。「非対応ブラウザでは submit、対応ブラウザでは command」という type 省略の二刀流は成立しない。
- **Playwright の `selectOption` では `:user-invalid` が立たない**: `fill` で空にしたテキスト欄は `:user-invalid` になるが、`<select>` に `selectOption` で値を入れ直しても Chromium 156 ではマッチしなかった（ユーザー操作扱いにならない）。select の検証表示を確かめるときは `focus` → `ArrowDown`/`ArrowUp` → `Tab` のキーボード操作で行う。
- **フォーム送信直後のスクリーンショットは view transition の途中を撮る**: POST→422 のページ表示にもクロスフェード（0.18s）がかかるため、`waitForLoadState()` 直後だと新旧ページが重なった画像になる。数百 ms 待ってから撮る。
- **`page.goto` では cross-document view transition が発火しない**: `pagereveal` の `e.viewTransition` で検証する場合、Playwright の `page.goto` 遷移では viewTransition が falsy のまま（pagereveal 自体は来る）。リンクの `page.click` 遷移では true になる。「発火しない = 壊れている」と読み違えないこと（CSP 導入時に一回誤診しかけた）。
- **CSP 導入後のブラウザ検証は「意図したブロック」と「壊れ」を分けて読む**: `script-src 'none'` でも Playwright の `page.evaluate` は動く（CDP/隔離ワールド経由）が、eval で注入した `<style>`/`<img>` のリソース処理はページの CSP に従う。注入物がブロックされること自体が CSP 有効の証拠になる — コンソールの違反ログは必ずしもバグではない。

- **`<fieldset>` は既定で `min-inline-size: min-content`**: 中身（例: `white-space: nowrap` のラベルバッジ）が長いと fieldset が親より広がり、ページ全体が横スクロールする。ellipsis で切り詰めたい要素を fieldset に入れるときは fieldset に `min-inline-size: 0` を付ける（`.label-picker`）。
- **`.field > label { display: block }` が `.label-option` に勝つ**: ラベル選択の `<label class="label-option">` は `.field` 直下なので、`.label-option { display: … }` は詳細度で負ける。`.label-picker > .label-option` のように詳細度を上げる。flex にした checkbox は縮むので `flex: none` も要る。
- **ブラウザの既定文字サイズを変えた確認は CDP の `Page.setFontSizes` で**: Playwright にはブラウザの「文字サイズ」設定を変えるオプションが無く、ページズーム（`deviceScaleFactor`）では rem/px の差が出ない。`context.newCDPSession(page)` で `Page.setFontSizes({ fontSizes: { standard: 24 } })` を送ってから遷移すると、rem は追従し px は追従しないことを確認できる。`<meta name="text-scale">` 自体の OS 連動は VM では再現できないので、この rem 連動で代理確認する。

- **`content-visibility: auto` のスキップ判定は要素自身ではなく子で見る**: `el.checkVisibility({ contentVisibilityAuto: true })` は、`content-visibility: auto` を持つ要素自身に対しては（中身がスキップ中でも）`true` を返す。スキップされるのは中身なので、子要素（`.comment-body`）に対して呼ぶ。
- **スキップ中の中身は CDP の AX ツリーに出ない / Playwright の `ariaSnapshot` は DOM ベース**: `Accessibility.getFullAXTree` ではスキップ中の要素は子なし、中身は `ignored`（`notRendered`）。一方 `locator.ariaSnapshot()` は DOM から組み立てるのでスキップ中でも全文が出て、ブラウザの AX ツリーの確認にはならない。`Accessibility.getPartialAXTree` は `fetchRelatives: false` だと子を返さないので、子の有無を見るなら `true` にする。
- **`content-visibility` の効果測定は CDP `Performance.getMetrics` の `LayoutDuration` / `RecalcStyleDuration`**: `Emulation.setCPUThrottlingRate` で 4x にし、`page.route` で `app.css` のルールを外した版と比べる。件数による損益分岐はスレッドの HTML を `page.route` で切り詰めると DB を作り直さずに測れる。
- **inline の `<script type="speculationrules">` は `script-src` の対象**: 「実行されない JSON だから CSP の外」という予想に反し、Chromium 156 は inline speculation rules を `script-src 'none'` でブロックする（"Applying inline speculation rules violates ... 'script-src 'none''"）。`Speculation-Rules: "<url>"` レスポンスヘッダー + 外部 JSON ruleset にすると `<script>` 要素自体が要らず共存できる。ヘッダー値は引用符付き sf-string が必須（無引用だと "Cannot parse" 警告）、外部 ruleset は `Content-Type: application/speculationrules+json` が必須（`application/json` では無視される）。
- **Chrome は CSP `prefetch-src` を未実装**: 指定しても "Unrecognized Content-Security-Policy directive" と警告されるだけ。投機 fetch を CSP で絞る術は現状無いので、speculation rules の `where.selector_matches` で対象リンクを絞るのが実質の防御線になる。
- **投機的読み込みは Playwright の `requests` / Network ドメインに出ない**: ブラウザプロセス発のフェッチなのでページ側からは観測不能。サーバー側で `Sec-Purpose: prefetch` / `Sec-Speculation-Tags` ヘッダーを見るのが確実（このアプリはリクエストログが無いので標準ライブラリのロギングプロキシを `:8081` に立てて観測した）。hover による `moderate` eagerness の発火は Playwright の `hover`（CDP 入力）で起きる。

- **`Document.parseHTML` は既定 Sanitizer を通す**: `Document.parseHTML(html)` で作った DOM は `class`/`id`（と `data-*` 系）が削られる（Chromium 156 実測 — `<li class="card comment" id="x">` が `<li>` になる）。同一オリジンのサーバー fragment なら `<template>` innerHTML でパースするのが正解（属性を保持し `<script>` も不活性のまま）。「safe parser だから使おう」とすると静かに属性が消えるので注意。
- **fetch の body に `FormData` をそのまま渡すと multipart になる**: 通常フォーム送信は `application/x-www-form-urlencoded` なので、JS 側だけ multipart だとサーバー側が「もう1つのパーサ経路」を持つことになる。`new URLSearchParams(new FormData(form))` で urlencoded に揃える。
- **minigo: `s.Call` の引数に渡した host `*http.Request` はフィールド代入不可**（minigo#1085 で修正済み）: storybook の app mount で受け取った `*http.Request` をそのまま解釈コード（`ServeMux` の内部で `r.pat` 等を書く）に渡すと `host value *http.Request has no settable field pat` で失敗していた。修正後は host フィールドが宣言パッケージ限定で書き込み可（gc の公開性ルールと同じ）になり、`StoryHttpApp` は再構築せず `r` をそのまま `ServeHTTP` に渡す。
- **minigo: 解釈実行の `httptest.NewRequest` は `bytes.Reader` body を解決できない**（minigo#1085 で修正済み）: 非 nil body を渡すと型 switch が `bytes.Reader` を引けず `undefined: bytes.Reader` だった。`bytes.Reader` が binding されて解消し、`storyPost` は実 body（`strings.NewReader`）を渡す形に戻した。
- **「multipart の `ParseMultipartForm` が空を返す」は minigo のバグではなく標準 net/http 挙動だった**: `parseForm` が `r.ParseForm()` を無条件に呼ぶと `r.Form` が非 nil になり、`PostFormValue` が `ParseMultipartForm` を起動しないため multipart のフィールドが全て `""` になる — **コンパイル済みバイナリでも同じ**。対策は `parseForm` で Content-Type を見て multipart なら `r.ParseMultipartForm` を呼ぶこと（bugs.md #7、minigo#1084 の調査で判明）。
（随時追記）
- **SQLite で FK CASCADE 付きの親テーブルを `DROP TABLE` すると子が全滅する**: `DROP TABLE` は暗黙 `DELETE` を発行し、子テーブルの `REFERENCES ... ON DELETE CASCADE` が発火する（migration 内でも同じ）。テーブル再構築（`_new` → `DROP` → `RENAME`）で親を作り直すときは `PRAGMA foreign_keys=OFF` を効かせる必要があるが、この PRAGMA はトランザクション内では no-op。接続専有 (`db.Conn`) → conn 上で OFF → `conn.BeginTx` → `pragma_foreign_key_check` で検証 → ON が正攻法（`internal/store/migrateWithFKOff` 参照、version 単位で `rebuildNeedsFKOff` に登録）。
