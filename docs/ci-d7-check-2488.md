# d7固定CIの確認記録（親2488・独立check2613）

対象はPR144にpushされた固定HEAD `d7d2b30ba0be788f2ad84f560016dd62082ff885`、tree `179e84815ef01e158a022e2fa5e181a49e4d2fc8`。
正規GitHub APIで本人所有のPRIVATEリポジトリ、PR HEAD、commit treeを照合した。

必要な実行はBuild 38478465876、CodeQL 38478465845、Readiness 38478465881、AgentProgress 38478465892、RecordRecovery 38478465833、Channel 38478465840、Browser 38478465853、Bookmark 38478465870。すべてrun_attempt=1、jobのHEAD/attemptも一致。旧sourceの実結果を流用しない。

作業の原証拠はこのworktreeの `.ci-work/d7-2488/` に保持する。APIレスポンス、各呼出のexit/path/bytes/SHA256、取得できた原全文、取得不能のstderrを保持する。秘密値を含み得る原ログ・PRIVATE JSONはGitへ追加しない。

途中状況（2026-10-14 07:37 JST）：8workflowとも未終端。20job中4jobが先行完了。全workspace、doctest、全targets Clippyはまだ未確認。GoのCodeQLのみ同SHAのuploadと解析error空/results_count=0を確認。ただし原ログにGo未処理ファイルの診断があるため、全Goソースを解析済みとは主張しない。

取得制限：3先行jobの通常ログ取得が端末制御文字でexit1。許可オプションは正規ghゲートのunknown flag拒否でexit2。制限を迂回せず、原全文は未確認として保持する。cmd wrapperの最初の3APIは構文エラーだった。wrapperが呼ぶ同一gh_gate.exeをCREATE_NO_WINDOWで起動し、通常APIのexit0を確認した（ゲートの無効化や直gh.exeは使っていない）。

再開時は `.ci-work/d7-2488/check-state.json` と最新pollのAPI、呼出receiptを読み、同じHEAD/run/attemptだけを確認する。有限checkの上限は2026-10-14 08:04:30 JST。未終端が残れば同ToDo2613のwait-retryへ保存し、別監視担当を作らない。修復・Cargo・再実行・merge・配布・Discord送信・GPUはこのcheckの範囲外。自分の状態はスケジューラーが更新する。親2488の全修復完了を主張しない。
