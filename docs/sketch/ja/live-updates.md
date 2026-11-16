# 他者コメントのリアルタイム反映（push 更新）の設計検討

調査・設計メモ（実装前の検討段階）。`partial-updates.md` の姉妹編で、同メモの残課題
「push 更新（他者コメント・一覧の変化）が欲しくなったら datastar/SSE の再評価」に
対応する。

問いは3つ:

1. **issue/discussion を開いて眺めているとき、他人（または CLI 経由の AI）がコメント
   したら、開いている画面が勝手に変わるようにしたい**。チャット的な仕組みが要る。
2. **変更の検知とブラウザへの伝達は WebSocket と SSE のどちらでやるか**。
3. **SQLite だけで大丈夫か**（メッセージキューや外部ブローカが要るか）。

結論を先に書くと、**SSE（Server-Sent Events）+ in-process pub/sub で十分、SQLite の
ままでよい**。WebSocket は双方向通信（presence / typing indicator）が要件に入った
時点の再評価先。SQLite を通知の経路にする必要もない — イベントは「再取得の
きっかけ」に過ぎず、本体は従来どおり DB にある。

## 現状の構造（コード確認済み）

### 書き込み経路は2系統、`store.Store` に収束する

- **web**: `POST /issues/{id}/comments`、`POST /discussions/{id}/comments`、
  `/comments/{cid}/answer`、`/status` 等。応答は PRG 303 か `X-Partial` 判定の
  HTML fragment。
- **API**: `POST /api/v1/issues/{id}/comments`、`/api/v1/discussions/...` 等。
  `cmd/issues`（gh 風 CLI）や AI エージェントはここを叩く。
- 両系統とも最終的に `*store.Store` の同一メソッド（`CreateComment` /
  `CreateDiscussionThread` / `CreateDiscussionComment` / `SetThreadAnswer` /
  `SetIssueStatus` / `UpdateIssue` / `DeleteIssue` …）を呼ぶ。
  **通知を仕掛けるならこの store 層が唯一の共通地点**。

### 部分更新の機構はすでにある

`X-Partial` fragment 基盤（partial-updates.md 案Bとして実装済み）:

- `templates/fragments.html` に差し替え用 define が揃っている:
  `issue_comment_append`（meta swap + 新 `<li>`）、`discussion_comment_append`、
  `discussion_reply_append`、`discussion_comment_swap`（answer 移動時の複数 `<li>`
  同時差し替え）、`issue_article`（status toggle 用の全体差し替え）。
- `partial-submit.js` に fragment の parse（`<template>`）・append/replace・
  `data-swap` ノード差し替え・scoped `startViewTransition`・`.comment-new`
  ハイライト・toast・announcer の一連の swap 機構がある。
- `dom-hooks.js`（MutationObserver 集約 registry）で swap 後に来た要素へも
  ライフサイクル（init/teardown）を追従できる。

つまり「イベントを受けて画面の一部を差し替える」道具は揃っていて、今回足すのは
**「いつ差し替えるか」のトリガー（push）と「何を取りに行くか」の fragment GET**
だけでよい。

### その他の関連事実

- **CSP は既に `connect-src 'self'`**。`EventSource` は connect-src に従うため、
  **SSE を使っても CSP の変更はゼロ**で済む（部分更新導入時に追加済み）。
- **`main.go` の `http.Server` が `WriteTimeout: 15s`**。長寿命の SSE 接続はこの
  ままだと 15 秒で切断される。SSE ハンドラ内で
  `http.NewResponseController(w).SetWriteDeadline(time.Time{})`（Go 1.20+、
  zero で期限解除）して当該ルートだけ無期限にする必要がある。
- **先例が repo 内にある**: `third_party/minigo/storybook/watch.go` の
  live-reload が「in-process broadcaster + `text/event-stream` + EventSource
  client」の同じ構成を既に実装している（`/_livereload`、切断検知→`onopen` で
  再同期、iframe は親に任せる方針）。
- **認証は識別のみ**（accounts Phase 1）・`127.0.0.1` bind・匿名利用可。
  イベントに機密を載せなければ SSE エンドポイントに追加の authorization は要らない
  — ページを見られる人は同じ内容を GET できるので露出範囲が変わらない。
