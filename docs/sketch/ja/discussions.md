# Discussion タブ（GitHub Discussions 風）の追加 — 調査・設計メモ

> **変更（2026-11-16）**: コメント構造を GitHub の 3 層モデルに合わせて
> `discussions` 1—N `discussion_threads` 1—N `discussion_comments` に分割した
> （本メモの `parent_id` self-FK 記述は初版設計）。スレッド自体が開始投稿
> （body/author/is_answer を持つ `discussion_threads`）、返信のみが
> `discussion_comments`（`thread_id` FK）。詳しくは `../../ja/decisions.md`
> の「discussion_comments を threads/comments 2 テーブルへ分割」を参照。
>
> **変更（2026-11-16）**: 返信フォームの `<details>` 畳み込みをやめ、各スレッドの
> 返信群の直下に常時表示の "Add a comment" フォームに変えた（本メモの
> `<details>` reply UX 記述は初版設計）。詳しくは `../../ja/decisions.md` の
> 「返信フォームをスレッド末尾の常時表示に」を参照。
>
> **変更（2026-11-16）**: 上記の常時表示は強調され過ぎだったため、`<details>` に
> 戻した — ただし summary は「Reply」トグルではなく畳まれた1行インプット風
> （GitHub の "Write a reply" 相当、「Add a comment」とだけ書かれた枠線付きの
> バー）。開くと本物のフォームに入れ替わる。詳しくは `../../ja/decisions.md` の
> 「返信フォームを折り畳みインプットに」を参照。

調査・設計メモ（実装前の検討段階）。`cli-and-web-api.md` / `partial-updates.md` の姉妹編。
「現状は issue 管理のみだが、GitHub Discussions のような機能を追加してトップのナビゲーションに
discussion タブを増やしたい。このときのテーブル定義・画面遷移を、issue と discussion の違いを
整理したうえで検討する」という問いへの回答を整理する。

**実装順序の前提**: アカウント機能（`accounts.md`）が先に入り、その後に本機能を実装する
（2026-11-15 確認）。よって本メモのテーブルは `users` テーブルが存在する前提で書く。

## issue と discussion は何が違うのか（GitHub モデルの整理）

| 観点 | Issue | Discussion |
|---|---|---|
| **目的** | 作業単位・タスクの追跡。バグ修正・機能追加など「終わらせるもの」 | 対話・相談・告知。結論がなくてもよいオープンな会話 |
| **ライフサイクル** | open → closed（閉じることがゴール） | 状態なし。Q&A カテゴリだけ「answered」を持つ。会話は閉じない |
| **分類** | ラベル（複数・自由に付け替え） | カテゴリ（1件に必ず1つ・管理者定義の閉じた分類） |
| **コメント構造** | フラットな時系列 | スレッド（コメントへの返信がネストする） |
| **成功状態の形** | 「closed」のフラグ | 「どのコメントが答えか」= コメントへのポインタ |
| **番号空間** | issues で採番 | GitHub でも issue/PR とは独立した採番 |
| **その他の GitHub 機能** | assignee、milestone、projects | reactions、poll 形式カテゴリ、pin、issue→discussion 変換 |

この差分のうち**機能的に効いてくるのは3点**: カテゴリ（単一選択の閉じた分類）、スレッド化された
コメント、Q&A の answer マーク。状態遷移（open/closed）が無いことは「discussion には
status 列も status POST も要らない」というスキーマ上の差分として効く。

## このアプリに移植するスコープ

採用:

- **カテゴリ**: ラベルと対になる概念として「単一選択・シード定義」の `discussion_categories`。
  形式は `open`（通常）と `qa`（answerable）の 2 種類だけ持たせる。
- **スレッド返信**: コメントに `parent_id` を足し、**1段ネストのみ**（返信への返信は
  その親のスレッドに属する。GitHub と同じ構造）。
- **answer マーク**: `qa` 形式カテゴリの discussion だけ、top-level コメント1件を
  「答え」としてマークできる。
- **独自採番**: `/issues/3` と `/discussions/3` が共存できるよう別テーブル・別 AUTOINCREMENT。
- **投稿者**: `users` が既にある前提なので `author_id` 列を最初から入れる
  （未ログイン投稿は NULL → "anonymous"、issues/comments と同じ表示ルール）。

見送り（最初の実装には入れない）:

- **reactions / 投票**: アカウント先行で「誰が押したか」は記録できるようになるが、
  reaction 対象・トグル UI・集計表示は別途の設計が要るため初回スコープ外。将来追加の
  前提条件は accounts で揃う。
