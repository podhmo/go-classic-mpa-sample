# Workspace（GitHub リポジトリ相当のコンテナ）の追加 — 調査・設計メモ

調査・設計メモ（実装前の検討段階）。

問い:

> GitHub のリポジトリに近い性質の「workspace」という概念を持たせたい。issues /
> discussions は workspace に所属する形に変更する。workspace は自由に作成・削除
> できる。初期状態（init コマンド実行時）は必ず 1 つ default の workspace を作る。
> workspace 間の issue 移動などは future work。

結論を先に書くと:

- **データモデル**: `workspaces` テーブルを新設し、`issues.workspace_id` /
  `discussions.workspace_id`（NOT NULL FK）で所属を持つ。issue / discussion の
  **採番は従来どおりグローバル**（workspace ごとの採番にはしない）。
- **URL**: HTML の正準 URL は `/w/{slug}/issues`・`/w/{slug}/discussions` 配下に
  移す。旧 `/issues/*` `/discussions/*` は該当 workspace へのリダイレクト。
- **API**: `/api/v1/w/{slug}/issues` 等に移し、`/api/v1/workspaces` を新設。
- **CLI**: `issues workspace list|create|delete` を新設し、各リソースコマンドに
  `--workspace` フラグ（既定 `default`、env `ISSUES_WORKSPACE`）。
- **default workspace**: migration v6 で必ず1件作られる。以後は「workspaces が
  0件のときだけ」init/起動時に再作成する。default 自体も削除可能だが、
  **最後の1件は削除不可**。

## 現状の構造（コード確認済み）

- **store 層**: `internal/store/`。スキーマは `store.go` の `migrations` 列
  （現行 v5）+ `PRAGMA user_version` で段階適用。テーブル: `issues` /
  `comments` / `labels` / `issue_labels` / `users` / `sessions` /
  `discussion_categories` / `discussions` / `discussion_threads` /
  `discussion_comments` / `api_tokens`。全てグローバル（workspace 概念なし）。
- **web 層**: `internal/web/`。ルートは `/issues*` `/discussions/*` に直置き
  （一覧・詳細・作成・編集・status・delete・comments・events・fragments の
  計 ~28 本）。`/` は `/issues` へ 303 redirect。`Section` フィールドで
  nav の aria-current を切替。
- **API 層**: `internal/api/`。`/api/v1/issues*` `/api/v1/discussions*` が
  Bearer token 認証の下にマウント。`/api/v1/issues/{id}` の詳細系は id だけで
  解決するフラット構造。
- **CLI**: `internal/cli/` + `internal/client/` + `cmd/issues`。
  `cli.Backend` インタフェースを `*store.Store`（local）と `*client.Client`
  （remote、`/api/v1` 経由）が共有する「同一契約の2実装」。
- **events**: `internal/events/` の topic は `issue:{id}` / `discussion:{id}` の
  グローバル id ベース + `issues` / `discussions` の list topic。
- **templates**: `internal/web/templates/*.html` に `/issues/...` 等のパスが
  直書き + `issueURL`/`issuesURL`/`discussionURL`/`discussionsURL` 系ヘルパ。
  `live-updates.js` 等の JS は `data-live-*` 属性経由で URL を受け取るため
  JS 側にパス埋め込みなし。`speculation-rules.json` は selector 基準で
  URL 非依存 → 変更不要。
- **init**: `main.go` の `init` サブコマンド = schema 適用 + `SeedMasterData`
  （default workspace + categories）+ `ANALYZE` + レポート。起動時は
  `store.Open` + `SeedMasterData`。demo データは `-seed` 指定時のみ
  `SeedDemoData`（issues/discussions が空のときだけ seed）。

## 論点と決定

### 1. 所属の持ち方と番号空間

issues / discussions に `workspace_id` を持たせる。採番の選択肢:

| 案 | 内容 | 評価 |
|---|---|---|
| A. **グローバル採番の維持**（推奨） | id は従来どおり `INTEGER PRIMARY KEY AUTOINCREMENT`。`/w/a/issues/5` の `5` は全 workspace で一意 | 差分最小。SSE topic（`issue:5`）・comment permalink（`#comment-N`）・store の参照系 API が全て無変更。**将来の workspace 間移動でも id が不変 = 正準 URL が解決可能**。GitHub との差分（repo ごとの採番ではない）を明記する必要あり |
| B. workspace ごとの採番 | `number INTEGER` + `UNIQUE(workspace_id, number)`。GitHub と同じ見え方 | detail 系の全クエリが `(workspace_id, number)` 検索に。SSE topic も `ws:n` 形に変更要。採番に「その workspace の max+1」の自前管理が入り競合考慮が要る。移動 future work で番号が振り直され permalink が死ぬ。**GitHub 忠実度は上がるがコストが大きい** |

**採用: 案A**。issue 番号はアプリ全体で一意のまま。URL の workspace 部は
「どこに表示されるか」のスコープであり、番号の一意性とは分離する。

### 2. workspace_id 列の足し方（migration v6）

SQLite の制約（コード確認・公式仕様）: **foreign_keys 有効時、REFERENCES 句付き
列を `ALTER TABLE ADD COLUMN` で追加するにはデフォルト NULL が必須**。
`NOT NULL DEFAULT <default-ws-id>` は宣言できない。選択肢:

| 案 | 内容 | 評価 |
|---|---|---|
| A. **テーブル rebuild**（推奨） | `issues`/`discussions` を新スキーマで作り直して rename（migrateV5 の `discussion_comments` と同じパターン） | schema が宣言どおり `NOT NULL REFERENCES` になる。index/FK も正しく引き継げる。SQL が長いだけ |
| B. nullable 列 + backfill | `ADD COLUMN workspace_id INTEGER REFERENCES workspaces(id)`（NULL 許可）→ UPDATE で default に埋める | 宣言は残念ながら NULL 許可のまま残る。NOT NULL 制約を外すので「必ず所属する」不変条件が schema で保てない |

**採用: 案A**。既存行は全て default workspace に帰属させる。
`ON DELETE CASCADE` を workspace FK に付け、workspace 削除で配下の issues/
discussions（→ comments/threads/labels 紐付け）が連鎖削除される GitHub リポジトリ
削除と同じ挙動にする。

index は `idx_issues_status_created` → `(workspace_id, status, created_at)`、
`idx_discussions_category_created` → `(workspace_id, category_id, created_at)`
相当に張り替え（一覧が必ず workspace で絞るため）。`comments.issue_id` 等の
既存 FK は触らない。

### 3. default workspace の保証

要件「init 時に必ず一つ default を作る」の解釈:

- **migration v6** が `workspaces` 作成時に `slug='default'` の1行を必ず挿入
  （新規 DB も既存 DB アップグレードも同じ経路で default ができる）。
- **削除後の再作成**: workspaces が **0件** のときだけ `EnsureDefaultWorkspace`
  が `default` を作る。`initDB` と通常起動（`main` の `SeedMasterData`
  経由）の両方から呼ぶ。
- **「default が消えたが他の workspace は残る」状態は再作成しない**:
  default を消して他だけ残す運用を許す（消しても復活するのは不親切）。
- **削除ガード**: workspaces の最後の1件は削除不可（`ErrLastWorkspace` →
  UI は 422、API も 422）。0件状態は init/起動でしか到達しない。

`slug='default'` / `name='default'` / `description='Default workspace.'`。

### 4. workspace の作成・削除ルール

- **自由に作成**: 認可なし（accounts Phase 1 = 識別のみ、authorization 未導入の
  現状方針に合わせる）。作成者は記録しない。
- **slug**: URL パス要素。`^[a-z0-9][a-z0-9_-]{0,62}$`、store 層の共有
  validation（`ValidateWorkspaceInput`）で title/body と同じ場所に置く。
  `/w/` 以下は `{slug}` しか来ないのでルート衝突予約語は不要。
