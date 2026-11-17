# コマンド名・バイナリ名の命名検討 — 調査メモ

調査・設計メモ（実装前の検討段階）。リネーム自体は未実施。

問い:

> コマンド名及びリポジトリ名のぎこちなさをなくしたい。つまり `go build` した
> ときの名前の問題。
>
> - リポジトリ名部分: 意図に沿ってはいる。ただしルートで `go build` したときの
>   名前が嬉しくない
> - `cmd/issues`: 完全に名前がアップデートに追いついていない。変えたい

結論を先に書くと:

- 推薦は **CLI = `mpa`、サーババイナリ = `mpad`**。ペア命名は
  `docker` / `dockerd` 型。ただし `mpa` はアーキテクチャ種別の略語であり、
  プロジェクトの固有名ではない点は弱み（後述）。
- 名前は現時点で**未定** — リポジトリ名 `go-classic-mpa-sample` は実験の
  題材を説明する名前であって、アプリ・コマンドの自称ではない。
- 方向を変えるなら **`bbs` / `bbsd`**（classic な Web の空気 + discussion を
  自然に含む）、日本語枠なら **`fuda` / `madoguchi` / `kakari`**。
- リポジトリ名・module path は据え置き（意図に沿っているため）。ルート
  `go build` のバイナリ名は module path 末端由来なので、素直な解決は
  `go build -o` の運用か、配布用 entrypoint を `cmd/` に移すこと。

## 背景の整理（このプロジェクトは何か）

命名の前に、名前が指すべき対象を確認しておく。

- **出発点**（`docs/ja/prompts.md`）: `net/http` + `html/template` + SQLite の
  「古典的な MPA」として issue tracker を作る実験。JS 禁止・フレームワーク
  不使用・ORM 不使用が仕様として固定されていた。
- **その後の拡張**（`docs/ja/roadmap.md` と各 sketch メモ）: accounts
  （argon2id + opaque token）、discussions（category / thread / Q&A answer）、
  JSON API `/api/v1`、gh 風 CLI、workspaces（GitHub のリポジトリに相当する
  コンテナ）、SSE live updates、自作 storybook。JS は「storybook で検証
  できる範囲の外部 ES module」として緩和済み。
- **作者の言い方**（html-sandbox/slops の MPA シリーズ）: 「レガシー風の
  Web アプリ開発を体験する実験場」「古い設計でモダンな機能を全部試す」。
  記事は `MPA round-N` という連載名を持つが、これは MPA（multi-page
  application）というアーキテクチャ種別への言及であり、プロジェクトの
  自称ではない — 名前はいまのところ決まっていない。
- **ドメインの現状**: workspace（≈GitHub リポジトリ）> issues / discussions
  > comments / labels / auth。つまり GitHub のミニチュアであり、CLI は
  その世界の `gh` に相当する。

## 現状の名前と、何がぎこちないか

| 名前 | 実体 | 問題 |
| --- | --- | --- |
| `go-classic-mpa-sample` | ルート `go build` の成果物（サーバ） | 長くて実行名として打てない。リポジトリ名自体は意図どおりなので残したい |
| `issues` | `cmd/issues` の成果物（gh 風 CLI） | ドメインが issue に留まらない（discussion / workspace / auth / api / completion も扱う）。`issues issue list` と語が重複する |
| `ISSUES_*` | 環境変数 prefix（サーバ・CLI 共通） | 同上。改名するなら prefix をどうするかが論点 |
| `issues.db` | 既定の DB ファイル名 | 同上 |

### Go の命名ルール（実測で確認）

- `go build` / `go install` の成果物名は **main パッケージの import path 末端**
  になる。`cmd/foo` なら `foo`、module root の main なら **module path の末端**。
  チェックアウト先のディレクトリ名ではない（`module example.com/prettyname` を
  `namecheck/` に置いて `go build` → `prettyname` が出ることを実測）。
- つまりルートバイナリ名を変える手段は実質 2 つ: `go build -o <name>` するか、
  配布用 entrypoint を `cmd/<name>` に置くか。リポジトリ dir のリネームでは
  変わらない。
- `go run .` で起動できることは `docs/ja/prompts.md` の spec → root の
  `main.go` は残す。