- **static 配信はルート単位の明示登録**。`live-updates.js` を足したら
  `GET /static/live-updates.js` の mux 登録も要る（web.go の既存パターンどおり）。
- `EventSource` はリクエストヘッダを足せないが、GET + same-origin なので
  `CrossOriginProtection`（Sec-Fetch-Site/Origin 検査）には引っかからない。

## 需要の分解

| 方向 | 必要か | 経路 |
|---|---|---|
| server → client（変更通知） | **要る** | これが push（SSE / WebSocket） |
| client → server（投稿） | 既に満たしている | 既存の POST / PRG / partial-submit。push 経路を流用する必要なし |
| client ↔ client（誰が見ているか・入力中） | 今回の要件ではない | presence / typing が欲しくなったら双方向の再評価 |

つまり必要なのは**一方向の「変わったよ」通知**だけ。payload に本文を載せる必要も
ない — 表示の真実はサーバの HTML で、クライアントは受信後に fragment を取りに
行けばよい（markdown の二重化を避ける原則は partial-updates.md と同じ）。

## 選択肢

| 案 | 内容 | 評価 |
|---|---|---|
| A. ポーリング | `setInterval` で fragment GET または `meta refresh` | 最も簡単だが無変更でも打ち続ける。遅延は interval に比例。`hidden` タブでも走り続けるか自前で制御が要る。「誰かが来たら変わる」体験としては即時性が足りない |
| B. **SSE + in-process hub**（推奨） | `GET /issues/{id}/events` 等に EventSource で購読、イベント受信で fragment GET → 既存 swap 機構へ | **stdlib のみ**。EventSource が自動再接続・`Last-Event-ID` を持つ。`connect-src 'self'` 既存で CSP 変更なし。repo 内に storybook live-reload の先例あり。単方向要件に過不足ない |
| C. WebSocket | `gorilla/websocket` or `coder/websocket` を vendored | 双方向が本領だが、今回の要件（push のみ）では upgrade handshake・ping/pong・独自再接続・新規 vendored 依存のコストだけが残る。**presence / typing indicator が要件に入った時点で再評価** |
| D. Datastar | `datastar-patch-elements` の SSE 応答 + signals | partial-updates.md の「push が欲しくなった時点で再評価」の条件は満たしたが、vendored 依存・signals 往復モデル・CSP（nonce 機構か unsafe-eval）のコストは変わらない。自作の hub+swap で ~200 行程度に収まるため不採用方向 |
| E. SSE 受信 → `location.reload()` | minigo live-reload と同じ最短経路 | 実装は最小だが、下書き中の textarea・開いた `<details>`・スクロール位置を失う。自分が返信を書いている最中に消えるのは致命的。**チャット的用途では fragment swap が本質** |

## 推奨設計（案 B）

### イベントの運び方: invalidation only

イベントは **id と種別だけを運び、fragment は運ばない**。

```
id: 42
event: comment
data: {"target": 128, "parent": 0}
```

- メリット: publish 時にレンダリング不要（購読者ゼロでもコストが無駄にならない）、
  SSE の `data:` 行の multi-line escape 問題を踏まない、payload が小さいので
  購読者ごとに自分の見え方（ログイン状態・権限）で fragment を取りに行ける。
- デメリット: 受信ごとに1回 fragment GET が飛ぶ。localhost・少数閲覧者では無視
  できる量。AI が高頻度投稿する場合は client 側 debounce（下記）で吸収。

### Hub（in-process pub/sub）

```go
// internal/events (新規)
type Change struct {
    Seq    int64  // hub 全体の単調増加（Last-Event-ID 用）
    Topic  string // "issue:5" / "discussion:3"
    Kind   string // "comment" | "thread" | "reply" | "answer" | "status" | "edit" | "delete" | "resync"
    Target int64  // comment/thread/reply の id
    Parent int64  // reply 時の thread_id
}

type Hub struct {
    mu   sync.Mutex
    seq  int64
    subs map[string]map[chan Change]struct{} // topic → subscriber channels
}

func (h *Hub) Publish(c Change)
func (h *Hub) Subscribe(topic string) (<-chan Change, func())
```

