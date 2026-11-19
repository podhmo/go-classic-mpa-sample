# Pull requests を git コマンドで再現する — 調査・設計メモ

調査・設計メモ（実装前の検討段階）。`workspaces.md` / `discussions.md` の姉妹編。

問い:

> git コマンドを使って Pull request を再現したい。GitHub と異なり、remote
> repository だけでなく **手元の git リポジトリ**（例: `zatudan.db` が置かれ
> ているリポジトリ）も対象にしたい。保存先はなるべく混乱が少ない仕組みに
> する — 例えば close/merge など完了したタイミングで DB に保存し、それ以外
> は git を見る。git にアクセスできない環境では機能ごと隠す。対象は
> **Pull request、commits、comments、ファイル+行番号つき comments**。
> 重要なのは gh cli のように**エージェントが対話して情報を取れる**こと、
> **コメントできる**こと、そしてファイルの特定箇所への言及を **AI に渡し
> やすい形で抽出できる**こと。conflict は表示できればよいレベル。基本的な
> git 操作（commit/push/branch 等）は載せないが、merge / squash merge /
> rebase merge / close はアプリから実行できるようにする。

結論を先に書くと:

- **PR の実体 = DB 行 + (base_ref, head_ref) のペア**。git には PR 概念が
  ないため、「何を open とみなすか」は `zatu pr create` / web フォームでの
  **明示的な登録**にする（自動検出はしない）。
- **保存先の切り分け**: メタデータ（title/body/status）とコメントは
  **作成時から DB**。コミット列・diff・変更ファイル・conflict 可否は
  **open 中は常に git コマンドで都度計算**し、同期テーブルを持たない。
- **完了時の保存は「ref を刺す」**: close/merge のタイミングで
  `git update-ref refs/zatudan/pr/<id>/head <head_sha>` を打ち、DB には
  base_sha/head_sha/merge_sha/method を記録する。コミットを DB にコピー
  しない（GitHub の `refs/pull/N/head` と同じ仕組みで gc から守る）。
- **repo 解決**: `-repo` フラグ（env `ZATUDAN_REPO`）。未指定なら `-db`
  ファイルのあるディレクトリから上方に `.git` を探索する **auto**。
  見つからなければ / `-repo=off` なら **機能ごと非表示**。単一 repo、
  workspace 非所属のトップレベル機能（将来 `workspaces.repo_path` に
  変わる余地を残す）。
- **merge 実行**: web / zatu の両方から可。ただし 2 経路に分かれる
  （後述「merge 実行の 2 経路」— base が checked-out なら実 worktree で
  `git merge` 系、そうでなければ plumbing（`merge-tree` + `commit-tree`
  + `update-ref`）で worktree に触れない）。
- **AI 向け**: `zatu pr view N --markdown` / `--json` で、review comments
  を `path:L42` + コードスニペット + 本文の形で出力する。

## git の概念と PR の対応付け

| PR の要素 | 対応する git 操作 | 保存先 |
|---|---|---|
| PR 番号・タイトル・本文・status | — | DB（`pull_requests`） |
| コメント（会話） | — | DB（`pr_comments`） |
| コメント（ファイル+行） | — | DB（`pr_comments` の anchor 列） |
| コミット列 | `git log base..head` | git（都度計算） |
| 変更ファイル・diff | `git diff base...head`（3dot=merge-base 起点） | git（都度計算） |
| conflict 可否 | `git merge-tree --write-tree base head` | git（都度計算） |
| merge 実行 | `git merge` 系 または plumbing | git（実行）+ DB（結果） |
| close/merge 後の再現性 | `refs/zatudan/pr/N/head` | git（ref）+ DB（sha） |

「混乱が少ない」の定義: **同じ情報を 2 箇所に持たない**。コード由来の
情報（コミット・diff・衝突）は常に git が真実、会話由来の情報
（title・body・comment・status）は常に DB が真実。完了時だけ git→DB の
一方向の記録（sha と結果）が発生し、逆方向（DB を git に書く）は
ref ピン以外しない。

## なぜ完了時に保存が要るのか（ユーザーの指摘の検証）

