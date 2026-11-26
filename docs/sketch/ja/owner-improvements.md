# コードオーナー視点の改善案 — 棚卸しメモ

調査・設計メモ（実装前の検討段階）。2026-11-26 時点の main（`00d4516`）を対象に、
コードを読んで見つけた改善候補を4つの観点で棚卸しした。

問い:

> あなたはコードのオーナーです。自由に変更できます。どのような改善をしますか？
> コードの改善。機能の追加/洗練。UX の洗練。性能の改善。

調査範囲は `internal/{store,api,cli,client,gitrepo,export,web}` と `cmd/` のコード、
`docs/ja/decisions.md` / `docs/ja/ui-rules.md` の確定事項。決定済みの方針
（authorization を入れない、SPA 化しない、バンドルしない、markdown は表示時変換、
ダークモードのトグルを作らない、など）は覆さない前提で書いている。覆す提案をする箇所は
明示する。

## 結論を先に

最初にやるべき 5 件:

1. **PR merge の二重実行を防ぐ**（§1.1）。squash merge は並行リクエストで 2 回適用されうる。
   git 操作の前に DB で「merging」を CAS で確保する。
2. **SQLite を WAL + `_txlock=immediate` にする**（§1.3）。
   SSE の 250ms ポーリング・CLI プロセス・web の書き込みが同居している割に、ロック設定は既定のまま。
3. **PR の業務ロジックを `prservice` に一本化する**（§3.1）。
   create/close/merge/comment が api・cli・web に3重実装されており、§1.1・§1.5・§1.8 はここを直さないと3回直すことになる。
4. **`/static/` を `withUser` の外に出し、圧縮 + ETag を付ける**（§2.3）。
   今は静的ファイル1本ごとにセッション照会の DB クエリが走っている。
5. **PR files 画面を作り直す**（§2.5 + §5.3）。
   diff 1行ごとに hidden form を出していて、大きな diff で HTML が MB 単位になる。
   マークアップも `<pre>` の中にブロック要素を入れていて不正。

小さく確実なバグ修正（§1.2 normErr、§1.4 API の page 上限、§1.9 CheckMerge の TOCTOU）は、上記と独立に即日直せる。

---

## 1. バグ・堅牢性

### 1.1 並行 merge で squash が二重適用される（重要度: major）

`mergePull`（`internal/api/pulls.go:662-716`。web と cli にも同形のコードがある）の手順は次のとおり。

1. `pr.Open()` を確認する
2. `repo.Merge(...)` で git の書き込みを行う
3. `store.MarkMerged`（`WHERE status='open'`）を呼ぶ

merge/rebase でも、2本目が1本目の base 更新前に読んだ同じ SHA で ref 更新を競わせるなら git 側の CAS で失敗する。ただし、1本目の更新後に base を読んだ場合は rebase でも空の差分をコミットでき、squash では head が base の祖先にならないため常に2回目の squash を作りうる。`MarkMerged` は後から `ErrInvalidPRTransition` で失敗するが、git 側の副作用はもう残っている。

**案**: `pull_requests` に `merging_at` 列（または一時 status `merging`）を足し、
`UPDATE ... SET merging_at=? WHERE id=? AND status='open' AND merging_at IS NULL` で
所有権を取ってから git を叩く。失敗時は解放するが、プロセスが予約を持ったまま終了した場合に備えて、起動時または期限切れ予約の回収時に git 側の適用有無を照合して解放する復旧手順も必要になる。local CLI は別プロセスなので、in-process の mutex だけでは足りない。DB の CAS が要る。

### 1.2 `createPull` の `normErr` 上書き（重要度: minor）

`internal/web/pulls.go:367-373` は base と head の `NormalizeRef` エラーを同じ変数に代入している。
base が不正で head が正しいと、base のエラーが消える。不正な base はそのまま `RevParse` に渡って 500 になる。
422「refs invalid」になるべきケース。
**案**: `baseErr` / `headErr` に分けて `errors.Join`。

### 1.3 SQLite の並行設定

