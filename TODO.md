# TODO — Go classic MPA Issue Tracker

> **Note on updating this file:** upkeep rules live in
> [`.agents/skills/todo-md/SKILL.md`](.agents/skills/todo-md/SKILL.md) —
> follow them whenever you add or complete a task. Short version: this file
> lists actionable work only (`[ ]` / `[-]`); mark finished work `[x]` — a
> periodic GC sweep (`make todo-gc`) appends such entries to `TODO-archive.md`.

Source spec: `docs/ja/prompts.md` (Go 1.27+, `net/http`, `html/template`, `database/sql`, SQLite via `modernc.org/sqlite`, no JavaScript, no web frameworks, no ORM).

Module path: `github.com/podhmo/go-classic-mpa-sample`

## To Be Implemented

Guides referenced below are in the `modern-web-guidance` catalog (`.agents/skills/modern-web-guidance/`): run `npx -y modern-web-guidance@latest retrieve "<id>"` before implementing. Prefer declarative HTML/CSS; JavaScript is allowed only when its behavior can be verified via the storybook (`docs/ja/decisions.md` "JavaScript 方針") — entries marked **JS** need a `/static/` external file (CSP `script-src 'self'`; inline `<script>` stays blocked). When the first JS file lands, update the "No JavaScript." claim in `layout.html`'s footer.