指摘どおり、squash merge / close 後にブランチを削除すると head 側の
コミットが **unreachable** になり、reflog expire（既定 90 日）と
`gc.pruneExpire`（既定 2 週間）を過ぎると `git gc` で消える。

- **squash merge / rebase merge / close（未マージ）+ ブランチ削除**: 元の
  コミットは結果コミットの祖先ではない → 消えうる。**対策必須**。
- **通常 merge + ブランチ削除**: head のコミットは merge commit の祖先
  になるので残る。ただし **sha を知る手段**（ref or DB 記録）がないと
  「この PR の head はどれか」を再現できない。

### 採用: ref ピン（GitHub 式）

close/merge 時に `git update-ref refs/zatudan/pr/<id>/head <head_sha>`。
ref が指すオブジェクトは gc 対象外なので、ブランチ削除後も
`git diff base...head` が再現できる。GitHub の `refs/pull/N/head` と同じ
仕組みだが、これは remote 専用の機能ではなく **git の ref/reachability
そのものなので `git init` しただけのローカルリポジトリでも同じように
動く**（実測済み）。ローカル用途ではむしろ自然な形。

|| 案 | 内容 | 評価 |
|---|---|---|---|
| A. **ref ピン**（推奨） | `refs/zatudan/pr/N/head` に head sha を刺す | データ重複なし・`git log`/`git diff` がそのまま使える・「真実は git」を維持。ref 名は `refs/pull/` ではなく `refs/zatudan/`（fetch される GitHub 慣習名と衝突しない・アプリ所有が明確・worktree 間で共有される） |
| B. パッチを DB 保存 | 完了時の `git diff` 出力を TEXT 列へ | repo 自体が消えても残るが、データ重複・大きい・コミット単位の参照は死ぬ |
| C. sha のみ記録 | DB に sha だけ書く | 通常 merge は生き残るが squash/close 系は gc で死ぬ。不十分 |

補助規則:

- ref は **create 時にも刺す**（open 直後に head ブランチが消えても
  表示を継続できる）。open 中の追従更新は **しない** — pin を毎回
  進めると force-push 前のコミットが unreachable に戻り、anchor 付き
  コメントの指す sha が死ぬため「create 時点 + close/merge 時点」の
  2 点固定が一番整合的。
- **1 本の pin で祖先チェーン全体が生きる**: reachability は推移的
  なので `refs/zatudan/pr/N/head` さえ残れば、squash + ブランチ削除
  + `gc --prune=now` 後でも `git log` の個々のコミット・各 `git show
  <sha>`・`git show <sha>:<path>` が全部見える（実測済み）。
  個々のコミットへの ref は不要。保護されないのは pinned head の
  **祖先でない**コミット（force-push で押し出された旧版など）だけ。
- **anchor_sha のピン（推奨オプション）**: review comment 作成時に
  `refs/zatudan/pr/N/c/<comment_id>` をその anchor_sha に刺す。これで
  force-push / rebase で押し出されたコミットも保持され、AI 向け
  スニペット抽出（`git show <anchor_sha>:<path>`）が常に成功する。
  コメント削除時に対応 ref も消す。
- open 中は **head_ref 名を優先して live 解決**し、rev-parse 失敗時は
  pinned sha で描画（「head ブランチ消失」の表示を添える）。

## 論点と決定

### 1. 対象リポジトリの決め方

| 案 | 内容 | 評価 |
|---|---|---|
| A. **DB 位置から auto + `-repo` 上書き**（採用） | `-db` の dir から上方に `.git` を探索し `git -C <dir> rev-parse --show-toplevel` で確定。`-repo PATH`（env `ZATUDAN_REPO`）で上書き | ゼロ設定で動く・ユーザーの指示どおり「フラグ指定なし→auto、指定あり→それを使う」。zatu CLI も `--db` 起点で同じ解決（`--repo` で上書き） |
| B. workspace ごとに repo パス | `workspaces.repo_path` 列 | GitHub の repo=workspace モデルに近いが、スキーマ変更 + 「どの workspace がどの repo」管理が増える。ユーザー判断で初回は見送り（後々変わりうる） |
| C. フラグ必須 | `-repo` がないと無効 | 明示的だが毎回の指定が面倒 |

