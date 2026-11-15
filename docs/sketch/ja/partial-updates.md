# コメント投稿の全体再読込と部分更新（htmx / Datastar）の検討

調査・設計メモ（実装前の検討段階）。`cli-and-web-api.md` の姉妹編。
「コメント一つ書いても全体を読み込む。このときスクロール位置は固定されているのか？
コメントは API 経由のほうが嬉しいかもしれない。htmx や Datastar を使うべきか？
その場合どのような観点を考える必要があり、手軽に確認可能な状態か、storybook との
整合性は？」という問いへの回答を整理する。

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

## 「API 経由のほうが嬉しい」を噛み分ける

ページ内の部分更新で JSON API を直接叩くと、**本文の markdown 描画をクライアント側
に持たせる必要が出る**。goldmark（GFM + alert 拡張 + sanitization）を JS で再現する
のは現実的でなく、「表示の真実はサーバ」という MPA の芯を崩す。
`cli-and-web-api.md` の JSON API は**CLI/外部クライアント向け**であって、
ページ部分更新の正解は **サーバが HTML fragment を返す POST**（htmx が典型例）。
クライアントは「差し替えるだけ」をやればよい。

## 選択肢

| 案 | 内容 | 評価 |
|---|---|---|
| A. 現状維持 + anchor 徹底 | 全 POST の redirect に適切な fragment を付ける（status→ `#comment-form` や該当箇所へ戻す等） | JS ゼロ。scroll 問題の大半はこれで消える。**まずやるべき一手** |
| B. 手書き fetch + DOM 差し替え | comment form だけ `fetch` → 返ってきた `<li>` を `beforeend` で append | 依存ゼロ・~50 行。だが要件が増えると「htmx の車輪の再発明」になりやすい。検証可能性では最も素直（後述） |
| C. htmx（vendored `/static/`） | 属性駆動（`hx-post`/`hx-swap="beforeend"`）。サーバは fragment を返すだけ | MPA 思想（サーバレンダリング・宣言的）と相性が良い。1 ファイル vendored で CSP `script-src 'self'` のまま動く |
| D. Datastar | SSE + signals 基調。他者コメントの push 更新まで欲しいなら有力 | 今回の「投稿で部分更新」には過剰。概念・導入面が大きい。**push が欲しくなった時点で再評価** |
| E. JSON API + クライアント描画 | fetch → JSON → JS で DOM 生成 | markdown 描画の二重化が致命傷。不採用方向 |

## htmx / Datastar を入れる場合に考える観点

1. **progressive enhancement**: JS 無しで従来の POST→303 がそのまま動くこと。
   htmx は同じフォームに属性を足すだけで自然に成立する（JS があれば hx-post に
   乗り換え、無ければ普通の submit）。datastar も同じ考え方。
2. **CSP**: vendored ファイルなら `script-src 'self'` のままで済む（現行 CSP に
   変更不要）。htmx の inline script 相当機能（`hx-on:` 等）は CSP とは無関係だが
   「挙動をテンプレートに散らさない」観点では使い方を決めておく。
3. **エラー UX**: 422 応答を fragment で返してフォームだけ差し替えるか（htmx は
   `htmx:responseError`/`hx-target` 側で非 2xx の扱いを指定できる）。
   `form-errors.js` の aria-invalid ↔ :user-invalid 同期機構と同居させる。
4. **a11y**: swap 後のフォーカス管理、新コメント到着の polite announcement
   （`aria-live`）。全ページ遷移時の「ページタイトルが読まれる」に相当する通知を
   部分更新では自分で用意する必要がある。
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
   要るなら `hx-swap-oob` 相当が論点になる。

## storybook との整合性

- **見た目は確認できる**: fragment 用テンプレートを story（print 型 or StoryHttp）に
  出せば、新 `<li>` やエラー時フォームの見た目は storybook で回る。テンプレート/
  CSS/fixture の試行錯誤は既存の仕組みに乗る。
- **挙動（イベント発火→fetch→DOM swap）は storybook では足りない**:
  StoryHttp story は `httptest.NewRequest(GET /)` 固定で POST ハンドラを呼べない。
  fragment を GET で出す設計にすれば StoryHttp に乗せられるが、それでも
  「htmx が発火して swap する」動作自体は実ブラウザが要る → **verify-ui
  （実サーバ + playwright-cli）の領域**。
- これは JS 方針（decisions.md）の条件 (1)「storybook で挙動を検証できる」への
  適用範囲の問題: 手書き JS（案 B）なら自作イベントリスナの発火を storybook で
  直接検証できるが、htmx の属性駆動は「htmx 自身の挙動」を信頼する形になる。
  storybook で見た目 + playwright で挙動、という分担に読み替えるなら decisions.md
  の運用メモ追記が要るかもしれない。
- 検証可能性という点では **B が最も素直**（発火するコードが自作で、storybook で
  イベント発火込みの確認ができる）。C は「htmx の挙動はライブラリに信用させる、
  アプリ側は fragment と属性だけテストする」割り切りになる。

## 結論のたたき台

1. **まず A**（redirect に適切な fragment を付ける）で scroll 問題の大半は解消できる。
2. 部分更新は **B（手書き fetch）または C（htmx vendored）**。repo の思想では
   C が機能対コストで良いが、検証可能性では B が素直。datastar は今回の要件には
   過剰（SSE/push が欲しくなったら再評価）。
3. JSON API 化は `cli-and-web-api.md` 側の話であり、**ページ内部分更新には HTML
   fragment が正解**。JSON API と fragment endpoint は将来同居しうる（用途が違う）。

## 残課題

- B/C のどちらにするか（htmx 採用なら decisions.md の JS 検証条件の解釈を追記するか）
- fragment 用 `{{define}}` の切り出し粒度（`<li>` 単位か comment-list 全体か）
- エラー時 fragment の設計（422 をそのまま差し替えるか）
- push 更新（他者コメント・一覧の変化）が欲しくなったら datastar/SSE の再評価
