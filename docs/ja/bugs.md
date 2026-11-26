# 既知のバグ・問題点（Bugs）

実装・検証中に見つかった問題を記録する。すべて修正するとは限らない — 重要度と対応状況を併記する。

発見方法: 実サーバーでの手動スモークテスト + マージ後の専用 QA パス（エッジケースの curl 検証・コードレビュー）。

## 未修正

| # | 重要度 | 状態 | 概要 | 詳細 |
|---|--------|------|------|------|
| 25 | nit | 未修正 | 部分更新の append パスで "Last updated" メタ行が stale | PR #49 の Devin Review 指摘から発覚した同種問題: コメント/返信の部分投稿（`createComment`/`createDiscussionComment` の `X-Partial` 応答）は store の `updated_at` を進めるが、fragment は新 `<li>` のみを返すためヘッダの "Last updated" がリロードまで古いまま。answer swap 側は `discussion_meta` ノード同梱で解決済み（PR #49）。append モードは全ノードを挿入先へ追加する設計なので、自己ターゲットする swap ノードを混ぜるにはプロトコル拡張（例: `data-swap` 印付きノードは `replaceWith`）が要る → TODO.md にタスク記録済 |
| 27 | info | 未修正 | remote CLI（`*client.Client`）が store sentinel エラーに写像しない | 同じ追検証で発見: `cli.Backend` の HTTP 実装は `*client.Error{Status,Message,Fields}` を返し `store.ErrNotFound`/`ErrNotAnswerable` に写像しないため、remote モードでは `errors.Is(err, store.ErrNotFound)` が成立しない。現状 sentinel を判定する消費者は local 限定の TokenStore のみで実害なし（潜在乖離）。根治候補: 404→ErrNotFound 写像 or API の機械可読エラーコード化 → TODO.md にタスク記録済 |
| 36 | major | 未修正 | 並行 merge で git 側だけ二重適用されうる | 2026-11-26 コードレビューで発見（`docs/sketch/ja/owner-improvements.md` §1.1）: `mergePull`（api/web/cli 共通の流れ）は `pr.Open()` 確認 → `repo.Merge` → `MarkMerged(WHERE status='open')` の順で、git 副作用が DB の状態遷移より先。squash は常に2回目を作りえ、rebase も base 更新後に読んだ2本目が空差分をコミットしうる。対応案: git 前に DB で CAS による merging 確保 + 中断予約の復旧 |
| 39 | nit | 未修正 | `CheckMerge` が解決済み SHA ではなく ref 名で merge-tree を実行 | `internal/gitrepo/ops.go:284-299`。報告 SHA と判定対象が競合時にずれうる |
| 40 | nit | 未修正 | エラー見出し `"Bad request"` が英語直書き（i18n 漏れ） | `internal/web/comments.go:295,437,573`、`internal/web/pulls.go:894`。`msgFor(r).ErrBadRequestH` を使う |

## 修正済み

