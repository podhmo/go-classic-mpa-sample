# AGENTS.md

このリポジトリで作業するエージェントは `docs/ja/policy.md` に従うこと。

## ドキュメント

- `docs/ja/policy.md` — 作業ポリシー（タスク分解・サブエージェント運用・記録ルール・検証方針）。**必ず読んで従う**
- `docs/ja/pitfalls.md` — 過去にハマった落とし穴（作業前に目を通すとよい）
- `docs/ja/decisions.md` — 設計判断の記録（変更時は追記する）
- `docs/ja/bugs.md` — 既知のバグ・問題点（発見したら重要度つきで記録）
- `docs/ja/prompts.md` — 元の仕様書
- `docs/ja/roadmap.md` — バージョン計画（v1: issue tracker 現行 / v2: アカウント+discussions / v3 候補）
- `docs/ja/for-ui-designer.md` — デザイン担当者向け。css/js/html を書き換えながら見た目を確認する人が、自作 storybook でアプリ本体なしに試行錯誤する手順
- `TODO.md` — タスクの分解・進捗（追加・完了・GC の運用ルールは `.agents/skills/todo-md/SKILL.md` に必ず従う）
- `TODO-archive.md` — 完了済みタスクの追記専用アーカイブ（直接編集しない、`make todo-gc` で自動追記）

## スキル

- `.agents/skills/todo-md/` — TODO.md / TODO-archive.md の運用ルール。タスクの追記・完了・部分完了時は必ず参照する
- `.agents/skills/modern-web-guidance/` — HTML/CSS・クライアント側UIの実装・更新前に `npx -y modern-web-guidance@latest search "<query>"` でモダンなベストプラクティスを検索するスキル。テンプレートや app.css を触る作業では先に実行する
- `.agents/skills/iterate-ui/` — UI を変更すると決めたときのスキル。デザイン部分(templates/static/story_pages.go)の試行錯誤をアプリ本体の環境構築なしで行う。自作 storybook(`make storybook` → :6006)を使う。UI を触る作業では必ず参照する
- `.agents/skills/verify-ui/` — UI 変更後に動作確認をアプリ全体へ広げるときのスキル。実サーバ(`go run .` → :8080、demo データ自動シード)と playwright-cli で実ブラウザ検証を行う

## 主要コマンド

```sh
go run .                    # 起動 (http://localhost:8080)
go run . -db app.db init    # DB スキーマ+index を明示的に初期化
go test ./...               # 全テスト（unit + store + E2E）
go vet ./... && gofmt -l .  # 静的解析・フォーマット確認（コミット前に）
go fix ./...                # モダナイズ（必要に応じて）
make todo-gc                # TODO.md の [x] 項目を TODO-archive.md へ移す
```

実ブラウザでの画面検証が必要なら `playwright-cli`（`.claude/skills/playwright-cli` スキル参照、`pnpm dlx @playwright/cli` 経由）を使える。UI 変更後の動作確認フローは `.agents/skills/verify-ui/` 参照。
