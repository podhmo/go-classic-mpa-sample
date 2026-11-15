# no-comments スキル適用の記録

[cursor/plugins の pstack](https://github.com/cursor/plugins/tree/main/pstack) にある
`no-comments` スキルを `.agents/skills/no-comments/` にインストール（このリポジトリ向けに
改変: Cursor のサブエージェント spawn 系の記述を落とし、実行者自身が `agents/comment-sicko.md`
の判定規則を適用する形にした。参照先の `why` スキルも `.agents/skills/why/` に vendored）し、
ファーストパーティのコード全量（`main.go`, `cmd/`, `internal/`, `tools/` の
Go ファイル、および `internal/web/static/` の JS/CSS、templates の HTML コメント）に適用した結果の記録。
`third_party/` は対象外（vendored なので our-code ではない）。

## スキルの規則（要約）

comment-sicko の keep-list はこれだけ:

- ライセンスヘッダ
- **外部依存・プラットフォーム・プロトコル由来の非自明な振る舞い**の説明
- lint 抑制系（ルールが実害を防ぐもののみ）
- public API の契約を定義する doc コメント
- 制約を説明する issue/RFC リンク

それ以外はすべて削除対象（ナレーション、バナー、コメントアウト、自社コードの驚きを弁明する
ワークアラウンド説教）。自社コードの驚きはコメントで擁護せず `MUST KILL` として
リネーム/抽出/型付け/再設計の対象に挙げる。

## 適用して消したもの

約 240 行（34 ファイル）。内訳:

- **セクションバナー**: `// --- xxx ---` / `/* ---------- xxx ---------- */` / `// ---- xxx ----`
  を全削除（api.go, accounts.go, output.go, client/*.go, story_pages.go, web_test.go,
  integration_test.go, および **app.css の 22 個のセクション見出し**）。
- **テストのステップ逐語コメント**: `// create`、`// missing category`、`// open → closed` のような
  「次の行を読めば分かる」手順ラベル。api/store/web/cli/e2e のテストで最大の削除量。
- **ファイル先頭の中身のないラベル**: `// Issue, comment and label endpoints.` など
  ファイル名をなぞるだけのもの。
- **自明な doc コメント**: `// contextKey is the private key type ...`、`// perPage is the
  fixed page size ...` のような定数名の言い換え。
- **exported doc 内の実装詳細**（下記「温度差」参照）。

## 残したものと理由

keep-list に該当するか、コードが言えない情報を運ぶものは残した。残ったコメントの大分類:

- **外部制約の why**: SQLite の挙動（user_version、コネクション毎の in-memory DB、COLLATE
  NOCASE）、html/template のエスケープ（`+` → `&#43;`）、goldmark、flag パッケージの
  位置引数仕様、Go のリダイレクトでの Authorization 転送、CSP/ブラザ仕様（Safari の
  list-style、Chrome の view-transition、Sanitizer の属性除去、VoiceOver の重複読み上げ
  スキップ）など。**このリポジトリのコメントの大半は実はここに属していた**。
- **セキュリティ不変条件**: 「plaintext は保存しない」「認証エラーを区別しない」
  「fail-closed」など、コードに書けない意図。
- **フィクスチャ導出事実**: `25 open issues → page size 20 → 2 pages` のような、別ファイルの
  定数を知らないと導けない期待値。
- **アクセシビリティ規則**: アクセシブルネーム・ライブリージョン・フォーカス管理の規則は
  markup からは読み取れないため残した。
- **設計参照リンク**: docs/sketch/ja/*.md、bugs.md #23、minigo#1081、modern-web-guidance
  のガイド名。
- **テーブルケースの行コメント**: `{"issue", "view"}, // missing number` のような、構造体に
  name フィールドのないテーブル駆動テストの事実上のケース名。

## 観察: exported と unexported の温度差

予想通り温度差があった。

- **exported 側はほぼ無傷だった**。このリポジトリの godoc は既に「契約」寄りに書かれて
  いて、消せる実装詳細が少ない。実質の修正は 2 箇所だけ:
  `main.go` の `initDB`（シード方針と log.Fatalf の注意を関数内コメントへ移動）と
  `web.New`（ミドルウェア構成の記述を return 直前のコメントへ移動）。
- **unexported 側は「why」を残す方向に倒した**。`contentSecurityPolicy` の長大な
  CSP 根拠、storyStore の契約説明、copyTree のシンボリックリンク処理などは
  実装詳細だが「なぜそうなるか」を運んでいて、消すと同じ間違いが再発しうるので残した。
  つまり *exported = 契約のみ、unexported = why 許容* という非対称が自然に決まった。
- 「消さない場合でも doc string から外す」ルールの適用対象は exported の doc comment に
  限定した。unexported な doc コメント（`//` + 関数名で始まる形式）は godoc に載らないので
  実装詳細を含んでいても許容と判断した。

## 観察: その他

- **テストの逐語コメントが最大の削除ソース**。シナリオテストの手順ラベルは
  「テスト名になっていない t.Run の代替」として一定の価値があるが、assert と
  失敗メッセージが同じことを言っているので規則通り消した。行数は減ったが
  スキャン性は低下したかもしれない —— レビューで判断してほしい点。
- **app.css のセクション見出しを全削除したのは一番効いた/一番疑問な変更**。
  CSS はシンボル構造が薄く（エディタのアウトラインが効きにくい）、1800 行ファイルの
  ナビゲーションにバナーは実用的だった。規則を適用するなら消すのが一貫だが、
  CSS だけは「keep-list にないが残す」というローカル例外を検討する余地がある。
- **`// Same contract as the real store: ...` の反復**は keep したが、スキルの語彙では
  MUST KILL の匂いがある: fakeStore/storyStore が実装契約を手で書き写している
  （契約ドリフトのリスク）。根治は fake と実 store の共有 contract-test 化で、
  コメント掃除の範囲外なので open として残す。
  → **追検証（why 相当: 履歴+意味突き合わせ）の結果**:
  - シグネチャの乖離は構造的に起きえない（`var _ Store = (*fakeStore)(nil)`、
    `New(newStoryStore())` でコンパイル時強制）。メソッド集合は web.Store 24 本と完全一致。
  - 履歴の乖離も確認できず: 契約変更（#43 セッション、#44/#49 answer マーク、
    #62 updated_at bump）は全て同一コミットで fake も更新されている。
  - 一方で**未宣言の意味論乖離を現行コードに発見**:
    1. `storyStore.SetCommentAnswer` — 本物は reply へのマークで `ErrNotAnswerable`
       を返すが storyStore は `ErrNotFound`。qa 以外カテゴリへのマーク拒否チェックも無い
    2. `storyStore.ListIssues` は `ListFilter` を完全無視（fixture をそのまま返す）
    3. `fakeStore.SessionUser` は ttl を無視（失効・スライド更新なし）、
       `APITokenUser` は常に `ErrNotFound` のスタブ
    いずれも storybook/テスト用途の意図的簡略化だが、storyStore の
    「implements the Store contract」という記述がやや過大で、乖離は各メソッドに
    宣言されていない。「コメントの複写が正しいか」ではなく「複写の裏の実装が
    すでに緩く乖離している」のが実態 —— 共有 contract-test 化の動機が補強された。
  - **全コード探索（interface 列挙 → 実装突き合わせ）の追加発見**:
    `cli.Backend` の第2実装 `*client.Client`（HTTP 越し）は wire の
    `*client.Error{Status,Message,Fields}` をそのまま返し、`store.ErrNotFound`
    /`ErrNotAnswerable` 等の sentinel に写像しない。「同一契約の2実装」なのに
    remote では `errors.Is(err, store.ErrNotFound)` が成立しない潜在乖離。
    現状 sentinel を判定する消費者は local 限定の TokenStore のみなので実害は
    ないが、404→ErrNotFound の写像 or API 側の機械可読なエラーコード化が根治。
    なお `listWindow` の perPage>100 ページ縫合は逆に契約を守る側の好例。
    他の interface（api.Store/cli.TokenStore）には fake 実装自体が存在しなかった
    （api/cli/client/e2e テストは全て本物実装を使用）。
- **`// Regression: Devin Review findings — ...`** は issue リンク相当として keep。
- `//go:embed`, `//go:build` などディレクティブはコメントではないので無対象。
- HTML テンプレートの `{{/* ... */}}` コメントは「なぜこの script タグがここにあるか」の
  説明（CSP・Invoker Commands の前提）が中心でほぼ keep だった。

## 未実施（open）

- MUST KILL 相当: fake/storyStore の store 契約の手動複写 → 共有 contract test 化が望ましい。
- CSS セクション見出しの扱い（上記）。
- comment-sicko 本来の「サブエージェントを spawn して審査させる」フローは、クォータの
  関係で今回は自分自身が sicko 役を兼ねた。独立視点の審査は受けていない。SKILL.md 自体も
  この運用に合わせて改変済み（spawn 指示・poteto-mode・/architect 参照を除去）。

## 検証

`go test ./...` / `go vet ./...` / `gofmt -l .` / `go build ./...` すべてクリーン。

## 追記: 契約乖離の全コード sweep（2026-11-16）

TODO 項目「fake/stub の契約乖離を全コードから探索し整理する」の探索部分の結果。
手順は TODO に記載の通り（interface 列挙 → 実装列挙 → 本物契約の特定 → 意味論突き合わせ）。

### interface と実装の棚卸し

| interface | メソッド数 | 実装 |
|---|---|---|
| `web.Store` | 24 | `*store.Store`（本物）/ `*fakeStore`（web_test.go）/ `storyStore`（story_pages.go、story tag） |
| `api.Store` | 19 | `*store.Store` のみ（fake は存在しない。doc コメントの "tests may substitute a fake" は予約語で実在しない） |
| `cli.Backend` | 19 | `*store.Store`（local）/ `*client.Client`（remote HTTP） |
| `cli.TokenStore` | 6 | `*store.Store` のみ |
| `store.scanner` | 1 | `*sql.Rows` / `*sql.Row` のみ（実質 seam、fake なし） |

api/cli/e2e テストは全て本物実装を使用（api_test.go / client_test.go / cli_test.go / e2etests は `store.Open` or `api.New(st,…)`）。

### 本物（*store.Store）の契約

- `ListIssues`/`ListDiscussions`: フィルタ（status/label/category/answered/query）・ソート（"oldest" 以外は created_at,id DESC）・ページング（Page<1→1, PerPage<=0→20, total はページング前）。ラベルは `attachLabels` が name 順で populate
- `CreateIssue`/`CreateDiscussion`: LabelIDs の重複・不存在は OR IGNORE で silent drop、不存在 categoryID は `ErrInvalidCategory`
- 更新系: updated_at は `SetIssueStatus` のみ「値が変わったときだけ」、それ以外は無条件 bump。コメント投稿・answer マークも親の updated_at を bump
- `SetIssueStatus` は値検証（open/closed 以外エラー）を存在チェックより先に行う。`UpdateDiscussion` も `requireCategory` を先に行う
- `SetCommentAnswer`: reply へのマーク/アンマークは `ErrNotAnswerable`、非 qa への mark は `ErrNotAnswerable`、discussion/comment 不在は `ErrNotFound`、マーク移動は既存をクリア
- `SessionUser`: 期限切れは行削除+`ErrNotFound`、残り半分でスライド更新。ttl 必須
- `APITokenUser`: 不明/失効トークンは `ErrNotFound`
- `CreateUser`: 名前重複は `ErrDuplicateName`（COLLATE NOCASE=ASCII 大小無視）。`AuthenticateUser`: 失敗は一律 `ErrInvalidCredentials`
- FK 制約有効（`foreign_keys(1)`）: 存在しない `author_id`/`user_id` への insert は制約エラー
- `ListComments`/`ListDiscussionComments` は親の存在を検証しない（不在なら空+nil）。返す値は毎回新しい row（呼び出し側の変更が store に漏れない）
- Query の大小文字無視は SQLite の `lower()` = ASCII のみ

### fakeStore の乖離

**実バグ寄り（本物に揃える候補）**:

1. `SetIssueStatus` — 値検証なし（本物は不正値でエラー）。チェック順序も逆（本物は検証→存在、fake は存在→検証）。
   現状は web 層が先に検証するので不可達だが、契約テストを通すと引っかかる
2. `ListIssues` — `fl.LabelID != 0`（本物は `> 0`）。負の LabelID で fake は全件除外、本物はフィルタ無視
3. `CreateIssue`/`UpdateIssue`/`addIssue` — LabelIDs の重複が重複ラベルになる（本物は dedup）
4. ラベル順序 — 挿入順のまま返す（本物は name 順）
5. FK 非実装 — 存在しない authorID/userID を匿名扱い or そのまま通す（本物は制約エラー）:
   `CreateIssue`/`CreateComment`/`CreateDiscussion`/`CreateDiscussionComment`/`CreateSession`
6. `UpdateDiscussion` — (missing id, bad category) で `ErrNotFound`（本物は `ErrInvalidCategory` 先）。チェック順序のずれ
7. Query マッチ — `strings.ToLower` は Unicode 全体、SQLite `lower()` は ASCII のみ。非 ASCII 大文字でずれる

**意図的スタブ（宣言で済む）**:

- `SessionUser`/`CreateSession`: ttl・期限・スライド更新なし（期限切れ・失効シナリオは fake では書けない）
- `APITokenUser`: 常に `ErrNotFound`（API 認証経路は fake では到達不可）
- `sessionUserErr`: テスト用エラー注入シーム
- `ListComments`/`ListDiscussionComments` が `*Comment`/`*DiscussionComment` を共有
  （本物は毎回 fresh row）。返り値の変更が store に漏れる点は本物と違うが、
  `discussions.go` の answer-swap ハンドラが storybook 経路でこの共有に依存している

### storyStore の乖離

**実バグ寄り（swap フラグメント挙動に関わるので揃える）**:

1. `SetCommentAnswer` — reply への mark/unmark は `ErrNotFound`（本物は `ErrNotAnswerable`）、
   非 qa カテゴリへの mark 拒否なし
2. `GetDiscussion`/`ListDiscussions` — `Answered` が fixture 固定のまま再計算されない
   （マーク移動・全解除後も古い値。本物はコメントから EXISTS 導出）

**意図的スタブ（宣言で済む）**:

- `ListIssues`/`ListDiscussions` — フィルタ完全無視（fixture をそのまま返す）
- `ListDiscussionComments` — 未知の discussionID にも fixture コメントを返す
- `CreateComment`/`CreateDiscussionComment` — 親の存在チェックなし（常に 99）
- `Create`/`Update`/`Delete` 系 — no-op（`99` or `nil` を返すだけ）
- accounts 系全般 — `CreateUser`→1、`AuthenticateUser`→常に成功、`CreateSession`→固定
  "story-token"、`SessionUser`→常に demo ユーザー、`DeleteSession`→nil、
  `APITokenUser`→`ErrNotFound`
- `SetIssueStatus` — 値検証なし（fakeStore と同じ穴）

### *client.Client（cli.Backend remote）の乖離

- **sentinel 未写像**: 404→`*Error{Status:404}`、422→`*Error{Status:422,Fields}`。
  `ErrNotFound`/`ErrNotAnswerable`/`ErrInvalidCategory`/`ErrInvalidParent`/`ErrDuplicateName`
  が remote では `errors.Is` 不可。消費者は local-only の `cli/auth.go:64`（TokenStore 経由）
  のみなので実害なし。根治は 404→`ErrNotFound` 写像 or API の機械可読エラーコード
- `ListComments`/`ListDiscussionComments` — 親不在 → 404 `*Error`（local は空+nil）。
  CLI は GetX を先に呼ぶので実害なし
- authorID 無視 — remote はトークンの actor になる（cli/issues.go:214 に宣言済み）
- **契約を守っている好例**: `listWindow` の perPage>100 縫合、`UpdateIssue` の
  nil→[] による replace 意味論合わせ、未知 LabelID → 空結果（本物と同じ）

### API 層自身の差（fake/stub ではないが wire 契約のずれ）

- `?label=<unknown name>` → 422（store の `LabelID>0` フィルタなら空結果）。
  client は LabelID 側で空に揃えているので CLI 経由では一貫

### 分類まとめ

- **修正要**: storyStore.SetCommentAnswer（reply→ErrNotAnswerable + 非 qa 拒否）、
  fakeStore.SetIssueStatus の値検証、fakeStore ListIssues の `LabelID > 0`、
  LabelIDs dedup、ラベル name 順ソート、storyStore の Answered 再計算
- **宣言で済む**: storyStore/fakeStore の「意図的スタブ」欄全部 + client.ListComments の 404
- **根治**: `*store.Store`/`fakeStore`/`storyStore` に同一振る舞いスイート
  （bump 規則・sentinel・フィルタ意味論）を流す共有 contract test。
  fake のポインタ共有は storybook が依存するので「本物に揃える」のではなく
  「fake の仕様として宣言」する方向が安全
- **削除漏れ発見（sweep の副産物）**: `web_test.go` 末尾 1092 行目に
  `/* ---------- accounts ---------- */` のバナーが宙吊りで残っている（パスの削除漏れ）
