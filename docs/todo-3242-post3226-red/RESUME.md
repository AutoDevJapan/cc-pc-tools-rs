# ToDo3242の再開メモ

測定は完了した。固定source a1aafe73c9e0564a79dc11d67de4fea362ca6020で指定コマンドを1回実行し、2 FAIL / 2 PASS、exit100。全1,186 sourceファイルのGit生blob一致と前後不変を確認。全文stdout/stderr、実行情報、各SHAを自分のブランチへcommit/push済み。**Cargoを再実行しない。** 親の実装・配布の完了を意味しない。

残作業は後片付けだけ。所有ID bcd13dfcd2f6a3494a968c067cd0c48f、元の場所はpreflight.json。planは3,298,968,579 byteのためquarantine(size)を指定。quarantineは移動前にQUARANTINE_CROSS_DRIVEで終了した。同Dの予定退避先もguardがclassification_unverified=true / client_resource=trueを返したため、作成・参照・移動はしていない。元の所有ルートとsource/targetを保持し、finishは未実行。cleanup-status.jsonに根拠を保存。

通常の整理経路で、この所有物だけを、保護設定やcaller許可を広げず同Dへ退避できることを確認する。既に判明したcross-driveの呼び直し、guard回避、分割削除、無関係なsource/設定の閲覧はしない。移動が可能になったら、所有・絶対境界・全links・停止・全ファイルの前後同一性・source_absentを確認してからfinishする。

自分の共有ToDo状態はスケジューラが更新する。親のcode・PR・ToDo状態は変更しない。独立PR・他のCargo検査・子Codex・受領用helper・Discord通知は追加しない。測定結果は通常CLIの最終応答で親が受領する。最後にToDo3242、現在のCC_JOB_ID、実HEADでcheckpoint-workspaceを保存する。
