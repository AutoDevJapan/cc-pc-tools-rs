# 元質問への説明補足

通常の `discord_ask post --thread <質問> --body-file <固定本文>
--clarification-file <照合JSON>` から、本人が説明を求めた元質問へ説明だけを補う。
既存の通常CLI、Go conversation log、Rust recovery log の順に処理する。
質問、判断範囲、ToDoの待機、旧台帳、共有設定は変更しない。

照合JSONの用途は `discord-question-clarification-v1`、purpose は
`explain_original_question`。次を固定する。

- 送信側request_id、元question_request_id、元todo_id、thread_id、question_message_id。
- question_fingerprint、question_body_sha256、bot_id、owner_id。
- reply_message_id、reply_body_sha256、reply_timestamp、reply_edited_timestamp（未編集ならnull）。
- body_sha256（通常入口の改行正規化後、送信側が先頭BOMを除いたUTF-8本文）。

全項目は必須で未知項目・重複キーを拒否する。照合JSONは私的な保存先に置く。
公開コード・Issue・PRへ実際の質問・返信・本文・費用・IDを含めない。

## 受入条件

1. 元質問の独立したquestion:0受領記録、元ToDo、原文・fingerprint、送信者、
   設定済み本人、説明要求の原文・版・日時を正規Discord GETで照合する。
   本人の説明要求は説明を求める文であり、別の判断や課金の承認とは扱わない。
2. 元の未送信log要求を同じID・本文・投稿先のまま再開する。元intentを編集せず、
   別の用途付き保存記録へ照合結果を追記する。既に送信予約のある要求へ後付けしない。
3. 通知・メンション・添付・新質問・選択肢の再提示は拒否する。未知先・別本人・
   別質問・原文改変・版違いを拒否する。通常log/postの進捗を質問へ許可しない。
4. 同じ要求の再実行はGETで回復する。送信応答の消失は既存のPending/POST/GET記録で
   回復し、POSTを繰り返さない。同じ本人返信を別ID・別本文へ結び直さない。
5. 説明投稿後も、そのquestionを通常logの進捗先へ昇格しない。
   原質問・本人返信・元ToDo・判断待ちは保持する。

通常CLIで新方式が無効なら説明送信は失敗する。旧送信へ切り替えない。
Go側の元入力hash、通常ID、送信側IDは照合JSONの追加で変えない。
JSONは通常入口で固定コピーし、Goは同じ保存済み本文を送信側へ渡す。
検査は架空APIで行い、実Discordへの投稿と区別する。実送信は元の配送担当が行う。

## 配備対象の照合

既存の保護判定は通常CLIの実行物 `D:/CC/Tools/discord-ask/discord_ask.exe` を
分類できていなかった。修復repoが名指すこの実行物だけを、既存runnerと同じ
物理パス・通常ファイル・リンク不在・既存の保護除外との照合で分類する。
資格をディレクトリ、送信台帳、固定本文、認証情報、コピー先へ広げない。
元の消失した一覧や共有設定は作成・変更しない。
