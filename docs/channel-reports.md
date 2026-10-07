# チャンネルごとのDiscord報告

2026-10-07 20:48 JSTのオーナー指示。制作・投稿・解析・数値測定・失敗の活動があるたびに、固定topicへ通知なしで本文を送る。

|対象|固定topic|仕組み|
|---|---|---|
|麻臭の語源動画|麻臭 a.k.a. mash|mash-etymologyのRust投稿CLIと制作指示|
|XB|XB Bittensor|cc-channel-watchが登録する解析指示|
|Motocrab|Motocrab|cc-channel-watchが登録する解析指示|
|健康ナビ|健康ナビ|cc-channel-watchが登録する解析指示|
|参考チャンネル|EtymoMaster|語源動画制作時に新動画を観察した場合。現在は独立した定期巡回なし|
|ADJの毎日Shorts|AutoDevJapan|adj-daily-shortsの制作指示と公開確認|

`discord_ask.exe log --topic "固定topic" --text "短い日本語の報告"`。通常の報告にメンションを付けない。チャンネルtopicでは誤って--notifyを渡しても通知を抑える。旧Motocrab・健康ナビのtopicは保存済みスレッドを再利用し、固定名へ統一する。topic別の登録と作成はOSロックで直列化する。logはthread_idとurlを返す。

制作完了は題名と公開予定日時、投稿完了は題名・URL・実公開日時、失敗は理由と次の対応、解析完了は題名・URL・作成/更新したSkill、数値測定は対象URL・測定日時・値を記載する。`japanese-report-writing`の確認を通す。本文で読める文章をファイル添付しない。オーナーだけが決める問題があるときだけaskを使う。

Skills化依頼スレッドには各専用スレッドのリンク一覧を一度だけ載せ、活動を混ぜない。既存の過去投稿は保持し、以後の送信先を移す。巡回の動画台帳は保持する。古い未起動の解析指示・共有ToDoの保存済み指示もtopicへ移し、登録途中の復旧では現行の指示を書き直す。

新規コードはRust。Pythonと従量課金APIキーは使わない。実行ファイルはテスト・別モデルのMust 0・CI・PRマージ後に差し替え、各topicに現在の活動を一度投稿して確認する。
