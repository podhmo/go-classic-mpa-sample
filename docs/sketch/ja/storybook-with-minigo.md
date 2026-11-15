# minigo で「sqlite を実行しない storybook 的プレビュー環境」を実現する

調査＋スパイクの記録。`podhmo/minigo`（unstable・未 publish）をローカル clone して取り込み、
`examples/storybook` と `docs/sketch/ja/experiment-storybook.md` を参考に、本リポジトリの全ページを
sqlite なし・モックデータのみで表示確認できる環境を作った。

## 動かし方

```sh
make setup-minigo   # third_party/minigo に clone（gitignore 済み・1度だけ）
make storybook      # http://127.0.0.1:6006 で storybook 的 UI が立つ
```

- `GET /` … 左ナビ（story 一覧）＋ iframe で各 story を表示。`?w=320|360|768` で iframe 幅を固定できるビューポート切替付き（モバイル確認用）
- `GET /story/{name}` … story 個別描画（毎回 minigo インタプリタが走る）
- `GET /static/` … `internal/web/static` をそのまま配信（本物の CSS/favicon）
- CLI 単発描画: `go run . <pkgdir> <StoryName>`、一覧: `-list`、静的エクスポート: `-o <dir>`

試した結果、**全 12 story（5 ページ × 複数状態 + ハンドラ同型 2 本）が描画でき、テンプレート/CSS/Go コードの編集はブラウザリロードだけで反映される**（毎リクエスト `Engine.NewSession()` で package cache が空の session を fork し、ソースを再インタプリトするため）。1 リクエストあたり warm で 70〜110ms。

> 2026-11-15 更新: minigo 側に共有パッケージ `github.com/podhmo/minigo/storybook` が入った（minigo#1068 / PR #1071–#1077）ため、`cmd/storybook` は自前の探索・描画機構を捨ててそれを利用する形に整理した。変更点は「何を作ったか」「minigo へのフィードバック」節を参照。

## 何を作ったか

```
cmd/storybook/          # ネストした Go module（go.mod に replace でローカル参照）
  main.go               # CLI/HTTP エントリポイント
  book.go               # storybook.Book の薄いラッパ + /static/ 配信 + bindApp(Setup hook)
  page.go               # index(ナビ+grid+iframe) と /story, /grid, /static のハンドラ
  book_test.go          # 探索・描画・grid・export・ヘッダ replay の回帰テスト
internal/web/
  story_pages.go        # `//go:build story` 付きの story 群（fixtures + StoryXxx 関数 + renderXxx helpers）
Makefile                # setup-minigo / storybook ターゲット
.gitignore              # /third_party/ を除外
```

2026-11-15 の更新で **`render.go` の ParseFS 回避は元に戻した**（script 側 embed.FS が bound `template.ParseFS` に橋渡しされるようになったため）。

## 方式の決定と理由

### minigo の取り込み: third_party clone + ネスト module + replace

`git submodule` / `go.work` / `vendor` / そのまま require ではなく、**gitignore した `third_party/minigo` への plain clone** を選んだ。

- minigo は未 publish でバージョンが無い。`require` + `replace` が必要になるのはどの方式でも同じ。
- **`cmd/storybook` をネスト module にした**のが本質。ルート module（アプリ本体）の `go.mod` を minigo 依存で汚さず、`go build ./...` / `go test ./...` が clone 無しでもグリーンのまま。storybook は opt-in ツール。
- submodule は「minigo のコードを書き換えてフィードバックを集める」という今回の用途ではコミット追従管理が重い。clone なら `third_party/minigo` をそのまま編集して minigo 側リポジトリとして push もできる。
- go.work はルート module 全体に影響するので避けた（`.gitignore` 済みの選択肢だが、今回は cmd/storybook の閉じた依存で十分）。

### コンポーネント = ページ × 状態

要望どおり「ページ × 状態」をコンポーネントの単位とした。`internal/web` の `renderXxx(w, status, viewModel)` を `httptest.ResponseRecorder` に向けて呼び、stdout に出す story 関数を `//go:build story` の `story_pages.go` に書く（minigo storybook と同じ形: `func StoryXxx()` が HTML を stdout に print する）。

### storybook サーバ: 共有パッケージ `minigo/storybook` + 薄い差分

