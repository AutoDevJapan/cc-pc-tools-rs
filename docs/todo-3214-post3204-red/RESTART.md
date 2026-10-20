# ToDo3214 の測定と再開

対象は親3103 / Issue289 / PR302の追加受入条件だけ。固定ソースは
`684bfba418f1b293b5ed2672038654ede6323fd6`。親の未コミット成果や後続コミットは使わない。
実装・テストを編集せず、指定された2つのnextestコマンドを各1回、同じ専用targetで順番に実行する。

指定の2コマンドはともに完了（exit100）。7件中5件が意図したassertで失敗、2件は成功。
所有付き一時領域は `ownership.json` に記録する。
`command-1.started.json` / `command-2.started.json` があるコマンドは再実行しない。
完了時の `command-N.result.json` と全文stdout/stderrで終了と到達箇所を確認する。
中断時はPID・ログを照合し、未実行のコマンドだけを続ける。未到達のassertをRed/Greenに数えない。

固定ソースの全1,184 blobは初期・各コマンドの前後・最終の6回すべて一致した。
結果解釈はREADME.mdとresults.jsonを参照する。
残る工程は結果のcommit/push、所有付き一時領域の plan → 別呼出しで通常削除 → finish、
後片付け記録のcommit/pushと実HEADでの正規checkpoint。
削除拒否は再試行せず同じ所有IDをquarantineする。原資料と他担当の領域には触れない。
親の状態・PR・配布は変更しない。独立PR、追加ビルド、子Codex、Discord通知は行わない。
このToDoの状態はスケジューラーが更新する。
