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
  base_sha/head_sha/merge_base_sha/merge_sha/method を記録する。
  コミットを DB にコピーしない（GitHub の `refs/pull/N/head` と同じ
  仕組みで gc から守る）。
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

解決のエッジケース:

- `.git` はディレクトリとは限らない — linked worktree・submodule
  では `gitdir:` を指す **ファイル**。上方探索は file も受理する。
- `-db` が `file:` DSN や `:memory:` のときは auto 起点がない →
  機能無効扱い（明示 `-repo` があればそれを使う）。
- **auto 失敗はサイレント無効、明示 `-repo` の失敗は起動エラー**
  にする（`-db` が空で fail fast するのと同じ思想 — 指定したのに
  黙って無効化されるほうが混乱する）。
- `-repo` に **bare repo**（または `.git` dir 自体）を指した場合:
  `--show-toplevel` が失敗する。plumbing 表示は可能だが
  in-worktree 経路は成立しないので、受けるなら「plumbing 限定」
  として明記（初版は非対応でエラーでもよい）。
- zatu local モードの repo 探索起点は **cwd ではなく `--db` の
  ディレクトリ** — git CLI 自身の慣習（cwd 起点）と違うので
  `--repo` で明示できる旨を help に書く。

**無効時の挙動**: repo が解決できなかったら「利用不可」ではなく
**機能が存在しない**かのように振る舞う。

- web: nav に Pull requests を出さない、`/pulls*` はルート自体を
  マウントしない（404）。
- CLI: local モードの `zatu pr ...` は `Error: not a git repository
  (set --repo or ZATUDAN_REPO)` で exit 1。**remote モードで server
  側が無効な場合は API の 404 をこの文言と混同しない**
  （"pull requests are not enabled on the server" 系に分ける）。
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
  の select）。`--head` 省略時は現在ブランチ（gh と同じ）。
  `head == base`（同じ ref/sha）は作成時に拒否。
- **ref 名は正規化して保存**: `main` / `refs/heads/main` / `origin/main`
  / sha は文字列として別物だが同じ対象を指しうるので、create 時に
  `git rev-parse --symbolic-full-name <ref>` で正規形にして保存する
  （sha だけ指定された場合は sha のまま）。checked-out 判定・重複
  チェック・merge 可否判定はすべてこの正規名で行う。
- **重複チェック**: 同じ (base, head) の open PR の重複作成は拒否
  （closed/merged 済みの同名ペアの再作成は許す、GitHub と同じ）。
  アプリ側チェックには TOCTOU があるので `CREATE UNIQUE INDEX ...
  WHERE status='open'` の部分一意 index（`idx_dthreads_answer` の
  先例）を併用する。
- **採番**: `INTEGER PRIMARY KEY AUTOINCREMENT` の独立番号。
  GitHub は issue/PR で番号を共有するが、このアプリは discussion も
  独立採番なので同じ方針。URL は `/pulls/{id}`。
- **workspace 非所属**: `/w/{ws}/pulls` ではなくトップレベル `/pulls`。
  repo が workspace と 1:1 でない現状ではスコープを偽らない。

### 3. スキーマ案

