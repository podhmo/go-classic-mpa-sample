# minigo で「sqlite を実行しない storybook 的プレビュー環境」を実現する

調査＋スパイクの記録。`podhmo/minigo`（unstable・未 publish）をローカル clone して取り込み、
`examples/storybook` と `docs/sketch/ja/experiment-storybook.md` を参考に、本リポジトリの全ページを
sqlite なし・モックデータのみで表示確認できる環境を作った。

## 動かし方

```sh
make setup-minigo   # third_party/minigo に clone（gitignore 済み・1度だけ）
make storybook      # http://127.0.0.1:6006 で storybook 的 UI が立つ
```

- `GET /` … 左ナビ（story 一覧）＋ iframe で各 story を表示
- `GET /story/{name}` … story 個別描画（毎回 minigo インタプリタが走る）
- `GET /static/` … `internal/web/static` をそのまま配信（本物の CSS/favicon）
- CLI 単発描画: `go run . <pkgdir> <StoryName>`、一覧: `-list`、静的エクスポート: `-o <dir>`

試した結果、**全 10 story（5 ページ × 複数状態）が描画でき、テンプレート/CSS の編集はブラウザリロードだけで反映される**（毎リクエスト新規エンジンで再インタプリトするため）。1 リクエストあたり 100〜150ms。

## 何を作ったか

```
cmd/storybook/          # ネストした Go module（go.mod に replace でローカル参照）
  main.go               # CLI/HTTP エントリポイント
  book.go               # story 探索 + エンジン生成 + minigo への bind
  page.go               # index(ナビ+iframe) と /story, /static のハンドラ
internal/web/
  render.go             # parsePages を修正（ParseFS → ReadFile+Parse）
  story_pages.go        # `//go:build story` 付きの story 群（fixtures + StoryXxx 関数）
Makefile                # setup-minigo / storybook ターゲット
.gitignore              # /third_party/ を除外
```

## 方式の決定と理由

### minigo の取り込み: third_party clone + ネスト module + replace

`git submodule` / `go.work` / `vendor` / そのまま require ではなく、**gitignore した `third_party/minigo` への plain clone** を選んだ。

- minigo は未 publish でバージョンが無い。`require` + `replace` が必要になるのはどの方式でも同じ。
- **`cmd/storybook` をネスト module にした**のが本質。ルート module（アプリ本体）の `go.mod` を minigo 依存で汚さず、`go build ./...` / `go test ./...` が clone 無しでもグリーンのまま。storybook は opt-in ツール。
- submodule は「minigo のコードを書き換えてフィードバックを集める」という今回の用途ではコミット追従管理が重い。clone なら `third_party/minigo` をそのまま編集して minigo 側リポジトリとして push もできる。
- go.work はルート module 全体に影響するので避けた（`.gitignore` 済みの選択肢だが、今回は cmd/storybook の閉じた依存で十分）。

### コンポーネント = ページ × 状態

要望どおり「ページ × 状態」をコンポーネントの単位とした。`internal/web` の `renderXxx(w, status, viewModel)` を `httptest.ResponseRecorder` に向けて呼び、stdout に出す story 関数を `//go:build story` の `story_pages.go` に書く（minigo storybook と同じ形: `func StoryXxx()` が HTML を stdout に print する）。

### storybook サーバ自体は自前実装

minigo 側の `examples/storybook` は example 内のハーネスで、外部パッケージ向けに整備された CLI はまだ無い。`cmd/storybook` はそのハーネスをこのリポジトリ用に移植したもので、以下を自前で補った：

- **`/static/` の実アセット配信**（`http.FileServer`）。example は静的ファイルを持たない。layout が参照する `/static/app.css` を実ディレクトリから出すので CSS 編集も即反映。
- **index ページ（ナビ＋iframe）** は example と同型。
- **engine の per-request 生成** = 「浅い探索の遅延実行インタプリタが都度実行される」形。`b.Render` 毎に `minigo.NewEngine` を作るので、ソース・テンプレート・embed・story の全てが最新のファイル内容で解釈される（要望のキャッシュ懸念はこの方式では起きない＝実測済み）。

### bind の切り分け（sqlite を実行しない仕組み）

minigo では import をホスト側の実コードに「bind」できる。今回のキーは **何を bind して何をインタプリタ実行させるか**。

| パッケージ | 扱い | 理由 |
|---|---|---|
| `internal/web` | インタプリタ（source） | story 本体・テンプレート・embed を毎回読み直すため |
| `internal/store` | インタプリタ（source） | story が作る `store.Issue{...}` 等を「script 側構造体」にするため（後述の制限） |
| `modernc.org/sqlite` | **空の bind** | `import _ "modernc.org/sqlite"` を解決だけさせて init（driver 登録）を走らせない。`store.Open` を story が呼ばなければ sqlite は一切実行されない |
| `html/template` 等 | minigo builtin の bound | そのまま動く（script 側構造体のフィールド評価・FuncMap 適合あり） |
| goldmark 群 | ホスト関数 bind（`WrapFunc`） | `markdown.go` のパッケージ init が実物の markdown 変換を必要とするため |