- **poll 形式カテゴリ**: options/votes のテーブルが別途要る。`format` を拡張する形で後追加可能。
- **issue→discussion 変換**: 移行フロー（本文+コメントのコピー）としては面白いが、
  まず本体を作ってから。
- **カテゴリ管理 UI**: ラベル管理画面と同じく「シードの固定値のみ」（prompts.md の方針を踏襲）。
- **pin**: 安いが初回スコープ外。`pinned INTEGER` 列を後から足せばよい（additive）。

## テーブル定義

### 方針の選択肢

| 案 | 内容 | 評価 |
|---|---|---|
| A. issues 共用（`kind` 列） | `issues.kind`（'issue'/'discussion'）、`issues.category_id`、`comments.parent_id` を追加 | **非推奨**。既存 DB への `ALTER TABLE` = このリポジトリ初の「本当のマイグレーション」になる（現行は `CREATE TABLE IF NOT EXISTS` の additive のみで済んできた）。status/labels は issue 専用、category は discussion 専用で、NULL 列と kind 依存の CHECK が不格好 |
| B. 並列テーブル | `discussions` / `discussion_categories` / `discussion_comments` を新設。issues 系は無変更 | **推奨**。additive なので `schemaDDL` に足すだけで既存 DB も新規 DB も同じ `store.Open` で揃う。コードの重複は「小さく対称な構造」として読める。このリポジトリの「説明のための抽象化を増やさない」方針にも合う |
| C. threads 大統一 | issue も discussion も `threads` + `kind` に畳み込み、コメントも共有 | 抽象化が先に来る設計。既存ハンドラ・テンプレートの全面改修が要り、「まず動くもの」から遠い |

### 案 B の DDL

```sql
CREATE TABLE IF NOT EXISTS discussion_categories (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	slug TEXT NOT NULL UNIQUE,          -- ?category= と URL 表記用
	description TEXT NOT NULL DEFAULT '',
	format TEXT NOT NULL DEFAULT 'open' CHECK(format IN ('open', 'qa'))
);
CREATE TABLE IF NOT EXISTS discussions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	category_id INTEGER NOT NULL REFERENCES discussion_categories(id),
	author_id INTEGER REFERENCES users(id),          -- NULL = anonymous
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS discussion_comments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	discussion_id INTEGER NOT NULL REFERENCES discussions(id) ON DELETE CASCADE,
	parent_id INTEGER REFERENCES discussion_comments(id) ON DELETE CASCADE,
	author_id INTEGER REFERENCES users(id),          -- NULL = anonymous
	is_answer INTEGER NOT NULL DEFAULT 0,
	body TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
-- 「答えは1件だけ」を DB 制約でも担保する部分一意 index
CREATE UNIQUE INDEX IF NOT EXISTS idx_dcomments_answer
	ON discussion_comments(discussion_id) WHERE is_answer = 1;
CREATE INDEX IF NOT EXISTS idx_discussions_category_created
	ON discussions(category_id, created_at);
CREATE INDEX IF NOT EXISTS idx_dcomments_discussion_created
	ON discussion_comments(discussion_id, created_at);
```

設計上のポイント:

- **`status` 列は無い**。discussion は閉じない。「答えが出たか」は `is_answer = 1` の
  コメントが存在するかで導出する（一覧の Answered バッジは `EXISTS` サブクエリ）。
- **answer の表現は `discussion_comments.is_answer`**。別案の
  `discussions.answer_comment_id` もあるが、そちらはコメント削除時に参照が宙に浮く
  （`ON DELETE SET NULL` が追加で要る）。is_answer なら答えコメントの削除と一緒に
  消え、dangling が起きない。マーク付け替えは「旧コメントの is_answer 解除 + 新コメント
  設定」の2行更新を1トランザクションで行い、部分一意 index が競合を最後の砦として
  弾く。
- **`parent_id` の深さは DB では制約できない**（self-FK は存在性だけ見る）。
  「parent は同一 discussion の top-level コメント」の検証は store 層のトランザクション
  内で `SELECT` して行う。返信への返信の扱いは「投稿側で弾く」に決定済み
  （2026-11-15 確認。フォームは top-level コメントにしか出さないので通常経路では
  発生しない。UI も top-level にしか Reply を出さない方針で確定）。
- **コメント個別削除は初回スコープ外**（issues のコメントと同じ）。それでも
  `parent_id` には `ON DELETE CASCADE` を付けておき、将来親削除を許したときに
  孤児が残らないようにする。
- **discussion 本体の削除**: `ON DELETE CASCADE` でコメント・返信ごと消える
  （issue の delete と同じ2段階 GET 確認 → POST 実行）。

### シードデータ

カテゴリ（labels と同じく固定シード）:

- `general` — General（format: open）
- `ideas` — Ideas（open）
- `qa` — Q&A（qa: answerable）
- `announcements` — Announcements（open。本来は管理者のみ投稿、という性質だが
  このアプリに権限はないので単なるカテゴリ）

discussion 3〜4 件: ネスト返信付きの会話、answered 済みの Q&A、コメント0件の告知、
など。既存の「issues テーブルが空なら投入」の冪等チェックは、discussions 側も同じ
判定（`SELECT COUNT(*) FROM discussion_categories` か discussions）で合わせる。

## 画面・ルート設計

### ナビゲーション

ヘッダーの `.site-nav` を **Issues / Discussions のタブ** にする。現在地タブに
`aria-current="page"` とアクティブスタイルを付ける。ヘッダの primary ボタンは
文脈依存にする — issues 配下では "New issue"、discussions 配下では "New discussion"
（view model に `Section string` を足し layout で分岐するのが素直）。

### ルート

issues と対称な形:

```
GET  /discussions                     一覧（category 絞り込み、q 検索、sort、page）
GET  /discussions/new                 作成フォーム（カテゴリ必須選択）
POST /discussions                     作成 → 303 → /discussions/{id}
GET  /discussions/{id}                詳細（本文 + カテゴリバッジ + スレッドコメント）
GET  /discussions/{id}/edit           編集フォーム（title/body/category）
POST /discussions/{id}/edit           更新 → 303 → /discussions/{id}
GET  /discussions/{id}/delete         削除確認
POST /discussions/{id}/delete         削除 → 303 → /discussions
POST /discussions/{id}/comments       コメント or 返信（hidden `parent_id` 任意）
POST /discussions/{id}/comments/{cid}/answer   answer の mark/unmark（hidden `answer=1|0`、qa のみ）
```

- issue の `/status` POST に相当するものは**無い**（discussion は閉じない）。
- 返信は専用ルートを作らず `parent_id` hidden フィールドで同じ `/comments` POST に
  載せる — フォームが「どのコメントへの返信か」を値で運ぶ形。別案として
  `POST .../comments/{cid}/replies` もあるが、検証がハンドラをまたぐ分だけ素直さに
  欠ける。parent の妥当性（同一 discussion・top-level）は store 層で見る。
- answer は `setStatus` と同じ「hidden フィールドに遷移先状態を持つ」パターンで
  トグルにする（mark 済みなら `answer=0`、未 mark なら `answer=1` のボタンを出す）。

### 画面遷移図

```
header:  Issues | Discussions        （aria-current 付きタブ）
             │
 /discussions ──┬── GET /discussions/new ── POST /discussions ──303──┐
 (一覧: category│                                                      ▼
   select/q/sort└── GET /discussions/{id} ◄────────────────────── 詳細ページ
   /page)        │   ├─ POST /discussions/{id}/comments ──303──→ #comment-{newId}
                 │   │   （返信は各 top-level コメントの <details> 内フォーム、
                 │   │     hidden parent_id 付きで同じ POST）
                 │   ├─ POST .../comments/{cid}/answer ──303──→ /discussions/{id}#comment-{cid}
                 │   ├─ GET/POST /discussions/{id}/edit ──303──→ /discussions/{id}
                 │   └─ GET/POST /discussions/{id}/delete ──303──→ /discussions
                 ▼
            （category バッジ → /discussions?category={slug} で絞り込みへ）
```

### 詳細ページのコメント表示と reply UX

- コメントは top-level の `<ol>`、返信は親 `<li>` 内のネスト `<ol>`（1段）。
  `is_answer` コメントには "Answer" バッジ + 強調枠（issues の `:target` ハイライトと
  同じ仕組みを流用できる）。
- 投稿者表示は issues と同じルール: `Opened by <name>` / スレッドは `<name>` のみ
  （動詞なし）/ 返信は `<name> commented` で NULL は "anonymous"。author は作成時に固定。
- **返信フォームは各スレッド末尾の `<details>` 内に畳む**。summary は
  「Add a comment」とだけ書かれた畳まれた1行インプット風のバーで、クリックで
  開くと本物のフォームに入れ替わる（`[open] > summary { display:none }`）。
  宣言的 HTML だけで GitHub の collapsed "Write a reply" input と同じ UX に
  なり、JS も専用ページも要らない。別案は
  `GET /discussions/{id}/comments/{cid}/reply` の専用フォームページ — こちらは
  「返信先コンテキストを1画面で見せる」利点があるが、ページ遷移が増える。
  details 方式を推奨。
- コメント投稿後の着地は issues と同じく `303 → /discussions/{id}#comment-{新ID}`。
  返信でも同じアンカー先（返信 `<li>` にも `id="comment-N"` を付ける）。