- **name**: 表示名。省略時は slug と同じ（最大 200 runes = MaxTitleRunes 準拠）。
- **description**: 任意のプレーンテキスト（markdown なし）。
- **削除**: workspace ごと配下を cascade 削除（GitHub の repo 削除と同型）。
  GET 確認ページ → POST の PRG。確認ページには所属 issue/discussion 件数を出す。
- **rename/edit は見送り**（作成と削除のみ。slug 不変で良い）。
- **issue/discussion の workspace 間移動は future work**（要件どおり）。

### 5. 参照・更新系のスコープ検査をどこでやるか

| 案 | 内容 | 評価 |
|---|---|---|
| A. store の全メソッドを ws スコープ化 | `GetIssue(ctx, wsID, id)`、`SetIssueStatus(ctx, wsID, id, ...)` 等全署名変更 | 「必ずスコープされる」が強い。ただし全 interface × fake × client × テストの大改修。id はグローバル一意なので二重チェック |
| B. **list/create のみ ws 引数 + 参照更新系はグローバル id**（推奨） | `ListIssues(ctx, wsID, f)`/`CreateIssue(ctx, wsID, ...)` は ws を取る。`GetIssue(id)` 等は従来どおりで `Issue.WorkspaceID` を返し、handler 層の共通 resolve（slug→ws→一致確認）で所属を検査 | id のグローバル一意と整合、差分が小さい。スコープ漏れは web/api 両側の単一 resolve ヘルパで塞ぐ |

**採用: 案B**。詳細・更新・削除・コメント・answer・SSE はグローバル id 解決の
まま。`/w/{slug}/issues/{id}` で slug≠実所属の場合は **正準 URL へ 303
リダイレクト**（404 ではない — 旧 URL 互換と将来の移動で「古い URL が動き
続ける」性質を両方ここで確保できる）。

### 6. HTML ルーティング

正準形: `/w/{slug}/...`。既存の ~28 ルート全てに `{ws}` セグメントを足す。

```
GET  /workspaces                     一覧 (+ 各 ws の issue/discussion 件数)
GET  /workspaces/new                 作成フォーム
POST /workspaces                     作成 (slug/name/description)
GET  /w/{ws}/delete                  削除確認（件数表示）
POST /w/{ws}/delete                  削除実行 → /workspaces
GET  /w/{ws}/issues[...]             従来 /issues[...] と同型
GET  /w/{ws}/discussions[...]        従来 /discussions[...] と同型
GET  /w/{ws}/issues/{id}/events|fragments/*    SSE/fragment も ws 配下
```

- `/` → `/w/default/issues`（default が無ければ workspaces 先頭、それも無ければ
  `/workspaces`）へ 303。
- nav: site-title `/issues` → `/w/{current}/issues`、Issues/Discussions リンクも
  current workspace 配下。workspace 名をヘッダに表示し `/workspaces` への
  切替導線にする。
- **旧 URL 互換**: `/issues` `/issues/new` `/discussions` `/discussions/new` →
  default workspace の同名へ 303。`/issues/{id}[...]` `/discussions/{id}[...]` →
  実所属 workspace の canonical へ 303（query/hash 保存）。
- view model に `Workspace *store.Workspace` を全ページへ追加
  （breadcrumb・form action・nav の組み立て元）。`Section` 維持。

### 7. API / client / CLI

- **API**: `/api/v1/workspaces`（GET/POST）、`/api/v1/workspaces/{slug}`
  （GET/DELETE）。リソースは `/api/v1/w/{slug}/issues...`、
  `/api/v1/w/{slug}/discussions...` に移し、detail も含めて全てスコープ付き
  （slug≠所属なら 404 — HTML と違い redirect ではなく 404 が API らしい）。
  旧 `/api/v1/issues*` 系は廃止（サンプルアプリであり同梱 CLI を同時更新、
  互換維持のコストを払わない）。