```sql
CREATE TABLE pull_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  base_ref TEXT NOT NULL,               -- symbolic-full-name 正規形 or sha
  head_ref TEXT NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'open'   -- open | closed | merged
    CHECK (status IN ('open','closed','merged')),
  author_id INTEGER REFERENCES users(id),
  base_sha TEXT,                        -- create 時点の解決値（表示用）
  head_sha TEXT,                        -- close/merge 時点の最終値
  merge_base_sha TEXT,                  -- close/merge 時点の merge-base
  merge_sha TEXT,                       -- merged のみ: 結果コミット
  merge_method TEXT,                    -- merged のみ: merge|squash|rebase
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  closed_at INTEGER
);
CREATE INDEX idx_pull_requests_status_created
  ON pull_requests(status, created_at DESC);
-- open 中の (base, head) 重複禁止（closed/merged の同名ペアは許す）
CREATE UNIQUE INDEX idx_pull_requests_open_pair
  ON pull_requests(base_ref, head_ref) WHERE status = 'open';

CREATE TABLE pr_comments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  pr_id INTEGER NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
  -- anchor: NULL なら会話コメント、非NULL なら review comment
  path TEXT,                            -- リポジトリ相対パス
  line INTEGER,                         -- anchor_sha の blob 上の行番号
  side TEXT NOT NULL DEFAULT 'right'
    CHECK (side IN ('left','right')),   -- left=削除/旧側
  anchor_sha TEXT,                      -- 行番号が参照する blob を持つコミット
  diff_snippet TEXT,                    -- 作成時に採取した周辺行（後述）
  body TEXT NOT NULL,
  author_id INTEGER REFERENCES users(id),
  edited_at INTEGER,                    -- 既存 moderation 3 列と同型
  hidden_at INTEGER,
  hidden_reason TEXT
    CHECK (hidden_reason IN ('spam','abuse','off-topic','outdated',
                             'duplicate','resolved','low-quality')),
  created_at INTEGER NOT NULL,
  CHECK ((path IS NULL) = (line IS NULL))  -- anchor は path+line セット
);
CREATE INDEX idx_pr_comments_pr ON pr_comments(pr_id, created_at);
```

| 案 | 内容 | 評価 |
|---|---|---|
| A. **1 テーブル + NULL anchor**（推奨） | `pr_comments` に path/line/side/anchor_sha を nullable で | 「PR へのコメント」は一つの時系列に並ぶ（GitHub の UI も混在表示）。テーブル数を増やさない。anchor 有無は CHECK で表現 |
| B. review comments を別テーブル | `pr_review_comments` 分離 | GitHub API の分離に忠実だが、表示時に 2 ソースを時系列マージする必要がある |

各列の意味論（レビュー指摘を反映した確定事項）:

- **`anchor_sha`**: 「行番号が参照する blob を持つコミット」。
  side=right → コメント時点の **head**、side=left → コメント時点の
  **merge-base**（削除/旧側の行は head 側 blob に存在しないので、
  スニペット取得元もそちらになる）。
- **`diff_snippet`**: コメント作成時に `git show <anchor_sha>:<path>`
  から対象行前後 2 行を採取して TEXT で保持（GitHub の review
  comment が `diff_hunk` を持つのと同型）。anchor が指す「当時の
  コード」は本来 immutable なので、DB 保持は「真実は git」原則の
  例外ではなく **過去状態の記録**として正しい。これで per-comment
  の ref ピン（`refs/zatudan/pr/N/c/<id>` 案）は不要になる —
  force-push や gc で anchor_sha のコミットが消えてもスニペットは
  表示でき、ref のリーク問題（コメント削除と ref 削除の連動）も
  回避できる。
- **`merge_base_sha`**: close/merge 時点の merge-base。closed/merged
  後に `git diff base...head` を再現するには live の base ref が
  必要で、base ブランチ削除後は base tip 自体が gc で死にうる
  （pinned head の祖先でないので保護外）。merge-base は pinned head
  の祖先なので確実に残る → closed 後の diff は
  `merge_base_sha...head_sha` で描画する。
- **moderation 列**: 既存コメント系テーブルと同じ
  `(edited_at, hidden_at, hidden_reason CHECK)` に揃え、
  `setHidden`/`scanModeration` を再利用する。動詞も
  `zatu pr comment create|edit|delete|hide|unhide` と discussion
  comment に合わせる（作成のみだと列が死に、削除と pin 系処理の
  連動も破綻するため）。
- **`merge_sha`**: method ごとの意味 — merge は merge commit、squash
  は squash commit、**rebase は replay 後の tip**（複数コミット）。

**outdated 判定**: anchor_sha が現 head でも `base..head` の歴でも
到達不能になったコメントを「outdated」表示にするか — GitHub の
精密な判定は複雑なので、初版は `git merge-base --is-ancestor
anchor_sha head_ref` で cheap に近似し、判定不能（sha 自体が無い）
なら "outdated" バッジ。**詳細は見送り節**。

### 4. 表示側の git コマンド（都度計算）