当初は `examples/storybook` のハーネスを fork して自前で補っていたが、minigo#1068 のフィードバックから **story 探索（`Discover`）・2 種の story 形式・base engine + `NewSession` fork の描画ループが `github.com/podhmo/minigo/storybook` に引き上げられた**。`cmd/storybook` の `Book` は `*storybook.Book` を埋め込んだ薄いラッパになり、残っている差分は：

- **`Setup: bindApp` hook** — goldmark 系と sqlite 空 bind（後述）を base engine に一度だけ登録する場所。
- **`/static/` の実アセット配信**（`http.FileServer`）。layout が参照する `/static/app.css` を実ディレクトリから出すので CSS 編集も即反映。
- **variants grid**（`/grid/<renderFunc>`）— `Story.Group`（story 本体が最後に呼ぶ無修飾関数名）で render メソッド単位にグルーピングし、同一ページの全状態を横並び iframe で表示する。example と同じ仕組み。
- **per-request な session fork** = 「浅い探索の遅延実行インタプリタが都度実行される」形は変わらず。さらに minigo#1077 で fork 先 session が base engine の FileSet・parse キャッシュ（sha256 再検証で編集検知）・index キャッシュを共有するようになり、再解釈コストが下がった。

### bind の切り分け（sqlite を実行しない仕組み）

minigo では import をホスト側の実コードに「bind」できる。今回のキーは **何を bind して何をインタプリタ実行させるか**。

| パッケージ | 扱い | 理由 |
|---|---|---|
| `internal/web` | インタプリタ（source） | story 本体・テンプレート・embed を毎回読み直すため |
| `internal/store` | インタプリタ（source） | 従来は「bound host struct の script リテラルは bound template から見えない」制限の回避だったが、**今でもこれがゼロコストの選択**。代替として `minigo.HostTypeOf` で型ごと bind する方法ができた（後述） |
| `modernc.org/sqlite` | **空の bind** | `import _ "modernc.org/sqlite"` を解決だけさせて init（driver 登録）を走らせない。`store.Open` を story が呼ばなければ sqlite は一切実行されない |
| `html/template` 等 | minigo builtin の bound | そのまま動く（script 側構造体のフィールド評価・FuncMap 適合あり）。`ParseFS` も script 側 embed.FS を橋渡しできるようになった |
| goldmark 群 | ホスト関数 bind（plain Go 値） | `markdown.go` のパッケージ init が実物の markdown 変換を必要とするため。`Bind` は plain Go 値を受け取るので `WrapFunc` は不要になった |

```go
// modernc.org/sqlite を「解決はするが何も登録しない」空パッケージとして bind
e.Bind("modernc.org/sqlite", map[string]runtime.Value{})

// declared 名が path 末尾と違うパッケージは BindAs で指定（旧: BoundOf().Name 修正）
e.Bind("github.com/yuin/goldmark-alert", map[string]runtime.Value{
    "NewParser": alert.NewParser, // plain Go func は自動で WrapFunc される
}, minigo.BindAs("alert"))
```

### `parsePages` は `ParseFS` に戻した（2026-11-15）

当初は bound な `html/template` に `ParseFS` が無く、script 側 `fs.FS` をホスト側 reflect 経由で呼べなかったため `ReadFile`+`Parse` に変えていた。minigo#1074 で **bound `ParseFS` の実装と script 側 `fs.FS` → ホスト `fs.FS` のプロキシ（`Open` 経由、`ReadDir`/`ReadFile`/`Stat` は script 側が宣言していれば使う）が入った**ので、アプリ側のコードは元の `ParseFS` 形に復元した。

## 再起動不要性（実測済み）

storybook に追いつくための要点である「書き換えたら再起動不要」は以下の通り全て成立する（サーバ起動中に実測）:

| 編集対象 | 再起動 | 仕組み | 2026-11-15 再実測 |
|---|---|---|---|
| テンプレート (`templates/*.html`) | **不要** | embed.FS の中身はパッケージ再解釈の都度ディスクから読み直される（`ParseFS` に戻っても同じ: bound `ParseFS` が script `fs.FS` を `Open` 経由で読む） | `<h1>` 書き換え → 即反映 |
| モックデータ (`story_pages.go`) | **不要** | 同上。fixture 値を書き換えると次のリクエストで反映 | fixture のタイトル書き換え → 即反映 |
| story の追加 | **不要** | index は `b.Stories()`（= `storybook.Discover`）を毎リクエスト実行してナビを再構築する。新しい `StoryXxx` 関数は即座に一覧・描画とも有効 | `StoryZzzRuntimeCheck` 追加 → 一覧・描画とも即反映 |
| CSS・静的アセット (`static/`) | **不要** | `http.FileServer` が毎回ディスクから出すだけ（インタプリタすら通らない） | app.css 追記 → 即反映 |
| 本番コード (`web.go`, `handlers.go` 等) | **不要** | story が呼ぶ renderer もインタプリタ実行なので同じく都度再評価される | — |
| storybook 本体 (`cmd/storybook`) | **必要** | ここだけは普通の Go バイナリ。`go run` なら再起動で再コンパイル | — |

