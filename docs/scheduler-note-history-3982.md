# schedulerによるToDoメモの置換と履歴（元3982）

## 仕様

共有ロック内で最新のToDoを読み、既存のCAS条件を満たした更新がメモを変えた場合、
更新前の `note` と更新前の `updated` を `{note, updated}` として履歴の末尾に残す。
同じメモへの更新、CAS不成立、メモが元から存在しない場合には履歴を増やさない。
空文字も既存のメモとして扱う。不正な履歴は捨てず、保存前にエラーにする。

対象はrequeueの置換reason、結果のtodo/done/block反映、取消し、旧回数制限の解除、
準備失敗、回答反映を含むschedulerの共通更新経路。状態・担当・依存・再開時刻・
状態文面・共有ロック・CAS条件は変更しない。
`stale_review_spec` は旧メモへの追記を保ち、今回の履歴追加から外す。

保存は3651の共通 `cc_job_policy::todo_store::write` に任せ、scheduler独自の容量判定・
候補選択は持たない。今回のブランチは既存PR458の固定source
`06282d8a8fc879fcdca22fb6312c55b42ddad5f0` を依存として取り込み、その実装を変更しない。
**PR458のレビュー・マージ完了前にはPR557をマージしない。**
共有writerは上限超過時に、大きい既存履歴から同じ `note-history-v1` 形式へ退避する。
別行を退避しても、その行の論理内容とupdatedを変えない。
退避後も `todo.exe history` から従来の順序で読めることを確認する。
segmentの公開と読み返し確認を先に行い、参照を含むsnapshotを後から保存する。
途中の失敗では元のsnapshotを保持し、履歴を削って保存を通さない。
履歴以外のデータ自体が上限を超える場合は従来の上限エラーを保つ。
共有writerが退避した場合だけ、同じ共有ロック内で保存後の行を読み返して返す。
後続の行全体を比べるCASへ、退避前の古い配列を渡さない。

## 受入条件とテスト

`cc-scheduler/tests/support/note_history.rs` で、各経路の旧メモ・旧更新時刻・
履歴順序・同一文面の再試行・CAS・共有ロック・不正履歴・容量境界・退避失敗と
cc-todoのHistoryコマンドによる読取を確認する。
直接の影響範囲はcc-schedulerとcc-todoで、依存側のcc-jobs・cc-shepherd・
localai-web-rsもCIの選択に含まれることを確認する。依存の3651が変更した共通writerの
テストと、cc-job-policyの既存todo_historyテストも接続の確認に利用する。

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
- PR: https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/557 。初回の固定head
  `16941735` は3985の実Opus5.5/maxレビューでMust 0。原返信SHA256は
  `8b291ab1f286cf7df502a3d317ca81ac6f75b4bb74719adb045a8d3c7b72708b`。
  Build speed `40360776046` とCodeQL `40360775544` も成功した。
- 依頼の追補を受け、3982独自の容量退避ループを削除し、既存3651を利用する形へ整理した。
  旧候補のreview/CI/releaseを、新候補の成功や配布済みの根拠には使わない。
- 追補の18回帰テストは成功（run `da737291-aa60-4da9-82af-755c48f234ae`）。
  回答の再開/停止、旧回数制限と回答の同時反映、モデル混雑の終了、レビュー準備3回目、
  追記の除外、退避できない容量超過、保存後のCASを追加で確認した。
- 3985のSuggestion 2/3は容量処理を3651へ集約して対応。Suggestion 4/5とNit 1/2は
  テストと仕様へ反映。Suggestion 1の壊れた参照を飛ばす案は、3651の既存仕様が破損を
  エラーにするため採用せず、元の整合性契約を保持する。
- 残作業: 新候補の関連nextest/doctest/Clippy、追加差分レビュー、3651の完了、
  最終CI・マージ、稼働版の並行変更確認・配布・稼働確認、所有一時領域の清掃。
- 過去の紛失メモの復元、文面や表示の変更、上限や保護設定の変更は行わない。