| # | 重要度 | 状態 | 概要 | 詳細 |
|---|--------|------|------|------|
| 41 | minor | 修正済 | API の PR diff が 8MiB で切り詰められてもクライアントに通知されない | `pullDiff` が `repo.Diff` の truncation flag を捨て、remote CLI が不完全な差分を正常結果として受け取っていた。対応: API は `X-Truncated: true` を返し、client は header を解釈、remote `zatu pr diff` は stderr に警告する。回帰テスト追加済 |
| 38 | minor | 修正済 | API と web の PR 一覧で `page` に上限がなく OFFSET がオーバーフローしうる | #2 の修正は web の issue/discussion 一覧のみで、API の `queryInt` と web の PR 一覧に上限がなかった。対応: store 層の共通 `normalizePage` + `MaxListPage`、API/web の入力クランプ、PR 一覧の最終ページ再取得。回帰テスト追加済 |
| 37 | minor | 修正済 | `createPull` で base の ref エラーが head の結果で上書きされる | `internal/web/pulls.go` が `normErr` を共用し、base 不正・head 正常だと不正 base が `RevParse` に渡り 422 ではなく 500 になっていた。対応: `baseErr`/`headErr` に分離し、どちらかの不正でも 422 のフォームエラーにする。回帰テスト追加済 |
| 1 | major | 修正済 | 未知/重複 `label_id` で 500 | `POST /issues -d 'title=x&label_id=999'` → FK違反500。`label_id=1&label_id=1` → UNIQUE違反500。edit も同様。`label_id=abc/-3/0` は silently drop されるのと非一貫。原因: store の `setIssueLabels` が素の `INSERT`。対応: `INSERT OR IGNORE INTO issue_labels SELECT ?,id FROM labels WHERE id=?` で未知・重複を無視（既存の drop 方針と一貫）。回帰テスト追加済 |
| 2 | minor | 修正済 | 範囲外 `page` で誤解を招く空表示 + int64 オーバーフロー | `?page=99999` → 「No issues here yet」（実際は42件）+ ページネーションが消え復帰不能。`page=4611686018427387904` → `(page-1)*perPage` が int64 オーバーフロー → 負の OFFSET → "Showing -19–0"。対応: パース時に `n <= 1_000_000` 制限 + total 判明後に `f.Page` を `TotalPages` にクランプして再クエリ。回帰テスト追加済 |
| 3 | minor | 修正済 | mux レベルの 404 が素の text/plain | `GET /nonexistent`, `/issues/` → `404 page not found`（styled error page を通らない）。対応: `GET /` + `POST /` のメソッド限定キャッチオールで styled 404。**後日談**: 当初はメソッド無指定 `"/"` にしたら PUT/DELETE の自動405まで404に飲まれた → メソッド限定に分割して405復帰（pitfalls.md 参照） |
| 4 | nit | 修正済 | `GET /static/` がディレクトリリスティングを返す | `FileServerFS` の挙動。対応: `GET /static/app.css` を `ServeFileFS` で明示登録、`/static/` は 404 |
| 5 | nit | 修正済 | 変化のない status POST が `updated_at` を更新 | closed issue に `status=closed` で `updated_at` が動いた。対応: `updated_at = CASE WHEN status != ? THEN ? ELSE updated_at END`（CASE を status 代入より前に配置して評価順の曖昧さを回避）。回帰テスト追加済 |
| 6 | nit | 修正済 | コメント投稿が issue の `updated_at` を更新しない | GitHub 慣習では bump する。対応: `CreateComment` を tx 化（存在確認の TOCTOU も解消）し同一 tx で `issues.updated_at` 更新。回帰テスト追加済 |
| 7 | nit | 修正済 | `multipart/form-data` の POST はフィールドを読めず 422 になる | 当初「`ParseForm` が multipart を PostForm に入れない仕様」として wontfix 記録していたが、minigo#1084 の調査で実因が判明: `parseForm` が `r.ParseForm()` を無条件に呼ぶと `r.Form` が非 nil になり、`PostFormValue` が `ParseMultipartForm` を起動しない（コンパイル済みでも同じ標準挙動）。対応: `parseForm` で media type を見て `multipart/form-data` なら `r.ParseMultipartForm` を呼ぶ |
| 8 | minor | 修正済 | 存在しない issue への edit POST が検証エラー時 422 フォームを返す | `POST /issues/999/edit -d 'title='` → 存在しない issue の編集フォームが出ていた。対応: バリデーション前に `GetIssue` で存在確認 → 404。回帰テスト追加済 |
| 9 | nit | 修正済 | 詳細ページのラベルリンクが `status` を保持しない | `issue_detail.html` が `/issues?label=N` 固定。closed issue から飛ぶと open フィルタで自身が見えない。対応: `?status=all&label=N` |
| 11 | nit | 修正済 | `-db ""` で無名 ephemeral DB が開かれデータが黙って消える | 対応: main.go で空文字を `log.Fatal` |
| 12 | nit | 修正済 | 生成リンクが常に `status=open` を含む | デフォルト値の冗長なクエリ。対応: `issuesURL` で open/"" 時は省略 |
| 13 | info | 修正済 | `X-Content-Type-Options` / `X-Frame-Options` 等のヘッダが無い | 対応: `securityHeaders` middleware で `nosniff` + `frame DENY` + `referrer-policy: same-origin` を全レスポンスに付与 |
| 14 | minor | 修正済 | HTML5 `required` 属性がサーバー側バリデーションを阻害 | **実ブラウザ検証で発見**（curl/httptest では見えない）。title 入力・コメント textarea の `required` がブラウザネイティブのバブル（「このフィールドを入力してください」）を発火させ、サーバー側の 422 + インラインエラー再表示に一切到達しない。仕様の「検証エラー時は入力内容を保持してフォームを再表示」をブラウザ上で満たせていなかった。対応: 両フォームに `novalidate` 追加（`required` 属性は意味論として残存） |
| 15 | nit | 修正済 | `favicon.ico` が 404 でコンソールエラー | 対応: layout に data-URI の SVG アイコンを埋め込み（追加リクエスト・ファイル不要） |
| 17 | minor | 修正済 | todo-gc: archive 書き込み失敗時に完了項目を喪失 | `tools/todo-gc/main.go` `run`: TODO.md を先に書き換えるため、archive 側の `WriteFile` 失敗時に完了項目が両ファイルから消えていた（PR #4 Devin Review 指摘）。対応: archive を先に書き、その後 TODO.md を書く（TODO.md 側で失敗しても項目は両方に残るだけで失われない）。回帰テスト追加済 |
| 18 | minor | 修正済 | todo-gc: 空行でネスト項目が root 化 | `parse` が空行で `stack = nil` するため、空行の後のネスト項目がトップレベル扱いになり、[x] 親が open な子を残したままアーカイブされたり子の祖先チェーンが失われたりしていた（同上）。対応: 空行を保留し、次の行がインデントされたマーカー/継続行ならその項目の一部として扱う。回帰テスト追加済 |
| 19 | minor | 修正済 | todo-gc: note 配下の完了項目が sweep されない | `sweepNode` が `note` 状態の子を丸ごと skip するため、`- 完了の条件:` 等の配下にある `- [x]` 項目が TODO.md に残り続けていた（同上）。対応: note 自体は残しつつ配下を再帰的に sweep する（チェーンは `P > 完了の条件 > **C**`）。回帰テスト追加済 |
| 20 | minor | 修正済 | ラベルバッジの文字色コントラスト不足 | ダークモード対応時に計算で発見（ライトモードでも同じ）: `.label-badge` の文字色が固定 `#1f2328` のため、シードの Bug `#d73a4a` で 3.46:1、Documentation `#1f6feb` で 3.41:1、Feature `#0075ca` で 3.31:1 と WCAG AA（4.5:1）未満。対応: 色を `--label-color` で渡し `contrast-color()`（非対応時は RCS の XYZ 計算）で白/黒を選ぶ。結果 Bug/Enhancement は黒、Documentation/Feature は白（いずれも 4.5:1 以上）。実ブラウザ（Chromium）の計算値で確認 |
| 21 | minor | 修正済 | サーバー側エラーが読み上げ・フォーカスともに不確実 / Firefox・Safari で未操作の必須欄が invalid 扱い | フォームフィードバック整備時にガイド照合で発見: (1) 422 再表示のエラー文が `role="alert"` だったが、ページロード時から存在する live region は確実には読み上げられず、フォーカスも先頭のまま（エラー欄へ移動しない）。(2) エラーなしの欄は `aria-invalid` 未指定で、Firefox/Safari は空の `required` 欄をロード直後から invalid として公開する。対応: `role="alert"` を外し、最初のエラー欄に `autofocus`（読み上げは `aria-describedby` 経由）。エラーなしの欄は `aria-invalid="false"` を明示。テスト追加済 |
| 22 | minor | 修正済 | 文字サイズが px 固定でブラウザ/OS の文字サイズ設定に追従しない / 640px 以下で本文を 14px に縮小 / 一覧の長いタイトルが無制限に伸びる | Responsive & typography pass の監査で発見: `body { font-size: 15px }` と各所の px 指定のため、ブラウザの既定文字サイズを変えても本文が大きくならなかった。さらに狭幅で本文を 14px に縮めていた。長いタイトル・ラベル名は一覧の行を何行にも伸ばしていた。対応: rem 化 + `<meta name="text-scale">`、狭幅での縮小を廃止、一覧タイトル 2 行 clamp・ラベル ellipsis。px font-size が無いことをテストで固定 |
| 24 | info | 修正済 | CSP 未設定（#13' の CSP 部分を解消） | `securityHeaders` middleware に `default-src 'none'` ベースの CSP を付与。ラベル色の inline `style=` は `style-src-attr 'unsafe-inline'` でのみ許可し `<style>` 要素は遮断、外部画像は `img-src 'self' data:` で遮断。COOP/CORP/Permissions-Policy/Vary も追加（decisions.md「CSP・セキュリティヘッダ」）。実ブラウザで違反なし・注入 `<style>`/外部 `<img>` がブロックされることを確認 |
| 26 | minor | 修正済 | storyStore が store 契約から緩く乖離 | no-comments パスの追検証で発見（記録は PR #71）: `storyStore.SetCommentAnswer` は reply へのマークで `ErrNotFound`（本物は `ErrNotAnswerable`）を返し、非 qa カテゴリへのマーク拒否チェックも無い。`storyStore.ListIssues` は `ListFilter` を完全無視して fixture をそのまま返す。対応（PR #71）: `SetCommentAnswer` を本物と同じ検証順序に揃え、`Answered` をコメント fixture から再計算。フィルタ無視・no-op・auth スタブは意図的簡略化として doc コメントで宣言。`fakeStore.SessionUser` の ttl 無視・両 fake の `APITokenUser` スタブも同様に宣言済み |
| 28 | minor | 修正済 | fakeStore/storyStore のその他契約乖離（全コード sweep で発見） | 同 sweep 発見の実バグ寄り: `fakeStore.SetIssueStatus` の値検証欠落、`fakeStore.ListIssues` の `LabelID != 0`（本物は `> 0`）、LabelIDs の dedup・name 順ソート欠落、`fakeStore.UpdateDiscussion` のチェック順序逆転、storyStore の `SetIssueStatus` 値検証欠落。対応（PR #71）: 全て本物と同じ契約に揃え（ラベル解決は `resolveLabels` ヘルパ化）。fakeStore の FK 非実装と Query の Unicode 大小文字は意図的/許容として宣言 |
| 29 | minor | 修正済 | cmd/storybook の `go test .` が3件 fail | `StoryDiscussionCommentFragment`/`StoryIssueCommentFormFragment`（潜在バグの `StoryDiscussionReplyFragment` も）が `.Workspace.Slug` nil で 500 — fragment ストーリーが `Workspace` を data に含めていなかった。`TestAppMount` の partial POST 404 は story fixture の `WorkspaceID` 未設定（0）が原因で `legacySub → resourceWorkspace → WorkspaceByID(0)` が ErrNotFound。対応: 各 fragment ストーリーに `Workspace: storyWorkspace` を渡し、issue/discussion fixture に `WorkspaceID: 1` を付与。story の POST 先と `TestAppMount` を canonical `/w/default/...` パスに更新（legacy パスは 307 を返すため noRedirect クライアントでは fragment を取れない） |
| 31 | minor | 修正済 | `-tags story` のビルドが i18n 対応（#109）以降壊れていた | storybook 用 `story_pages.go` は build tag 外のため `go test ./...` で検出されず、#109 で render メソッドに `lang` 引数が追加された際に追従漏れ: 16箇所の呼び出しが `lang` 未指定（minigo 解釈では runtime arity trap、gc ではコンパイルエラー）。さらに `storyStore` が Store interface の新メソッド（`DeletePullRequest` 等）未実装で gc コンパイル不可。対応: 各呼び出しに `supportedLangs[0]` を渡し、`storyStore` に `Store` を embed（未実装メソッドは従来どおり呼ばれない想定、将来の interface 追加にも追従） |
| 30 | minor | 修正済 | minigo: `template.HTMLEscaper` が未 binding で storybook のページ story が 500 | minigo#1228 の修正確認中に発覚: `internal/web/i18n.go` の `messageFuncs` が使う `html/template.HTMLEscaper` が minigo の stdlib binding に無く（`HTMLEscapeString`/`URLQueryEscaper` はある）、dialog/confirm ヘルパを含むページ story 11件が `undefined: template.HTMLEscaper` で 500。gc コンパイル版は正常。対応: 文字列には `HTMLEscaper` と等価な `template.HTMLEscapeString` を使うよう変更（こちらは binding 済み）。minigo 側の binding 欠落自体は podhmo/minigo#1398 → minigo#1401 で binding 済み（本家コードへの差し戻しは任意） |
| 32 | minor | 修正済 | minigo 更新後に `StoryHttpSetCommentAnswerPartial` のみ無限ハング | third_party/minigo を 191eb9e→65ca6f5 に更新したら、answer マークの `X-Partial` POST（`SetThreadAnswer` → swap fragment 再描画）だけが VM 内で応答しなくなった。切り分け: 外側 `{{range}}`（interpreted `*struct` slice）× 内側 `{{range .C.Comments}}` × `{{template}}`+`dict`（非 trivial 先）の組合せで、**内側 reply 数に対し超線形**に爆発（1件0.4s→3件47s→全件ハング）。原因は hostbridge projection merge の eager-call フィードバックループ（promoted member 対応 #1228 の副作用）。podhmo/minigo#1399 → minigo#1402（`ee26f60`）で修正、同 story 0.3s・`TestRender` 11s でパス確認 |
| 33 | minor | 修正済 | 部分投稿中の textarea を編集すると成功時の reset で後続入力が消える | go-spa-sample#23 の対照確認で発見。`partial-submit.js` は submit ボタンだけを無効化していたため、fetch 完了前に追記でき、成功時の `form.reset()` がその追記も消していた。対応: FormData 捕捉後に input/select/textarea を lock し、成功 reset・エラー復帰の直前に元の disabled 状態へ戻す。実ブラウザで送信中 disabled・失敗時ドラフト保持・422 後の編集可を確認 |
| 34 | minor | 修正済 | 削除/折り畳み済みフィールドの遅延エラー読み上げが残る | 同対照確認で発見。`form-errors.js` は blur/change から 500ms 後に polite live region へ書くが、その間に partial swap でフィールドが消えたり reply cancel で `<details>` が畳まれたりすると、画面に無いエラーを読み上げていた。対応: timer 発火時に同一フィールド・`isConnected`・`checkVisibility()` を確認。実ブラウザで表示中は読み上げ、cancel 後は抑止されることを確認 |
| 35 | minor | 修正済 | invalid な返信フォームの Cancel がマウスで押せない | 上記検証中に発見。blur で `:user-invalid` のエラー行が出ると Cancel ボタンが mouseup 前に押し下げられ、クリックが別要素に retarget されて `<details>` が畳めなかった。対応: `pointerdown` 時に Cancel ボタンへ pointer capture を設定し、mousedown/mouseup を同じボタンへ届ける。実ブラウザで invalid/valid 両方の cancel を確認 |