`store.Open`（`internal/store/store.go:471`）の DSN は `busy_timeout(5000)&foreign_keys(1)` だけ。

- rollback journal のままなので、読み手が書き手をブロックする。読み手には SSE の `WatchChanges` の 250ms ポーリング、一覧、詳細が含まれる。
- tx は既定の DEFERRED。read-then-write の tx（`CreateComment` / `UpdateIssue` など）は書き込みへの昇格時に SQLITE_BUSY で即失敗しうる。これは busy handler の対象外。

**案**: `_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate`。
書き込み専用の 1 接続プールを分ける案もあるが、まずは pragma だけで効果を測る。
`go test` の並行 writer テスト（SQLITE_BUSY が出ないこと）で固定する。

### 1.4 API の `page` に上限がない

web の issue/discussion 一覧は `1..1_000_000` に制限済み（bugs.md #2）だが、`internal/api/api.go:338` の `queryInt` と web の PR 一覧には上限がない。
巨大な page を渡すと `(page-1)*perPage` がオーバーフローする。
**案**: store 層の共通 `normalizePage(page, perPage)` に寄せ、API と web の PR 一覧を含む全呼び出し元を一度に守る。

### 1.5 merge が HTTP の WriteTimeout を超えうる

`WriteTimeout: 15s`（`cmd/zatudan/main.go:147`）に対し、git は 1 コマンドごとに 15s の予算を持つ。
rebase はコミット数 × 約5回 git を呼ぶ。そのため、merge は成功したのにクライアントは切断を見る、という状態が起こりうる。
**案**:
- merge 全体に 1 つの context deadline を付ける。
- merge ハンドラだけ `http.ResponseController.SetWriteDeadline` で延長する。
- per-commit のメタデータ読み出しを `git cat-file --batch` にまとめる。

### 1.6 その他の小粒

- **diff の切り詰めが無言**: `internal/api/pulls.go:725` は 8MiB で切った diff を何の印もなく返す。`X-Truncated: true` ヘッダで知らせる。
- **remote CLI の HTTP client に timeout がない**: `internal/client/client.go:73`。30s 程度を既定にする。
- **pin 更新エラーの扱いが不揃い**: `_ = repo.UpdatePin(...)`（api/cli）は無視、web はログを出す。最低限ログに揃える。
- **`CheckMerge` の TOCTOU**: `internal/gitrepo/ops.go:284-299` は SHA を解決したあと、ref 名で `merge-tree` を実行している。報告する SHA と判定結果がずれうる。解決済みの SHA を渡す。
- **git subprocess を書き込み tx 内で実行している**: `CreatePullRequest` は tx 内で `git update-ref`（最大 15s）を実行しており、その間 RESERVED ロックを握り続ける。commit 後に pin し、失敗したら補償削除する。
- **global `pullsEnabled`**: `internal/web/web.go:156` で `New` の中から書き換えている。同一プロセスで複数の handler を作ると（テスト、storybook）競合する。Renderer ごとの設定にする。
- **i18n 漏れ**: `comments.go:295,437,573` と `pulls.go:894` の見出しが英語 `"Bad request"` の直書き。`msgFor(r).ErrBadRequestH` を使う。
- **`Accept-Language` の q 値を無視している**: `wantsJapanese`（`i18n.go:1330`）は `en, ja;q=0.1` で ja を選ぶ。簡易な q パースで直る。
- **bugs.md #27**: `*client.Error` に `Is(target error) bool` を実装し、404 を `store.ErrNotFound` に写像する。1メソッドで local と remote の乖離が消える。

§1.1・§1.2・§1.4・§1.6（CheckMerge・i18n 漏れ・diff 切り詰め）は `docs/ja/bugs.md` に #36〜#41 として追記した。

---

## 2. 性能

計測（`rendering-and-visual-diff.md`）によれば、今のデータ量では LCP 70ms で十分速い。
ここでの改善は「件数が増えたとき・PR が大きいとき・タブを多く開いたとき」に効くものに絞る。

### 2.1 ワークスペース解決の度に COUNT(*) を2本

