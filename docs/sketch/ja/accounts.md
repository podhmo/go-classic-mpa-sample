# アカウント（ログイン + 投稿者識別）の追加 — 調査・設計メモ

調査・設計メモ（実装前の検討段階）。`discussions.md` の前提となる機能。
「ログインしなくてもすべての機能は現状どおり使える（anonymous）。そのうえで、
自分が投稿したという識別のためにログイン機能が欲しい」という要求を設計する。

## 問いの整理

- **現状**: 認証・認可なし。ローカル単一ユーザー前提（prompts.md §8、decisions.md
  「意図的に実装しないもの」）。コメント投稿者は「固定のローカルユーザー」として
  扱われ、issues/comments に author 列は存在しない。
- **要求**: ログイン機能。ただし目的は**権限制御ではなく投稿者の識別**。
  「ログインしていなくても全機能は使える」は維持する。
- つまり追加するのは **authentication（誰であるか）** であって、
  authorization（何ができるか）は今回変えない — ログインの有無で操作可否が
  変わることはない。

## 識別と認可の切り分け（重要な前提）

今回のスコープは「**誰が書いたかを記録し表示する**」だけ。

- 投稿（issue/comment/将来的に discussion）に `author_id` を記録し、
  「Opened by X」「X commented」式に表示する。未ログイン投稿は `NULL` →
  表示は "anonymous"。
- 「自分の投稿しか編集・削除できない」「author だけが answer を mark できる」
  等の権限制御は**入れない**。現行どおり全員が全操作できる。
  入れるとすると author_id が揃った後の future work（discussions.md の
  answer マークもここに連動する）。
- anonymous ユーザーは users テーブルに行を**作らない**。`author_id IS NULL`
  が anonymous の意味。「名無しさん」的な固定行を作る案もあるが、ユーザー
  管理画面ができたときに紛らわしいので NULL 表現が素直。

## テーブル定義

```sql
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,        -- ログイン名兼表示名
	password_hash TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,              -- ランダムトークンの SHA-256（平文は保存しない）
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
```

既存行への author 列（NULL 許容・FK のみ、index 不要）:

```sql
ALTER TABLE issues   ADD COLUMN author_id INTEGER REFERENCES users(id);
ALTER TABLE comments ADD COLUMN author_id INTEGER REFERENCES users(id);
-- discussions.md の新設テーブルは最初から author_id を列に入れる
```

### ここで初めて「本当のマイグレーション」が要る

`schemaDDL` は `CREATE TABLE IF NOT EXISTS` だけで成り立ってきた（discussions.md
案 B が additive を選んだのもこの恩恵）が、`ALTER TABLE ... ADD COLUMN` は
「既にあるか」を見てからでないと再実行できない。扱いは2案:

- **A. `PRAGMA table_info` で列の有無を見て ALTER**: 軽い。schemaDDL とは別の
  「列追加リスト」を Open 時に処理する小さな機構。
- **B. `PRAGMA user_version` でスキーマ版数を持つ**: 定番。`Open` が
  `user_version` を読み、不足分のマイグレーション関数を順に適用する。
  今後の変更すべてに通用する形になり、discussions 実装もこの線に乗せられる。

**B に決定済み**（2026-11-15 確認）。初めて migration の仕組みを持つ機会として
`user_version` を導入し、「version N → N+1 の移行関数の列」を store に持たせる。
新規 DB でも同じ関数を辿るので「初期化と移行が同じ経路」になる
（既存の「`init` サブコマンド＝冪等な検証ステップ」の意味も維持できる）。

## 認証方式

| 案 | 内容 | 評価 |
|---|---|---|
| A. パスワード + サーバーセッション | users + sessions テーブル、cookie はランダムトークン | **推奨**。classic MPA の教科書的構成。ログアウト=行削除で revoke できる |
| B. 署名 cookie（stateless） | HMAC 署名した user_id を cookie に持つ | DB 不要だが revoke 不可・期限ローテーション設計が要る。このアプリの「DB に置けるものは置く」筋からも外れる |
| C. passkey / WebAuthn | パスワードレス | この規模では過剰。登録・認証に JS/WebAuthn 機構が要る |
| D. HTTP Basic | ブラウザ組み込み認証 | ログアウトできない・UX が悪い |