- subscriber は buffered chan（cap 32 程度）。**溢れたら `resync` を1個入れて
  drop** — 取りこぼしを静かに捨てるより「全部取り直せ」の方が安全。
- store に emitter を持たせる形にする:

```go
// store.go
type Emitter interface{ Emit(Change) }
// *Store に SetEmitter(e Emitter) を足し、mutating メソッドの成功直後に呼ぶ
```

  - `CreateComment` → `issue:{issueID}` `comment` target=commentID
  - `CreateDiscussionThread` → `discussion:{id}` `thread`
  - `CreateDiscussionComment` → `discussion:{id}` `reply` target=replyID parent=threadID
  - `SetThreadAnswer` → `discussion:{id}` `answer` target=markedID parent=prevMarkedID
  - `SetIssueStatus`/`UpdateIssue` → `issue:{id}` `status`/`edit`
  - `DeleteIssue`/`DeleteDiscussion` → `gone` 系（閲覧者に削除を知らせる）
  - 代替案として「web/api の各ハンドラで個別 publish」は ~12 箇所の呼び出しが散り、
    新しい mutator を足したときに emit を忘れる形になる。store 層の1箇所が確実。

### SSE エンドポイント

```
GET /issues/{id}/events
GET /discussions/{id}/events
```

```go
func (h *handler) issueEvents(w http.ResponseWriter, r *http.Request) {
    // id 検証 → hub.Subscribe("issue:"+id)
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    rc := http.NewResponseController(w)
    // main.go の WriteTimeout: 15s が長寿命接続を殺すため当該ルートだけ解除
    _ = rc.SetWriteDeadline(time.Time{})
    fmt.Fprint(w, "retry: 3000\n\n") // すぐ送って onopen を発火
    rc.Flush()
    ping := time.NewTicker(30 * time.Second)
    defer ping.Stop()
    for {
        select {
        case <-r.Context().Done():
            return
        case c := <-ch:
            fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", c.Seq, c.Kind, payload(c))
            if err := rc.Flush(); err != nil { return }
        case <-ping.C:
            fmt.Fprint(w, ": ping\n\n") // heartbeat: プロキシ/クライアント切断検知
            if err := rc.Flush(); err != nil { return }
        }
    }
}
```

### fragment 供給（受信側が取りに行く先）

GET fragment エンドポイントを新設し、**既存 define をそのまま返す**:

```
GET /issues/{id}/fragments/comments?ids=128        → issue_meta + issue_comment(128)
GET /issues/{id}/fragments/comments                 → コメントリスト全体 + meta（resync 用）
GET /discussions/{id}/fragments/threads?ids=3,7    → discussion_meta + discussion_comment(3) + discussion_comment(7)
GET /discussions/{id}/fragments/threads             → スレッドリスト全体 + meta（resync 用）
```

- `?ids=` 指定版は `issue_comment_append` / `discussion_comment_append` と同じ
  「meta の data-swap ノード + `<li>`」を返せる。**answer 移動も
  `?ids=marked,prev` で1発** — `discussion_comment_swap` と同じ構造。
- ids を指定しない版は resync（reconnect・overflow drop・初回同期ずれ）用。

### クライアント `live-updates.js`

page-detail.js から import する新 module（~150 行想定）。

- `document.body` の `data-events-url`（または detail ページ要素の data 属性）から
  topic を拾い `new EventSource(...)`。
- swap 処理は **partial-submit.js から `parseFragment` / `runScopedTransition` /
  replace/append のコアを `fragments.js` として切り出して共有**（submit 奪取側と
  push 適用側で DOM 操作を二重実装しない）。
- イベント→fragment GET→挿入のマッピング:

  | event | 動作 |
  |---|---|
  | `comment` / `thread` | `?ids=target` を GET → 末尾 append（既に同 id がある=自分の投稿→skip）+ meta の data-swap 差し替え + `.comment-new` ハイライト + announcer「New comment」 |
  | `reply` | thread fragment（`?ids=parent`）で `#thread-{parent}` ごと replace — 返信リスト部分だけ取る設計でもよいが thread 単位が実装最小。下書き中の自分の `<details>` は退避して復元（下記「下書き保護」） |
  | `answer` | `?ids=target,parent` で2つの `<li>` replace |
  | `status` / `edit` | `issue_article` 相当の fragment で差し替え |
  | `delete` | `resync` → 一覧への toast 誘導（「この issue は削除されました」+リンク） |
  | `resync` | リスト全体 fragment を取り直して差し替え |

