# 描画計測・CSS 整理・視覚差分検知の確立

「Chrome DevTools で描画を最適化する（主因は CSS かも）」という調査の結果と、CSS を壊さないことを検知しながら整理するための仕組み。調査日: 2026-11-20。

## 計測結果: CSS はボトルネックではない

`chrome-devtools performance_start_trace` を /issues と StoryPullFiles（最重 story）に取ったところ:

- **LCP 70ms**（TTFB 2ms + render delay 67ms）、CLS 0
- ParseAuthorStyleSheet 計 ~0.5ms、style 計算 ~0.8ms、Layout 計 ~7-11ms、Paint ~1ms
- 残りは RunTask / V8 / ナビゲーション初期化の固定コスト

→ **「main になるのは CSS」という仮説は否定的**。2228 行・53KB の手書き CSS でもこの規模のページでは style/layout 合成で ~10ms しか寄与しない。

### 本当に効いた最適化: /static/ のキャッシュ

trace の Cache insight が唯一の実害を指摘していた: `/static/*` の応答に Cache-Control/ETag/Last-Modified が無く、**MPA の全ナビゲーションで 59.4kB の静的資産を毎回再取得**していた（embed.FS は modtime を持たないので ServeFileFS も validator を出せない）。

スパイクとして `Cache-Control: public, max-age=3600` を全 /static/ ハンドラに付与（web.go の `serveStatic` helper に集約）。再 trace で TTL 3600s / wasted bytes 0 B を確認。modern-web-guidance の performance ガイドでは versioned asset の CacheFirst が本命なので、hash 入り URL + `immutable` は残課題（TODO.md 参照）。

## CSS 整理（見た目不変の範囲）

CDP の rule usage tracking を全 story に union 適用し、静的解析と突き合わせて実施したのは:

- **`--danger-hover` 系の除去**: 3 カ所で定義（light/dark/light-dark 各ブロック + raw `--danger-hover-light/-dark`）されるが `var()` 参照がどこにも無い死に変数
- **`--font-mono` の :root 集約**: 未定义のまま `var(--font-mono, ui-monospace, monospace)` と各所フォールバックしていた → `:root` に定義して参照を `var(--font-mono)` に簡素化

見送ったもの: `.markdown-body` の dup セレクタ（grouped rule との組み合わせで実害なし）、`html`/`textarea` の複数ルール（scroll-padding/container-type/field-sizing の別関心ごと）。

### coverage 分析が示した fixture の穴と検証盲点

2026-11-27 に 58 story で取り直した union では 309 style rule 中 **85 rule が未到達**だった。当初 fixture として欠けていたものは story を追加して潰した:

- `.user-handle` → `StoryIssueListAuthenticated`
- `.issue-body-empty` → `StoryIssueDetailNoBody`
- markdown の heading/pre/blockquote/table/image/hr と `TIP/IMPORTANT/WARNING/CAUTION` alert → `StoryIssueDetailMarkdownRich`
- `.badge-file-D/R` → `StoryPullFiles` に削除・rename fixture を追加
- `.comment-list:has(> .comment:nth-child(50))` → long story を 51 件へ増量
- `.error-page` は追加不要（既存 `StoryError404` で到達済み）

残りは dead code ではなく、静的 render の union では発火しない **検証盲点** に分類できる:

- **pseudo-state**: `:hover`, `:focus-visible`, `:user-invalid`（実ブラウザでポインタ/キーボード/入力操作が必要）
- **JS runtime state**: `.toast`, `[data-copied]`/`[data-failed]`, `.comment-new`, `.comment-form > progress`, `.markdown-preview-empty`, `textarea[hidden]`
- **open/collapsed state**: `.confirm-dialog:modal`/`::backdrop`、closed `<details>` 内の `.comment-hidden-body .comment-body`、mobile nav の `[open]`/`:not([open])`
- **環境依存 media/feature query**: dark scheme, reduced motion, narrow viewport, coarse pointer, `@supports not (order: sibling-index())` fallback
- **pseudo-element / transition**: `::marker`, `::view-transition-*` は rule usage の観測対象になりにくく、navigation を伴わない story では発火しない

→ naive な dead-code 除去はここで誤爆する（`badge-file-{{.StatusLetter}}` のようなテンプレート展開クラスは grep に出ない）。**coverage は「storybook の検証盲点の定量化」に使うのが正しい**。pseudo-state/JS/open state は story を増やすより `verify-ui` 側の実 app 検証へ振る。