- [ ] **fake/stub 向けの共有 contract test を書く**: `*store.Store` / `fakeStore` / `storyStore` に同一振る舞いスイート（bump 規則・エラー種別・フィルタ意味論）を流す。fake のポインタ共有は answer-swap ハンドラが依存しているので本物に揃えず「fake の仕様として宣言」する方向（詳細: PR #71）
- [ ] **CSS coverage 未到達ルールの仕分け**: 全 story の rule usage union で 68 ルール未到達 — pseudo-state/JS 駆動（toast, [data-copied], dialog :modal）と fixture gap（.issue-body-empty, .user-handle, .error-page, .badge-file-D/R）を分けて story 追加 or 実 app 検証に振る（詳細: [`docs/sketch/ja/rendering-and-visual-diff.md`](docs/sketch/ja/rendering-and-visual-diff.md)）
- [ ] **Long story を content-visibility 閾値超にする**: StoryIssueDetailLong/StoryDiscussionDetailLong が 24 件で `.comment-list:has(> .comment:nth-child(50))` を発火しない → 50+ 件にして CV パスを視覚検証可能にする
- [x] **/static/ の versioned URL 化**: `max-age=3600` は妥協案。hash 入り URL + `immutable` にすると長期キャッシュが効く（modern-web-guidance performance の CacheFirst 方針、[`docs/sketch/ja/rendering-and-visual-diff.md`](docs/sketch/ja/rendering-and-visual-diff.md)）
- [ ] **visual-diff の拡張**: (a) dark モードの baseline を別途撮る、(b) JS 駆動状態（dialog open / toast / copied）の story 化または実 app 撮影で `.toast` 等の視覚カバレッジを取る
- [x] **`createPull` の base/head 正規化エラーを分離する**: 片方の `NormalizeRef` エラーがもう片方で上書きされないようにし、不正 ref を 422 として返す（[bugs.md #37](docs/ja/bugs.md)、[owner-improvements §1.2](docs/sketch/ja/owner-improvements.md)）
- [x] **API と web の PR 一覧で page をクランプする**: 巨大な `page` で `(page-1)*perPage` がオーバーフローしないよう、store 層の共通クランプに寄せる。API・web の回帰テストを足す（[bugs.md #38](docs/ja/bugs.md)、[owner-improvements §1.4](docs/sketch/ja/owner-improvements.md)）
- [x] **PR diff の切り詰めをクライアントへ通知する**: API は `X-Truncated: true` を返し、remote CLI は不完全な diff であることを警告する（[bugs.md #41](docs/ja/bugs.md)、[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [x] **remote CLI の HTTP client に timeout を付ける**: 共有 `client.HTTPClient` に 30s 程度の timeout を設定し、無応答サーバーで CLI が永遠に待たないようにする（[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [x] **pin 更新エラーの扱いを揃える**: api/cli/web で `repo.UpdatePin` の失敗を少なくともログへ記録する。必要なら共通 helper へ寄せる（[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [x] **`CheckMerge` に解決済み SHA を渡す**: ref 名ではなく `RevParse` 済みの base/head SHA で `merge-tree` を実行し、報告 SHA と判定対象を一致させる（[bugs.md #39](docs/ja/bugs.md)、[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [x] **`pullsEnabled` を Renderer ごとの状態にする**: global variable をやめ、同一プロセス内の複数 handler/storybook が競合しないようにする（[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [x] **hide reason の Bad request 見出しを翻訳する**: `comments.go` / `pulls.go` の直書き `"Bad request"` を `msgFor(r).ErrBadRequestH` に置き換える（[bugs.md #40](docs/ja/bugs.md)、[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [x] **`Accept-Language` の q 値を解釈する**: `en, ja;q=0.1` で英語を選ぶよう簡易 q パースを実装し、`wantsJapanese` の回帰テストを足す（[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [x] **SQLite を WAL + immediate transaction にする**: `_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate` を検証し、並行 writer で `SQLITE_BUSY` が出ないテストを足す（[owner-improvements §1.3](docs/sketch/ja/owner-improvements.md)）
- [x] **`/static/` を `withUser` の外に出し gzip + ETag を付ける**: 静的ファイルごとのセッション照会をなくし、事前圧縮と 304 を入れる。versioned URL 化は既存タスクで後段とする（[owner-improvements §2.3](docs/sketch/ja/owner-improvements.md)）
- [x] **workspace 解決から COUNT(*) を外す**: slug→ID の軽量クエリを用意し、件数が必要な画面だけで数える。`/w/{ws}/...` 全リクエストの定常クエリを減らす（[owner-improvements §2.1](docs/sketch/ja/owner-improvements.md)）
- [x] **markdown レンダリング結果を LRU キャッシュする**: `sha256(body)` をキーに表示時変換の結果だけキャッシュし、DB に生 markdown を置く方針は維持する（[owner-improvements §2.4](docs/sketch/ja/owner-improvements.md)）
- [x] **PR 詳細の git 呼び出しをまとめる**: `rev-parse` の束ね方と `(baseSHA, headSHA)` キーの小さな cache を検討し、`Log` / `CheckMerge` の重複実行を減らす（[owner-improvements §2.6](docs/sketch/ja/owner-improvements.md)）
- [x] **テンプレート parse をロケールごとの clone に寄せる**: layout+fragments を 1 回 parse して `Clone()` し、`jsDictFor` の JSON 化もロケール単位にする（[owner-improvements §2.7](docs/sketch/ja/owner-improvements.md)）
- [x] **API のラウンドトリップを減らす**: workspace 解決を scoped update に寄せる、`GetComment` または `INSERT ... RETURNING` を足す、`LabelByName` を足す、の小さな store API 拡張を行う（[owner-improvements §2.8](docs/sketch/ja/owner-improvements.md)）
- [ ] **hidden タブの SSE を閉じる**: `visibilitychange` で hidden 時に `EventSource` を閉じ、visible 時に再接続 + resync する。storybook/実 app で確認する（**JS**、[owner-improvements §2.9](docs/sketch/ja/owner-improvements.md)）
- [ ] **web handler の小さな定型重複を削る**: `requireIssue` / `requireDiscussion`、generic な `filterByID`、フォーム page builder を入れ、未使用の `Renderer.execute` / `Renderer.notFound` を整理する（[owner-improvements §3.4](docs/sketch/ja/owner-improvements.md)）
- [ ] **重複テンプレートを共通化する**: pager と delete confirm の双子テンプレートを整理し、pulls 側の空 `q=` 出力も直す。markup 差分は storybook で確認する（[owner-improvements §3.5](docs/sketch/ja/owner-improvements.md)）
- [ ] **i18n catalog を分割し ja の空フィールドを検出する**: `messages.go` / `messages_en.go` / `messages_ja.go` / `locale.go` に分け、en fallback に黙って落ちるフィールドをテストで防ぐ（[owner-improvements §3.6](docs/sketch/ja/owner-improvements.md)）
- [ ] **`internal/cli/pulls.go` と `internal/client/pulls.go` の単体テストを足す**: 状態遷移・エラー写像・markdown/diff 出力の最低限を固定する（[owner-improvements §3.7](docs/sketch/ja/owner-improvements.md)）
- [ ] **古いドキュメント記述を現状に揃える**: labels 管理画面は実装済み、`style-src-elem` の CSP 記述は実装とコメントのどちらかに揃える。CSP hash 化の可否は実ブラウザで確認する（[owner-improvements §3.8](docs/sketch/ja/owner-improvements.md)）
- [ ] **writing experience を1PRで実装する**: Write/Preview タブ + `POST /preview`、localStorage の下書き自動保存（投稿成功時だけ削除・保持期限/手動クリアなし）、server-side `#12` リンク化をまとめて入れる（[open questions](docs/ja/open-questions.md) 回答済み、[owner-improvements §5.1](docs/sketch/ja/owner-improvements.md)）
- [ ] **モバイル nav を `<details>` メニューに畳む**: 小幅 viewport では layout のリンク群を `<details>`/`<summary>` に畳み、storybook の `?w=360` と keyboard-only 操作で確認する（[open questions](docs/ja/open-questions.md) 回答済み、[owner-improvements §5.2](docs/sketch/ja/owner-improvements.md)）
- [ ] **検索クエリパーサを FTS より先に入れる**: `is:open label:bug author:demo` 形式を web/CLI/API 共通の文法として parse し、条件はすべて AND にする。FTS5 への置き換えは後段とする（[open questions](docs/ja/open-questions.md) 回答済み、[owner-improvements §2.2](docs/sketch/ja/owner-improvements.md)）
- [ ] **remote CLI の 404 を endpoint 文脈付きで写像する**: API 変更は入れず、record missing と分かる endpoint だけ `store.ErrNotFound` に写像する。missing route を record missing に誤分類しない（[open questions](docs/ja/open-questions.md) 回答済み、[bugs.md #27](docs/ja/bugs.md)）
- [ ] **PR files の不正マークアップと N+1 git 起動を直す**: 行コメント form は JS を増やさず `?line=` 付き GET でサーバー側に描画する。テーブル化や word-diff などの大規模再設計は今回入れない（[open questions](docs/ja/open-questions.md) 回答済み、[owner-improvements §2.5](docs/sketch/ja/owner-improvements.md)）
- [ ] **更新 API に任意 `If-Match` と web hidden version を入れる**: `If-Match` 指定時だけ `version` を照合し、web フォームは常に hidden version を送る。不一致時の差分表示は実装しやすい最小範囲に留める（[open questions](docs/ja/open-questions.md) 回答済み、[owner-improvements §4.1](docs/sketch/ja/owner-improvements.md)）
- [ ] **PR merge を最小 CAS で直列化する**: まず api/cli/web の3箇所に merging 予約を入れ、service 抽出は後で行う。lease/復旧は実装しやすい最小範囲に留める（[open questions](docs/ja/open-questions.md) 回答済み、[bugs.md #36](docs/ja/bugs.md)）
- [ ] **PR の pin 欠損を修復可能にする**: pin 失敗で PR 作成自体を巻き戻す設計に寄せず、pin 欠損を検出して後で再作成できる導線を持たせる（[open questions](docs/ja/open-questions.md) 回答済み、[owner-improvements §1.6](docs/sketch/ja/owner-improvements.md)）
- [ ] **ログイン後に元のページへ戻る**: login link に safe な `?next=` を付け、認証後に元の path+query へ戻す（[owner-improvements §5.2](docs/sketch/ja/owner-improvements.md)）
- [x] **ES module の waterfall を modulepreload で平らにする**: page detail 系の依存 module を `<link rel="modulepreload">` で並列化し、バンドルしない方針は維持する（[owner-improvements §2.3](docs/sketch/ja/owner-improvements.md)）
- [x] **i18n の全面適用**: `docs/sketch/ja/i18n.md` 案C（薄いカタログ + ロケール別テンプレートセット + `?lang=`/cookie スイッチャ）を全画面へ横展開。実施済み: store 層 `FieldError` のコード化→web 層で訳す（`fieldErrText`）、静的 JS の文字列は `<html data-i18n>` の JSON → `i18n.js` の `t()`、`fmtTime`/`authorName`/`hideReasonLabel`/`noun`/`repliesClause` はロケール別 funcs 束縛、文中リンクは Pre/Link/Tail 分割フィールド、スイッチャは `GET /locale?lang=` が Referer のパス+クエリへ 303。残課題: UGC（issue 本文・コメント）の `lang`/`dir` 付け
- [x] **Pull requests を git コマンドで再現する**: `docs/sketch/ja/pull-requests.md` の設計どおりに実装。実体は DB 行 + (base_ref, head_ref)、コミット列・diff・conflict は open 中 git 都度計算、close/merge 時に `refs/zatudan/pr/<id>/head` を pin。repo 未検出時は機能ごと非表示（`-repo`/`ZATUDAN_REPO`、auto は -db 起点の `.git` 上方探索）
  - [x] **api: /api/v1/pulls\***: pulls GET/POST、{id} GET/PATCH/DELETE、comments GET/POST、comments/{cid} PATCH/DELETE + hide/unhide、status POST、merge POST、diff GET（text/plain）、markdown GET。repo 無効 server では未マウント（404）（§7）
  - [x] **cli: zatu pr**: list/view/diff/create/edit/close/reopen/merge/comment 動詞（create は `--path P --line L [--side left]`）。local モードは `--db` 起点の repo 解決（`--repo` 上書き、未検出は `not a git repository` で exit 1）、remote は /api/v1/pulls\* 経由（`--repo` は警告して無視）。completion.go と printUsage の機械的更新（§7）
  - [x] **export: PrMarkdown**: review comment を `path:L42` (side) + `path L<開始>-<終了>` + プレーン fenced スニペット（DB の diff_snippet、行番号プレフィックスなし）+ 本文の形で出力。呼び出し側が集めた構造体を受ける純粋関数。`--json` は同構造の struct（§7）
  - [x] **seed/storybook/demo**: storybook story は gitrepo の fake + 偽データ（PR #103）。実動作確認用スクリプトは `tools/demo-pr-history.sh`（fixture repo + `zatu pr` リプレイ）
  - [x] **docs 更新**: decisions.md（PR の実体・ref pin・merge 2 経路・auto-detect の解決規則）、roadmap.md、ui-rules.md（/pulls の UI 規約）