| 目的 | コマンド | 備考 |
|---|---|---|
| コミット列 | `git log --format=%H%x00%h%x00%s%x00%an%x00%aI base..head` | NUL 区切りで安全に parse |
| 変更ファイル一覧 | `git diff --name-status base...head` | 3dot = merge-base 起点（GitHub の Files changed と同じ意味論）。rename 検出のため anchor path 照合はこの old+new 集合で行う |
| diff 本体 | `git diff -U3 base...head -- <path>` | 巨大 diff は件数/行数でキャップしてリンク化 |
| merge-base | `git merge-base base head` | diff の 3dot と整合させる |
| conflict 判定 | `git merge-tree --write-tree --name-only base head` | **要 git ≥ 2.38**。下記「merge-tree 出力の解釈」参照 |
| head/base 解決 | `git rev-parse --verify <ref>` / `--symbolic-full-name <ref>` | 不存在 → PR を「head 消失」表示 |
| version プローブ | `git --version` | repo 解決時に1回実行。「conflict 判定」「plumbing merge」の双方をゲート |

**merge-tree 出力の解釈**（実測 2.56）: `--write-tree` は exit 0 =
clean（stdout = tree OID）、exit 1 = conflict。`--name-only` 付きだと
**先頭行が tree OID**、以降の非空行が衝突ファイル名、CONFLICT メッ
セージは stderr に行く（`--no-messages` で抑制可）。exit!=0 を
「conflict」と即断せず、先頭行が OID であることを確認してから
残りを衝突ファイルとして読む（bad ref 等の別要因との区別）。
**git <2.38 では `--write-tree` 自体がない**ので、conflict バッジは
"unknown" に落とし、さらに §5 の plumbing merge 経路も使えない
（in-worktree のみ）— version プローブで両方をゲートする。
unrelated histories（`--no-merge-base` が必要な相手）はエラー →
GitHub と同様に merge/作成を拒否でよい。

実行は全て `os/exec` で `git -C <repo>` 直打ち（シェル経由なし、
`--` でパスと引数を分離）。**`GIT_DIR`/`GIT_WORK_TREE`/
`GIT_INDEX_FILE` 等の `GIT_*` 環境変数は exec 時に scrub する**
（`os.Environ` をそのまま継ぐと `-C` の探索を上書きしうる）。
新パッケージ `internal/gitrepo`（仮）に集約し、web/api/cli の
3 surface から共有する。

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
| base が **いずれかの worktree で checked-out** | in-worktree | `git -C <そのworktree> status --porcelain` が空なことを確認（dirty なら拒否して手動マージを促す）→ `git merge --no-ff <head>` / `git merge --squash <head> && git commit` / rebase-merge は `git cherry-pick --no-merges base..head` |
| base が **どこにも checked-out されていない** | plumbing | `merge-tree --write-tree` → conflict なら即結果返却 → `commit-tree`（merge: `-p base -p head`、squash: `-p base`）→ `update-ref refs/heads/base` |

- checked-out 判定: `git worktree list --porcelain` の各 worktree の
  `branch refs/heads/X` 行を、§2 で正規化した base_ref と照合する
  （symbolic-ref `main` → `refs/heads/trunk` 等をすり抜けない）。
- **merge 可能な base は `refs/heads/*` に限定**（GitHub でも merge 先は
  ブランチ）。`origin/main` や sha 指定の PR は作成・表示・コメントは
  可だが **merge は明示拒否**（remote-tracking や detached sha を
  「進める ref」にできない）。
- **`update-ref` は CAS で**: `git update-ref refs/heads/base <new>
  <old_base_tip>`（第3引数に事前解決した base tip）— 並行 merge
  （web+CLI 等）で last-write-wins のサイレント lost update を防ぐ。
  close/merge 時の pin 更新も同様に oldvalue 付き。
- **rebase merge の plumbing**: `git rev-list --reverse --no-merges
  base..head` の各コミットを順に `merge-tree --write-tree <newbase>
  <c>` → `commit-tree -p <newbase>` で積む。conflict した時点で中断
  +報告。in-worktree なら `git cherry-pick --no-merges base..head`
  — **cherry-pick は merge コミットを skip せず `-m` なしではエラー
  になる**ので `--no-merges` 必須（GitHub の rebase-merge が
  merge コミットを含まない直線化なのと同じ意味論）。