- **dedupe**: append 前に `document.getElementById(node.id)` が既にあれば skip
  （自分が partial-submit で投稿したものは DOM 挿入済みのため自投稿の二重化を
  自然に防げる）。
- **debounce/coalescing**: イベント受信を 300ms バッファして pending ids をまとめて
  `?ids=a,b,c` で1回 GET。AI が連続投稿しても fetch/render が線形に増えない。
- **reconnect**: EventSource の自動再接続に乗せ、`onopen` で「前回と同じ
  Last-Event-ID ではない再接続」なら resync（リスト全体取り直し）。minigo
  live-reload の `location.reload()` より軽い部分同期で済む。
- **下書き保護**: 他者のコメント到着時に自分が返信フォーム（`<details open>` の
  textarea）を書いていても、`append` 系ならフォーム自体は触らないので干渉しない。
  `reply`/`answer`/`resync` の replace 系がフォームごと差し替える場合だけ、
  開いている details と textarea の値を swap 前に退避→swap 後に復元する
  （leave-guard.js の dirty 検知と同じ発想）。
- **背景タブ**: SSE は background tab でも受信し続けるので、そのまま DOM 更新
  しておいてよい（視覚的変化は見えていないだけ）。`visibilitychange` での
  追加フェッチは不要（再接続時の resync で十分）。

## SQLite で足りるか

**足りる。** 理由:

1. **通知の経路は DB である必要がない**。このアプリは単一バイナリ・単一プロセス・
   `127.0.0.1` bind。全書き込み（web POST・`/api/v1`・CLI `issues` 経由）が
   同じ `*store.Store` に収束するため、**in-memory hub が全変更を見られる**。
   SQLite は永続化だけを担い、メッセージキューの役割は持たせない。
2. **イベントは揮発でよい**。イベントは「再取得のきっかけ」であって本体は DB の
   コメント行。切断・溢れ・再起動で取りこぼしても、クライアントの resync
   （リスト fragment の取り直し）で回復する。永続キューや replay ログは要らない。
3. **スケールも問題ない**。hub は購読者ごとに1 goroutine + buffered chan。
   localhost の実用閲覧者数（数人〜十数人）では goroutine とコネクションの
   コストは無視できる。SQLite への負荷も変わらない — SSE はDB 読み取りを
   保持しない（イベント受信時だけ fragment GET が通常の読み取りを走らせる）。

**スケールアウトの逃げ道（将来の話として記録）**:

- もし複数プロセスが同一 SQLite ファイルを共有する形に進化したら（このアプリの
  想定デプロイではないが）、in-process hub は他プロセスの書き込みを見られない。
  その時点での選択肢:
  - (a) `changes` 追跡テーブル（各 mutator が `INSERT INTO changes(topic, kind, target, created_at)` を同じ tx で書き、hub が poll して読み捨てる）
  - (b) 各テーブルの `MAX(id)` / `MAX(updated_at)` を定期 poll する簡易監視
  - (c) NATS / Redis 等の外部ブローカ
  - その際は `journal_mode=WAL` も前提になる（現行 DSN は `busy_timeout`・
    `foreign_keys` のみで WAL 未設定）。
- SQLite 単体に `LISTEN/NOTIFY` はないため、DB 経由の通知を選ぶなら poll 型に
  なる。単一プロセスの現状では in-process hub が正解で、DB ポーリングは
  マルチプロセス化の際のフォールバック位置づけ。

## AI スレッド・チャット的用途への適合

- **discussion の thread-per-AI**: 「複数の AI が自分のスレッドを持ちコメントを
  連ねる」は既存の構造にそのまま乗る（thread が AI ごとの「席」）。
  `/api/v1/discussions/{id}` 系を叩く AI の投稿は全て store → hub → SSE →
  閲覧中のブラウザに届く。