- 「CLI はサーバとは別コマンド」は `docs/ja/decisions.md` の決定事項
  （案2。クライアント配布にサーバを同梱させないため）→ server と CLI は
  **別名のペア**として考える。

## 命名の軸

1. **ドメイン網羅性**: issue だけでなく discussion / workspace / auth / api を
   覆えるか
2. **短さ・打ちやすさ**: gh 風に日々打つ名前として適切か
3. **アイデンティティとの一致**: 「classic MPA の実験場」という立場を表せるか
4. **サーバとのペア感**: 2 バイナリが並んだときに家族に見えるか
5. **衝突回避**: 短い tracker 名の領域はすでに混んでいる（`gh`, `tea`, `glab`,
   `docket`, `bmo`, `mint` …）

## 候補

### 案 A: `mpa` / `mpad`（推薦）

slops 連載の記事タイトルでこのリポジトリを指すのに使われている略称を
そのまま採用する。

```sh
mpa issue list
mpa discussion list
mpa auth token --user alice
mpa api /api/v1/issues -f q=bug
mpad -addr :8080 -db app.db init     # サーバ側
```

- 良い点: 連載を読んだ人には「このアプリの CLI」と伝わる。3 文字で短い。
  env prefix も `MPA_*`（`MPA_DB` / `MPA_SERVER` / `MPA_TOKEN` / `MPA_ADDR`
  / `MPA_WORKSPACE` …）に自然に揃う。`mpad` は dockerd 型の daemon 命名で
  読みやすい。
- 悪い点: MPA は multi-page application の一般的略語であり、固有の名前
  ではない（`spa` という名のバイナリを名付けるようなもの）。プロジェクト
  外から見て意味を持たず、npm 界隈に同名の webpack-mpa CLI もいくつか
  ある（Go バイナリとしては実害薄い）。

### 案 B: `bbs` / `bbsd`

「classic な Web」というプロジェクトの魂に寄せる。discussion はまさに BBS、
issue 一覧も板に近い。

- 良い点: 雰囲気が一番プロジェクトらしい。3 文字。`bbsd` も自然。
- 悪い点: BBS = 掲示板なので issue tracker 側のニュアンスがやや弱い。
  冗談っぽく見えるかもしれない。

### 案 C: `track` / `trackd`

英語の普通の単語で「追跡するもの」。verb としても読める（`track issue close 3`）。

- 良い点: ドメイン網羅性が高い（discussion も track できる）。意味が通る。
- 悪い点: 平凡で、他の文脈（時間管理・物流・git の track 等）と被りやすい。

### 案 D: `it` / `itd`

issue tracker の initials。`gh` と同じ 2 文字枠。

- 良い点: 最短。
- 悪い点: 英単語 `it` との衝突が大きく、スクリプトや会話で紛らわしい。
  env は `IT_*` になって美しくない。issue tracker の頭文字なので結局
  「issue 中心」の匂いは残る。

### 案 E（日本語枠）: `fuda` / `madoguchi` / `kakari`

`kamidana` や `slops` と同じ系統の名付け。

- `fuda`（札）: issue が板に貼る札、という絵。短い・可愛い。discussion の
  カバーは弱め
- `madoguchi`（窓口）: issue も discussion も「窓口に持ち込むもの」と読めて
  意味的には一番綺麗。ただし 9 文字で日々打つには長い（短縮の `mado` なら
  短いが、madoguchi の語感は残らない）
- `kakari`（係）: 「担当者」が CLI に居る感じ。`kakari issue list` で読める。
  6 文字

### 却下寄りの候補

- `hub` / `hubd`: 「この世界の GitHub クライアント」としては概念的に正しいが、
  GitHub 公式旧 CLI `hub` そのもので、衝突が大きい
- `forge` / `forged`: forge 系サイトへの連想は良いが、`forged`（偽造された）
  の語感がサーバ名として悪い。`foundry` だと長い
- `issues` 維持: ドメインの広がりに追いついていないという依頼動機そのもの。
  `issues issue list` の重複も解消しない

## サーババイナリ名の取り方（構造オプション）

CLI の名前とは別に、ルート `go build` の成果物名をどう綺麗にするか。

