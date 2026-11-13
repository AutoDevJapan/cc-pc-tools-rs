# 本人の単独投稿を読んだことを報告する

`discord_ask recovery-log` の `--owner-post-receipt-file <JSON>` は、設定済み本人がrequest forumの本人所有threadへ投稿したメッセージへの、静かな受領報告です。
質問・承認・ToDo待機には結び付けません。リンク先の解析や実作業の完了も表しません。

bindingは次の項目を持ち、未知fieldを拒否します。

```json
{
  "domain": "discord-owner-post-receipt-v1",
  "purpose": "acknowledge_owner_post",
  "request_id": "original-sender-log-request",
  "guild_id": "<configured guild>",
  "request_forum_id": "<configured request forum>",
  "thread_id": "<owner request thread>",
  "bot_id": "<authenticated bot>",
  "owner_id": "<configured owner>",
  "message_id": "<read owner post>",
  "message_body_sha256": "<exact UTF-8 content SHA256>",
  "message_timestamp": "<Discord timestamp>",
  "message_edited_timestamp": null,
  "body_sha256": "<report text SHA256>"
}
```

`message_edited_timestamp`は未編集ならnull、編集済みなら実際の日時です。ID・作成編集版・本文SHAは読み取った実物から得ます。
`--check-only`はGETだけで検証し、bindingや送信phaseを保存しません。既存senderのlog要求を指定し、本文・題名・宛先を再試行で変えません。
本文は「受け取りました。」または「受け取りました。Skills化の依頼として元の管理担当へ伝えました。」（末尾LFは任意）です。後者は元管理担当へ伝達できた場合に限って使います。

送信は元のIntent/Pending/Posted/Verifiedと同じlock・journalを使います。原投稿はPOST直前にも認証済みAPIで再確認します。
reply referenceは原投稿へ固定し、メンションとreply ping、添付を付けず、silentとembed抑止を維持します。
同じ原投稿版への他経路の静かな受領返信もGETで確認します。編集後の版ではmarkerのない過去受領を流用しません。
既存PendingはGETによる回収だけを許し、応答不明を新しいPOST権限へ変えません。Posted/Verifiedは元の版のまま読み返せます。
保存済みbindingは同じ通常post要求の読み返しでも用い、通常ログ一般へのowner threadの解放はしません。

この機能を使う担当が本文を実際に読んだこと・管理先へ伝達したことは、その担当の記録で確認します。機能自体は動画の視聴やSkills反映を認定しません。
