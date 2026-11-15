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
