# コメント投稿の全体再読込と部分更新（htmx / Datastar / 手書き fetch）の検討

調査・設計メモ（実装前の検討段階）。`cli-and-web-api.md` の姉妹編。
「コメント一つ書いても全体を読み込む。このときスクロール位置は固定されているのか？
コメントは API 経由のほうが嬉しいかもしれない。htmx や Datastar を使うべきか？
その場合どのような観点を考える必要があり、手軽に確認可能な状態か、storybook との
整合性は？」という問いへの回答を整理する。

> **2026-11-15 再検討**: accounts・discussions 実装後かつ JS 方針緩和（decisions.md
> 「JavaScript 方針」）後の再評価。modern-web-guidance を引き直し、他画面操作の棚卸しと
> storybook 検証経路の整理を追記した。「現状の挙動」「Datastar 補足」の初回調査部分は
> 当時の調査記録として残し、変化した点は「再検討で変わった前提」に集約する。

## 現状の挙動（コード確認済み）

- `POST /issues/{id}/comments` → `303` → `/issues/{id}#comment-{新ID}`。
  つまり「**スクロール位置の維持」ではなく「新コメントのアンカーへの着地**」。
  狙いは「投稿したコメントに連れていく」であって、位置保持ではない。
- 他の POST（status toggle は `/issues/{id}` へ、edit も同様、delete は `/issues` へ）は
  fragment なしでリダイレクトするため**ページ先頭に戻る**。
- `history.scrollRestoration`（既定 `auto`）が効くのは**同一 URL のリロードと履歴
  traversal（back/forward）だけ**。PRG の新規ナビゲーションには適用されず、
  fragment があればアンカー、なければ先頭に移動する。
- コメントフォームはページ下部にあるため、長いスレッドでの投稿は「全体を再描画
  してから `#comment-N` まで飛ぶ」= 再読込が体感として残る。status/編集ボタンは
  ページ上部なので先頭リセットの実害は小さい。
- コスト面: 詳細再描画では**全コメントの markdown（goldmark）を再変換**する。
  200 件級スレッド（seed/story に長スレッドあり）では無視できない仕事量で、
  「1 件増えただけ」に対して全体を再計算している。

## 再検討で変わった前提（2026-11-15）

- **discussions が実装された**。コメント・返信投稿は issues と同じ PRG + アンカー
  （`#comment-{新ID}`、返信も同じ）。answer マークは既に `#comment-{cid}` へ着地する
  （初回メモの「案 A: redirect に fragment を付ける」は answer に関しては済んでいる）。
- **JS 方針が緩和された**（decisions.md）。CSP は `script-src 'self'` で `/static/` の
  外部ファイルは読める。条件は「storybook で挙動を検証できる（イベント発火を含む）」。
- **declarative 系は既に厚い**: cross-document View Transitions・方向付きページング
  遷移・speculation rules・Invoker Commands 削除ダイアログが入っており、
  部分更新は「残った最後の大きな遷移」という位置づけになった。
- **CSP に `connect-src` が無い**: `default-src 'none'` がフェッチ全般を塞ぐため、
  fetch ベースの部分更新は **`connect-src 'self'` の追加なしには動かない**。
  どの案を選んでも共通の前提変更（minigo storybook の live-reload が `relaxConnectSrc`
  を必要としたのと同じ問題）。

## その他の画面操作の棚卸し（部分更新の対象選定）

「どこが不快か」を全操作に広げて見直した結果:

| 画面・操作 | 現在の動き | 評価 |
|---|---|---|
| issue 詳細: コメント投稿 | 全体再描画 → `#comment-N` 着地。全 markdown 再変換 | **主対象**。長スレッドで再読込感・描画フラッシュが残る |
| discussion 詳細: コメント・返信 | 同上。返信は `<details>` 内フォーム、着地は新コメント | **主対象**（コメントと同機構で扱える） |
| discussion: answer マーク | 303 → `#comment-{cid}`（アンカー済み） | **副対象**。バッジ反転だけに全体再描画は過剰。同一機構に乗せられる |
| issue: status toggle | 303 → ページ先頭 | ボタンが上部なので実害小。余力があれば状態ピルの部分更新（低優先） |
| 削除（issue/discussion） | dialog → POST → 一覧へ | コンテキスト自体が変わるので navigation が自然。**対象外** |
| 編集（issue/discussion） | 別ページフォーム | 「編集画面」という文脈移行自体が UX。**対象外** |
| 一覧: フィルタ Apply / status タブ / ページング | GET 遷移。URL が状態を持ち共有可能 | navigation が正解。prefetch + 方向 VT 済みで体感も良い。**対象外**（select 自動 submit は小改善候補） |
| login / register / logout | 通常フォーム + PRG | **対象外** |