つまり storybook が要求する「書き換えたら再起動不要」は 2026-11-15 時点でも全項目成立している。minigo#1077 の共有キャッシュ（FileSet + sha256 再検証付き parseCache + indexCache）で fork あたりのコストも下がっており、warm で 70〜110ms/req（goldmark 実変換を含む詳細ページでも）。

### JS が入ってきた場合（再検証済み）

- **静的 JS ファイル**: `internal/web/static` 配下に置けば `/static/` FileServer がそのまま配信する（実測: `hot.js` → `200 text/javascript`）。CSS と同様にリロードで反映。
- **テンプレート内の inline `<script>`**: テンプレートはただのテキストとして出力されるので print story の body にそのまま出る（実測）。ブラウザでは普通に実行される — index ページ自体は CSP を付けない。
- **`StoryHttp` 経路では実 CSP が replay される**: アプリのハンドラが付ける `script-src`（現行 `'self'`、外部ファイルのみ許可）を含むヘッダがそのまま返るので、本番で塞がれる inline script はプレビューでも塞がれる。「プレビューと本番で JS の可否がずれる」問題はハンドラ同型 story で既に解消済み。
- **storybook 側の JS**: index ページは JS 無しのナビ＋iframe + variants grid で足りている。本物の storybook のような「props を UI でいじる」機能を足すならここに JS を書くことになる（future work）。

## ハンドラ同型 story（`StoryHttp` prefix）

「story が Response を返せるようにすれば status code や header も取れる。いっその事 `http.Handler` と同型にする」という発想で追加した2つ目の story 形式。

- **宣言**: `StoryHttp` で始まる名前の func は `func(http.ResponseWriter, *http.Request)` 型を必須とする（AST で構造的に判定するので import alias 不問）。それ以外の `Story` func は従来どおり `func()`。
- **実行**: `Render` は `httptest.NewRecorder` + `httptest.NewRequest(GET /)` をホスト側で作り `s.Call(ctx, pkg, name, rec, req)` で story に渡す。host の Go オブジェクトは `vm.SnapshotOf` 経由でそのまま script に見えるので、`*httptest.ResponseRecorder` を script から書き込める。
- **返却**: 戻り値は `*Page{Status, Header, Body}`。`/story/<Name>` が recorder の status・headers をそのまま replay するので、story が実 mux（`web.New(storyStore{})`）を通せば CSP・`Referrer-Policy`・`Speculation-Rules` など実ハンドラのヘッダや 404 等の非 200 状態までプレビューできる。print 型 story は従来どおり常に 200。
- **fixture**: `Store` interface の script 側 fake（`storyStore`）を実装して mux に渡す。`web`・`store` パッケージは interpreted なので、script 内で定義した struct が interface をそのまま満たす。
- **実測**: `StoryHttpIssueList`（実 mux → `GET /issues`）は 200 + CSP/X-Content-Type-Options/nosniff 等つき、`StoryHttpNotFound` は 404 + styled body を返すことを確認。

print 型と使い分け: **見た目（マークアップ）の確認は print 型**、**ヘッダ・status・ルーティングを含めた検証は `StoryHttp` 型**と使い分ける。

## 見つかったこと（minigo へのフィードバック素材）

### 事実として確認できたこと

