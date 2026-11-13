# AGENTS.md

このリポジトリで作業するエージェントは `docs/ja/policy.md` に従うこと。

## ドキュメント

- `docs/ja/policy.md` — 作業ポリシー（タスク分解・サブエージェント運用・記録ルール・検証方針）。**必ず読んで従う**
- `docs/ja/pitfalls.md` — 過去にハマった落とし穴（作業前に目を通すとよい）
- `docs/ja/decisions.md` — 設計判断の記録（変更時は追記する）
- `docs/ja/bugs.md` — 既知のバグ・問題点（発見したら重要度つきで記録）
- `docs/ja/prompts.md` — 元の仕様書
- `TODO.md` — タスクの分解・進捗

## 主要コマンド

```sh
go run .                    # 起動 (http://localhost:8080)
go run . -db app.db init    # DB スキーマ+index を明示的に初期化
go test ./...               # 全テスト（unit + store + E2E）
go vet ./... && gofmt -l .  # 静的解析・フォーマット確認（コミット前に）
go fix ./...                # モダナイズ（必要に応じて）
```

実ブラウザでの画面検証が必要なら `playwright-cli`（`.claude/skills/playwright-cli` スキル参照、`pnpm dlx @playwright/cli` 経由）を使える。