「不快」は**コメント/返信投稿の再描画コスト**に集中する。次点が answer マーク。
それ以外は PRG + アンカー + 遷移演出で既に妥当な快適さがある。

## 「API 経由のほうが嬉しい」を噛み分ける

ページ内の部分更新で JSON API を直接叩くと、**本文の markdown 描画をクライアント側
に持たせる必要が出る**。goldmark（GFM + alert 拡張 + sanitization）を JS で再現する
のは現実的でなく、「表示の真実はサーバ」という MPA の芯を崩す。
`cli-and-web-api.md` の JSON API は**CLI/外部クライアント向け**であって、
ページ部分更新の正解は **サーバが HTML fragment を返す POST**（htmx が典型例）。
クライアントは「差し替えるだけ」をやればよい。

## 選択肢（2026-11-15 更新）

| 案 | 内容 | 評価 |
|---|---|---|
| A. 現状維持 + anchor 徹底 | 全 POST の redirect に適切な fragment を付ける | **ほぼ済み**。answer マークは実装済み、status toggle は先頭で実害小、残りは「コメント再描画コスト」だけ。anchor 追加だけでは描画コストは消えない |
| B. 手書き fetch + DOM 差し替え | comment form の submit を奪い `fetch` → 返ってきた `<li>` を `beforeend` で append | **推奨**。ガイドのネイティブ API（`setHTML`/`moveBefore`/`ariaNotify`/`<progress>`）が htmx の売りをかなり埋めたため「車輪の再発明」度が下がった。~100 行の汎用 submit 奪取機構に留められる。検証可能性は最も素直（全コードが自作で storybook 経路に乗る） |
| C. htmx（vendored `/static/`） | 属性駆動（`hx-post`/`hx-swap="beforeend"`）。サーバは `text/html` fragment を返すだけ | MPA 思想と相性は良いが、vendored 依存の持ち込み + `hx-swap-oob` 等を使い始めるとテンプレートに属性が散る。B の汎用機構が「対象が増えて散らかってきた」ときの再評価先として残す |
| D. Datastar | `data-*` 属性 + signals + SSE（`text/event-stream`） | 初回調査どおり「SSE 応答モデル・signals 往復・CSP コスト（unsafe-eval or nonce 機構）」の3点で過剰。**push が欲しくなった時点で再評価** |
| E. JSON API + クライアント描画 | fetch → JSON → JS で DOM 生成 | markdown 描画の二重化が致命傷。不採用方向 |
| F. 宣言的部分更新（html-setters / html-streaming-setters） | 後から届く HTML で画面の一部を宣言的に更新（`out-of-order-html-streaming` ガイド） | **volatile（単一エンジン）でガイド自体が opt-in ゲート付き**。採用不可だが「部分更新の標準化された宣言的解」が来ている兆し。ウォッチ項目 |

## modern-web-guidance 適用（実装案への差分）

部分更新を実装する場合にガイドが要求・推奨する点。前回メモの「考える観点」に
追加・修正される形で効いてくる:

1. **CSP `connect-src 'self'` 追加**（必須・どの案でも）。`default-src 'none'` 下で
   `fetch()` は塞がれる。
2. **fragment の挿入は `setHTML()`（Sanitizer API）**（`sanitize-untrusted-html`）。
   parse+sanitize+挿入を一括で行うネイティブ API。`innerHTML` 文字列経由は mXSS の
   再導入経路になりうるとガイドが明記。サーバ fragment は same-origin の信頼できる
   出力なので厳密には過剰だが、defense in depth として使う。Safari 未対応（Chrome
   146+/FF 148+）→ 未対応時は `innerHTML`/`insertAdjacentHTML` にフォールバック
   （同一オリジンのサーバ出力なので許容。ガイドの DOMPurify フォールバックは
   vendored 依存を新たに持ち込むため本用途では重い）。