`findWorkspace`（`internal/store/workspaces.go:83-89`）は issues と discussions の件数をサブクエリで毎回数える。
これが `/w/{ws}/…` の全 API・web リクエストで走っている。
**案**: `WorkspaceIDBySlug` のような軽量版を作り、件数は一覧・詳細画面だけで取る。

### 2.2 全文検索を FTS5 に

`instr(lower(title))… OR instr(lower(body))…` はテーブルを全件走査し、件数用の `COUNT(*)` も同じ述語でもう1回走査する。
**案**:
- `issues_fts` / `discussions_fts` / `pull_requests_fts`（external content + trigger 同期）を作り、`MATCH` を使う。
- 日本語を考えるなら `trigram` tokenizer。modernc sqlite は FTS5 を同梱している。
- 部分一致の意味論（現在の substring）は trigram ならほぼ維持できる。3文字未満のクエリは従来の instr にフォールバックする。

### 2.3 静的配信: `withUser` の外へ、圧縮、ETag

- `withUser`（`internal/web/web.go:375`）が `/static/*` も包んでいる。
  詳細ページは約 12 本の ES module を読み、その1本ごとに `SessionUser` の DB クエリが走る。
  DB の一時エラーで JS 読み込みが HTML の 500 になる点も問題。
  `/static/` は外側の mux に登録する。
- **圧縮がない**。app.css は約 59kB。起動時に embed の中身を gzip（可能なら br）で事前圧縮し、`Accept-Encoding` を見て返す。HTML は gzip middleware を通す。
- **ETag がない**。embed.FS に modtime がないため、`max-age=3600` が切れると全量を取り直す。起動時にファイルごとの SHA-256 を ETag にすれば 304 になる。
  TODO の「versioned URL 化」の前段として安く入る。versioned URL（`/static/app.<hash>.css` + `immutable`）まで行けば ETag も不要になる。
- **`<link rel="modulepreload">`**: page-detail.js → partial-submit.js → fragments.js… の 3 段 waterfall を平らにする。バンドル不採用（decisions.md）とは矛盾しない。

### 2.4 Markdown のレンダリング結果をキャッシュする

`markdown` 関数は表示のたびに goldmark を回す。対象はコメントごと、SSE fragment の再取得、resync を含む。
「DB には生 markdown、表示時変換」（decisions.md）は維持したまま、
`sha256(body)` キーの LRU（数千件、`template.HTML`）を挟めば決定を覆さずに済む。
50+ コメントのスレッドで効く。

### 2.5 PR files 画面

`internal/web/pulls.go:574-607` は次の作りになっている。

- `ChangedFiles` のあと、ファイルごとに `git diff` を起動する。
- diff の全行に hidden form（hidden 4 本 + textarea + button）を出す。

2000 行の diff なら form も 2000 個になる。
**案**:
- `git diff` は1回だけ実行し、ファイル単位に分割する。
- コメントフォームは1個を共有する。JS で該当行の直後へ移すか、`?line=` 付き GET でサーバー側で開く。
- N 行を超えるファイルは `<details>` で畳み、「大きな diff を読み込む」にする。

### 2.6 `GET /pulls/{id}` は git を 5 回起動する

`Log` + `CheckMerge`（`RevParse` ×2 + `MergeBase` + `merge-tree`）。
結果は (baseSHA, headSHA) のペアに対して不変なので、このペアをキーにした LRU に載せられる。
`rev-parse a b` も 1 回にまとめられる。

### 2.7 テンプレートの parse

22 ページ × 2 ロケール = 44 回、layout.html と fragments.html を毎回含めてフル parse している（`render.go:38-68`）。
**案**: ロケールごとに layout + fragments を 1 回 parse し、`Clone()` してから page を足す。
起動時間とメモリが減る。`jsDictFor` の JSON 化はページ描画ごとではなく、ロケールごとに 1 回にする。

### 2.8 API のラウンドトリップ削減