### パスワードハッシュ

- **stdlib だけでは書かない**（sha256 を自前でストレッチするのは脆弱性の温床）。
  `golang.org/x/crypto` の `bcrypt` か `argon2id` を使う。x/crypto はフレーム
  ワークでなく準公式の低レイヤライブラリなので、リポジトリの「標準ライブラリ
  中心・フレームワーク非依存」方針と整合する。
- **argon2id に決定済み**（2026-11-15 確認）。ハッシュ文字列は PHC 形式
  （`$argon2id$v=19$m=...,t=...,p=...$salt$hash`）でパラメータ埋め込みにし、
  将来のパラメータ変更を検証時に読み取れる形にする。
- 照合は `subtle` 系の定数時間比較（x/crypto の verify 関数が内蔵）。

### セッション

- cookie 値は 32 バイト `crypto/rand` の base64url。**DB には SHA-256(トークン) を
  保存**し、DB を読まれても session を盗用できないようにする（セッション ID を
  平文保存する定番の失敗を避ける）。
- cookie 属性: `HttpOnly; Path=/; SameSite=Lax`。`Secure` は付けられない —
  このアプリは localhost 平 HTTP 前提で、付けるとブラウザが cookie を送らない
  （TLS 化した場合は付ける、を注意書きとして記録）。
- 有効期限は 30 日 + sliding renewal（アクセス時に残り半分を切っていたら延長）。
  `expires_at` 切れの行は `Open` 時などに `DELETE FROM sessions WHERE expires_at < now`。
- ログイン成功時は**必ず新規トークンを発行**して古い session を破棄
  （session fixation の構造的回避。anonymous には session 自体発行しないので
  そもそも fixation の入口は薄い）。

## ユーザー登録 — 誰がアカウントを作れるか

| 案 | 内容 | 評価 |
|---|---|---|
| A. 自己登録（/register） | 誰でも name+password で作成 | **これに決定済み**（2026-11-15 確認）。ローカルアプリに spam 概念はなく、権限も絡まないので開いてよい。GitHub 的な UX でもある |
| B. seed の固定ユーザー | 事前定義ユーザーのみ | 管理 UI 不要だが増やせない。「識別」の要求には行き過ぎ |
| C. 最初の1人 + 招待制 | admin がユーザーを招待 | 権限モデルを持たない今の構造では意味をなさない |

`name` はログイン名と表示名を兼ねる（分離は future work）。UNIQUE 制約 +
大小文字の扱いは `COLLATE NOCASE` かアプリ側の正規化で統一する点に注意。
パスワードは最低長だけ軽く（例: 8 文字）。それ以上の複雑性ルールは
ローカルアプリに要らない。

## ルート・画面遷移

```
GET  /login             ログインフォーム（ログイン済みなら /issues へ 303）
POST /login             認証 → session 発行 + Set-Cookie → 303 → 直前ページ or /issues
POST /logout            session 削除 + cookie 消去 → 303 → /issues
GET  /register          登録フォーム
POST /register          作成 → 自動的にログイン状態で 303 → /issues
```

- **認証状態の検出は middleware**: cookie → sessions 引き → `context.Context` に
  `*store.User` を差し、ハンドラは `userFromContext` で取る。`nil` が anonymous。
- **ヘッダー**: 未ログイン → `Log in` リンク（+ Register）。ログイン中 →
  `@name` と Logout。Logout は状態変更なので **POST フォームのボタン**
  （GET でログアウトさせない — prefetch/投機で踏まれる事故を防ぐ）。
- **投稿フォームは変わらない**: submit 時に context の user から `author_id`
  を入れるだけ。anonymous なら NULL。フォーム上に「anonymous として投稿します」
  程度の注記を出すのは親切（optional）。