## 修正しない（記録のみ）

| # | 重要度 | 状態 | 概要 | 理由 |
|---|--------|------|------|------|
| 3' | minor | wontfix | 405 のボディが素の text/plain | 既知パスへの誤メソッド（PUT/DELETE 等）は Go mux の自動 405（`Allow` 付き）でステータス自体は正しい。styled 化するには mux をラップする複雑さが増すので見送り |
| 5' | nit | wontfix | `UpdateIssue` は同一内容でも `updated_at` を bump | フォーム送信=更新意思ありと解釈。GitHub も同様の挙動 |
| 10 | info | wontfix | 検索の `lower()` は ASCII のみ（`Ä`↔`ä` 不一致） | SQLite の `lower()` の制限。ローカルデモとして許容 |
| 13' | info | wontfix | `fmtTime` が分単位のため同分内の updated_at 変化が見えない | 表示解像度の話（この項目に同居していた「CSP 未設定」は #24 で解消済み） |
| 16 | nit | wontfix | 空白のみのコメントのドラフトが trim されて空で再表示される | E2Eテストで確認（E2Eエージェント指摘）: `createComment` が `strings.TrimSpace` 後の値を `CommentDraft` に使うため `"   "` 投稿失敗時は空の textarea になる。「入力内容を保持」の字面どおりではないが、空白のみは意味を持たないため実害なし。挙動はテストで固定済（`integration_test.go`） |
| 23 | minor | 部分緩和 | 50 件以上のスレッドで画面外コメントの**本文**がアクセシビリティツリーに出ない | 長いリストのレンダリング最適化で実ブラウザ（Chromium 156）の AX ツリーを確認して発見: `content-visibility: auto` でスキップ中のコメントは子なしの listitem になり、中身は `notRendered` で ignored。Tab 移動・ページ内検索・`#comment-N` は問題なし。50 件未満のスレッドには `content-visibility` を当てない。→ 緩和（2026-11-14）: ルールを `<li>` から `.comment-body` に移し、スキップ中でも listitem・パーマリンク（コメント識別・アンカー）は AX ツリーに残るようにした（CDP `Accessibility.getPartialAXTree` で確認: `listitem ignored=false children=2`）。残存: スキップ中の本文テキストだけはブラウズモードに出ない — これは `content-visibility` の本質的限界で、完全解消には最適化自体の撤去が必要（perf 回帰 ~169ms→のため見送り、decisions.md「長いリストのレンダリング」）。閾値は 50 のまま。本文まで重要なら閾値を上げるかルールを消す |

