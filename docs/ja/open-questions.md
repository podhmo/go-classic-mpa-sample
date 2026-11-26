# Open questions

`docs/sketch/ja/owner-improvements.md`（PR #113）から派生した、実装前に判断が必要な論点を記録する。
1項目につき、止まっている作業・選択肢・決める人を書く。回答が得られたら TODO.md に移し、ここでは解決済みとして残すか整理する。

## PR merge の直列化と `prservice` をどこまで先に入れるか

止まっている作業: bugs.md #36（並行 merge）、`owner-improvements.md` §1.1・§1.5・§3.1。

選択肢:

- A: 先に `internal/prservice` へ create/close/merge/comment を集約し、その1箇所に `merging_at` CAS・deadline・復旧を入れる。安全だが差分が大きい。
- B: まず api/cli/web の3箇所に最小の CAS を入れ、service 抽出は後で行う。早いが一時的に重複が増える。
- C: service 抽出と CAS を1つの PR にまとめる。レビューは重いが中間状態を避けられる。

判断: podhmo。予約の lease/復旧（プロセス死亡時に git 側の適用有無をどう照合するか）もあわせて決めたい。

## PR files 画面の再設計の範囲をどこまでにするか

止まっている作業: `owner-improvements.md` §2.5・§4.4・§5.3。

選択肢:

- A: まず不正マークアップと N+1 git 起動だけ直す。行コメント form は1個共有にし、JS で移動する。
- B: JS を増やさず、`?line=` 付き GET でサーバー側に form を描画する。MPA らしいが遷移が増える。
- C: テーブル化・インラインコメント・ファイル折り畳み・word-diff までまとめて再設計する。

判断: podhmo。C にする場合は storybook での検証観点も決める。

## FTS5 と検索クエリ構文の仕様

止まっている作業: `owner-improvements.md` §2.2・§4.2。

選択肢:

- A: `trigram` tokenizer を使い、現在の substring 検索に近い意味論を保つ。3文字未満は従来の `instr` に fallback。
- B: `unicode61` を使い、日本語は限定的と割り切る。実装は単純。
- C: FTS より先に `is:open label:bug author:demo` のパーサだけ入れる。

判断: podhmo。AND/OR の意味論、web/CLI/API で同じ文法にするかも決める。

## 楽観的排他の API 互換性と衝突 UX

止まっている作業: `owner-improvements.md` §4.1。

選択肢:

- A: すべての更新 API で `If-Match` を必須にする。安全だが既存 CLI/API クライアントを壊す。
- B: `If-Match` は任意。指定時だけ `version` を照合し、web フォームは常に hidden version を送る。
- C: API は version ベース、web は updated_at 確認だけに留める。

判断: podhmo。不一致時に差分をどこまで表示するか（本文だけか、title/labels/status も含めるか）も決める。

## API 拡張の優先順位と互換性ポリシー

止まっている作業: `owner-improvements.md` §4.3。

論点:

- `Link` ヘッダだけ先に入れるか、最初から keyset cursor にするか。
- コメント一覧/単体取得を resource ごとに追加するか。
- API token の scope/expiration を入れるか。token なし起動後の API mount をどうするか。

判断: podhmo。外部クライアントを壊してよいバージョン境界がないため、原則 additive に進めるかも確認したい。

## 書く体験（Preview・下書き・参照リンク）の範囲

止まっている作業: `owner-improvements.md` §5.1。

選択肢:

- A: `POST /preview` + Write/Preview タブだけ先に入れる。
- B: 下書きの localStorage 自動保存だけ先に入れる。
- C: `#12` を issue リンク化する server-side markdown 拡張だけ先に入れる。
- D: 3つを1つの「writing experience」PR にまとめる。

判断: podhmo。下書き保存の保持期間・手動クリア導線を入れるかも決める。

## ナビゲーション・キーボード操作の方針

止まっている作業: `owner-improvements.md` §5.2。

選択肢:

- モバイル nav は `<details>` メニュー化するか、横スクロールにするか。
- `/`、`c`、`j`/`k`、`?` の shortcut を入れるか。入れるなら無効化設定をどこに置くか。
- 最後に見た workspace を cookie に覚える仕様は、複数 workspace を使う人の意図しない切替を起こさないか。

判断: podhmo。storybook の `?w=360` と keyboard-only 操作で確認する。

## 大規模リファクタの境界と順序

止まっている作業: `owner-improvements.md` §3.2〜§3.6。

論点:

- `store.write` helper、ScopedStore への移行、interface 分割、モデレーション controller 化のどれを先に行うか。
- 巨大ファイル分割（i18n/web pulls/story_pages）をリファクタと同じ PR に入れるか、機械的移動だけ別 PR にするか。
- 重複テンプレート共通化で markup の細かい差分をどこまで許容するか。

判断: podhmo。レビューしやすさ優先なら、機械的移動→小さな helper→interface 変更の順がよい。

## 一覧のライブ化は passive banner に限定するか

止まっている作業: `owner-improvements.md` §4.5。

選択肢:

- A: 「新しい項目が N 件あります — 再読み込み」だけ出す。MPA の原則と両立しやすい。
- B: 一覧行の自動差し替えまで行う。便利だが表示位置やフィルタ状態の扱いが難しい。
- C: 一覧のライブ化は行わない。

判断: podhmo。

## 一括操作と空状態の導線をどこまで入れるか

止まっている作業: `owner-improvements.md` §5.4。

選択肢:

- A: 空状態の「作成する」「フィルタを解除」リンクだけ入れる。
- B: 一覧で close/label の一括操作まで入れる。
- C: 両方入れる。

判断: podhmo。一括操作は destructive action の確認 UX を決める必要がある。