採用は 案A。追加で `-repo=off`（または env `ZATUDAN_REPO=off`）を
**明示的無効化**として用意する — auto 検出に引っかかる環境でも
機能を確実に OFF にでき、テストの決定性にも効く。

**無効時の挙動**: repo が解決できなかったら「利用不可」ではなく
**機能が存在しない**かのように振る舞う。

- web: nav に Pull requests を出さない、`/pulls*` はルート自体を
  マウントしない（404）。
- CLI: `zatu pr ...` は `Error: not a git repository (set --repo or
  ZATUDAN_REPO)` で exit 1。
- API: `/api/v1/pulls*` も未マウント（404）。
- 起動時に一度だけ解決（実行中に repo が現れても再起動まで反映しない
  — `-api-token` の再評価が restart 必須なのと同じポリシー）。

### 2. PR の実体と作り方

`pull_requests` 行 = `(base_ref, head_ref)` + メタデータ。base/head は
ブランチ名に限らず **任意の ref 名**（`origin/main` などリモート追跡
ref や sha も受理 — これで「remote repository も対象」は自然に満たす。
remote に push する操作は載せない、読むだけ）。

- **作成は明示**: `zatu pr create --base main --head feat` /
  web フォーム（base/head は `git branch --format` + `git for-each-ref`
  の select）。同じ (base, head) の open PR の重複作成は拒否
  （`UNIQUE` ではなくアプリ側チェック — closed/merged 済みの同名ペア
  の再作成は許す、GitHub と同じ）。
- **採番**: `INTEGER PRIMARY KEY AUTOINCREMENT` の独立番号。
  GitHub は issue/PR で番号を共有するが、このアプリは discussion も
  独立採番なので同じ方針。URL は `/pulls/{id}`。
- **workspace 非所属**: `/w/{ws}/pulls` ではなくトップレベル `/pulls`。
  repo が workspace と 1:1 でない現状ではスコープを偽らない。

### 3. スキーマ案

```sql
CREATE TABLE pull_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  base_ref TEXT NOT NULL,               -- "main" / "origin/main" / sha 可
  head_ref TEXT NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'open'   -- open | closed | merged
    CHECK (status IN ('open','closed','merged')),
  author_id INTEGER REFERENCES users(id),
  base_sha TEXT,                        -- create 時点の解決値（表示用）
  head_sha TEXT,                        -- close/merge 時点の最終値
  merge_sha TEXT,                       -- merged のみ: 結果コミット
  merge_method TEXT,                    -- merged のみ: merge|squash|rebase
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  closed_at INTEGER
);
CREATE INDEX idx_pull_requests_status_created
  ON pull_requests(status, created_at DESC);

CREATE TABLE pr_comments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  pr_id INTEGER NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
  -- anchor: NULL なら会話コメント、非NULL なら review comment
  path TEXT,                            -- リポジトリ相対パス
  line INTEGER,                         -- head 側の行番号（side=right）
  side TEXT CHECK (side IN ('left','right')),  -- left=削除/旧側
  anchor_sha TEXT,                      -- コメント時点の head sha
  body TEXT NOT NULL,
  author_id INTEGER REFERENCES users(id),
  hidden INTEGER NOT NULL DEFAULT 0,    -- moderation 既存列と同型
  hidden_reason TEXT NOT NULL DEFAULT '',
  edited_at INTEGER,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  CHECK ((path IS NULL) = (line IS NULL))  -- anchor は path+line セット
);
CREATE INDEX idx_pr_comments_pr ON pr_comments(pr_id, created_at);
```

| 案 | 内容 | 評価 |
|---|---|---|
| A. **1 テーブル + NULL anchor**（推奨） | `pr_comments` に path/line/side/anchor_sha を nullable で | 「PR へのコメント」は一つの時系列に並ぶ（GitHub の UI も混在表示）。テーブル数を増やさない。anchor 有無は CHECK で表現 |
| B. review comments を別テーブル | `pr_review_comments` 分離 | GitHub API の分離に忠実だが、表示時に 2 ソースを時系列マージする必要がある |

**outdated 判定**: anchor_sha が現 head でも `base..head` の歴でも
到達不能になったコメントを「outdated」表示にするか — GitHub の
精密な判定は複雑なので、初版は `git merge-base --is-ancestor
anchor_sha head_ref` で cheap に近似し、判定不能（sha 自体が無い）
なら "outdated" バッジ。**詳細は見送り節**。