- **author の保持**: plumbing の rebase replay は `commit-tree` を
  コミットごとに呼ぶため、author は `GIT_AUTHOR_NAME/EMAIL/DATE` を
  元コミットから渡さないと merge 実行者に上書きされる（cherry-pick
  は自動継承するが commit-tree はしない）。GitHub 同様 author 保持。
- **commit identity の解決**: `commit-tree`/`merge`/`commit` はいずれも
  committer identity が要る。repo に user.name/email 未設定だと全て
  `unable to auto-detect email address` で落ちるので、解決順を
  `repo config → GIT_AUTHOR_*/GIT_COMMITTER_* env → フォールバック
  "zatudan <zatudan@localhost>"`（または明示エラー）として決める。
- **in-worktree 経路の副作用を明記**: worktree の index/ファイルが
  実際に書き換わり、git hooks も実行される（plumbing 経路はどちらも
  なし）。このプロジェクトは `.worktrees/` にサブエージェント用
  worktree を置く運用なので「base が他 worktree で checked-out →
  そちらが書き換わる」は実際に起こり得る。並行 git 操作との
  `index.lock` 競合はエラーとして表面化させる。
- squash の commit message: 既定 `title (#N)`、merge は git 既定の
  `Merge branch '<head>'` 系、`-m` で上書き可。
- **status 遷移ガード**: `merged` は終端（reopen 不可、GitHub 同様）。
  `closed → reopen` は `closed_at` をクリア。`base..head` が空
  （既に取り込み済み）の merge は拒否。closed PR の merge も拒否。
- merge 成功時の DB 記録: `status=merged, merge_sha, merge_method,
  head_sha(最終), merge_base_sha, closed_at` + ref ピン。close は
  `status=closed` + head_sha + merge_base_sha + ref ピンのみ
  （git 操作なし）。
- **失敗時は何も書き換えない**: conflict / dirty worktree / 実行
  エラーはすべて「実行前」に検出する設計（merge-tree で事前判定
  してから本体操作）。それでも in-worktree で中途失敗した場合の
  復帰は method ごと: merge は `git merge --abort`、**squash は
  `--squash` が MERGE_HEAD を作らないので `--abort` は効かず
  `git reset --hard HEAD`**（dirty チェック済み前提なので安全）、
  cherry-pick は `git cherry-pick --abort`。復帰自体が失敗したら
  手動復帰を促すエラーを返す。

### 6. UI

```
GET  /pulls                       一覧（open|closed|merged|all、issue 一覧と同じ filter-bar 構造）
GET  /pulls/new                   作成フォーム（base/head select・title・body）
POST /pulls
GET  /pulls/{id}                  詳細: meta / commits / conflict バッジ / コメント時系列
GET|POST /pulls/{id}/edit         編集
GET  /pulls/{id}/files            Files changed: ファイルごとの diff + anchored comments
POST /pulls/{id}/comments         コメント投稿（optional path/line/side で review comment — discussions の parent_id と同じ「同じ POST に optional フィールド」パターン）
POST /pulls/{id}/comments/{cid}/edit|delete|hide|unhide   コメント動詞（discussions と同型）
POST /pulls/{id}/status           close / reopen
GET  /pulls/{id}/merge            merge 確認（method 選択・conflict 予告表示）
POST /pulls/{id}/merge            ?method=merge|squash|rebase → 結果/失敗を flash 表示
GET  /pulls/{id}/markdown         export（下記）
GET  /pulls/{id}/events           SSE（§8）
GET  /pulls/{id}/fragments/*      部分更新 fragment
```

- nav: `Issues | Discussions | Pull requests`（repo 無効時は非表示）。
  `/pulls` 配下は workspace 非所属なので `.Workspace` が無い —
  layout の Pull requests リンクは `{{if .Workspace}}` 条件の**外側**
  に置き（Section="pulls"）、nav 用に default ws を埋める既存
  パターン（workspaces ページと同じ）で Issues/Discussions 側の
  リンクも機能させる。
