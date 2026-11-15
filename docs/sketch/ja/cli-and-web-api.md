# gh ライクな CLI 機能と Web API の設計検討

調査・設計メモ（実装前の検討段階）。「このアプリに gh コマンドのような機能を持たせたい。
ただしローカルだけで使えるのではうれしくないかもしれない。その場合どうするか、やはり
Web API か」という問いに対する回答を設計する。

## 問いの整理

1. **gh のような CLI** — `gh issue list` / `gh issue view 3` / `gh issue create` のように、
   ターミナルから issue を操作したい。
2. **ローカル限定では足りない可能性** — CLI が「そのマシンの SQLite ファイル」を直接
   触るだけなら、サーバが別ホストにいる・複数人で使うといった場面で使えない。
3. **だったら Web API が要るのか** — gh の実体は GitHub API の薄いクライアントである。
   「gh ライクな CLI」の本質は CLI ではなく、その裏にある公開 API 側にある。

結論を先に書くと、**着地点は「JSON Web API + 薄い CLI クライアント」で正しい**。
ただし到達までにレガシーアプリの開発チームが通りそうな道（と寄り道）があり、それを
踏まえた設計にするのがこのリポジトリの筋に合う。

## gh の構造を確認する

`gh` 自体がやっていること:

- 全コマンドが REST/GraphQL API への HTTP 呼び出し。**ローカルの状態は認証情報と
  設定だけ**で、issue データを直接は持たない。
- `-R owner/repo` や `GH_HOST` で「どのサーバを見るか」を切り替える。
- `--json` で機械可読出力、`--jq` で絞り込み、`-w/--web` で「代わりにブラウザを開く」。
- `gh api` で生 API への抜け道を常に持つ。

つまり gh 風 CLI を作るということは、**API を持つこと**を意味する。CLI 側はその
クライアント実装にすぎない。

## 現状の構造（このリポジトリ）

- `main.go`: `-addr`/`-db` フラグ + `init` サブコマンド（前例として重要）。
- `internal/store`: `*store.Store` が SQLite に対する全操作を持つ。
- `internal/web`: **consumer 側 `Store` interface（9メソッド）**を宣言し `*store.Store`
  が構造的に満たす。ハンドラはこの interface だけを知る。
- 認証なし・`127.0.0.1` bind が前提（decisions.md「意図的に実装しないもの」）。
- `POST` は全て form + PRG。`PUT/PATCH/DELETE` は HTML フォームから出せないので
  使っていない。

ここで重要な観察: **`web.Store` interface は既に事実上の「service contract」である**。
HTTP ハンドラがその消費者の1つ。CLI は「同じ契約の別の消費者」として追加でき、
ローカルでは `*store.Store`、リモートでは HTTP クライアントが同じ interface を満たせば
コマンド本体はどちらかを意識しない。新たな service 層を作る必要はない — 契約は
すでに存在している。

## 「ローカルだけではうれしくない」への選択肢

| 案 | 内容 | 評価 |
|---|---|---|
| A. SSH でリモート実行 | CLI はローカルのまま。`ssh host ./issues issue list` でリモート側のバイナリを動かす | **コード 0 行で今日から使える**。リモートにバイナリと DB が要る。「嬉しくない」を解消する最も安い答え |
| B. SQLite のリモート化 | litestream / rqlite / Turso 等で DB 自体をネットワーク化 | ストレージ層の入替え=大工事。MPA 本体の単一バイナリ性を壊す。今回の問い（操作面）とはレイヤが違う |
| C. HTML スクレイピング | 既存のフォーム POST と GET ページを CLI が叩き、HTML を parse する | **レガシーチームが一度は通る道**。認証も新規ルートも不要で動くが、テンプレート変更で死ぬ・エラーが 422 HTML で返る・意味論（303 先）を再解釈する必要がある。「やってはいけない」と明記する価値のある反例 |
| D. Web API + CLI クライアント | `/api/v1/...` の JSON API を追加し、CLI はそのクライアント | gh そのものの形。API がアプリの第 2 の表舞台になる。認証問題を必ず連れてくる（後述） |

**推奨は D**。ただし A は「サーバが別ホスト」ケースの即効性がある解として記録して
おく（チームが API を作る前に当面これで凌ぐ、という経路も自然）。

## 設計の骨格

### レイヤ構成: 同一契約の 2 実装

```
                 ┌──────────────┐
  HTML ハンドラ  │ internal/web │──┐
                 └──────────────┘  │   Service interface
                                   ├─ （web.Store と同形の契約）
  JSON API       ┌──────────────┐  │
                 │ internal/api │──┘
                 └──────────────┘        │
        ┌──────────┬─────────────────────┤
        │          │                     │
  *store.Store   *client.Client      （テスト用 fake）
  (ローカル直結)  (HTTP+JSON)
                 ▲
                 │ CLI コマンドは Backend interface だけを知る
          ┌──────────────┐
          │ internal/cli │
          └──────────────┘
```