### 4. 表示側の git コマンド（都度計算）

| 目的 | コマンド | 備考 |
|---|---|---|
| コミット列 | `git log --format=%H%x00%h%x00%s%x00%an%x00%aI base..head` | NUL 区切りで安全に parse |
| 変更ファイル一覧 | `git diff --name-status base...head` | 3dot = merge-base 起点（GitHub の Files changed と同じ意味論） |
| diff 本体 | `git diff -U3 base...head -- <path>` | 巨大 diff は件数/行数でキャップしてリンク化 |
| merge-base | `git merge-base base head` | diff の 3dot と整合させる |
| conflict 判定 | `git merge-tree --write-tree --name-only base head` | exit 0=clean / exit 1=conflict+ファイル名。**要 git ≥ 2.38**（実測 2.56 で exit=1 と `a.txt` 一覧を確認）。古い git では判定を "unknown" 表示に落とす graceful degrade |
| head/base 解決 | `git rev-parse --verify <ref>` | 不存在 → PR を「head 消失」表示 |

実行は全て `os/exec` で `git -C <repo>` 直打ち（シェル経由なし、
`--` でパスと引数を分離）。新パッケージ `internal/gitrepo`（仮）に
集約し、web/api/cli の 3 surface から共有する。

### 5. merge 実行の 2 経路（実測に基づく）

PR の merge は repo を**書き換える**唯一の操作（close は DB のみ）。
実測で分かった分岐:

- `git merge-tree --write-tree` は worktree/index に触れずにマージ
  結果の tree を作れる（exit 0/1 で conflict 判定も済む）。
- `git commit-tree <tree> -p ...` でコミット生成、`git update-ref
  refs/heads/base <sha>` でブランチを進める → **plumbing 経路は
  worktree に一切触れない**。
- ただし実測で `git update-ref` は **checked-out branch にも成功する**
  （拒否するのは `git branch -f` と `git checkout`、exit=128 確認）。
  その場合 index/worktree は古いまま残り、`git status` が phantom
  diff を出す — ユーザーの作業コピーを静かに壊すので **checked-out
  中の base への plumbing update は禁止**。

| 条件 | 経路 | 手順 |
|---|---|---|
| base が **いずれかの worktree で checked-out** | in-worktree | `git -C <そのworktree> status --porcelain` が空なことを確認（dirty なら拒否して手動マージを促す）→ `git merge --no-ff <head>` / `git merge --squash <head> && git commit` / rebase-merge は `git cherry-pick base..head` |
| base が **どこにも checked-out されていない** | plumbing | `merge-tree --write-tree` → conflict なら即結果返却 → `commit-tree`（merge: `-p base -p head`、squash: `-p base`）→ `update-ref refs/heads/base` |

- checked-out 判定: `git worktree list --porcelain` の各 worktree の
  `branch refs/heads/X` 行。
- **rebase merge の plumbing**: `git rev-list --reverse base..head` の
  各コミットを順に `merge-tree --write-tree <newbase> <c>` →
  `commit-tree -p <newbase>` で積む。conflict した時点で中断+報告。
  in-worktree なら `git cherry-pick base..head` でよい（cherry-pick
  は merge コミットを skip するので直線履歴の PR 想定に合う）。
- squash の commit message: 既定 `title (#N)`、`-m` で上書き可。
- merge 成功時の DB 記録: `status=merged, merge_sha, merge_method,
  head_sha(最終), closed_at` + ref ピン。close は `status=closed` +
  head_sha + ref ピンのみ（git 操作なし）。
- **失敗時は何も書き換えない**: conflict / dirty worktree / 実行
  エラーはすべて「実行前」に検出する設計（merge-tree で事前判定
  してから本体操作）。in-worktree merge は `--no-commit` 系で
  中途半端を残さない…いや、squash は `merge --squash` が index を
  汚すので失敗時 `git merge --abort`/`reset` で復帰を試み、ダメ
  ならユーザーに手動復帰を促すエラーを返す。

### 6. UI