## 確認済み・非バグ

- `?q=sqlite` が0件 → seed データに "sqlite" を含む issue が無いだけ。`q=search`/`q=dark`/`q=panics`（本文のみの語）で正しくヒットすることを確認済み。
- 削除済み issue 番号の再利用なし（AUTOINCREMENT 確認済み: #8 削除後に #10 が採番）。
- 再起動で再シードされない・データ永続化 OK。
- CSRF: 全 POST ルートで cross-site/Origin 不一致 → 403。同一サイトは通る。
- XSS: タイトル・本文・`value=` 属性いずれもエスケープ。DB に直接注入した悪意あるラベル色は `ZgotmplZ` にフィルタされる（html/template の CSS フィルタ有効）。
- CSRF なし・Origin なしの非ブラウザ POST（curl）は許可される — `NewCrossOriginProtection` の仕様通り。
- ページネーションの Prev/Next は `q`/`status`/`label`/`sort` を保持し `&amp;` も正しくエスケープ。
- タイトル 200/201 文字、コメント 10000/10001 文字、~1.2MB ボディ → styled 413 の境界確認済み。
- `PUT /issues` → 405、`Allow` ヘッダ付き。非数値/負数 id → 404。
- 40並行の read/write 混在負荷で異常なし（`-race` テストもパス）。