- **client**: `*client.Client` の Backend 実装は `--workspace` 値を
  path に埋めて scoped API を叩く。workspace CRUD メソッド追加。
- **CLI**:

  ```
  issues workspace list
  issues workspace create SLUG [--name N] [--description D]
  issues workspace delete SLUG
  issues issue list|create [--workspace S]        # default: "default"
  issues issue view|edit|close|reopen|delete N [--workspace S]
  issues comment create N [--workspace S]
  issues discussion ...（同様）
  ```

  `--workspace` は backendFlags に追加（env `ISSUES_WORKSPACE`、既定
  `default`）。id がグローバル一意でも scoped API を叩くため全コマンドで
  受ける（省略時 default = 従来の単一 workspace と同じ使い勝手）。

### 8. events / SSE

- topic はグローバル id（`issue:5`）のまま → **変更不要**。
- list topic `issues`/`discussions` は現状 subscriber がおらず、ws 分割は
  将来 list 側ライブ更新を実装するときに検討（`issues:{ws}` 形候補）。

### 9. labels / categories / users

workspace 非所属（グローバル共有）のまま:

- `labels`: GitHub では repo 単位だが、本アプリでは管理 UI がない seed 固定の
  taxonomy。workspace 別にすると issue 移動 future work で再考が必要になる
  のもあり今回はグローバルのまま（見送り理由を decisions に記録）。
- `discussion_categories`: 同上（seed 固定 taxonomy）。
- `users`/`sessions`/`api_tokens`: 認証基盤、workspace と無関係。

### 10. 見送り（future work）

- workspace 間の issue/discussion 移動（要件で明示）。
- workspace ごとの採番（論点1 案B）。
- workspace 別 labels / categories、workspace の権限・メンバーシップ。
- workspace の rename / description 編集 UI。
- workspace の archive、issue 一覧の cross-workspace 表示（global dashboard）。
- 一覧ページへの SSE 適用（list topic の ws 分割含む）。

## 実装順序

1. **store**: `internal/store/workspaces.go` 新設（Workspace 型・CRUD・
   `EnsureDefaultWorkspace`・`ErrLastWorkspace`・slug validation）+
   `store.go` に `migrateV6`（workspaces + issues/discussions rebuild + index
   張替え）+ `Issue.WorkspaceID`/`Discussion.WorkspaceID` フィールド +
   `ListIssues`/`CreateIssue`/`ListDiscussions`/`CreateDiscussion` に wsID 引数。
2. **web**: `resolveWorkspace` ヘルパ、全ルートの `/w/{ws}` 化、URL helper に
   ws 引数、workspaces 管理画面（`workspaces.html` / `workspace_form` /
   `workspace_delete`）、view model に `Workspace`、旧 URL redirect shim。
3. **api**: `/api/v1/workspaces` + 全ルート `/w/{slug}` 化（detail は 404）。
4. **client/cli**: workspace メソッド、`--workspace` flag、workspace コマンド群。
5. **init/seed**: `EnsureDefaultWorkspace` を initDB・startup へ、seed 投入先を
   default workspace に。
6. **tests**: store（migration・scoped list・CRUD・last-ws guard・redirect 解決）、
   web（URL 生成・slug 404・redirect 互換・delete confirm・nav 表示）、
   api、cli、integration。fakeStore/storyStore 追随。
7. **storybook**: workspaces 系 story 追加、既存 story に Workspace 埋め。
8. **docs**: decisions.md / roadmap.md / TODO.md 更新。

## テスト方針

- migration: v5 の DB を v6 へ上げたとき既存行が default に帰属・id 不変・
  `PRAGMA user_version=6`。
- 所属不変条件: 別 workspace の id を scoped URL で GET → canonical へ 303
  （API は 404）。POST も同様。
- workspace 削除: cascade で中身が消える、最後の1件は 422、default 再作成は
  0件時のみ。
- 旧 URL: `/issues/5` → 実所属 `/w/{slug}/issues/5` へ 303、query/hash 保存。