- `updateIssue` は約 10 文を発行している（workspace 解決 3 サブクエリ、GetIssue ×2、label の DELETE + N INSERT）。
  store に wsID を渡して `WHERE id=? AND workspace_id=?` でスコープすれば、事前読み出しが消える。
- コメント 1 件を取るのに `ListComments` → 線形探索している（api `createComment`、`requirePRComment`、`findPRComment`）。
  `GetComment` を追加するか、`INSERT … RETURNING` を使う。
- ラベル名の解決で全ラベルを取得している。`LabelByName` を追加する。

### 2.9 SSE の接続数

HTTP/1.1 の localhost では同一オリジンの接続が 6 本までなので、詳細タブを 6 枚開くと以降の遷移が詰まる。
**案**（軽い順）:
1. `visibilitychange` で hidden のタブは `EventSource` を閉じ、visible で再接続 + resync する。resync の仕組みはもうある。
2. `BroadcastChannel` でタブ間に 1 本だけ共有する。
3. h2（TLS）運用を README に書く。

1 だけで実用上は十分。

---

## 3. コード品質

### 3.1 PR の業務ロジックを `internal/prservice` に

create（pin）、close（head 解決 + merge-base + `UpdatePin`）、merge（既定の squash メッセージ、`UpdatePin`、`MarkMerged`）、行コメントのアンカー生成が、
`internal/api/pulls.go`、`internal/cli/pulls.go`、`internal/web/pulls.go` に 3 重に書かれている。
`prservice.{Create,Close,Reopen,Merge,Comment}(store, repo)` に寄せる。§1.1・§1.5・§1.6（pin エラー）は 1 箇所の修正で済むようになる。
issues/discussions は store 呼び出しが薄いので、今のところ service 層は不要と判断する。

### 3.2 store の書き込み定型を helper に

`BeginTx → Exec → checkAffected → commit → changes 記録` が約 36 回繰り返されている。
`s.write(ctx, func(tx *sql.Tx) error, changes...)` にまとめれば、§1.3 の immediate tx やリトライの差し込み口も 1 箇所になる。

### 3.3 インターフェースの分割と ScopedStore の再利用

`api.Store`、`cli.Backend`、`web.Store` がそれぞれ 50 メソッド超ある。
API は `requireScope` を 15 回手で書いているが、同じことをする `store.ScopedStore` がもうある。

- API を ScopedStore に乗せる。
- インターフェースはリソース単位（`IssueStore` / `DiscussionStore` / `PRStore`）に割る。

fake の実装量も減り、TODO の「fake/stub 向けの共有 contract test」も書きやすくなる。

### 3.4 web ハンドラの定型

- `parseID → GetX → xError → requireScope` が issues.go に 8 回、discussions.go に 8 回、events.go に 7 回ある。
  `requirePull` と同じ形の `requireIssue` / `requireDiscussion` を作る。
- モデレーションのハンドラ: comments.go に 18 本（comment/thread/reply × edit/update/confirm/delete/hide/unhide）、pulls.go に 6 本ある。
  小さな `moderationTarget` インターフェース（load、URL、update、delete、setHidden、fragment 名）による汎用コントローラにまとめると、約 400 行減る見込み。
- `?ids=` の fragment フィルタが 4 箇所にコピーされている。ジェネリック関数 `filterByID[T]` にする。
- フォームのエラー再描画で、ページ構造体を 2 回ずつ組み立てている。`newIssueFormPage(...)` 型の builder を作る。
- 未使用コードを整理する: `Renderer.execute`、`Renderer.notFound`。`renderError` は storybook からだけ呼ばれている。

### 3.5 テンプレートの共通化

- ページネーションの nav が issues、discussions、pulls の 3 テンプレートにコピーされている。`{{define "pager"}}` にする。
  pulls 側は空の `q=` を出し、最終ページへのクランプが `listPulls` にない。共通化で重複を減らし、クランプは取得処理で直す。
- `issue_delete.html` と `discussion_delete.html` は双子。
- 一覧の行マークアップも 3 回書かれている。

### 3.6 巨大ファイルの分割