- `internal/cli`: コマンド文法・フラグ・出力フォーマット。`Backend`（= ListIssues,
  GetIssue, CreateIssue, ... の interface）を受け取って実行するだけ。
- `internal/client`: `Backend` を満たす HTTP クライアント。`/api/v1` を叩く。
- `internal/api`: サーバ側の JSON ハンドラ。同じく consumer 側 Store interface を宣言。
- DTO: wire 形式の型（json tag 付き）は `internal/api` に定義し、client もそれを import
  して serialize/deserialize の単一ソースにする。`store.Issue` に json tag を足して
  そのまま流す手もあるが、DB 型と wire 型が密結合すると後で変えづらいので別型にする
  （小さいのでコストも小さい）。

### バイナリ構成

2 つの案がある。

- **案1: 単一バイナリ + サブコマンド**（gitea 式: `gitea` 1 バイナリが `web`/`admin`
  を持つ）。`issues` 裸起動=従来どおり serve、`issues issue list` 等=CLI。`init`
  サブコマンドの前例と同じ発想で、「シングルバイナリ動作」の価値も維持できる。
  欠点: `flag` が最初の位置引数で parse を止めるため、`os.Args[1]` による先回り
  dispatch が必要になる（`issue`/`label`/`comment`/`api` を見て `cli.Run` に丸ごと
  渡す）。`-init` フラグと `init` 位置引数の互換は残す。
- **案2: `cmd/issues` 別バイナリ**（storybook 式）。flag 処理が素直で名前も `issues`
  コマンドとして自然。ただし storybook が別 module なのは minigo 依存を隔離する
  ためで、CLI は標準ライブラリだけで書けるので隔離の動機がない。配布物が 2 つに
  増えるのも単一バイナリ方針とずれる。

**推奨: 案1**（単一バイナリ）。`go run . issue list` が動く形。

### CLI コマンド文法（gh 風）

```
issues issue list [--state open|closed|all] [--label NAME] [--search Q]
                  [--sort newest|oldest] [--limit N] [--json FIELD,...]
issues issue view N [--comments] [--json ...] [-w|--web]
issues issue create --title T [--body B|--body-file -] [--label NAME]... [-w]
issues issue edit N [--title T] [--body B] [--label NAME...]
issues issue close|reopen N
issues issue delete N
issues comment create N --body B
issues label list
issues api <path>            # 生 API への抜け道（gh api 相当）
```

- 出力はデフォルトで `text/tabwriter` の簡易表、`--json` で機械可読。
  gh の `--json field1,field2`（フィールド選択）は真似る価値あり。`--jq` は自前で
  持たず「`--json` を `jq` に pipe する」で十分（gh は内部に gojq を持つが再実装は
  過剰）。
- `-w/--web` は「URL をブラウザで開く」だけなのでリモートモードでもタダで実現
  できる gh 由来の便利フラグ。
- exit code: 0 成功 / 1 実行エラー / 2 使い方エラー。
- 色は出さない（最小）。TTY 判定や `NO_COLOR` も宿題でよい。

### ローカル/リモートの選択

- **ローカルモード**（デフォルトにできる）: `-db PATH`（既存の `ISSUES_DB` をそのまま
  流用）で `store.Open` → `*store.Store` を Backend にする。サーバ不要・認証不要で
  `init` と同じ立ち位置。SQLite の同時アクセスは短い tx + `busy_timeout(5000)` で
  サーバ稼働中でもほぼ問題ない（readers-writer のブロッキングが気になれば
  `journal_mode=WAL` を別途検討）。
- **リモートモード**: `--server URL` / `ISSUES_SERVER` 指定で `*client.Client`。
  認証は `--token` / `ISSUES_TOKEN`（gh の `GH_TOKEN` 相当）。`-H/--host` より
  `--server` のほうが素直（gh の `-R` は repo 指定なので真似る意味がない）。
- 両方指定されたら `--server` 優先で明示的にエラーにする or 優先順位を決める
  （要実装時決定。迷ったら「-server があれば remote、なければ local」でよい）。

### Web API の形

- **パス**: `/api/v1/issues` など **バージョンをパスに含める**。Accept ヘッダや
  クエリでのバージョン指定より明白で、HTML ルートと衝突しない。
