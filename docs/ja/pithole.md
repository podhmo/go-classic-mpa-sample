# 落とし穴・ハマりどころ（Pitfalls）

このドキュメントは実装中に遭遇した（あるいは事前に把握した）落とし穴を記録する。

## 環境・作業開始時

- **`prompts.md` はリポジトリルートではなく `docs/ja/prompts.md` にあった**。タスク仕様書の場所は先に `find` で確認する。
- **Go 1.27 の確認**: 要求は Go 1.27+。環境は go1.27.3 で問題なし。`http.ServeMux` のメソッド付きパターン（Go 1.22+）と `http.NewCrossOriginProtection`（Go 1.25+）が使える。

## SQLite / database/sql

- **`:memory:` DB は `database/sql` のコネクションプールで壊れる**: `sql.Open("sqlite", ":memory:")` では接続ごとに別の空DBが作られるため、スキーマ初期化した接続とクエリの接続が別物になり `no such table` になる。対策: memory DSN を `file:<name>?mode=memory&cache=shared` にする、または `db.SetMaxOpenConns(1)`（`SetMaxIdleConns` も併用してアイドル時にDBが破棄されないようにする）。store 側で検出して処理する方針。
- **外部キーは PRAGMA 必須**: SQLite はデフォルトで FK 無効。`PRAGMA foreign_keys = ON` を接続ごとに有効化しないと `ON DELETE CASCADE` が効かない。DSN の `_pragma` オプション（modernc.org/sqlite は `_pragma=foreign_keys(1)` 等が使える）か、接続後に PRAGMA を発行する。単に `db.Exec("PRAGMA foreign_keys=ON")` だけだとプール内の他接続に効かない点に注意 → DSN 指定が確実。
- **`AUTOINCREMENT` を付けないと id が再利用される**: 通常の `INTEGER PRIMARY KEY` は最大id削除後に再利用されうる。Issue番号の再利用禁止要件のため `AUTOINCREMENT` 必須。
- **`CreateComment` の FK 違反検知**: 存在しない issue へのコメント挿入は FK エラーになるが、ドライバのエラー型判定に依存するより先に `SELECT 1` で存在確認して `ErrNotFound` を返すほうが素直。
- **N+1 回避**: 一覧で各 issue のラベルを取る際、1 issue 1 クエリにしない。issue_id 単位のグループ化クエリ1本でまとめて取得する。

## net/http・Web

- **CSRF は標準 `http.NewCrossOriginProtection()` で**: Go 1.25+。Sec-Fetch-Site/Origin ヘッダベースのため、テストでは `Sec-Fetch-Site: cross-site` を付けたPOSTが403になることを確認すればよい。トークン方式を自前実装する必要はない（ローカルデモの範囲では）。
- **HTMLフォームは GET/POST しか出せない**: PUT/PATCH/DELETE は使わず、更新・削除・状態切替はすべて POST + PRG（303）に寄せる。
- **削除確認ページ**: `GET /issues/{id}/delete` は確認画面の表示のみ。GET で削除しない。
- **非数値の `{id}`**: `strconv.ParseInt` 失敗は 400 ではなく 404 寄りで扱う（仕様: 存在しない Issue は 404）。
- **ページングの total**: `LIMIT/OFFSET` の結果とは別に同じ WHERE で `COUNT(*)` を取らないと総ページ数が出せない。
- **`html/template` は自動エスケープされるが油断しない**: `<script>` を含むタイトルが `&lt;script&gt;` で出力されることをテストで担保する。
- **本文の改行**: プレーンテキストをそのまま出すと改行が潰れる。`white-space: pre-wrap` をCSSで指定する。

## 並行作業（サブエージェント分割）

- **契約未固定だと並行実装が破綻する**: web層がstore実装を待つと逐次になる。先にstore APIの型・シグネチャ・意味論をスケルトンとしてコミットし、web側は consumer-side interface + fake でテストする構成にした。
- **worktree はリポジトリ内 `.worktrees/` に置く**: サンドボックスの権限範囲をまたがないため。`.gitignore` への追加を忘れないこと。

（随時追記）