- **閲覧者がどこにいても伝わる仕組み**: Phase 2 として一覧ページ
  （`/issues`・`/discussions`）にも `topics` 購読を足し、新規コメント時に
  「n 件の新着」バッジや toast を出せる。エンドポイントは
  `GET /events?topics=issue:5,discussion:3` の複数 topic 対応に広げる余地を残す。
- **荒らし・暴走**: AI が秒間何十件も書く場合の防御は「hub の buffered chan
  overflow → resync」「client の 300ms debounce」「fragment GET の ids まとめ
  fetch」で、個別イベントを1対1で DOM に流さない構造が既に盾になっている。
- **presence / typing indicator**（誰がこのスレッドを見ているか・入力中）は
  双方向通信が要るため今回の SSE ではカバーしない。要件に入った時点で
  WebSocket（案C）か「beacon POST + SSE presence イベント」の組み合わせを
  再評価する。

## 実装フェーズ

- **Phase 1（今回のコア）**: hub + store emit + `GET /issues/{id}/events`・
  `GET /discussions/{id}/events` + `live-updates.js` + fragment GET エンド
  ポイント + WriteDeadline 解除。comment/thread/reply の live append のみ。
- **Phase 2**: `status`/`edit`/`answer`/`delete` イベントの対応（replace 系
  fragment）、一覧ページへの新着インジケータ（`topics` 購読）。
- **Phase 3**: BroadcastChannel による複数タブの SSE 共有（1 接続化）、
  他ページ閲覧中の通知（toast / badge）、presence の再評価。

## 検証経路

- **unit**: hub の fan-out・overflow → resync 変換・unsubscribe の cleanup。
- **handler**: `httptest` で SSE stream を読み、store への書き込みで
  `event: comment` が流れることを確認（httptest.ResponseRecorder ではなく
  実サーバ or `httptest.NewServer` で streaming を読む形）。
- **E2E**: playwright で2コンテキスト — A: 詳細ページを開く、B: API で
  コメント投稿 → A の DOM に新 `<li>` が現れることを assert。
  verify-ui スキルの実サーバ + playwright-cli 経路に乗る。
- **手動**: `curl -N http://localhost:8080/issues/1/events` でイベントを眺め
  ながら別端末で `issues issue comment` する確認手順を残す。

## 結論

1. **SSE を採用**。要件は server→client 一方向のみで、stdlib + EventSource の
   自動再接続 + 既存 `connect-src 'self'` で追加依存・CSP 変更なし。
   WebSocket は presence/typing が要件に入った時点で再評価。
2. **イベントは invalidation only**。payload は `{kind, target, parent}` のみで、
   受信側が既存 fragment define を GET して差し替える。描画の真実はサーバの
   まま。
3. **通知点は store 層**に emitter を足す形で1箇所に集約し、web/api の両経路を
   一括捕捉する。
4. **SQLite のままでよい**。通知経路は in-process hub、イベントは揮発ヒント、
   回復はクライアント resync。マルチプロセス化した時点で changes テーブル +
   poll 等を再評価。
5. **WriteTimeout: 15s の解除**と **static ルート登録**が、このリポジトリ固有の
   実装上の落とし穴として対応必須。

## 残課題

- `reply`/`answer` の replace 系で自分の書きかけフォームを守る退避・復元の詳細
  （`details.open` + textarea value + focus の3点）。
- fragment GET エンドポイントの URL 形の確定（`/fragments/` プレフィックス案か、
  `X-Partial` と同じく GET に判定ヘッダを見せるか）。
- `Last-Event-ID` での replay を server 側でやるか（hub に小さな ring buffer を
  持たせる）か、再接続時は常に resync に倒すか — 後者で済むなら hub は
  「最新 seq だけ」を覚えればよい。実装時に判断。
- 一覧ページへの拡張（`GET /events?topics=` 複数購読）の topic 命名と UI。
- storybook での検証: SSE ハンドラを story 側に供給するか、JS 挙動は verify-ui
  側に寄せて storybook は fragment の見た目だけに留めるか。