- conflict は merge-tree の結果を「This branch has conflicts in:
  a.txt, b.go」程度のバッジ表示（解決支援はしない = 要件どおり表示のみ）。
- `/files` の各行差分に「この行にコメント」フォーム（`<details>` 畳み
  込みで no-JS でも動く、discussions の reply と同じパターン）。
- commits 一覧は `git log` の subject + 短 sha + author + 日付。

### 7. CLI / API（エージェント向け — 主目的）

```
zatu pr list [--state open|closed|merged|all]
zatu pr view N [--markdown|--json F,...|--template T|-w]
                 # title+body+commits+mergeable+comments 一式
zatu pr diff N                       # unified diff 出力（remote モードでも取れる）
zatu pr create --base B [--head H] --title T [--body B|--body-file F]
                                     # --head 省略時は現在ブランチ
zatu pr edit N [--title T] [--body B]
zatu pr close N / reopen N
zatu pr merge N --method merge|squash|rebase
zatu pr comment create|edit|delete|hide|unhide N ...
                                     # create は [--path P --line L [--side left]]
```

- Backend interface に PR メソッドを追加（workspace 非所属なので
  `In(ctx, "")` の passthrough でよい）。git 側の合成は seam を
  決める: `Options.OpenLocal` が (Backend, Repo) を返す形にするか、
  `pr` コマンドが `--repo`/`--db` から `gitrepo.Resolve` を別途呼ぶか
  — 実装時に既存構造に合わせて確定。remote は `/api/v1/pulls*` 経由
  で、`--repo` は remote では無意味（警告して無視）。
- 出力規約は既存に揃える: `--json` はフィールド選択式
  （`--json F,...`）、`--template`・`-w/--web` も共通オプション。
  DTO は api パッケージの wire 型を使う。`zatu pr view` の人間向け
  出力には mergeable/conflict 状態を含める（エージェントが
  merge 可否を機械判断できるように）。
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

  スニペットは DB の `diff_snippet`（作成時に `git show
  <anchor_sha>:<path>` から対象行前後 2 行を採取したもの）をそのまま
  出す — anchor_sha が gc で消えていても表示できる。
  **各行に `41 |` のような行番号プレフィックスは付けない** — AI は
  行番号付きコードの番号を取り違えやすいので、範囲は直前の 1 行に
  「`path` L<開始>-<終了>」とだけ載せ、コードはプレーンに載せる。
  anchor の行番号は見出しの `path:L42` が担う。`--json` は同構造を
  struct で返す（エージェントが jq で絞れる）。
  `internal/export` は store 型だけを受ける純粋関数のまま保ち、
  `PrMarkdown` は呼び出し側で集めた構造体（metadata + commits +
  comments + snippets）を受ける形にする。
- API（既存の実パターンに揃える — `/api/v1` は PATCH/DELETE を
  普通に使っている）: `/api/v1/pulls`（GET/POST）、
  `/api/v1/pulls/{id}`（GET/PATCH/DELETE）、`/comments`（GET/POST）、
  `/comments/{cid}`（PATCH/DELETE + hide/unhide アクション）、
  `/status`（POST）、`/merge`（POST）、`/diff`（GET、text/plain）、
  `/markdown`（GET — remote の `view --markdown` がこれを叩く）。
  repo 無効な server では未マウント（404）。remote 経由の
  merge/status/comment は server 側で同じ gitrepo ロジックを実行。

### 8. events / SSE

`changes` テーブルの既存機構に乗る。topic は `pr:{id}` と一覧用
`pulls`（issue/discussion と同型）。コメント・status・merge で
mutator が変更行を積む → live-updates.js が fragment を再描画。
ただし `live-updates.js` の `data-live-kind` は `"issue"|"discussion"`
前提なので `"pr"` 用の分岐（resync 時の fragment 名選択・文言）は
追加が要る。イベント kind は既存の `comment`/`status`/`edit`/`new`
を再利用すれば JS 側は小変更で済む（`merge` を新 kind にすると
listener 追加が要るので避ける）。git 側の変化（誰かが head に push
した等）はアプリ外イベントなので SSE では拾えない — **表示は常に
都度計算なので鮮度問題は起きない**（「open 中は git を見る」設計の
副産物）。