3. **周辺の out-of-band 更新は `moveBefore()`**（`move-dom-element-without-losing-state`）。
   answer バッジの別コメントへの移動・`updated_at` 行の差し替えなど、既存要素を
   reparent する際にフォーカス・アニメーション・`:focus` 状態を壊さない
   （`appendChild`/`insertBefore` は remove+insert で状態をリセットする）。Safari
   未対応 → 機能検出して `insertBefore` フォールバック。
4. **通知は `ariaNotify()` またはページ1箇所の polite live region**
   （`persistent-toast-notifications`/`accessibility`）。`ariaNotify()` は Baseline
   2026-09 の Newly available。form-errors.js が既に持つ「動的生成の
   `aria-live="polite"` announcer」パターンを再利用する形が最小コスト。ガイドの
   「focus redirection と toast/通知を同時に出さない」に注意 — 投稿後は**フォーカスを
   textarea に残す**（連投しやすい・フォームはページ下部で新コメントが視界に入る）
   方が、新コメントへフォーカスを飛ばすよりこのガイド上も安全。
5. **送信中の busy 表示はネイティブ `<progress>`**（`spinner`）。`value` 無しで
   indeterminate になり支援技術に正しく伝わる。二重送信防止は「submit 奪取時に
   `disabled` + in-flight フラグ」で担保。`prefers-reduced-motion` 尊重はガイドどおり。
6. **Trusted Types の導入線**（`trusted-types`）。DOM sink（`setHTML`/innerHTML）を
   持つようになれば `require-trusted-types-for 'script'` が「DOM シンクに届く
   スクリプトが存在しないので無意味」から実効のある防御に変わる。部分更新導入後の
   CSP 強化候補として記録（Report-Only からの段階導入がガイドの推奨）。
7. **`deprioritize-background-fetches` は適用外**: コメント投稿はユーザー起点の
   fetch であり background fetch ではない。

## B（手書き fetch）の実装スケッチ（ガイド適用後の形）

- サーバ: `POST /issues/{id}/comments`・`POST /discussions/{id}/comments` が
  **`X-Partial: 1`（または `Accept: text/html-fragment`）なら新コメントの `<li>`
  fragment を `text/html` で返す**。通常フォームの 303 PRG はそのまま残す
  （progressive enhancement: JS が無い/失敗したら従来どおり動く）。
- fragment 用に `{{define "comment"}}`（+ エラー時フォームの `{{define}}`）を切り出し、
  renderer が block 単位で描けるようにする。現行の per-page template set に fragment
  用テンプレートを別名登録する小変更。
- JS（`/static/partial-submit.js`、~100 行）:
  - `data-partial` を持つ form の submit を奪い `fetch(form.action, {method: POST, body: new FormData(form), headers: {"X-Partial": "1"}})`。
  - 応答 fragment を `Document.parseHTML()` または `setHTML()` 経由で挿入
    （`beforeend` 相当: `target.append(...nodes)`）。
  - 送信中は submit `disabled` + `aria-busy` + `<progress>`。完了でフォームリセット、
    フォーカスは textarea に残す。
  - 新コメント到着は polite live region に「Comment posted.」。`<li>` の
    `id="comment-N"` と `:target` ハイライトは DOM 構造が同じなら効く
    （`:target` は URL fragment に反応するので部分挿入では付かない — 新 `<li>` に
    一時的な class で同等のハイライトを出すか、`history.replaceState`+scrollIntoView
    の選択は実装時決定）。
  - 422 時はエラーフォーム fragment を差し替え、`autofocus` 相当で最初のエラー欄へ
    （form-errors.js の `data-error` 機構と同居）。
  - `copy-permalink.js`/`enter-submit.js` は event delegation 化して swap 後の新
    `<li>` でも効くようにする（現状 `copy-permalink.js` は document capture の
    CommandEvent/click で delegated、`enter-submit.js` は静的 querySelectorAll →
    新規 textarea に掛からないので delegation 化要）。
- OOB 更新（`updated_at` 表示・answer バッジ移動）は `moveBefore` で。ただし初回は
  コメント append のみに絞り、OOB は Phase 2（answer マーク部分更新）で使う。

## Datastar 補足（初回調査の記録）