- **動詞**: HTML ルートと同じく POST 中心で揃えるか、REST 的に PATCH/DELETE を
  使うか。→ **API 側は素直な動詞を使う**（HTML フォームの制約は API には無い
  ので、`POST /issues` create / `PATCH /issues/{id}` edit / `POST /issues/{id}/status`
  / `DELETE /issues/{id}` / `POST /issues/{id}/comments`）。ただし「チームが最初は
  HTML 動詞のミラーから入る」のも自然なので、status だけ POST のまま残す、くらいの
  折衷もありえる。ここは実装時に再確認したい論点。
- **認証**: `Authorization: Bearer <ISSUES_API_TOKEN>` の**単一トークン**。
  `crypto/subtle` で定数時間比較。**`ISSUES_API_TOKEN` 未設定なら `/api/v1` を
  mount しない**（fail-closed: 認証なし API を localhost 以外に出さない）。これで
  「認証なしローカルアプリ」の前提を壊さずに API は opt-in の deploy 判断にできる。
- **API 認証だけでは公開時に足りない**: `/api/v1` にトークンを掛けても、同じ
  リスナーの HTML ルート（`/issues` 等）は無認証のまま — 閲覧・更新・削除が
  できる。サーバをリモート公開する場合の選択肢は、(a) アプリ本体は localhost bind
  のままにして `/api/v1` だけを別リスナーかリバースプロキシで公開する、
  (b) トークン設定時は HTML ルートにも同じ Bearer を要求する（ブラウザ UI まで
  認証付きになる＝モデルが変わる）、(c) HTML に別の仕組み（セッション等）を足す。
  (a) が「認証なしローカルアプリ」前提との整合が一番良い。
- **CSRF**: `http.NewCrossOriginProtection` は `Sec-Fetch-Site`/`Origin` を送らない
  リクエストを通すので、CLI からの呼び出しは既存 middleware のままで動く想定
  （要実測）。Bearer 認証は ambient credential ではないので CSRF モデル自体も
  変わらない。
- **エラー形式**: `{"error": {"message": "...", "fields": {"title": "..."}}}`。
  ステータスは HTML 側と同じ意味論（404 not found / 422 validation / 400 bad input /
  201 create / 204 delete）。
- **detail の形**: `GET /api/v1/issues/{id}` は issue + labels + comments を一発で返す
  （`issue view` が 1 リクエストで済む。コメント数が増えたら `?limit=` や分離 endpoint
  で後追い対応）。
- **ハンドラの共有**: HTML 側と API 側でバリデーションを二重実装しないよう、
  `validateIssueInput` を「`IssueInput` 値を検証する関数」に分解して web/api 両方から
  使う（現状は `*http.Request` から PostForm を読む形なので小リファクタが必要）。

### 検証

- API ハンドラは `StoryHttp*` story で storybook からプレビューできる（実ルーティング
  + 実ヘッダをリプレイする機構が既にある）。認証 middleware の挙動も story で固定
  できる。
- 実サーバ + curl / 実 CLI での E2E は verify-ui 系の流儀に乗る。

## 段階的導入（レガシーチームの道のりをなぞる）

このリポジトリは「いわゆるレガシーアプリケーションの開発チーム」をなぞる場なので、
一気に API を作るのではなく以下の段階を踏むのが自然。

- **Phase 1（ローカル CLI）**: `internal/cli` + argv dispatch。`issue list|view|create|
  edit|close|reopen|delete` と `label list`。Backend は `*store.Store` 直結だけ。
  `--json` 出力まで入れる。ここまでで「gh 風」の見た目は完成、しかも認証問題ゼロ。
- **Phase 2（API 追加）**: `/api/v1` + Bearer token（opt-in）。`internal/api` + DTO。
  この時点で「認証なし前提」を decisions.md からどうずらすかを記録する。
- **Phase 3（リモートモード）**: `internal/client` で `Backend` の HTTP 実装。
  `--server`/`--token`/`issues api`/`-w` を足して本当に gh ライクになる。
- **Phase 4（仕上げ）**: `issue create --web`、`api` の `--method/--field`、
  completion 出力、`--template` 出力等は必要になってから。

## 残課題・未決事項

- **認証を先に決める**: API を外に出すなら単一トークンで足りるか、ユーザー概念
  （コメント投稿者）まで入れるか。単一トークンなら「誰が投稿したか」の区別が API
  経由では消える点に注意（コメントは固定ローカルユーザー扱いの現状と整合はする）。
- **PATCH/DELETE 化の線引き**: 上記のとおり実装時に再確認。
- **ページネーション**: offset 方式のままでよいが `per_page` の上限を API 側でも
  設けるか。
- **トークンの管理**: 単一 env トークン→複数トークン/DB 管理への拡張は future work。
- **レート制限・監査ログ**: 公開するなら検討。ローカル/LAN 用途なら不要。
- **`issues serve` を明示するか**: 裸起動=serve のままか、`serve` サブコマンドを
  用意して将来の CLI 拡張に備えるか。