## 視覚差分検知の仕組み（確立済み）

`tools/visual-diff.sh` を追加:

```sh
tools/visual-diff.sh update   # 全 story を撮影して .visual-diff/baseline に保存
tools/visual-diff.sh check    # 再撮影して ImageMagick compare -metric AE で閾値判定
```

- storybook（`make storybook` → :6006）の `/` から `/story/*` を全列挙（現状 58）し、1280x800 で fullPage 撮影
- **決定性のための4条件**:
  1. `page.screenshot({animations: 'disabled'})` — 無いと `.back-to-top` の scroll-state fade が中間状態で写って 23860px の偽差分になった
  2. `document.fonts.ready` 待機 — フォント非同期描画対策
  3. **fixture の時刻 pin** — Pull 系 story が `time.Now()` を使っており実行ごとに時刻差分（263–1687px）が出たため、`story_pages.go` に `storyNow`（固定時刻）を導入して全 `time.Now()` を置換。これが無いと baseline 比較が成立しない
  4. **scroll position の reset** — server error story の `autofocus` は load 時に下へ scroll し、sticky header / fixed back-to-top が fullPage screenshot 内で別位置に写る（108300px の偽差分）。撮影前に `scrollTo(0, 0)` へ戻す
- 閾値は `PIXEL_THRESHOLD`（既定 100px）— sub-10px の AA ノイズ（StoryLabelFormEdit で 7px 観測）は許容し、レイアウト崩れは確実に拾う
- `.visual-diff/` は gitignore。baseline は「今の正」を撮り直す運用

### storybook vs 実 app の使い分け（判断プロセス）

| 検証対象 | 場所 | 理由 |
|---|---|---|
| CSS/HTML の見た目回帰 | **storybook + visual-diff** | 全 variant を決定的に列挙できる、DB 不要、速い（58 story ≒ 20s） |
| JS が付ける状態クラス（toast, [data-copied], .comment-new, dialog open） | **実 app + playwright-cli**（verify-ui） | storybook は静的 render で JS 駆動の状態に届かない |
| PRG 遷移・view transition・SSE 部分更新 | **実 app** | ナビゲーション横断の振る舞いは story 単体に無い |
| CSP/console エラー | **実 app**（`playwright-cli console` クリーン必須） | storybook は CSP を掛けない |

差分として「壊れた」と言える条件: (a) baseline 比で閾値超の pixel diff、かつ (b) 意図した変更差分でないこと。(a) だけだと fixture 非決定性や AA ノイズを拾うので、まず決定性の4条件を疑うのが運用のコツ。

#### 編集→反映までのキャッシュ挙動（実測）

max-age=3600 を入れたので「CSS を直しても反映されない」事故が起きうるかを `app.css` に一時変数を書き込んで追跡した:

- **storybook は無問題**。`/static/` はディスク直読み（`http.FileServer`）で Cache-Control を付けないため、編集→通常リロードで即反映（実測: 値の書き換えが reload で反映）。さらに `make storybook-watch`（`-watch`）なら変更検知で自動リロード。**CSS 編集の主戦場は storybook でよい**
- **実 app は max-age のとおり stale になる**。`go run` 再起動（embed に新CSS）後の通常リロードでも HTTP cache の古い bytes が使われる。回避は Ctrl+Shift+R（ハードリロード）、DevTools を開いた状態で Network の「Disable cache」、playwright なら CDP の `Network.clearBrowserCache`（実測で fresh を確認）
- Fragment 系 story（`Story*Fragment`）は裸の HTML 断片で `app.css` を読み込まない。CSS の反映確認は `StoryIssueList` などページ系 story で行うこと

## 検証済みの流れ（再現手順）

```sh
tools/visual-diff.sh update        # 変更前に baseline を撮る
# … app.css / templates を編集 …
tools/visual-diff.sh check         # 全 story が閾値内なら見た目不変
```

今回の CSS 変更は `--danger-hover` 削除 + `--font-mono` 集約で pixel diff 実質 0（差分は fixture 時刻 pin 由来のみ）を確認済み。

## 残課題（TODO.md へ転記）

- pseudo-state/JS/open state の coverage 盲点を実 app 検証（verify-ui）へ明示的に振る
- /static/ の versioned URL 化（`immutable` + hash query）で max-age=3600 の妥協を外す
- dark モードの差分検証（現在の baseline は light のみ）