- `//go:build story` で隔離したファイル群が tag 指定 (`WithBuildConfig{Tags: ["story"]}`) でちゃんと解釈される。
- story 関数の探索は例のパターン通り動く（`StoryXxx` + 引数なし func）。
- script 側構造体のフィールドは bound `html/template` から評価できる（`.Issue.ID` 等）。
- script で定義した FuncMap 関数（`formatTime` 等）も bound template 内で呼べる（`vm.RegisterFuncMapType` による型適合）。
- `//go:embed` は解釈モードでも動き、**ファイル内容はパッケージ再解釈の都度読み直される**（テンプレート編集→リロード反映を実測）。
- `httptest.ResponseRecorder` は bound 済みでそのまま使えた。
- キャッシュ問題（「パッケージのキャッシュが期待通りにリセットされない」）は**観測されなかった**。per-request session なら 100〜200ms/req で十分速く、より大きなパッケージでキャッシュが欲しくなって初めて問題になると思われる。なお未使用だった minigo の機能として `WithInitMode(LazyInit)`・`WithPackageModes` の ModeSource/ModeDeny（sourceModes は使用済み）・`inspect`・`BoundSymbols`・`WithHostPolicy`・`ScriptProfiler` がある（発見時の記録は html-sandbox の slops 記事に記載）。

### 明らかになった制限 → minigo 本家で全て解決済み（PR #1071–#1077）

当時挙げた制限は全て minigo 側に取り込まれた。対応関係：

| 当時の制限 | 解決策（現在の形） |
|---|---|
| 1. `Bind` が package 名を path 末尾から取る | **`minigo.BindAs("alert")`** で宣言名を指定。`BoundOf().Name` 修正は不要になった |
| 2. bound host struct の script リテラルが opaque | **`minigo.HostTypeOf(store.Issue{})`** で型を bind すると `store.Issue{...}` が実 host struct を mint する。internal/store は今でも source 解釈のまま（それも supported で変更不要）だが、bind に移す道はできた |
| 3. `ParseFS` 不在 + script `fs.FS` が渡せない | **bound `ParseFS` + scriptFS ブリッジ**。アプリ側は `ParseFS` に復帰（`fs.GlobFS`/`SubFS` 等は未橋渡し、必要になればその時に） |
| 4. bind は手動列挙 | **plain Go 値をそのまま `map[string]runtime.Value` に書ける**（func は自動 WrapFunc）+ パッケージ丸ごとは `minigo gen-intrinsics` で生成可能。今回は 8 個なので手書きのまま |
| 5. 存在しないフィールドが静かに空になる | **bound template が `can't evaluate field Nonexistent in type ...` でエラーを返す**ようになった。実測: テンプレートに `{{.Nonexistent}}` を足すと `printRec` のガード経由で story が 500 で落ち、typo が検知できる |
| （探索機構の再実装） | **`github.com/podhmo/minigo/storybook`** として共有化。`Story.Group` で render 関数 grouping、`Setup` hook でアプリ固有 bind |
| （per-request 再解釈コスト） | **session fork の共有キャッシュ**（FileSet・parseCache・indexCache）。ベンチでは数倍の高速化 |

### 今後試したいこと（future work）

- **コンポーネント分割への拡張**: story の単位を「ページ」から部品（フィルタバー、issue-row 等）へ。`define` で部品化したテンプレートを部品単位で render する方法の検討。
- **viewport/テーマの一括比較**: `/grid/` で variants 横並びは実現した。残るのは viewport（`?w=` は grid にも効く）と `prefers-color-scheme` 系のテーマ切替（iframe 単位で切り替えられないので、パラメータか wrapper で）。
- **story の自動発掘**: 現状は `//go:build story` ファイルを手で書いている。テンプレートと 1:1 の story を生成する lint/雛形コマンド。
- **`minigo gen-intrinsics` / `HostTypeOf` の活用**: goldmark の 8 個 bind を生成コードに置き換える、internal/store を bind 側へ移して script リテラルが実 host struct を mint する形にする、等はできるようになったが必要性が出てから。

なお以前ここに挙げていた「minigo 側を書き換える実験」「ヘッダ込みの検証（StoryHttp 型）」はどちらも実現済み。

## モバイル対応の検証（この環境でやったこと）

`?w=` のビューポート切替で全 10 story を 360px / 320px で目視確認した。フィルタバーの縦積み・ラベル折返し・ボタン全幅化・長いタイトルのクランプは先行の responsive 対応で済んでおり、WCAG 1.4.10 の 320px 基準でも横スクロール・はみ出しは検出されなかった。モバイル向けの試行錯誤自体はこの環境で十分回る — 残る課題は viewport やテーマの一括比較（下記 future work）。

## 部分更新・JS 挙動の検証経路（2026-11-15）

`partial-updates.md` の部分更新案（手書き fetch + HTML fragment）に向けて、
「どの層の確認が storybook で回るか」を整理した。

### 現状のまま回るもの

