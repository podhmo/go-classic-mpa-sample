# UI デザインの試行錯誤（for-ui-designer）

デザインを担当する人向けのドキュメント。CSS / HTML テンプレート
（およびビューモデルやハンドラの表示まわり）を書き換えながら、
見た目を確認して作業を進めるための手順をまとめる。

## 結論: アプリ本体は起動しなくてよい

このリポジトリには自作の **storybook 的なツール**（`cmd/storybook`）
がある。ここで言う storybook は npm でインストールする
[Storybook](https://storybook.js.org/) ではなく、
`internal/web` のページを **偽のデータで直接レンダリング** して
一覧・比較するための自前の仕組み（`github.com/podhmo/minigo/storybook`
パッケージベース）を指す。

ポイントは「デザインの試行錯誤にバックエンドの環境構築が要らない」
こと。DB ファイルも `init` もシードデータも `go run .` も不要で、
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

## index の見方

`http://127.0.0.1:6006` を開くと story 一覧が出る:

- **print stories** — `story_pages.go` の story 群。各 story は偽データで
  `<html>` を 1 ページ描く。末尾の `renderXxx` 呼び出し名でグループ化
  され、グループ名は `/grid/<renderFunc>` へのリンク = **同じページの
  全バリアントを並べた grid**。
- **StoryHttp\*** — 実際のルーティングを通した HTTP リプレイ。
  ステータスとヘッダ（CSP の `script-src 'none'` 含む）が記録・再現される。
  新しいタブで開く。
- **viewport 切替** — `?w=320|360|768` で iframe の幅を変えられる。
  狭幅の確認はウィンドウをリサイズしなくて済む。

## 作業の進め方

1. `make storybook` で起動してタスク中は立てっぱなしにする。
2. 既存ページの見た目を変えた → 対応 story（例: `?s=StoryIssueList`）
   と `/grid/<renderFunc>` で全データ状態をまとめて確認。
3. 新しいデータ状態（空・エラー・ページング等）を足したい →
   `story_pages.go` に story を追加。末尾を既存の `renderXxx(status, data)`
   呼び出しにすると index に即座に現れ、同じグループに並ぶ。
4. 新しいページ/ハンドラ → `renderXxx` helper と `StoryXxx` を追加。
   ルート自体（status・ヘッダ）が重要なら `StoryHttpXxx` も。
5. 狭幅が気になる → `?w=360` で grid を開く。
6. 回帰確認 → `cd cmd/storybook && go test ./...` がコーパス全体の
   不変条件（全 story が描画できる、グループが潰れない、status/CSP が
   リプレイされる）を検査する。story 追加時にテストを編集する必要はない。

## JS について

このリポジトリは **JavaScript を使わない方針**（no-JS）。HTML の
宣言的な機能（invoker commands, view transitions 等）で完結させる。

- `static/` 下のファイルはそのまま配信されるので、仮に `static/x.js`
  を置けば `200` で取れるが、ページからは読み込めない。
- print story に inline `<script>` を書くと描画はされるが、実アプリ経路
  （`StoryHttp*`）では CSP の `script-src 'none'` がリプレイされて
  本番と同じく塞がれる。つまり storybook で「動いた」JS が本番で
  動く保証はない — JS を足したい場合は先に方針自体を変える必要がある。

## 静的スナップショット（export）

```sh
cd cmd/storybook && go run . -o /tmp/book ../../internal/web
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
- `cmd/storybook/README.md` — ツールのリファレンス
