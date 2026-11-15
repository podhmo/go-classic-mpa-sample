# JS 資産の構成: ESM 化とバンドルの検討

> 2026-11-15 に追加。「そろそろ ESM 利用やバンドルの検討をするべきか」という問いへの調査結果。前提の部分更新方針は `partial-updates.md` 参照。

## 現状

- 5 つの classic script、合計約 12KB（非圧縮）。各ファイルは IIFE で囲み、`<script src="..." defer>` で**ページごとに必要なものだけ**読み込む構成。
- CSP は `script-src 'self'` — 外部ファイルのみ、インライン script 禁止（decisions.md の JS 方針どおり）。
- 配信は `ServeFileFS`（embed.FS）で、`Cache-Control` は未指定。`Last-Modified` ベースの条件付き GET は効く。
- 共有コードはまだないが、`partial-submit.js` 実装時に polite announcer を form-errors.js から複製する予定がある — **初めての重複が目前**。

## ESM（`type="module"`）化

### 変わること

- module script は defer 相当・strict mode・`import`/`export` 使用可。IIFE ラッパが不要になる。
- 共有ユーティリティを切り出せる: `announcer.js`（polite live region — form-errors と partial-submit で共有）、`dom.js`（`setHTML`/`parseHTML` と `moveBefore` のフォールバック付きラッパ）など。
- 「ページに必要なものだけ読む」性質は維持できる: 各ページ 1 エントリ module（`<script type="module" src="/static/xxx.js">`）にし、内部依存は import グラフで解決。

### 制約・注意点

- **import map は使えない**: `<script type="importmap">` はインライン script なので現行 CSP でブロックされる。全部自前ファイルなので bare specifier 不要、相対パス import で十分。
- **条件付き読み込みは `conditional-async-dependencies` ガイドのパターン**: Sanitizer API（`setHTML`）や `moveBefore` のフォールバックを条件付き import する場合は top-level await + **単一エントリ**にする。TLA は import 元の親 module しか止めないため、兄弟 static import に並べると先に評価されてしまう。さらに Safari 15–26.6 は複数 module からの同時 TLA import で壊れる（Safari 27 で修正）— 単一エントリ構成なら回避可能。
- **`optimize-script-priority`**: `fetchpriority` はデフォルト優先度が悪いときだけ。全ファイルが小さいので現状は何もしない。
- **storybook**: `/static/` は FileServer で配られるので module の fetch もそのまま動く想定（実装時に確認）。

### 移行タイミング

「次に共有コードが必要になった時点」= `partial-submit.js` 実装時が自然な切替点。全ファイルの一括 module 化も可能だが、まず `partial-submit.js` を最初の module として追加し、announcer を module として切り出す漸進移行が低リスク。classic と module の混在は許容される（どちらも deferred、実行は import グラフ順）。

## バンドル / minify / fingerprint の検討

### 現時点でやらない理由

- **ツールチェーン方針**: シングル Go バイナリ + `embed.FS`、npm/node パイプラインなしがこの repo の美学。バンドル導入は新規 toolchain（esbuild 等）を常設することになり、ビルド手順・CI・storybook の全てに波及する。
- **規模**: 合計 ~12KB。minify の効果は小さく、gzip すれば差は数 KB。リクエスト数もページあたり CSS1 + JS≤3 + speculation prefetch 程度で、HTTP/1.1 の接続制限にも余裕がある。
- **デバッグ性**: storybook でそのまま読めるファイル群は、検証・デバッグ・review の単位として都合が良い。バンドルすると story 単位との対応が曖昧になる。
- **fingerprint**: immutable キャッシュするならコンテンツハッシュ入りファイル名が要るが、embed.FS なのでバイナリごと更新され、更新単位が変わらない。`Cache-Control: max-age` を付けるか自体は別の小論点として残る。

### 再評価トリガー（どれか1つで見直す）

- JS 合計が ~50KB を超える
- 1 ページで読み込むファイルが 5 個を超える（共有 module 化後の話）
- htmx など外部依存を vendored で導入する（その時点で既に toolchain 議論が起きる）

候補は esbuild 単発バンドルを `go generate` / Makefile から回す形が最小侵入。

## 結論

- **ESM 化は「やる」**: `partial-submit.js` 実装と同時に開始。動機は共有コード（announcer）の重複回避。
- **import map / bare specifier は使わない**（CSP 上も使えない）。相対 import のみ。
- **バンドル・minify・fingerprint は現時点不採用**。再評価トリガーを上記どおり記録。
