# UI デザインの試行錯誤（for-ui-designer）

デザインを担当する人向けのドキュメント。CSS / HTML テンプレート
（およびビューモデルやハンドラの表示まわり）を書き換えながら、
見た目を確認して作業を進めるための手順をまとめる。

## 結論: アプリ本体は起動しなくてよい

このリポジトリには自作の **storybook 的なツール**（`cmd/storybook`）
がある。ここで言う storybook は npm でインストールする
[Storybook](https://storybook.js.org/) ではなく、
`internal/web` のページを **偽のデータで直接レンダリング** して
一覧・比較するための自前の仕組み（`github.com/podhmo/minigo/x/storybook`
パッケージベース）を指す。

ポイントは「デザインの試行錯誤にバックエンドの環境構築が要らない」
こと。DB ファイルも `init` もシードデータも `go run ./cmd/zatudan` も不要で、
見た目の確認だけを独立に回せる。バックエンドとフロントエンド（デザイン）
で作業が分かれているとき、デザイン側はこれだけあればよい。

## 起動

```sh
make storybook    # http://127.0.0.1:6006 で待ち受け
```

初回は `make setup-minigo` が自動で走り、`podhmo/minigo` を
`third_party/` に clone する（`cmd/storybook` は nested module で
ローカルの minigo に `replace` している）。

## ホットリロード — 全部リロードだけで反映される

storybook プロセスを立てたまま、以下の編集は **再起動不要**。
ブラウザをリロードするだけで反映される（ソースは毎リクエスト読み直される）:

| 編集対象 | 反映 |
|---|---|
| `internal/web/templates/*.html` | リロードのみ |
| `internal/web/story_pages.go`（表示用 fixture・story） | リロードのみ |
| `internal/web/static/*`（app.css 等） | リロードのみ（`/static/` 配信） |
| その他の `internal/web/*.go`（renderer・view-model） | リロードのみ |

テンプレートのパースエラーや `{{.MissingField}}` は story が **500**
を返す形で表面化する（静かに空描画にはならない）。

ブラウザのリロードすら省きたいときは `make storybook-watch`
（= `-watch` フラグ）で起動する。パッケージ配下のファイル変更を検知して
開いているプレビューページが自動でリロードされる（SSE ベース、
`/_livereload.js` を各ページに埋め込む。`-watch` なしでは一切の
JS を配信しない）。エラーで 500 になったページも watch 中は
スクリプトを保持するので、修正保存すれば自動で復帰する。

## index の見方

`http://127.0.0.1:6006` を開くと story 一覧が出る:

- **フィルタ** — 上部の検索ボックスで name/doc/group を絞り込める
  （入力中ライブ反映、URL は `?q=`）。`/` でフォーカス、Esc でクリア、
  Enter で先頭候補を開く、↑↓ で候補を移動。
- **print stories** — `story_pages.go` の story 群。各 story は偽データで
  `<html>` を 1 ページ描く。末尾の `renderXxx` 呼び出し名でグループ化
  される。クリックすると右のプレビューに開く（`?s=`、ページリロードなし）。
- **⊞ grid ピル** — グループ見出しの右にあるボタン。**同じページの
  全バリアントを並べた grid** をプレビュー領域に開く（`?g=`）。
  全幅で見たいときはツールバーの `open ↗` から `/grid/<renderFunc>`
  を新規タブで開く。
- **ツールバー** — プレビュー上部のバー。今出ているもの（group /
  story 名 / `story_pages.go:行`）と、`open ↗`（素の story を新規タブ）、
  `⊞ grid`、幅切替（`?w=320|360|768`、狭幅確認はリサイズ不要）がある。
- **StoryHttp\*** — 実際のルーティングを通した HTTP リプレイ。
  ステータスとヘッダ（CSP の `script-src 'self'` 含む）が記録・再現される。
  新しいタブで開く（`frame-ancestors 'none'` をリプレイするため
  iframe には描けない）。

## 作業の進め方

1. `make storybook` で起動してタスク中は立てっぱなしにする。
2. 既存ページの見た目を変えた → 対応 story（例: `?s=StoryIssueList`。フィルタで探すと早い）
   と `⊞ grid`（`?g=`）で全データ状態をまとめて確認。
3. 新しいデータ状態（空・エラー・ページング等）を足したい →
   `story_pages.go` に story を追加。末尾を既存の `renderXxx(status, data)`
   呼び出しにすると index に即座に現れ、同じグループに並ぶ。
4. 新しいページ/ハンドラ → `renderXxx` helper と `StoryXxx` を追加。
   ルート自体（status・ヘッダ）が重要なら `StoryHttpXxx` も。
5. 狭幅が気になる → ツールバーの幅切替で 360px にする（grid 内では各セルがその幅になる）。
6. 回帰確認 → `cd cmd/storybook && go test ./...` が mount の疎通を
   検査する（アプリ story コーパス全体の描画・グルーピングの回帰
   テストは fork の page.go と一緒に削除。chrome 側の回帰は
   `github.com/podhmo/minigo/x/storybook` のテストが持つ）。

## JS について

方針は「検証できるなら使ってよい」（`docs/ja/decisions.md`
「JavaScript 方針」参照）。元々 JS を避けていたのは動作検証が面倒に
なるためで、storybook 経由でイベント発火を含む挙動を確認できるなら
書いてよい。ただし宣言的な HTML/CSS で実現できるものはそちらを優先する。

- `static/` 下のファイルはそのまま配信されるので、`static/x.js` を
  置けば `200 text/javascript` で取れ、リロードだけで反映される。
- CSP は `script-src 'self'`: `/static/` の外部ファイルはそのまま動くが
  inline `<script>` はブロックされる。JS は外部ファイルに置くこと。
  実アプリ経路（`StoryHttp*`）が本番ヘッダをそのままリプレイするので
  CSP とのズレはプレビュー時点で分かる。

## 静的スナップショット（export）

```sh
cd cmd/storybook && go run ./cmd/zatudan -o /tmp/book ../../internal/web
python3 -m http.server -d /tmp/book
```

index.html + grid_*.html + static/ が出力される。ライブサーバーと同じ
内容（ホットリロードなし）で、PR への添付や diff 確認に使える。

## 関連

- `.agents/skills/iterate-ui/` — エージェント向けのワークフロー版（この
  ドキュメントと同じ内容を手順化したもの）
- `.agents/skills/verify-ui/` — 変更後に実アプリで動作確認へ広げる
  エージェント向けスキル（実サーバ + playwright-cli）
- `docs/sketch/ja/storybook-with-minigo.md` — ツール自体の設計・検証
  レポート
- `cmd/storybook/main.go` の doc コメント — ツールのリファレンス（フラグ一覧）
