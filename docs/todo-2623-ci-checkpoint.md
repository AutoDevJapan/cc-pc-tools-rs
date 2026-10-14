# ToDo2623: 固定コミットのCI確認

対象は親ToDo2488のPR144、固定コミット b9201e3a8bfd52bf90e17e2ae7a788aafcdabf48、tree 966bff6509ca39ba3f7f8a5de08467c4c2e17cc0。正規8workflowだけを有限確認する。ソース編集、手動Cargo、CI再実行、マージ、配布、Discord送信は担当範囲に含めない。

2026-10-14 10:13 JSTの取得では全8runが固定SHA・attempt1と一致。Browserのみworkflow終端success、他7workflowは未終端。全21job中、7success・14未終端。待機を成功として扱わない。

Browserの通常 `gh run view 38494167889 --repo AutoDevJapan/cc-pc-tools-rs --log` はexit0、原全文102,976 bytesを保存。残りは終端後に同じ通常コマンドで取得する。PR head全桁一致とPRIVATE/実owner AutoDevJapanを今回も確認した。

原全文、API metadata、取得exit/bytes/SHA256、固定workflow資料と根拠は、このworktreeの `.ci-work/todo-2623-b920/` に保存する。原全文と私的な記録をGitへ追加しない。

再開時は同じ8runのstatus/conclusion/jobsを読む。完了分の原全文と実検査を照合し、未終端が残る場合は自分のToDo2623のwait-retryへ保存する。別の監視jobを作らず、親2488全体の完了を宣言しない。親noteを更新する場合は最新本文を保持する。自分の状態更新はスケジューラに任せる。