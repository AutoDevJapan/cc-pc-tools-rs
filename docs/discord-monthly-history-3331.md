# 指定サーバーの本人投稿を調べる読取入口（ToDo3331、元3327）

## 仕様

元3327の本人指示（2026-10-22）に対応する。対象はguild
`1552944707464597524`、設定済み本人 `730822776147148835`、
`2026-09-22T00:00:00+09:00` 以上、`2026-10-22T16:08:09+09:00` 未満だけ。
DM・別guild・権限追加・新認証・通常受信の変更は行わない。

`monthly-history-inventory --scope <JSON> --output <new.private.json> [--cursor <prior.private.json>]`
は1回の一覧取得だけを行う。scopeはdomain
`discord-monthly-history-inventory-scope-v1`、todo_id=3327、guild_id、owner_id、
since、until、modeを持つ。modeは `channels`、`active`、`public-archived`、
`private-archived`、`joined-private-archived`。archiveにはchannel_idも指定する。
一覧はID/type/parentなどの構造だけを残し、名前・本文を保持しない。
作成時期やthread作成者で除外しない。public/private archiveのbeforeは時刻、
joined-private archiveのbeforeはsnowflake。時刻境界の未確認を累積して記録する。

`monthly-history-read` はdomain `discord-monthly-history-read-scope-v1` と
同じ固定条件、明示channel_ids（1〜10）を指定し、1回100投稿以下を取得する。
text/announcement、音声・stage内のテキスト、public/private/announcement threadを扱う。
音声・stageではCONNECTも照合し、音声への接続は行わない。既存の2forum専用
history-read/history-inventoryの資格条件は変えない。
本人本文だけを私的な原票へ残し、他者の本文・名前は保存しない。
本文の解釈・元担当への配送・全月の照合は元3327が行う。

既存credential loader、既存guard、GETのみ、flush済み非上書きreceiptを使う。
権限をrole/member/overwriteから照合し、必要なprivate membershipも通常GETで確認する。
403/404、壊れた応答、選択本文のsecret、scope/cursor不一致は成功原票を作らず、
cursorを進めない。削除済みと権限不足はAPIの具体的status以上に推測しない。

## 受入条件

1. 固定資格とGET限定、channel/active構造を取得し、名前・他者本文を出力しない。
2. 第三者作成・古いthread、普通のtext、既存権限で読めるprivateを選択できる。
3. public/privateのtimestampとjoined-privateのIDを別々にページングする。
   timestamp境界の同時刻未確認は最終ページにも残る。
4. 所有/guild/window/guard/権限/失敗/不正応答を拒否し、成功cursorを変更しない。
5. 固定候補の別モデルレビュー、影響するGo検査・CI、通常merge、最新runtimeを
   保ったCAS配布、少数の実API取得と原票読返しを終えて元3327へ返す。
   機能の受付・実装だけで月間調査全体を完了扱いにしない。

## 一次資料

- [Guild channels / active threads](https://docs.discord.com/developers/resources/guild#get-guild-channels)
- [Thread archive endpoints](https://docs.discord.com/developers/resources/channel#list-public-archived-threads)
- [History access and pagination](https://docs.discord.com/developers/resources/message#get-channel-messages)
- [Permission overwrites](https://docs.discord.com/developers/topics/permissions#permission-overwrites)

実APIの原票・本文・認証情報はGitへ含めない。実配布・実受入は完了後に記録する。