| 案 | 仕組み | 得失 |
| --- | --- | --- |
| S1. `go build -o` 運用 | root `main.go` そのまま、配布時に `-o <name>`（`make build` で `bin/<name>` を出すなど） | ゼロ変更。ただし「素の `go build` だと依然長い名前」は残る。リポジトリの外に出る配布物だけ綺麗にする割り切り |
| S2. 配布用 entrypoint を `cmd/` へ | 例: `cmd/mpad` に bootstrap を移し、root `main.go` は共有 `internal/...` 呼び出しの薄い入口に（`go run .` は維持） | `go build ./cmd/mpad` → 本当に `mpad` が出る。`cmd/issues` 系と並ぶ構造になる。root との二重 entrypoint が小さな重複になる |
| S3. 単一バイナリ化（案1 の再検討） | `cmd/<name>` が裸起動=serve、`<name> issue list` 等=CLI | 名前問題は半分になるが、decisions.md の案2 決定（クライアント配布にサーバ同梱しない）を覆すので基本は却下 |
| S4. module path 末端を改名 | `module github.com/podhmo/<name>` | 実質リポジトリ名の変更。リポジトリ名は意図どおりで残す前提なので対象外 |

有力なのは **S1 と S2 の 2 案**（cmd/ 以下に置く案は依頼主から許可済み）。

- **S1（`-o` 運用）**: ゼロ変更。「綺麗なバイナリ名」は配布・パッケージング
  時だけの話で、`go run .` が日常の起動手段ならこれで十分という割り切り。
- **S2（`cmd/` 移設）**: サーバも `cmd/<name>`（例: `cmd/mpad`）に entrypoint
  を置き、root `main.go` は共有 bootstrap を呼ぶ薄い入口にして `go run .`
  を維持。`cmd/issues` 系 CLI と構造が揃い、`go build ./cmd/mpad` で
  「本当のバイナリ名」が出る。root との二重 entrypoint が小さな重複に
  なるのがコスト。

S2 を採る場合、CLI とサーバを `cmd/mpa` / `cmd/mpad` のように
**同じ語根で並べられる**のが副次的な利点（`cmd/<cli>` / `cmd/<cli>d`）。

## 改名が及ぶ面（rename checklist）

CLI の名前を `issues` から変える場合に触るもの:

- `cmd/issues/` → `cmd/<name>/`（バイナリ名の本体）
- `internal/cli` の usage 文字列・doc コメント・エラーメッセージ
  （`Usage: issues …`、`issues api …` 等が直書き）
- `internal/cli/completion.go` — 生成スクリプト内のコマンド名・関数名
  （`_issues`, `issues.fish` 等が直書き）
- `internal/e2etests/cli_e2e_test.go` — ビルド対象パスと `cliBin` 名
- `Makefile` の `test-e2e` コメント
- README の CLI 節・Architecture 節・Configuration 節
- `docs/ja/decisions.md` / `docs/sketch/ja/cli-and-web-api.md` 等の言及
  （歴史的記述は追記で対応）
- 環境変数 `ISSUES_*` — 後述の論点
- 既定 DB ファイル名 `issues.db` — 同上

URL パス（`/issues`, `/w/{slug}`, `/api/v1`）・テーブル名・HTML 上の表示名は
ドメイン名なので対象外。

## 残留論点

前提: **開発中につき breaking changes は許可される**（リリース前なので
移行措置は不要と考えてよい）。

- **`ISSUES_*` をどうするか**。素直に `<NAME>_*` に全部付け替えられる。
  据え置き（`ISSUES_*` のまま）にする場合は「バイナリ名と env prefix が
  ずれる」状態を意図的に許容することになる。サーバ側と CLI 側で prefix を
  共通にするか分けるかもセットで決める（`MPA_*` なら共通で不自然ではない）
- **`issues.db` 既定ファイル名**。同じく付け替え可。旧ファイル名の DB を
  残したい場合は `-db` フラグで明示すればよいだけなので、既定だけ新しい
  名前にしても問題ない
- **サブコマンド側の語彙**。`issue list` 等のサブコマンド名は今回の問いの
  範囲外だが、CLI 名が変わると `issue` vs `issues` のような一貫性の見直し
  余地が出るかもしれない
- **README のプロジェクト名表記**。タイトルは repo 名のままでよいか、
  通称（例: MPA）を副題的に入れるか