- `i18n.go`（1454 行）は `messages.go`（struct）、`messages_en.go`、`messages_ja.go`、`locale.go` に分ける。
  **ja の空フィールドを検出するテスト**も足す。今は en に黙ってフォールバックしている。
- `web/pulls.go`（1134 行）は diff の解析とアンカー、コメント、merge に分ける。
- `story_pages.go`（1466 行）は fixture と Story 関数に分ける。

### 3.7 テストの穴

- `internal/cli/pulls.go`（894 行）と `internal/client/pulls.go` に単体テストがない。
- 並行 merge、API の巨大 page、並行 writer で SQLITE_BUSY が出ないこと、のテストがない。
- policy.md の「怪しい箇所を固定するテスト」として、§1 の修正ごとに 1 本ずつ書く。

### 3.8 ドキュメントの古い記述

- `decisions.md:79` は「ラベル管理画面は実装しない」のままだが、`labels.go` と `labels.html` はもうある。
- `layout.html:13` のコメントは「style-src-elem で hash 固定」と書いているが、実際の CSP は `'unsafe-inline'`。
  hash 化できるか（Chrome の view transition キャプチャ用の `<style>` が要るか）を実ブラウザで確かめてから、どちらかに揃える。

---

## 4. 機能の追加・洗練

### 4.1 楽観的排他（ETag / If-Match）

PATCH は read-modify-write で前提条件を見ないため、同時編集は後勝ちで黙って消える。
CLI 経由の AI エージェントと人が同じ issue を触る、というこのアプリの想定では起こりやすい。

- 編集ごとに進む `version` 列を足し、それを strong ETag として返す。`updated_at` は秒精度なので同じ秒の編集を区別できず、weak ETag は `If-Match` に使えない。
- 書き込み側は `WHERE version=?` で同時に照合し、`If-Match` 不一致なら 412 にする。
- web の編集フォームには hidden の `version` を持たせ、不一致なら「他の人が更新しました」と差分を示して再編集させる。
- GET には `If-None-Match` → 304 も付ける。

### 4.2 検索・フィルタの拡充

- issue: author、複数ラベル（AND/OR）、`sort=updated`。
- discussions と pulls: ラベル絞り込み（`discussion_labels` はもうある）。
- 簡易クエリ構文: `is:open label:bug author:demo foo`。
  検索窓 1 つで web と CLI が同じ文法を共有できる。パーサは store 側に置き、`ListFilter` に落とす。
  FTS（§2.2）とあわせて入れる。

### 4.3 API の改善

- `Link` ヘッダ（rel=next/prev）。長期的には keyset cursor（`?after=<created_at,id>`）。
  クライアントが 100 件の上限を越えるためにページを繋いでいる処理が単純になる。
- コメントの一覧・単体取得エンドポイント。今は issue 詳細から読んでいて、404 の意味が local と remote で違う。
- API トークンに `last_used_at`、`expires_at`、任意の scope（read/write）を持たせる。
  共有トークンか既存の DB トークンで API が登録済みなら、Bearer 認証はリクエストごとに DB を引く。残る問題は、トークンなしで起動してから最初のトークンを発行した場合に API が mount されないことなので、この起動条件を解消する。

### 4.4 PR のレビュー体験

- 行コメントを、その行の直下にインライン表示する。今は file card の末尾にまとめて出ている。
- 旧・新 2 列の行番号、ファイルツリー、ファイルごとの折り畳み、「viewed」チェック（localStorage）。
- word-diff（`git diff --word-diff=porcelain`）の切り替え。

### 4.5 一覧のライブ化（軽量）

サーバーは一覧の topic をもう配信している（decisions.md:524）が、それを受けるクライアントがない。
一覧で自動差し替えはせず、「新しい項目が N 件あります — 再読み込み」バナーだけ出す。
MPA の原則（ユーザー操作でページを取り直す）を崩さない範囲に収まる。

---

## 5. UX の洗練

### 5.1 書く体験

- **Write / Preview タブ**: 既存の fragment 基盤を使い、`POST /preview` → サーバー側 markdown → fragment を返す。
  JS は送信と差し替えだけ。CSP も storybook 検証経路も今のまま使える。
