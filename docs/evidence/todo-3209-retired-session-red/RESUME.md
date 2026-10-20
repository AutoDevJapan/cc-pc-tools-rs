ToDo3209は、元3081/Issue277の有限な追加Red比較検査だけを扱う。
比較source: scheduler/todo-3081-retired-session-red / 565ec9bf6d0145721884978eb73a05b03cf3b623。
04e02ba04e3e6e635e1954295f88ce24e170c3b9との違いは既存APIのテスト2ファイル65行のみ。
製品、fixture、親・他担当の作業場所は変更しない。PR・merge・Green・Clippy・release・配布・実AI要求は対象外。

準備済み: D:/CC/AGENTS.md、作業場所AGENTS.md、2026-10-20モデル方針と必要Skillを読了。
CLAUDE.mdは追跡ファイル中に存在しない。guardはclient_resource=false、GitHub実ownerはAutoDevJapan。
自分のbranchはscheduler/todo-3209-retired-session-check。
自分のjobは20261021-072955-todo-3209。共有ToDoの状態・所有はスケジューラが管理する。
初回branch checkpointは同フォルダのrequest/result/logで確認する。

run_windows.goは割当cargoで指定3コマンドを直列に各1回だけ実行し、完全stdout/stderrと時刻・終了コード・ソース・資源を保存する。
*.started.jsonがあるrunは再実行しない。*.result.jsonで終了を確認し、不在なら対応PIDの生死・ログ・正規jobを照合してから次を決める。
失敗したassert以降を実行済みと数えない。compile/fixture失敗は製品Redに数えない。

所有付き生成物: fd907bbfcd9076980359c0a5467c51cb（終了後に退避済み）。
絶対パスはC:/Users/makhmeto/AppData/Local/Temp/codex-owned-fd907bbfcd9076980359c0a5467c51cb。
生成物は使用プロセスの終了後にsafe-delete plan、返された通常削除を別呼び出し、成功後finishで片付ける。
拒否時は同じ削除を繰り返さず規定のquarantineを使う。証拠ログは作業場所へ保持する。

実行済み: 3コマンドを各1回、終了コード100/100/0。製品Red2件、陽性対照4件、fixture失敗1件、compile失敗0件。
取消ケースのcommitted_at="saved"は不正な時刻で、旧会話不使用のassertまで未到達。直さず、再実行しない。
詳細はresult.md/results.jsonと完全stdout/stderrを読む。3回とも比較sourceから製品/fixtureを変えていない。

原ログ/証拠記録はca3ff204eac546f98d48ee3e47728ff85eebb2dfへcommit/push済み。
所有生成物2,266,158,891バイト/2,262項目はplanのサイズ指定でquarantine済み。元パス不在・退避先存在を確認した。通常削除による容量回収とはしない。
最終の証拠commitは現在のgit HEADとoriginの同ブランチから確認できる。製品差分は追加テスト65行を含む比較sourceのまま。

終了直前の記録: 共有ToDo3081/3209へ証拠を追記し、状態・所有は変更せず、有限checkの実測進捗を100とする。
最終checkpointはcommit後の実HEADを使うため、final-checkpoint-*（このフォルダだけgitignore）とスケジューラの永続receiptへ保存する。
再開時はそのresult/receiptと実branch/headを照合する。未保存なら正規checkpointを実行する。3つのCargoコマンドは再実行しない。
比較検査の完了は製品全体の完了を意味しない。Discord通知/本人PC操作は行わない。
