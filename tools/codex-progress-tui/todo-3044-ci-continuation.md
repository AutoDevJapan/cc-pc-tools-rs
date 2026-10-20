# ToDo3044: 正式CIとマージ後の確認

2026-10-20 UTC。元の実装・受入・別モデルレビューは繰り返していない。

- PR256: https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/256
- 固定head: `ad93bffda210fbfa1780da87237eab0eff2c8d78`。正規originのbranchとAPIで一致。
- PRの正式Actionsは3 workflowすべて成功。check-runは15 success、Cursor Bugbotのみneutral（利用上限で未実行）。CodeQL自体はsuccess。空のlegacy status一覧のpendingを失敗や成功へ読み替えていない。
- mainの保護設定は404 Branch not protected、適用branch rulesは空。全Actions成功を確認してから通常mergeした。auto/admin/bypassは使用していない。
- 保存済みSonnet 5.5レビュー2応答はMustなし。原応答JSON SHA256は `118A0D3FBD39FD47FA4721464EDA86DBAC3EFFB3DD461580B4A824EAB4665C6E` と一致。範囲と机上確認・実行検査の区別は元の記録を保持。
- 通常mergeは2026-10-20T01:16:27Z成功。`--match-head-commit`に上記headを指定。
- merge: `942cc01894840b8018d072fafb14853e97815ee3`。両親は `842e513191c2ced4364f8382b3a8f9654d01f9d3` と固定head。

配備済み3ファイルは保護判定がclient_resource=false、classification_unverified=falseの範囲で再照合し、保存済み受入と同じSHA256だった。

| ファイル | SHA256 |
| --- | --- |
| codex.exe | 618BF6DCD243463D9058F0CE68B9E43BA2A2B2AC75C715F87AD40D06E453433F |
| codex-code-mode-host.exe | 07F3869F0849CD54B31CEC1F86EC796F004501BD3A215135C1FACC883A2B9988 |
| manifest.json | E9976284148C8CA7BDA2C74D3ACF31207D3466DA16EBFB64DF76961F6AC5922C |

保護判定ツール自身のpathはclient_resource=trueのため内容を読んでいない。保存済み配備receiptは保持。再配備・再起動・GPU・Cargo・新レビュー・子ジョブ・本人の現在会話の操作は行っていない。

## 残る工程

同じmerge SHAのmain pushで次の3件が自動起動した。queued/in_progressであり、まだ成功とはしない。

| Workflow | Run |
| --- | --- |
| CodeQL PC | https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/39352515800 |
| Unattended readiness tools | https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/39352515820 |
| Agent progress tools | https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/39352515945 |

再開時は上記run ID・headを固定してstatus/conclusionを確認する。失敗時だけ原因を調べて直す。成功したPR検査や配備済み受入を繰り返さない。全件成功後、配備ファイルの維持と親の既存条件を確認して3044を完了する。3021の7条件のうち残っていた常設版対応・配備受入は元receiptで確認済み。3020はConsole側の受入とPR253統合を保持し、3024はdone。main確認後に親3021・3020の古い未完記述を追記で更新し、条件がそろえば完了する。現在会話の画面は次の正常な起動・会話再開から常設版になるという制限を残す。

技術待機は同じ3044の有限wait-retryだけを使う。新しい引継ぎToDo・重複監視・親への逆向きwaitは作らない。共有ToDoの自項目の通常状態はスケジューラが更新する。原資料・rollback・検査用homeは保持し、所有外清掃へ広げない。通常完了通知とメンションは送らない。
