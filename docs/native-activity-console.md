# native実行の操作結果とCC jobを区別する

元3103/3084/3137の「実際に作業しているものだけ」の受入。UUIDや親からの状況文をCC job IDへ変換しない。登録・委譲・監査は製品開発の成果へ加算しない。

既存loopback専用APIに、ローカルの担当実行環境が実際に受け取ったTool結果の限定メタデータを渡す。APIは結果を独自に実証するOS監視器ではない。登録されたsourceだけの部分観測であり、全native数、継続heartbeat、全製品の自動監視が実装済みとは扱わない。過去の1回のsnapshotから継続稼働を作らない。原rollout・会話・コマンド本文・環境変数・秘密を受信/走査しない。

1. `POST /api/agent/native/register`: `session_id`（実際のnative UUID）, `todo_id`（主作業1件）, `role` (`work`/`review`/`audit`/`supervision`), 任意の `business_project`（既存公開カタログの製品）。応答の `generation` を保持する。登録だけでは実作業にならない。製品はコードの編集先repoと区別するが、本文の保護や元の製品identityを解除しない。
2. 実編集・検査・ビルド・調査などのToolが実行された結果だけを `POST /api/agent/native/activity` へ送る。`session_id`, `generation`, `actual_at_ms`（実結果の元時刻）, `event_id`（実call/resultの一意な対応）, `tool` (`exec_command`/`apply_patch`/`cua_repl`/`runtime_tool_metadata`), `kind` (`edit`/`test`/`build`/`inspect`/`publish`/`review`), `executed: true`, 任意の `exit_code`。上位審査で未実行なら `executed: false` とし、これは受理・計数しない。登録やこの受信APIへの書込み自体を作業結果にしない。時刻を後の受信時刻に置き換えない。
3. 正常終了・依存待ち・失敗・取消し・拒否で担当を閉じるときは `POST /api/agent/native/close` へ `session_id`, `generation`, `ended_at_ms`, `outcome` (`success`/`waiting`/`failed`/`cancelled`/`refused`)。即座にliveから除外し、結果は保持する。これはプロセス終了やToDo完了の偽装ではない。別世代の終了を現世代へ適用しない。

30秒以内の実Tool結果だけをliveへ投影する。監査・監督roleは実作業数へ含めない。完了ToDoは含めず、CC jobと同じ元ToDoを二重加算しない。再登録やファイル更新だけでは期限を延ばさない。`GET /api/agent/native` は部分観測・非可逆な実行キー・確認/終了時刻・結果を返し、書込みを行わない。API/画面にはnative UUIDをCC jobとして出さない。

保存は固定の `agent-progress/native-activity.json` のみで、job/ToDo原本には書かない。破損を空の正常結果に置き換えず、取得不能とする。登録変更/再開は世代を進め、古い結果と終了を再利用しない。全結果は元の担当から親へ返し、親が既存製品topicへ永続記録・配送確認する。本人対応不要の結果にメンションを付けない。

仕事量の観測cacheはキー変更に合わせて `workload-observations-v2.json` に分ける。元v1ファイルと過去結果を上書きせず、古いセッションキーを新計数へ混ぜない。
