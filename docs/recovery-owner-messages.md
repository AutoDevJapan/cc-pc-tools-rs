# 質問台帳が消失した間の新しい本人指定の投稿

元の質問台帳 `state.json` と `question-recovery-required.json` はそのまま保持する。
古い質問の再送、本人回答の読み替え、担当の起動を再開しない。
今回本人が直接指定した新しい通知・新しい具体判断だけを独立した領域で扱う。
これは失われた台帳の復元成功や、停止している全巡回の復旧ではない。

- `discord_ask recovery-log --request-id ... --title ... --body-file ... [--notify]`
- `discord_ask recovery-ask --request-id ... --todo-id ... --decision-file ... --body-file ... --origin-thread ...`
- `discord_ask recovery-repair-origin --request-id ... --todo-id ... --expected-origin-thread ... --owner-reply-message ... --progress-message ...`
- `discord_ask recovery-resume-ask --request-id ... --todo-id ...`
- `discord_ask recovery-replies --thread ... [--request-id ... --todo-id ... --message ...] --read-only`
- `discord_ask recovery-close --manifest <JSON>`

`recovery-repair-origin` repairs only an exact ask intent that has its saved bot
identity and no send reservation, POST receipt, verified effect, or completion.
It requires the unchanged request ID, original ToDo and original origin, fetches
the exact owner reply by Discord GET, and selects a progress starter already
verified by this ledger for that same ToDo and origin thread. The original intent and fingerprint remain append-only;
the repair event records the old origin and evidence IDs, and retries apply the
repaired origin without changing the question body or decision. A different
request, owner, ToDo, origin, target, or body stops before POST. A Pending or
posted request is never rebound because its delivery result may be uncertain.
After a repair, `recovery-resume-ask` runs the unchanged saved intent through the
same reserve, POST and GET-verification path. It takes no body or decision file,
so a retry cannot silently substitute new question text or scope. It requires
the exact original request ID, ToDo and a recorded origin repair.

投稿先は既存設定のサーバー・フォーラム・本人IDに限定する。
新しい作業投稿を元投稿として使う場合は、過去の投稿を復元したものではないことを本文で明記する。
新しい質問にだけ本人を1回メンションし、別の進捗投稿を通知なしで作り、元投稿と質問のURLを紐づける。
長い一覧は進捗投稿へ分割する。質問投稿へ後続の質問や進捗を積まない。

独立した `recovery-messages.json` のイベントを専用ロックと原子的な置換で保存する。
認証値はHTTPヘッダーの中だけで扱い、本文や台帳へ出さない。
Intent は取得処理より前に、Pending は取得成功後・投稿直前に保存する。
取得が失敗しただけなら同じ依頼番号で再開できる。
投稿結果が不明なPendingは、本人への再送をせず、同一本文・同一Bot・同一段階の履歴を取得して確かめる。
保存済みの成功結果も改めて取得して、本文・本人メンション・通知フラグ・投稿先を確認する。

返信は新しく確認した質問のスレッドだけ読む。本人IDと質問以降の時刻・メッセージIDを照合し、元ToDo・質問原文・判断範囲と一緒に返す。
`recovery-replies` は読み取り専用であり、担当への配送や回答の実反映を完了扱いにしない。
担当は返信を元の依頼に反映し、反映した内容と成果を別に記録する。

反映済みの新方式の質問1件は、`recovery-close` で終了する。
この操作は、元ToDoの残作業の完了や契約変更を意味しない。
元の質問台帳や復旧保留票を読み書きせず、既存のBot認証を内部で使用する。

manifestは、確認済みの質問と実反映記録を固定するJSONである。
以下の項目をすべて指定し、不明な値や推測した成功を入れない。

| 項目 | 確認元 |
|---|---|
| request_id / thread_id / question_message_id | 新方式の質問の正規受付。スレッドと先頭メッセージは同じID |
| origin_todo_id | 元の質問のToDo |
| todo_id / job_id / revision | 実操作を反映した既存受付と担当、40桁のソース版 |
| message_id / author_id | 設定済み本人の正規API返信 |
| question_fingerprint | recovery-messages.jsonの元質問Intentのfingerprint |
| reply_sha256 | 保存済みrecovery-repliesの該当owner_replies要素の正規化JSONのSHA256 |
| receipt_sha256 | 既存unattended/applied/<message_id>.jsonの生バイトのSHA256 |
| verification_sha256 | その反映記録のverificationオブジェクトの正規化JSONのSHA256 |
| completion | その反映記録の実反映済み内容を示す文。残作業の記述も保持 |

正規化JSONでは、オブジェクトのキーを再帰的に並べ、配列順を保持する。
UTF-8の圧縮JSONを使用する。本文・メール・金額はmanifestや終了結果へ転記しない。
manifestは新しい承認ではなく、担当が実操作と読み返しを終えた既存記録の照合入力である。
CLIは、反映記録の取得元、全対象ID、担当、版、実反映記述、検証記録と反映日時を照合する。
検証記録にはexecution・origin_saved・identityの確認結果を必須とする。
設定サーバー・フォーラム・Bot・本人と、正規GETした元質問本文・世代・範囲・返信も照合する。
返信が編集されていたり、ほかの本人返信があったりする場合はPATCHせず、返信IDを独立記録へ残す。
この拒否はPATCH前に確認できた返信に適用する。PATCH中・後の返信は成功扱いにせず保留を記録し、勝手に再開しない。
親担当は`recovery-replies`で原文と範囲を再確認する。勝手に別の回答を反映済み扱いにしない。

専用recovery-close.lockと質問領域のロックの下で、recovery-close.jsonへ原子的に保存する。
PATCH直前にpatch_pendingを保存し、対象スレッドだけに`{"archived":true}`を送る。
成功はGETのarchived=trueと再照合で確認する。
終了結果のoutcomeは、通常のarchived、PATCH結果消失後のrecovered_after_patch、既に閉じたalready_archivedを区別する。
GETが失敗した場合は同一manifestで再試行し、再送やメンションをしない。
GETが未終了を返す場合も成功としない。検証済みの質問が再び開かれた場合は自動で閉じ直さない。
未反映の返信の記録や終了結果には原文を入れず、元ToDoを完了にせず、担当を起動しない。
返信保留を記録してもPATCHの試行と検証済みの結果を保持する。reusedは同じ受付の再実行を示す。
1つのmanifestは本人返信1件だけを覆う。本人返信が複数ある質問は、このCLIで反映済みと推定して閉じない。
同じスレッドに記録したmanifestは変更できない。返信保留後も再bindingは許可せず、親が原文と範囲を確認する。
recovered_after_patchは「PATCHを試みた後、GETでアーカイブ済みを確認した」という意味であり、PATCHだけを終了原因と断定しない。
中断時のrecovery-close.*.tmpは旧台帳の代わりにせず、当該コマンドの終了と所有を確認して片付ける。

既存実行版が保存したorigin_correctedも読み取る。元の未送信Intentから投稿元だけを変え、
元質問に紐づく同一Botの通知なし進捗へ移した記録だけを認める。本文・判断範囲の変更やPOST予約後の修正は拒否する。
この互換読取のために台帳を書き換えたり、未知のイベントを捨てたりしない。

HTTPクライアントはDiscord公式の `DiscordBot (URL, version)` 形式で正確な識別情報を送る。
アーカイブの開始位置はISO8601を保持し、タイムゾーンの `+` もURLのクエリー値としてエンコードする。
実測の40333と400を、返信0件・送信成功・台帳復旧と読み替えない。

公式形式: https://docs.discord.com/developers/reference#user-agent