### 一覧ページ

- status タブ（Open/Closed/All）の位置には**カテゴリ絞り込み**を置く。issue の label
  select 相当で `?category={slug}`。`answered=yes|no` のフィルタは Q&A 絞り込みとして
  便利だが初回は optional（Phase 2 候補）。
- 行は issue-row と同じ構造: タイトル + `#N` + カテゴリバッジ + Answered バッジ +
  作成日時。検索・sort・ページネーションは `ListFilter` と同じパラメータ名
  （`q`/`sort`/`page`）で揃える。

## web 層・store 層への影響

- **`web.Store` interface の拡張**: consumer 側 interface パターンはそのまま。discussion 系
  メソッド（ListDiscussions / GetDiscussion / CreateDiscussion / UpdateDiscussion /
  DeleteDiscussion / ListDiscussionComments / CreateDiscussionComment /
  SetCommentAnswer / ListDiscussionCategories）を足す。storyStore の fake も対称実装。
- **テンプレート**: `discussions.html` / `discussion_detail.html` / `discussion_form.html` /
  `discussion_delete.html` を `parsePages` の names に追加。issues 側テンプレートとの
  共通部分（ページヘッド、フォーム部品）は既存同様「ページ単位の template set」内で
  完結させる — 無理な共通化はこのリポジトリの規模ではしない。
- **view model**: `issueListPage`/`issueDetailPage`/`issueFormPage`/`issueDeletePage` と
  対称な `discussionXxxPage` を用意し、layout 分岐用に `Section` を全ページモデルへ追加。
- **`updated_at` の意味論**: コメント・返信・answer マークはいずれも activity として
  bump する（issues の「コメント投稿で bump」と同じルールで統一）。answer の
  マーク/解除も bump 対象とすることは確認済み（2026-11-15）。
- **既存機構の流用**: `search-highlight.js` は `.issue-list` 固定なので、discussion 一覧にも
  適用するなら selector の一般化（`.entry-list` のような共通クラス化）が要る — 小さいが
  JS 変更を含むので初回スコープでは外してもよい論点。`Speculation-Rules` ヘッダーは
  discussions 一覧にも同じく返せる（selector に `.discussion-list a` を足す）。
  `content-visibility` のコメント skip ルールは `.comment-list` クラスを踏襲すれば
  そのまま効く（ネスト構造にする場合はルール対象の見直しが要る点に注意）。
- **markdown**: 本文・コメントとも goldmark の `markdown` テンプレ関数をそのまま使う。

## 検証

- 新規 story（`StoryDiscussionList` / `StoryDiscussionDetail`（answered + ネスト返信）/
  `StoryDiscussionForm` 等）で見た目確認。`story_pages.go` の render helper 追加で
  storybook の group 化にも乗る。
- handler テストは fake store で PRG・422・404・answer 制約（非 qa・非 top-level を弾く）
  を確認。store テストは並列採番・cascade・部分一意 index・1段ネスト強制を実 SQLite で。
- PRG 全体と `<details>` reply の動作は verify-ui（`go run .` + playwright-cli）で。
- テスト方針は「機構と独自実装部分の動作確認」に留め、ハンドラごとの網羅テストは書かない
  （2026-11-15 のユーザー方針）。

## 段階的導入

1. **Phase 1（骨格）**: DDL + store メソッド + 一覧/詳細/作成/編集/削除の HTML ルート +
   ナビタブ + シード。スレッド・answer は UI だけ先に骨組み（返信フォーム・バッジ）でも
   よいが、ルートとスキーマはここで確定させる。
2. **Phase 2（対話構造）**: スレッド返信（`<details>` フォーム + parent_id 検証）、
   answer マーク/解除、一覧の Answered バッジ・`?answered=` フィルタ。
3. **Phase 3（仕上げ）**: 検索ハイライト・speculation rules の discussion 対応、
   pin、カテゴリ別の一覧 UI 改善、storybook story の充実。

## 残課題・未決事項

- **issue 一覧と discussion 一覧の URL パラメータ共有**: `q`/`sort`/`page` は同じ
  名前で問題ないが、`status`（issue）と `answered`（discussion）は別名のまま。
  タブ横断の統合検索はしない（GitHub でも別タブ）。
- **将来 API 化したとき**: `cli-and-web-api.md` の `/api/v1` 設計にも discussions を
  並列追加するだけでよい構成になっているか — JSON API は同じ対称構造で足せる想定。
- **Announcements カテゴリの意味**: 権限のないアプリで「管理者専用」を表現できない。
  カテゴリとして残すか、open 形式の1バリエーションに留めるかは実装時に決める。
