# CLI（zatu）の依存と認証まわりの未決定事項

調査・設計メモ（未決定の問いの棚卸し段階）。`naming.md` の改称で
`zatu`/`zatudan` が確定したあとに残った、CLI の依存と認証の2論点を整理する。

問いは2つ:

1. **`zatu` を sqlite 非依存（remote only）にしたほうがインストールが楽なのでは**。
   ただし「一度インストールすれば local 直叩きも web API 経由も同じ形式で渡せる」
   現行のほうが楽、という気持ちもある。
2. **`auth login` 相当の機構がない**。発行したトークンをファイルに保存して以後
   自動で使う、という挙動が欲しいか。

## 現状の整理

- `zatu` は `internal/cli` → `internal/store` → `modernc.org/sqlite` を import
  している。**local mode**（`--db` 直結、サーバ不要）と **remote mode**
  （`--server`/`--token`、HTTP で `/api/v1`）を、同一の Backend インターフェース
  経由で切り替える。
- modernc.org/sqlite は **pure Go（cgo 不要）**なので `go install` は今でも通る。
  sqlite を外す利点はビルド時間・バイナリサイズ・依存面積（`modernc.org/libc`
  等の indirect が付いてくる）であって、toolchain 要件ではない。
- `auth token`（発行・list・revoke）は **local mode 専用** — DB を直接触る前提
  の管理コマンド。
- remote mode の認証は毎回 `--token` フラグか `ZATUDAN_TOKEN` env が要る。
  トークンを設定ファイルに保存する機構は**未実装**。

## 論点1: sqlite-free 化

sqlite-free に倒すなら、依存の輪切りはこうなる:

- `zatu` = 純 HTTP クライアント（`internal/client` のみ、実質 net/http）
- local mode そのもの（`--db` 直結の全コマンド）を捨てる
- `auth token` の発行・管理は DB アクセスが要るのでサーバ側へ移す
  （`zatudan` の admin 系サブコマンド等）
- local CLI→server 間の書き込み通知（changes テーブル + `WatchChanges` で
  SSE に届ける仕組み）は「別プロセスが同一 DB を開く」前提だったので不要化
  — ただし複数サーバや今後の外部書き込みを考えると消すかは別判断

メリット:

- `go install` のビルドが速い、バイナリが小さい、依存が薄い
- CLI の概念モデルが「API クライアント」一本で簡潔になる（gh と同じ形）

コスト・失うもの:

- 「サーバを立てずに DB だけで使う」経路が消える。dogfooding・試し撃ち・
  サーバ未起動時の確認が全部 remote 前提になる
- `auth token` を API 化すると chicken-and-egg（API を呼ぶのに token が要る）が
  発生する。env トークンで bootstrap するか、サーバ側サブコマンドに逃がすかが要る
- e2e テストは local mode の経路も使っているので構成変更が要る

## 論点2: `auth login` 相当のトークン保存

gh モデルに倣うなら:

- `zatu auth login`（または `token save` 等）でトークンを
  `~/.config/zatu/`（`os.UserConfigDir` 準拠）配下に保存
- remote mode の資格情報解決順は `--token` > `ZATUDAN_TOKEN` > 設定ファイル、
  という順が素直（gh は `GH_TOKEN` env が先）
- `auth logout` でファイル削除、`auth status` で現在の解決元の表示

sqlite-free 化とは独立に実装できるが、**remote only にするならほぼ必須** —
token を毎回渡す CLI は日常使いに耐えないため、セットで考える。

## 組み合わせの方向性

- **案A（現状維持 + auth login 追加）**: local/remote の同一インターフェースの
  利便を保ったまま、remote mode の日常利用だけ楽にする。sqlite 依存は残るが
  `go install` 自体は既に動くため実害は薄い。最小の変更。
- **案B（sqlite-free + auth login 追加）**: `zatu` を純 HTTP クライアントに
  絞り、配布・依存を最小化。`auth token` は `zatudan` 側へ移す。「CLI は
  サーバと別物」の当初の設計意図（decisions.md）と一番整合する。変更は大きい。
- 両案とも `auth login`（トークン保存）は共通して必要。

## 残留論点

- `auth token` の置き場所: `zatudan` サブコマンド化（chicken-and-egg 回避で
  無難）vs token 発行 API エンドポイント（web UI からも発行できるが bootstrap
  に env トークンが要る）
- 設定ファイルの配置: `os.UserConfigDir()/zatu/` が標準だが、workspace や
  サーバが複数あるときのプロファイル切替をどうするか（gh は `hosts.yml` で
  ホスト別保存）
- local mode を残す場合の「実用上の頻度」: 現状の2バックエンド統一が実際に
  どれだけ使われているか。dogfooding で local が便利なら案A寄り
- changes テーブル + `WatchChanges` の SSE 通知は local mode 前提のため、
  案B採用時は存続判断が要る（複数プロセス対応として残す手もある）