```
GET  /pulls                     一覧（open|closed|merged|all、issue 一覧と同じ filter-bar 構造）
GET  /pulls/new                 作成フォーム（base/head select・title・body）
POST /pulls
GET  /pulls/{id}                詳細: meta / commits / conflict バッジ / コメント時系列
GET  /pulls/{id}/files          Files changed: ファイルごとの diff + anchored comments
POST /pulls/{id}/comments       会話コメント
POST /pulls/{id}/review-comments  path,line,side 付きコメント
POST /pulls/{id}/status         close / reopen
POST /pulls/{id}/merge          ?method=merge|squash|rebase → 結果/失敗を flash 表示
GET  /pulls/{id}/markdown       export（下記）
```

- nav: `Issues | Discussions | Pull requests`（repo 無効時は非表示）。
- conflict は merge-tree の結果を「This branch has conflicts in:
  a.txt, b.go」程度のバッジ表示（解決支援はしない = 要件どおり表示のみ）。
- `/files` の各行差分に「この行にコメント」フォーム（`<details>` 畳み
  込みで no-JS でも動く、discussions の reply と同じパターン）。
- commits 一覧は `git log` の subject + 短 sha + author + 日付。

### 7. CLI / API（エージェント向け — 主目的）

```
zatu pr list [--state open|closed|merged|all]
zatu pr view N [--markdown|--json]   # title+body+commits+comments 一式
zatu pr diff N                       # unified diff 出力（remote モードでも取れる）
zatu pr create --base B --head H --title T [--body B|--body-file F]
zatu pr edit N [--title T] [--body B]
zatu pr close N / reopen N
zatu pr merge N --method merge|squash|rebase
zatu pr comment create N --body B|--body-file F \
     [--path internal/web/x.go --line 42 [--side left]]
```

- Backend interface に PR メソッドを追加。local は `*store.Store` +
  `internal/gitrepo` の合成、remote は `/api/v1/pulls*` 経由。
  `zatu pr` は `--repo` フラグも受ける（local モードの repo 解決用）。
- **AI 向けフォーマット**: `internal/export` に `PrMarkdown` を追加。
  review comment は次の形 — 「どのファイルのどこへの言及か」を機械
  可読に保つのが目的:

  ````markdown
  ### Review comment on `internal/web/issues.go:L42` (right) · @alice · 2026-11-19T...

  `internal/web/issues.go` L40-L44:

  ```go
  func resolve(...) {
      ws, err := ...
      ...
  ```

  この行の nil チェックが抜けています
  ````

  スニペットは `git show <anchor_sha>:<path>` から対象行前後 2 行。
  **各行に `41 |` のような行番号プレフィックスは付けない** — AI は
  行番号付きコードの番号を取り違えやすいので、範囲は直前の 1 行に
  「`path` L<開始>-<終了>」とだけ載せ、コードはプレーンに載せる。
  anchor の行番号は見出しの `path:L42` が担う。sha が死んでいても
  `path:L42` と本文は残る。`--json` は同構造を struct で返す
  （エージェントが jq で絞れる）。
- API: `/api/v1/pulls`（GET/POST）、`/api/v1/pulls/{id}`（GET/PATCH/
  DELETE 相当は POST ベースの既存 API スタイルに合わせる）、
  `/comments`、`/merge`、`/diff`（text/plain で返す案）。

### 8. events / SSE

`changes` テーブルの既存機構に乗る。topic は `pr:{id}` と一覧用
`pulls`（issue/discussion と同型）。コメント・status・merge で
mutator が変更行を積む → live-updates.js が fragment を再描画。
git 側の変化（誰かが head に push した等）はアプリ外イベントなので
SSE では拾えない — **表示は常に都度計算なので鮮度問題は起きない**
（これが「open 中は git を見る」設計の副産物）。

### 9. セキュリティ・堅牢性

- **git 引数インジェクション**: ref 名・path は全て引数として渡し
  shell を通さない。`--` 区切り、ref は `git check-ref-format` 相当の
  検証、`--repo` のパスは `--git-dir` ではなく `-C`（worktree 経由）。
- **path の正当性**: review comment の `path` は `..`/先頭 `/` 禁止、
  `git diff --name-only` の集合に含まれるかを投稿時に確認（存在
  しないファイルへの anchor は拒否）。