- **fragment の見た目**: `{{define "comment"}}` 等の部品テンプレートを print 型
  story で出せば、新 `<li>`・エラー時フォームの見た目は従来どおり回る。
- **POST ハンドラの意味論**: `StoryHttp*` story は `func(w, r)` 型だが、**渡される
  `r`（minigo storybook が合成する `GET /`）を無視して、story 内で
  `httptest.NewRequest(POST, "/issues/1/comments", strings.NewReader(form))` を自前で
  組み立てればよい**。これで POST → fragment / 303 / 422 の応答・ヘッダ（CSP 含む）
  が storybook で確認できる。`partial-updates.md` 初回版の「StoryHttp story は POST
  ハンドラを呼べない」は不正確だった — 呼べるのは外側のリクエストではなく story 自身が
  組み立てた request であり、handler の意味論検証にはそれで足りる。

### 現状では回らないもの（= 改善対象）

「ブラウザが submit を奪って fetch を飛ばし、返ってきた fragment で DOM を差し替える」
という一連の JS 挙動は storybook では確認できない:

- storybook の HTTP mux は `GET /story/<name>`・`/grid/`・`/static/` しか持たず、
  ページ内 JS が `fetch("/issues/1/comments", {method: "POST"})` しても宛先ルートが無い。
- `storybook.Book.Render` は story に渡す request を `httptest.NewRequest(GET /)` で
  **固定合成**するので、外側の実リクエスト（メソッド・パス・ボディ）を story に
  届ける経路が無い。
- storyStore は script 側 fake で、**Render が毎リクエスト session を fork するため
  script 状態はリクエストごとにリセットされる**。POST → GET の跨いだ状態遷移は
  script 内の fake では再現できない。

### 改善案

1. **Tier 2 の活用（コード変更不要・すぐできる）**: POST を受ける handler story を
   「story 内で POST request を組み立てる」形で追加する。部分更新実装時の最初の
   story として `StoryHttpCreateComment`（POST → fragment or 303）・
   `StoryHttpCreateCommentError`（POST → 422 fragment）を置く。これで
   「サーバが正しい fragment を返す」は storybook で固定できる。
2. **upstream に request パススルーを足す（真の解消）**: minigo/storybook に
   「外側の `*http.Request` を story に渡す」経路（例: `RenderRequest(ctx, name, w, r)`
   や `Render` の request 注入オプション）を追加し、cmd/storybook の mux に
   catch-all を足して `web.New(storyStore)` を実パスに mount する。すると storybook が
   **「DB なしの実アプリ」として丸ごと動き**、ページ内 JS の fetch → fragment →
   DOM swap まで実ブラウザで検証できるようになる。page.go の薄い差分方針と
   整合するので upstream PR としてフィードバックする想定（いつもの経路）。
3. **状態を跨ぐ検証には host 側 fake が要る**: パススルーを入れても script 側
   storyStore はリクエストごとにリセットされる。POST で追加したコメントが続く GET で
   見えることを検証するなら、`Setup` hook（bindApp）で **host 側の stateful な
   in-memory fake store** を bind する必要がある（`minigo.HostTypeOf` で型ごと
   bind する道は既にある — story_pages.go の storyStore を host Go 側に移す設計論点）。
   単発の fragment 応答確認なら stateless fake のままで足りる。
4. **分担は変えない**: end-to-end（実 SQLite・実 PRG・view transition 横断）は
   引き続き verify-ui（`go run .` + playwright-cli）。storybook app-mount は
   「fake で速く決定的に JS 挙動を回す」層として補完する。

## 実測メモ（2026-11-15 更新）

- 12 story 全て HTML 出力 OK（一覧・空・ページング・詳細・コメントエラー・フォーム new/errors/edit・削除・404 + handler 型 2 本）。
- 詳細ページでは goldmark の GFM task list・goldmark-alert callout も実物変換で描画されている。
- story 描画は warm で ~70-110ms/req（従来 ~100-200ms/req より低下。共有キャッシュの効果）。`/static/` は FileServer なので ~1ms。
- variants grid: `/grid/renderIssueList` 等、render メソッド単位で全状態を横並び表示。`?w=` の幅固定も効く。
- `go test ./...` `go vet ./...` `gofmt -l .` はルート module で全グリーン。`cmd/storybook` 側は `cd cmd/storybook && go test ./...` で探索・描画・grid・export・ヘッダ replay の回帰テストが通る（~2s）。
