# 質問台帳が消失した間の新しい本人指定の投稿

元の質問台帳 `state.json` と `question-recovery-required.json` はそのまま保持する。
古い質問の再送、本人回答の読み替え、担当の起動を再開しない。
今回本人が直接指定した新しい通知・新しい具体判断だけを独立した領域で扱う。
これは失われた台帳の復元成功や、停止している全巡回の復旧ではない。

- `discord_ask recovery-log --request-id ... --title ... --body-file ... [--notify]`
- `discord_ask recovery-ask --request-id ... --todo-id ... --decision-file ... --body-file ... --origin-thread ...`
- `discord_ask recovery-replies --thread ... [--request-id ... --todo-id ... --message ...] --read-only`

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

HTTPクライアントはDiscord公式の `DiscordBot (URL, version)` 形式で正確な識別情報を送る。
アーカイブの開始位置はISO8601を保持し、タイムゾーンの `+` もURLのクエリー値としてエンコードする。
実測の40333と400を、返信0件・送信成功・台帳復旧と読み替えない。

公式形式: https://docs.discord.com/developers/reference#user-agent