- 認証失敗は 422 相当でフォーム再表示（「name または password が違います」で
  どちらが違うかは言わない）。登録の重複 name はフィールドエラー。

### 画面への author 表示

- issue 詳細: `Opened by <name> <time>`、コメント: `<name> commented <time>`
  のメタ行に差し込む。discussion/discussion_comments も同じ。
- NULL は `anonymous` と表示。ユーザー名は将来の重複衝突に備えて
  「name 重複は UNIQUE で防ぐ」前提でそのまま出す。
- author は**作成時に固定**、編集では更新しない。「誰が編集したか」を記録するなら
  `edited_by` など別の仕組みが要る（スコープ外）。

## セキュリティ周辺（既存機構との整合）

- **CSRF**: `http.NewCrossOriginProtection` は fetch metadata ベースなので
  cookie を使い始めてもそのまま有効（ambient credential に対する防御としては
  SameSite=Lax の cookie が第 2 の防御になる）。
- **CSP**: 変更なし。login/register は通常の POST フォーム。
- **autocomplete**: login の password は `autocomplete="current-password"`、
  register は `"new-password"` で password manager を活かす。
- **ブルートフォース**: ローカル前提とはいえ、パスワード検証の前に短い
  固定遅延（例 200ms）か失敗カウンタを入れるかは論点。最小では「検証失敗時に
  一律で少し sleep」でも効く（timing でユーザー有無を探られるのも緩和）。
  本格的な rate limit はスコープ外。
- **HTTPS なしでパスワードを送る**: localhost 前提だが「平 HTTP で資格情報を
  送る設計である」ことは注意書きとして記録すべき（公開時は TLS 必須）。

## discussions 設計・cli-and-web-api 設計との関係

- `discussions.md` の `discussions` / `discussion_comments` テーブルは新設なので
  **最初から `author_id` 列を入れる**（ALTER を要しない）。
- answer マーク・reactions など「誰が」を絡めたい機能は、users ができてからが
  本番。特に reactions は今回の識別機構が前提条件になる。
- `cli-and-web-api.md` の「actor は避けて通れない」論点に答える布石になる:
  users テーブルができれば、API 側の Bearer token も `tokens`/`sessions` と同じく
  token → user_id の対応表に乗せられる（将来の API 設計で参照する前提条件）。

## 段階的導入

1. **Phase 1（認証と記録）**: `user_version` マイグレーション機構 + users/sessions +
   register/login/logout + middleware + 投稿時の author_id 記録 + 表示。
   seed にデモユーザー 1 人を入れる（初回起動ですぐログインできる形にする —
   2026-11-15 確認）。パスワードは固定の簡単なもの（例: `demo`）でよいが、
   seed でハッシュを生成して埋め込むこと。
2. **Phase 2（discussions 連携）**: discussions 実装時に author 列を初めから入れ、
   投稿者表示を揃える。
3. **Future work**: 権限制御（author 以外の編集・削除・answer mark 禁止）、
   パスワード変更/ユーザー管理、API token → user_id、reactions、プロフィール。

## 残課題・未決事項

- **マイグレーション機構の形**: `user_version` 方式（決定済み）の具体的なレイアウト
  — version 一覧を `[]func(tx)` で持つか、DDL 文字列の列で持つか。
- **name の正規化**: `COLLATE NOCASE` を列に付けるか、登録時に正規化して保存か。
- **ログイン前の遷移先**: `?next=` をフォームに持たせて投稿フォーム直前に戻すか
  （issue 詳細 → login → 元の issue）。`/issues` 固定でも動くが next が自然。
- **ブルートフォース対策の強度**: 固定遅延のみか、失敗カウンタまで持つか。
- **「anonymous」の表記**: 固定英語でよいか、設定可能にするか。
- **複数セッションの扱い**: 同一ユーザーの複数ログインを許可する（推奨・普通）
  か、ログイン時に他 session を掃除するか。