- **ライセンス**: コアは MIT で無料。ただし freemium — `datastar-pro` の利便
  プラグイン群は商用ライセンスで「公開リポジトリに置くのは違反」。このリポジトリ
  （public）に vendored するなら MIT コアのみ、という前提を明確にしておく。
- **応答モデルが htmx と違う**: POST の応答は HTML fragment そのものではなく
  **`text/event-stream` の SSE** で、`datastar-patch-elements`（HTML を patch）や
  `datastar-patch-signals`（フロント状態を patch）のイベント列を返す。サーバ側は
  手書きでも組めるフォーマットだが「fragment を返せば終わり」の htmx より一段
  特殊。`datastar-go` SDK あり。OOB（画面の別領域の同時更新）がデフォルトで強い。
- **signals**: `data-bind`/`data-signals`/`$foo` のフロント状態モデルを持ち、
  既定で**全 signal を毎リクエストに送信**（GET は `datastar` query param、他は
  JSON body）。バックエンドがフロント状態を常時読める設計だが、「コメントを
  投稿したら末尾に `<li>` を足す」だけならこの往復は使わない。
- **CSP が論点**: 既定は属性内 expression を `Function()` で評価するため
  **`script-src 'unsafe-eval'` が要る** — 現行 CSP（`script-src 'self'`）とは衝突。
  v1.0.3 以降の CSP mode（`<html data-nonce>` + `script-src 'nonce-…'`）なら
  unsafe-eval を避けられるが、nonce を毎レスポンス発行して CSP ヘッダと html に
  埋める機構が要る（現行の静的ヘッダ middleware には無い）。htmx の基本属性
  （hx-get/post/swap 等）は eval 不要で `script-src 'self'` のまま動くので、
  **CSP コストは htmx < Datastar**。
- **progressive enhancement**: `data-*` 属性は JS 無しでは無害で、普通の form
  submit に落ちる点は htmx と同じ。
- **サイズ**: ~11KB（htmx ~14KB）と小型。

## htmx / Datastar を入れる場合に考える観点（初回調査 + 追記）

1. **progressive enhancement**: JS 無しで従来の POST→303 がそのまま動くこと。
   htmx は同じフォームに属性を足すだけで自然に成立する（JS があれば hx-post に
   乗り換え、無ければ普通の submit）。datastar も同じ考え方。**手書き fetch でも
   「submit 奪取しないと従来 submit」なので同じ性質を持つ**。
2. **CSP**: vendored ファイルの読み込み自体は `script-src 'self'` で済む。ただし
   **expression 評価に `Function()` を使う方式（Datastar 既定）は `unsafe-eval` が
   要り現行 CSP と衝突** — 上記「Datastar 補足」参照。htmx の基本属性なら eval
   不要で現行 CSP のまま動く。追記: **fetch を使う案はすべて `connect-src 'self'`
   が追加で要る**（htmx も内部で XHR/fetch するため同じ）。
3. **エラー UX**: 422 応答を fragment で返してフォームだけ差し替えるか（htmx は
   `htmx:responseError`/`hx-target` 側で非 2xx の扱いを指定できる）。
   `form-errors.js` の aria-invalid ↔ :user-invalid 同期機構と同居させる。
4. **a11y**: swap 後のフォーカス管理、新コメント到着の polite announcement
   （`aria-live`/`ariaNotify()`）。全ページ遷移時の「ページタイトルが読まれる」に
   相当する通知を部分更新では自分で用意する必要がある。
5. **描画の真実はサーバ**: fragment 用に `{{define "comment_list"}}` や
   `{{define "comment"}}` を切り出し、renderer に block 単位の描画を足す。
   現状の per-page template set（layout+page をセットで parse）のまま、fragment 用
   テンプレートを別名で登録する小変更で済む。
6. **既存 JS との干渉**: swap で新しい `<li>` が入ると、`copy-permalink.js` や
   `enter-submit.js` のイベント紐付けを再実行/移譲する必要がある（htmx なら
   `htmx:load` か event delegation）。`content-visibility` のコメント skip ルール
   （`:nth-child(n+3) .comment-body`）は DOM 構造が同じならそのまま効く。
   view transitions はドキュメント遷移が発生しない部分更新では無関係。
   `pagination-transition.js` も一覧のページング限定なので干渉しない。
