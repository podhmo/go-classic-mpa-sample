# ブラウザ自動化ツールの使い分け調査（playwright-cli / chrome-devtools CLI / MCP）

ブラウザをエージェントから操作する道具が3系統ある。今回の描画最適化・視覚差分検知の調査で実際に併用し、それぞれの得手/不得手を整理した。調査日: 2026-11-20。

## 三者の概形

| | playwright-cli | chrome-devtools CLI | chrome-devtools MCP |
|---|---|---|---|
| 起動 | `pnpm dlx @playwright/cli`（repo に `~/.local/bin/playwright-cli` の shim あり） | `npm i -g chrome-devtools-mcp` に同梱の `chrome-devtools` コマンド（v1.13.0） | MCP サーバとして Devin/クライアントに登録 |
| 実体 | 常駐デーモン + CLI クライアント | 同上（`chrome-devtools start` でブラウザ daemon を起動） | 同じ MCP サーバを MCP プロトコルで呼ぶ |
| ブラウザ | `.playwright/cli.config.json` で chromium/executablePath を指定 | `chrome-devtools start --executablePath <path> --headless` で1回だけ指定 | 同上 |

MCP 版と CLI 版は**同じツール群の別フロント**（CLI は MCP サーバの薄いラッパ）。この repo の作業では CLI で十分。エージェントに常駐ツールとして渡したい場合だけ MCP で登録する意味がある。

## 用途整理 — 何に効くか

### playwright-cli が強いもの

- **定型操作と E2E 確認**: `open`/`snapshot`/`click`/`fill`/`screenshot`/`console` — verify-ui スキルの実ブラウザ検証はこれ
- **スクリーンショット差分（視覚回帰）**: `page.screenshot({fullPage, animations:'disabled'})` がそのまま pixel-diff の材料になる
- **任意の計測コード**: `run-code`（`--filename` 可）で Playwright API を丸ごと書ける。特に `page.context().newCDPSession(page)` で **生 CDP セッション**が取れる → CSS rule usage tracking (`CSS.startRuleUsageTracking`/`takeCoverageDelta`) や `Page.captureScreenshot` 等、CLI に無い CDP 機能も自前で呼べる
- **アクセシビリティスナップショット**: a11y tree の `snapshot` は DOM grep より構造的に UI を把握できる

### chrome-devtools CLI が強いもの

- **「何が遅いか」の一次診断**: `performance_start_trace <pageId>` で trace を取ると **insight が自動分析済み**で返る（LCPBreakdown, RenderBlocking, NetworkDependencyTree, DocumentLatency, Cache 等）。生 trace を自前で解釈する必要がない。詳細は `performance_analyze_insight <pageId> <insightSetId> <name>`
- **CSS カスケードの確認**: `get_css_styles` で要素への適用 cascade（継承・!important・出所 stylesheet）を一覧できる — 「なぜこの見た目か」の調査用
- **Lighthouse 監査**: `lighthouse_audit` で丸ごとスコアリング
- **heap snapshot / network mock / screencast**: メモリ・ネットワーク条件の再現はこちらの管轄

### 使い分けの結論

- **確認・操作・差分のループ**（verify-ui, visual-diff）→ **playwright-cli**。`run-code` で不足機能は CDP まで降りられる
- **性能のボトルネック診断**（遅い/重い原因探し）→ **chrome-devtools**。trace→insight の自動分析が手間を大きく減らす
- **CSS の詳細調査**（スタイルの出所追跡）→ chrome-devtools `get_css_styles`
- **常駐ツールとしてエージェントに持たせる** → MCP 登録。repo 作業の都度起動なら CLI の方が軽い

両者は排他ではない。実際、今回は「chrome-devtools で insight を眺めて Cache 問題を特定 → playwright-cli の CDP 経由で CSS coverage を取る」という分担が自然にハマった。

## 運用メモ（ハマりどころ）

- `chrome-devtools` の `--executablePath` は **start コマンドのフラグ**（new_page 等の per-command 引数ではない）
- `playwright-cli` は **repo ルートから**実行する（`.playwright/cli.config.json` を拾って chromium のパスが解決される）
- `playwright-cli run-code` 内で `await import('fs')` は不可（dynamic import 未指定）。ファイル出力は `page.screenshot({path})` 等の引数側に寄せる
- `performance_start_trace` は `--reload` がデフォルト true。warm load を測るには先に `new_page`/`navigate_page` で URL を開いてから実行
- ImageMagick `compare -metric AE` は **metric を stderr に**吐く。`2>/dev/null` で潰すと値が取れない（`2>&1` で拾う）
