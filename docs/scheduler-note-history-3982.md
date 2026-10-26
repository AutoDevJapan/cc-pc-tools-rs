# schedulerによるToDoメモの置換と履歴（元3982）

## 仕様

共有ロック内で最新のToDoを読み、既存のCAS条件を満たした更新がメモを変えた場合、
更新前の `note` と更新前の `updated` を `{note, updated}` として履歴の末尾に残す。
同じメモへの更新、CAS不成立、メモが元から存在しない場合には履歴を増やさない。
空文字も既存のメモとして扱う。不正な履歴は捨てず、保存前にエラーにする。

対象はrequeueの全reason、結果のtodo/done/block反映、取消し、旧回数制限の解除、
準備失敗、回答反映を含むschedulerの共通更新経路。状態・担当・依存・再開時刻・
状態文面・共有ロック・CAS条件は変更しない。

履歴追加後の実際の保存形式が64 MiBを超える場合だけ、既存の
`cc_job_policy::todo_history::archive` で不変segmentへ退避する。
退避後も `todo.exe history` から従来の順序で読めることを確認する。
segmentの公開と読み返し確認を先に行い、参照を含むsnapshotを後から保存する。
途中の失敗では元のsnapshotを保持し、履歴を削って保存を通さない。
履歴以外のデータ自体が上限を超える場合は従来の上限エラーを保つ。

## 受入条件とテスト

`cc-scheduler/tests/support/note_history.rs` で、各経路の旧メモ・旧更新時刻・
履歴順序・同一文面の再試行・CAS・共有ロック・不正履歴・容量境界・退避失敗と
cc-todoのHistoryコマンドによる読取を確認する。
影響範囲はcc-schedulerとcc-todo。既存のarchive実装は変更せず、cc-job-policyの
todo_historyテストも互換性の確認に利用する。

## 作業記録と再開

- Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/555 。
- 開始時のmain/HEAD: `234d04dcacd313755375534d4c2fa9d574f58de2`。
- 作業ブランチ: `scheduler/todo-3982-scheduler-todo`。ブランチは変更していない。
- 元ToDo3982と割当cargoを保持し、自分の状態はスケジューラーが管理する。
- 専用一時領域の所有ID: `9724e0353c7fc771c07d04a94d709efa`。
- Red: 変更前の関連8テストで履歴欠落・上限超過・不正履歴の保存を再現した。
  準備失敗のfixtureは、戻り値のエラーと旧保留文面を実装の既存条件に合わせて修正した。
- Green: 追加3ケースを含むcc-scheduler/cc-todoのnextestは606件すべて成功
  （run `76055ee2-ad5e-446a-ba87-2c3874396690`、223.492秒）。
- 既存cc-job-policyのtodo_historyテスト5件と、cc-scheduler/cc-todoのdoctestは成功
  （doctestは対象例0件）。Clippyが指摘した等価な降順ソートの書式を修正した。
- PR: https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/557 。別モデルレビューは
  共有ToDo3985へ登録し、通常の割当を待っている。子ジョブは直接起動していない。
- 残作業: Red/Green、関連nextest/doctest/Clippy、別モデルの固定差分レビュー、
  PR/CI/マージ、並行変更を照合した実行物の配布と稼働確認、所有一時領域の清掃。
- 過去の紛失メモの復元、文面や表示の変更、上限や保護設定の変更は行わない。