7. **二重送信・送信中の表示**: `hx-indicator`/disabled 化。現行は submit→ナビゲーション
   で二重送信しにくいが、部分更新では明示的に防ぐ。
8. **差分か追記か**: コメント投稿は `beforeend` で新 `<li>` 1 個を追加するだけなので
   差分アルゴリズムは要らない。ただし issue の `updated_at` 表示等、周辺の更新が
   要るなら `hx-swap-oob` 相当（手書きなら `moveBefore`）が論点になる。

## storybook との整合性（2026-11-15 訂正・追記）

- **見た目は確認できる**: fragment 用テンプレートを story（print 型 or StoryHttp）に
  出せば、新 `<li>` やエラー時フォームの見た目は storybook で回る。テンプレート/
  CSS/fixture の試行錯誤は既存の仕組みに乗る。
- **POST ハンドラの意味論も実は確認できる（初回メモの訂正）**:
  `StoryHttp*` story は引数として `*http.Request` を受け取るが、**story 内部で
  `httptest.NewRequest(POST, "/issues/1/comments", strings.NewReader(form))` を自前で
  組み立てて `New(storyStore{}).ServeHTTP` に流せば、POST ハンドラの応答
  （fragment・303・422・ヘッダ）を storybook で確認できる**。minigo storybook の
  `Render` が合成する `GET /` は story 側で無視してよい。つまり「POST ハンドラを
  呼べない」という初回の記述は不正確で、handler の意味論レベルでは storybook で
  足りる。
- **それでも足りない部分**: 「ブラウザが submit を奪い fetch を飛ばし DOM を
  差し替える」挙動の確認には実ブラウザ + 実エンドポイントが要る。storybook の
  HTTP サーバは GET `/story/<name>` しか持たず、story 内 JS の fetch が
  `/issues/1/comments` に飛ぶ宛先が無い。改善案は
  `storybook-with-minigo.md` の「部分更新・JS 挙動の検証経路」節を参照
  （upstream に request パススルーを足して実アプリパスを mount する案）。
- 検証可能性という点では **B が最も素直**（発火するコードが自作で、storybook で
  イベント発火込みの確認ができる）。C は「htmx の挙動はライブラリに信用させる、
  アプリ側は fragment と属性だけテストする」割り切りになる。

## 結論（2026-11-15 確定版）

1. **案 A（anchor 徹底）はほぼ済み** — answer マークは既に `#comment-{cid}` 着地。
   残る不快はコメント/返信投稿の再描画コストのみ。
2. 部分更新は **B（手書き fetch + HTML fragment）をコメント/返信に限定して採用**。
   ガイドのネイティブ API（`setHTML`/`moveBefore`/`ariaNotify`/`<progress>`）が
   htmx の売りを埋めたことで、依存ゼロ・検証素直・vendored 無しの B が筋良く
   なった。前提変更として `connect-src 'self'` を CSP に足す。
3. **answer マーク・status toggle は同じ機構に Phase 2 で乗せる**（`<li>` 差し替え・
   状態ピル差し替え、`moveBefore` 活用）。一覧フィルタ・削除・編集・認証フォームは
   navigation のまま（対象外確定）。
4. **htmx は「対象が増えて data-partial 機構が散らかった時点」の再評価先**、
   Datastar は push 更新が欲しくなった時点の再評価先。宣言的部分更新
   （html-setters 系）は volatile でウォッチのみ。
5. JSON API 化は `cli-and-web-api.md` 側の話。**ページ内部分更新には HTML
   fragment が正解**。JSON API と fragment endpoint は将来同居しうる（用途が違う）。

## 残課題

- fragment 用 `{{define}}` の切り出し粒度（`<li>` 単位か comment-list 全体か）と
  fragment を返すための判定ヘッダ（`X-Partial` か `Accept` か）の確定。
- エラー時 fragment の設計（422 でエラー付きフォームをそのまま返す形で良いか）。
- 新 `<li>` のハイライト方法（`:target` が効かない部分挿入での代替表示）。
- `enter-submit.js` の delegation 化、form-errors.js との共存確認。
- push 更新（他者コメント・一覧の変化）が欲しくなったら datastar/SSE の再評価。
- 部分更新導入後: `require-trusted-types-for 'script'`（Trusted Types）の Report-Only
  からの段階導入を CSP 強化として検討。