### 9. セキュリティ・堅牢性

- **git 引数インジェクション**: ref 名・path は全て引数として渡し
  shell を通さない。`--` 区切り、ref は `git check-ref-format` 相当の
  検証、`--repo` のパスは `--git-dir` ではなく `-C`（worktree 経由）。
- **path の正当性**: review comment の `path` は `..`/先頭 `/`/先頭
  `-`/pathspec magic（`:(`, `::`）禁止。`git diff --name-status` の
  old+new パス集合に含まれるかを投稿時に確認（rename では
  `--name-only` が post-image 名しか返さないので side=left の旧
  パスが弾かれないよう old 側も集合に入れる）。`line` が実際に
  diff hunk 内の行かまで検証するかは実装時に決める（GitHub は
  diff 表示行のみコメント可 — 初版は hunk 内検証も入れる方向）。
- **resource cap**: diff 表示はファイル数/行数上限（超過は「too
  large」+ CLI での取得を促す）。`git` プロセスは `context.Context`
  で timeout（例 10s）+ 出力サイズ cap。
- **副作用の明示**: merge は破壊的 — web は GET 確認 → POST の
  PRG（削除確認と同じ 2 段階）、CLI は `--yes` がなければ確認…
  いや、gh も `gh pr merge` は即実行なので CLI は即実行でよいが、
  **in-worktree 経路で dirty なら確実に拒否**する。

## 実装順序

1. **`internal/gitrepo`**: repo 解決（auto-detect + `-repo` + version
   プローブ）＋ git 実行ラッパ（`log`/`diff`/`name-status`/
   `merge-base`/`merge-tree`/`commit-tree`/`update-ref`/`worktree
   list`/`status --porcelain`/`symbolic-full-name`/`check-ref-format`）
   + merge 実行（plumbing/in-worktree 2 経路）。git <2.38 では
   conflict 判定 "unknown" + plumbing merge 不可に落とす。
   **web/api/cli から呼べる interface seam**（storybook の fake と
   テストのため）。
2. **store**: `pull_requests` / `pr_comments` migration + CRUD +
   `ClosePullRequest`/`MarkMerged`（status+sha+closed_at の tx）。
   このリポジトリは squashed `schemaDDL` + `migrations` 配列の
   二重管理なので、新テーブルは `schemaDDL` への追加 **と**
   `migrateV<n>` からの適用の両方が要る（`changesDDL`/`migrateV2`
   と同じパターン — `schemaDDL` だけだと既存 DB に適用されない）。
3. **web**: `/pulls` 系ルート + テンプレート（一覧/詳細/files/new/
   edit/merge 確認/comment 動詞）。view model に `PREnabled` +
   `Repo` を持たせ、nil なら nav 非表示・ルート未マウント。
   live-updates.js に `"pr"` kind 分岐。
4. **cli/client/api**: Backend 拡張、`zatu pr` 群、`/api/v1/pulls*`。
   local モードの `zatu pr merge` は gitrepo を直接叩く（server 不要
   — `--db` と同じく手元完結）。`completion.go` の静的スクリプトと
   `printUsage` 一覧は機械的更新。
5. **export**: `PrMarkdown`（呼び出し側で集めた構造体を受ける形）。
6. **seed/storybook**: `zatudan.db` 自身の repo を対象にした seed PR
   は難しい（他 repo の git 状態に依存）ので storybook story は
   gitrepo の fake + 偽データ、実動作確認は `tools/demo-history.sh`
   的な git 操作スクリプトを別途用意するか要検討。
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
- **git < 2.38**: conflict 判定は "unknown" 表示、plumbing merge 経路
  も不可（in-worktree のみ）に落とす — 旧式 `git merge-tree` の
  parse はしない。
- **shallow clone**: `.git/shallow` がある repo は merge-base が
  取れず diff/merge 判定が degrade する。制限事項として受け入れ
  （検出したら "shallow repository" 表示で処理しないだけ）。
- **conflict 解消 UI**: 表示のみ（要件どおり）。
