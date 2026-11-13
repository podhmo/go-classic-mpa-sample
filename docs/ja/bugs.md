# 既知のバグ・問題点（Bugs）

実装・検証中に見つかった問題を記録する。すべて修正するとは限らない — 重要度と対応状況を併記する。

発見方法: 実サーバーでの手動スモークテスト + マージ後の専用 QA パス（エッジケースの curl 検証・コードレビュー）。

## 修正対象（今回直す）

| # | 重要度 | 状態 | 概要 | 詳細 |
|---|--------|------|------|------|
| 1 | major | 修正中 | 未知/重複 `label_id` で 500 | `POST /issues -d 'title=x&label_id=999'` → FK違反500。`label_id=1&label_id=1` → UNIQUE違反500。edit も同様。`label_id=abc/-3/0` は silently drop されるのと非一貫。原因: store の `setIssueLabels` が素の `INSERT`。対応: `INSERT OR IGNORE ... SELECT id FROM labels` で未知・重複を無視（既存の drop 方針と一貫） |
| 2 | minor | 修正中 | 範囲外 `page` で誤解を招く空表示 + int64 オーバーフロー | `?page=99999` → 「No issues here yet」（実際は42件）+ ページネーションが消え復帰不能。`page=4611686018427387904` → `(page-1)*perPage` が int64 オーバーフロー → 負の OFFSET → "Showing -19–0"。対応: パース時に上限チェック + total 判明後に `f.Page` を `TotalPages` にクランプして再クエリ |
| 3 | minor | 修正中 | mux レベルの 404 が素の text/plain | `GET /nonexistent`, `/issues/` → `404 page not found`（styled error page を通らない）。対応: キャッチオール `"/"` で styled 404 |
| 4 | nit | 修正中 | `GET /static/` がディレクトリリスティングを返す | `FileServerFS` の挙動。対応: `GET /static/app.css` だけ明示登録し、`/static/` は 404 へ |
| 5 | nit | 修正中 | 変化のない status POST が `updated_at` を更新 | closed issue に `status=closed` で `updated_at` が動く。対応: `CASE WHEN status != ?` で実変化時のみ bump |
| 6 | nit | 修正中 | コメント投稿が issue の `updated_at` を更新しない | GitHub 慣習では bump する。対応: `CreateComment` の tx 内で `issues.updated_at` も更新 |
| 8 | minor | 修正中 | 存在しない issue への edit POST が検証エラー時 422 フォームを返す | `POST /issues/999/edit -d 'title='` → 存在しない issue の編集フォームが出る。対応: バリデーション前に存在確認 |
| 9 | nit | 修正中 | 詳細ページのラベルリンクが `status` を保持しない | `issue_detail.html` が `/issues?label=N` 固定。closed issue から飛ぶと open フィルタで自身が見えない。対応: `?status=all&label=N` にする |
| 11 | nit | 修正中 | `-db ""` で無名 ephemeral DB が開かれデータが黙って消える | 対応: main.go で空文字をエラーにする |
| 12 | nit | 修正中 | 生成リンクが常に `status=open` を含む | デフォルト値の冗長なクエリ。対応: `issuesURL` で open 時は省略 |
| 13 | info | 修正中 | `X-Content-Type-Options` / `X-Frame-Options` 等のヘッダが無い | 対応: レスポンスに `nosniff` + `frame DENY` + `referrer-policy` を付与する middleware |

## 修正しない（記録のみ）

| # | 重要度 | 状態 | 概要 | 理由 |
|---|--------|------|------|------|
| 3' | minor | wontfix | 405 が素の text/plain | 既知パスへの誤メソッドは Go mux の自動 405（`Allow` 付き）。ブラウザ UI からは発生しない。styled 化するには mux をラップする複雑さが増す |
| 5' | nit | wontfix | `UpdateIssue` は同一内容でも `updated_at` を bump | フォーム送信=更新意思ありと解釈。GitHub も同様の挙動 |
| 7 | nit | wontfix | `multipart/form-data` の POST は 422 "Title is required" になる | `ParseForm` が multipart を PostForm に入れない仕様。フォームは multipart を使わないので実害なし。fail closed で安全 |
| 10 | info | wontfix | 検索の `lower()` は ASCII のみ（`Ä`↔`ä` 不一致） | SQLite の `lower()` の制限。ローカルデモとして許容 |
| 13' | info | wontfix | `fmtTime` が分単位のため同分内の updated_at 変化が見えない / CSP 未設定 | 表示解像度の話。CSP は inline style（ラベル色）と相性を考えると過剰 |

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
