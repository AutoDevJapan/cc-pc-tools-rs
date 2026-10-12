# 新しい1質問だけの返信監視

旧質問台帳が消失している間も、明示的に登録した新しい質問1件だけを既存PCアプリの120秒処理で読む。旧`state.json`、復旧要求ファイル、旧担当起動、停止中の定期タスクには触れない。登録がなければ新しい処理は何もしない。

`discord_ask.exe recovery-followup register --config <JSON>`に、request_id、thread、question_message、todo_id、work_todo、parent_lock、worker_cwd、parent_messagesを渡す。元の質問世代と部署判断の元作業は独立した送信台帳と正規Discord APIで照合する。parent_messagesは元担当が既に反映を引き受けた返信IDだけであり、APIに存在する本人返信以外を登録できない。登録は配送・反映の完了ではない。

既存のOperations監視が`recovery-followup tick`を有限時間で実行する。本人ID、返信ID、質問世代、元の本文と対象範囲、投稿日時、取得日時、取得元を`recovery-followup.json`に保持する。既存送信台帳は読み取り専用で利用する。`recovery-followup-resident.json`は担当の手動確認と別に、常駐処理自身が観測日時と結果を保存する。

取得した返信を新しい専用ロックと永続予約で保存してから配送する。親のロックや同じ元作業の担当が稼働中なら、返信を保存したステアリングとして保持し、別の適用担当を起動しない。元担当が引き受け済みの返信は、親ロックの解除だけを理由に再起動しない。必要な起動は既存の担当起動CLIの同じ`--launch-key`で行う。応答消失後も予約キーを変えない。元ToDoにはこの質問の配送証拠だけを追加し、元本文と対象外の項目は保持する。

実反映した担当は`recovery-followup applied --message <返信ID> --result-file <JSON>`で報告する。結果にはhandler（起動されたジョブID、元担当の場合はparent）、revision、verification、summary、applied_at（RFC3339）が必要。同じ返信への結果を変えて上書きしない。結果を永続保存し、別の通知なし進捗投稿への送信をGETで検証してから判断ToDoを完了する。未反映の別返信があれば判断ToDoを完了しない。通知失敗や再起動後も同じ結果IDを使い、受領・起動だけを実反映としない。

`recovery-followup inspect`は配送の段階と定期観測数だけを読み取りで返す。過去の質問を列挙せず、APIキーを用いるモデル呼び出しも行わない。古い台帳の原本を復旧したという意味ではない。

同じ範囲の稼働中workerに対する追加返信は、そのジョブの専用`recovery-followup-<予約キー>.json`へ保存する。担当は作業の区切りで`recovery-followup claim --message <返信ID> --handler <ジョブID>`を実行し、原文と範囲を受領する。保存・受領・実反映を別々に記録する。親への配送は独立台帳の親向け受信ファイルへ保存し、元担当が引き受け済みの返信は明示して保持する。

標準の担当起動には元の`--todo-id`を必ず付ける。cc-jobsは共有queue→ToDoロックの中で同じToDoの既存担当を再確認してから起動予約を保存するため、別の標準担当起動との競合も排他する。親の既存activeファイルはその親の作業が終わるまで保持し、作業完了後の新担当も標準起動経路を使う。結果通知後、判断ToDoを完了する直前にも正規APIで有限照合し、その間に届いた返信を未反映の予約として残す。
