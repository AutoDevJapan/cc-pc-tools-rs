---
name: channel-activity-reporting
description: YouTubeなどのチャンネルで制作・投稿・解析・数値測定が起きたとき、Discordの固定topicに通知なしで報告する。
---

# チャンネルごとの活動を報告する

オーナーの2026-10-07 20:48 JST指示「各種報告はチャンネルごとに専用の投稿を作って、アクティビティがあるごとに報告して」。新しく書くコードはRust。Pythonと従量課金APIキーは使わない。

|対象|固定topic|
|---|---|
|麻臭の語源動画|麻臭 a.k.a. mash|
|XB|XB Bittensor|
|Motocrab|Motocrab|
|健康ナビ|健康ナビ|
|参考チャンネルの観察|EtymoMaster|
|ADJの毎日Shorts|AutoDevJapan|

`D:/CC/Tools/discord-ask/discord_ask.exe log --topic "固定topic" --text "短い日本語の本文"`で同じスレッドへ積み上げる。topicは回・日付・動画ごとに変えない。未知のチャンネルは確認したチャンネル名を固定topicにする。logが返すurl、thread_id、message_idを記録する。保存済みの対応はD:/CC/Tools/discord-ask/log_threads.jsonにある。通常の報告に--notifyやメンションを付けない。

報告前に[japanese-report-writing](../japanese-report-writing/SKILL.md)を確認する。

- 制作完了: 題名と公開予定日時。オーナーに見せる動画・画像は同じtopicへ直接添付する。
- 投稿完了: 実URL・題名・実公開日時。予約は公開予定と書く。実公開日時が不明なら未確認と書き、確認日時を分ける。
- 投稿失敗: 理由と次の対応。処理待ち・受領不明・公開制限を公開完了としない。
- 新動画の解析: 題名・URL・作成/更新したSkill名と用途。未解析は未解析と書く。
- 数値の測定: 測定日時・対象URL・値・取得範囲。累計と日別増加を混同しない。巡回の測定報告はJSTで1日1回までとし、送れなかった古い測定は最新の1件へまとめる。

文章はDiscord本文に書き、文章ファイルを添付しない。2,000字を超える場合は分ける。mashの投稿結果はRustのmash-uploaderが自動報告するため、同じ結果を手で重ねて送らない。未送信は`mash-uploader.exe report --publication <台帳>`で再試行する。

Skills化依頼スレッド1554792868974039150には専用スレッドへのリンク一覧を一度だけ載せる。以後の活動をそこへ混ぜない。既存の過去投稿と解析台帳は保持する。EtymoMasterは現在独立した定期巡回がなく、実際に観察したときだけ報告する。

失敗が続いても、担当が直せる技術的な問題は自分で直す。お金・公開・権利・顧客との約束・製品の方向などオーナーだけが決める問題がある場合だけ、[discord-owner-ask](../discord-owner-ask/SKILL.md)に従ってaskで聞く。