- **resource cap**: diff 表示はファイル数/行数上限（超過は「too
  large」+ CLI での取得を促す）。`git` プロセスは `context.Context`
  で timeout（例 10s）+ 出力サイズ cap。
- **副作用の明示**: merge は破壊的 — web は GET 確認 → POST の
  PRG（削除確認と同じ 2 段階）、CLI は `--yes` がなければ確認…
  いや、gh も `gh pr merge` は即実行なので CLI は即実行でよいが、
  **in-worktree 経路で dirty なら確実に拒否**する。

## 実装順序

1. **`internal/gitrepo`**: repo 解決（auto-detect + `-repo`）＋ git
   実行ラッパ（`log`/`diff`/`name-status`/`merge-base`/`merge-tree`/
   `commit-tree`/`update-ref`/`worktree list`/`status --porcelain`）
   + merge 実行（plumbing/in-worktree 2 経路）。git <2.38 では
   conflict 判定を "unknown" に落とす。
2. **store**: `pull_requests` / `pr_comments` migration + CRUD +
   `ClosePullRequest`/`MarkMerged`（status+sha+closed_at の tx）。
3. **web**: `/pulls` 系ルート + テンプレート（一覧/詳細/files/new/
   merge 確認）。view model に `PREnabled` + `Repo` を持たせ、
   nil なら nav 非表示・ルート未マウント。
4. **cli/client/api**: Backend 拡張、`zatu pr` 群、`/api/v1/pulls*`。
   local モードの `zatu pr merge` は gitrepo を直接叩く（server 不要
   — `--db` と同じく手元完結）。
5. **export**: `PrMarkdown`（metadata + commits + comments + review
   comments with スニペット）。
6. **seed/storybook**: `zatudan.db` 自身の repo を対象にした seed PR
   は難しい（他 repo の git 状態に依存）ので storybook story は
   偽データ、実動作確認は `tools/demo-history.sh` 的な git 操作
   スクリプトを別途用意するか要検討。
7. **docs**: decisions.md / roadmap.md / ui-rules.md 更新。

## テスト方針

- `internal/gitrepo`: `t.TempDir()` に `git init` した sandbox repo で
  実コマンド検証（外部コマンド依存は許容 — このリポジトリは git
  必須の機能として扱う。CI でも git は存在する）。
  - merge-tree: clean → exit 0、conflict → exit 1 + ファイル一覧。
  - plumbing merge: base 非 checked-out で ref が進み worktree 無変化。
  - in-worktree merge: dirty → 拒否、clean → merge commit 生成。
  - ref ピン: ブランチ削除 + `git gc` 後も `git diff base...head` 可。
- store: migration、status 遷移（open→closed/merged）、anchor 付き
  コメント CRUD、CHECK 制約（path/line の組）。
- web: repo 無効時に nav/ルートが出ない（404）、有効時の一覧/詳細
  描画、merge 確認の PRG。
- cli: `zatu pr view --markdown` が `path:L42` 形式を含むこと、
  `--repo`/`--db` 解決、`--server` 経由の remote 動作。

## 見送り（future work / 意図的にやらない）

- **remote への push / `refs/pull/*` の fetch**: remote は read-only
  対象（base/head に remote-tracking ref を指せるだけ）。GitHub 等の
  実 PR との同期はしない。
- **基本的な git 操作**: commit/push/branch 作成/checkout は載せない
  （要件どおり）。merge 系だけ例外。
- **ブランチ自動検出による PR 候補**: 明示 create のみ。
- **review の approve / request-changes 概念**: コメントのみ。
- **完全スナップショット**（diff 本文の DB 保存）: ref ピンで十分。
  repo 自体が消えるケースは対象外。
- **outdated 精密判定**（force-push 後の anchor 追従）: 初版は
  ancestor 近似 + sha 消失時の "outdated" バッジのみ。
- **workspace ごとの repo** / 複数 repo 同時表示。
- **git < 2.38 の conflict 判定**: "unknown" 表示に落とすだけ
  （旧式 `git merge-tree` の parse はしない）。
- **conflict 解消 UI**: 表示のみ（要件どおり）。
