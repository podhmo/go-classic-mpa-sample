# 既知のバグ・問題点（Bugs）

実装・検証中に見つかった問題を記録する。すべて修正するとは限らない — 重要度と対応状況を併記する。

発見方法: 実サーバーでの手動スモークテスト + マージ後の専用 QA パス（エッジケースの curl 検証・コードレビュー）。

## 未修正

（なし）

## 修正済み

| # | 重要度 | 状態 | 概要 | 詳細 |
|---|--------|------|------|------|
| 1 | major | 修正済 | 未知/重複 `label_id` で 500 | `POST /issues -d 'title=x&label_id=999'` → FK違反500。`label_id=1&label_id=1` → UNIQUE違反500。edit も同様。`label_id=abc/-3/0` は silently drop されるのと非一貫。原因: store の `setIssueLabels` が素の `INSERT`。対応: `INSERT OR IGNORE INTO issue_labels SELECT ?,id FROM labels WHERE id=?` で未知・重複を無視（既存の drop 方針と一貫）。回帰テスト追加済 |
| 2 | minor | 修正済 | 範囲外 `page` で誤解を招く空表示 + int64 オーバーフロー | `?page=99999` → 「No issues here yet」（実際は42件）+ ページネーションが消え復帰不能。`page=4611686018427387904` → `(page-1)*perPage` が int64 オーバーフロー → 負の OFFSET → "Showing -19–0"。対応: パース時に `n <= 1_000_000` 制限 + total 判明後に `f.Page` を `TotalPages` にクランプして再クエリ。回帰テスト追加済 |
| 3 | minor | 修正済 | mux レベルの 404 が素の text/plain | `GET /nonexistent`, `/issues/` → `404 page not found`（styled error page を通らない）。対応: `GET /` + `POST /` のメソッド限定キャッチオールで styled 404。**後日談**: 当初はメソッド無指定 `"/"` にしたら PUT/DELETE の自動405まで404に飲まれた → メソッド限定に分割して405復帰（pitfalls.md 参照） |
| 4 | nit | 修正済 | `GET /static/` がディレクトリリスティングを返す | `FileServerFS` の挙動。対応: `GET /static/app.css` を `ServeFileFS` で明示登録、`/static/` は 404 |
| 5 | nit | 修正済 | 変化のない status POST が `updated_at` を更新 | closed issue に `status=closed` で `updated_at` が動いた。対応: `updated_at = CASE WHEN status != ? THEN ? ELSE updated_at END`（CASE を status 代入より前に配置して評価順の曖昧さを回避）。回帰テスト追加済 |
| 6 | nit | 修正済 | コメント投稿が issue の `updated_at` を更新しない | GitHub 慣習では bump する。対応: `CreateComment` を tx 化（存在確認の TOCTOU も解消）し同一 tx で `issues.updated_at` 更新。回帰テスト追加済 |
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

## 修正しない（記録のみ）

| # | 重要度 | 状態 | 概要 | 理由 |
|---|--------|------|------|------|
| 3' | minor | wontfix | 405 のボディが素の text/plain | 既知パスへの誤メソッド（PUT/DELETE 等）は Go mux の自動 405（`Allow` 付き）でステータス自体は正しい。styled 化するには mux をラップする複雑さが増すので見送り |
| 5' | nit | wontfix | `UpdateIssue` は同一内容でも `updated_at` を bump | フォーム送信=更新意思ありと解釈。GitHub も同様の挙動 |
| 7 | nit | wontfix | `multipart/form-data` の POST は 422 "Title is required" になる | `ParseForm` が multipart を PostForm に入れない仕様。フォームは multipart を使わないので実害なし。fail closed で安全 |
| 10 | info | wontfix | 検索の `lower()` は ASCII のみ（`Ä`↔`ä` 不一致） | SQLite の `lower()` の制限。ローカルデモとして許容 |
| 13' | info | wontfix | `fmtTime` が分単位のため同分内の updated_at 変化が見えない / CSP 未設定 | 表示解像度の話。CSP は inline style（ラベル色）と相性を考えると過剰 |
| 16 | nit | wontfix | 空白のみのコメントのドラフトが trim されて空で再表示される | E2Eテストで確認（E2Eエージェント指摘）: `createComment` が `strings.TrimSpace` 後の値を `CommentDraft` に使うため `"   "` 投稿失敗時は空の textarea になる。「入力内容を保持」の字面どおりではないが、空白のみは意味を持たないため実害なし。挙動はテストで固定済（`integration_test.go`） |

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