```go
// modernc.org/sqlite を「解決はするが何も登録しない」空パッケージとして bind
e.Bind("modernc.org/sqlite", map[string]runtime.Value{})
```

### `parsePages` のリファクタ（ParseFS → ReadFile + Parse）

bound な `html/template` には `ParseFS` が無く、仮にあっても script 側の `fs.FS`（解釈された embed.FS）をホスト側 reflect 経由で呼べない（`io/fs` が source 解釈に留まる minigo の境界）。そこで `parsePages` は `templateFS.ReadFile` + `t.Parse(string(src))` に変更した。挙動は同一で、本番コードパスもこの関数を通るのでテスト済み。

## 見つかったこと（minigo へのフィードバック素材）

### 事実として確認できたこと

- `//go:build story` で隔離したファイル群が tag 指定 (`WithBuildConfig{Tags: ["story"]}`) でちゃんと解釈される。
- story 関数の探索は例のパターン通り動く（`StoryXxx` + 引数なし func）。
- script 側構造体のフィールドは bound `html/template` から評価できる（`.Issue.ID` 等）。
- script で定義した FuncMap 関数（`formatTime` 等）も bound template 内で呼べる（`vm.RegisterFuncMapType` による型適合）。
- `//go:embed` は解釈モードでも動き、**ファイル内容はパッケージ再解釈の都度読み直される**（テンプレート編集→リロード反映を実測）。
- `httptest.ResponseRecorder` は bound 済みでそのまま使えた。
- キャッシュ問題（「パッケージのキャッシュが期待通りにリセットされない」）は**観測されなかった**。per-request 新規 engine なら 100〜150ms/req で十分速く、より大きなパッケージでキャッシュが欲しくなって初めて問題になると思われる。

### 明らかになった制限（ドキュメント化か改善を要望したい点）

1. **`Bind` が package 名を import path 末尾要素から取る**。`github.com/yuin/goldmark-alert` の declared name は `alert` なのに `goldmark-alert` という名前の package になり、script 側の `alert.NewParser` が `undefined: alert` になる。回避: `e.BoundOf(path).Name = "alert"`（API はあるが発見しにくい）。`Bind` 時に宣言名を渡す/自動解決する仕組みがあると良い。
2. **bound host struct の script リテラルは script 側構造体になり、bound `html/template`（ホスト reflect）からフィールドを読めない**。`store.Issue{...}` を script で書くと `can't evaluate field ID in type interface {}` で死ぬ。回避: その型を提供するパッケージを source 解釈して script 構造体のままにする（今回は internal/store を解釈）。「bind したパッケージの型は値の受け渡し専用で、リテラル生成＋template 評価は非対応」との認識でいるが、正しい使い方のガイドがあると助かる。
3. **bound `html/template`/`text/template` に `ParseFS` が無い**。加えて script 側 `fs.FS`（embed.FS）を bound 関数に渡してもホスト reflect から呼べないので、単に ParseFS を追加するだけでは不十分。`ReadFile`+`Parse` への書き換えで回避したが、`fs.FS` 経路を越えて bind する何らかのブリッジが欲しい。
4. **bind はシンボル単位の手動列挙**。goldmark でも 8 個程度だが、1 パッケージ丸ごと bind する helper（あるいは example 流儀のベストプラクティス）が欲しい。

### 今後試したいこと（future work）

- **コンポーネント分割への拡張**: story の単位を「ページ」から部品（フィルタバー、issue-row 等）へ。`define` で部品化したテンプレートを部品単位で render する方法の検討。
- **viewport/テーマの一括比較**: 狭幅 360px・ダークモードなどの state を story 名で切り替える仕組み（`prefers-color-scheme` は iframe 単位で切り替えられないので、パラメータか wrapper で）。
- **minigo 側を書き換える方向**: 上記制限 1〜3 を minigo 側で直す実験を `third_party/minigo` 上でやってみる。
- **story の自動発掘**: 現状は `//go:build story` ファイルを手で書いている。テンプレートと 1:1 の story を生成する lint/雛形コマンド。

## 実測メモ

- 10 story 全て HTML 出力 OK（一覧・空・ページング・詳細・コメントエラー・フォーム new/errors/edit・削除・404）。
- 詳細ページでは goldmark の GFM task list・goldmark-alert callout も実物変換で描画されている。
- story 描画 ~100-150ms/req、`/static/` は FileServer なので ~1ms。
- `go test ./...` `go vet ./...` `gofmt -l .` はルート module で全グリーン（cmd/storybook は別 module）。