- **下書きの自動保存**: textarea の内容を `localStorage` にキー `draft:<path>:<form>` で保存し、送信成功で消す。
  今は leave-guard.js の beforeunload 警告しかなく、タブのクラッシュやブラウザの再起動で書きかけが消える。
- **@mention / #参照の補完**: `#12` を issue リンクにする markdown 拡張から始める（サーバーだけで完結）。
  補完 UI はその後。

### 5.2 ナビゲーション

- **ログイン後に元のページへ戻る**: `layout.html:50` の `/login` に `?next=` を付ける。`safeNext` は既にある。
- **/pulls で workspace を失わない**: `navWorkspace` は先頭の workspace を選んでいる。最後に見た workspace を cookie に覚える。
- **キーボードショートカット**: `/` で検索にフォーカス、`c` で新規作成、`j`/`k` で一覧の移動、`?` でヘルプ dialog。
  修飾キーなしのキーは入力中に無効化し、WCAG 2.1.4 に従ってヘルプから無効化できるようにする。
- **モバイルのナビ**: `.site-nav` は約 9 項目を `gap:16px` で折り返すので、狭い画面では 3 段になる。
  `<details>` によるメニュー化か、横スクロール（`overflow-x:auto` + `scroll-snap`）にする。
  storybook の `?w=360` で確かめる。

### 5.3 PR files 画面のアクセシビリティ

- `<pre class="diff">` の中に `<div>`、`<details>`、`<form>` がある。不正なマークアップ。
  行は `<table>`（または `role="table"`）で組む。
- 行コメントの textarea にラベルがない（ui-rules §3 違反）。視覚的に隠したラベル「{path} の {N} 行目にコメント」を付ける。
- `.line-comment > summary { display: contents }`（`app.css:2169`）は、エンジンによってはフォーカスできなくなる。実ブラウザでキーボード操作を確かめ、ダメなら実体のある box にする。

### 5.4 細部

- 空状態に次の行動を置く。例: 「まだ issue がありません — 作成する」、フィルタで 0 件なら「フィルタを解除」。
- 一覧で一括操作（close、label）。`<form>` + チェックボックスで素直に実装でき、JS は不要。
- フィルタの状態保存は**しない**。URL が正であるという MPA の性質を優先する。

---

## 6. やらないこと（再確認）

既存の決定に沿い、今回の提案には含めない:

- authorization、ログインのレート制限の本格化、session cookie の `Secure` 常時付与。
  ただし `r.TLS != nil` のときに `Secure` を付けるのは安価なので入れてよい。
- assignee、avatar、相対時刻（ui-rules §5、§8）。
- ダークモードのトグル、SPA 化、htmx/Turbo、JS のバンドル。
- 書き込み時の markdown プリレンダリング（§2.4 の表示時キャッシュで代える）。

## 7. 進め方の案

| 段 | 内容 | 規模 |
|----|------|------|
| A | 小粒バグ: §1.2、§1.4、§1.6（CheckMerge、i18n 漏れ、q 値、client timeout、#27）+ 固定テスト | S |
| B | SQLite WAL/immediate（§1.3）+ 並行 writer テスト、`/static/` の外出し + gzip + ETag（§2.3） | S〜M |
| C | `prservice` 抽出（§3.1）→ merge の CAS（§1.1）、deadline（§1.5）を 1 箇所で | M |
| D | PR files の作り直し（§2.5、§5.3、§4.4 の一部） | M〜L |
| E | 定型の削減: `s.write`、`requireIssue`、モデレーションコントローラ、pager テンプレート、ファイル分割（§3） | M |
| F | FTS5 + クエリ構文（§2.2、§4.2） | M |
| G | 書く体験: Preview、下書き保存、`#N` リンク（§5.1）、ETag/If-Match（§4.1） | M |

A と B は独立なので、サブエージェント 2 本で並行できる。C と D は同じファイル（pulls 系）に触るので直列にする。
着手時は `TODO.md` に分解して追記する。
