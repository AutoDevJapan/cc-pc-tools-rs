# 通常受付が先行した返信の対応付け（ToDo3703）

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/462

## 目的と範囲

同じ本人返信が通常受付で別ToDoへ登録された後でも、現在の正規質問と返信の
対応を検証し、会話の仕様記録と質問終了まで進める。通常受付のToDo付替え、
原取得ファイルの置換、短いa/bからの質問推定、制作の重複起動はしない。

## 入出力

`conversation source-link --request-id ID --message-id ID --actor-todo-id N
--intake-todo-id N --source-sha256 SHA --revision HEAD` を追加する。
SHAは先行した `requests/<message>.json` の生bytes SHA256。
現在のCC_JOB_ID、実ジョブ、保存済み仕様と実行promptにある元ToDo・質問・返信を照合する。
元ToDo、受付ToDo、担当の登録プロジェクトとguardをそれぞれ確認する。

受付ToDoの実担当自身も `actor-todo-id` と `intake-todo-id` に同じ番号を指定できる。
元ToDoは受付・担当とは別でなければならない。同じ番号の場合も、稼働中の実ジョブ、
保存済み仕様、実際の起動prompt、本人停止、現在APIと原sourceの検証をすべて行う。
受付の担当・所有・依存を付け替えず、原sourceと通常受付は保持する。

正規senderの現在API証拠と、質問・decision・本人・thread・返信の最新版を照合する。
先行受付は同じ本人・投稿・本文・編集版・参照・ordinary source版でなければ拒否する。
別質問への結合済み受付、別担当、本人停止、登録範囲の不一致も拒否する。
元の依存待ちは解除しない。状態や所有を変更するコマンドは起動しない。

通常受付はそのまま保持し、`conversation_captures` に専用の会話受付を保存する。
元受付とsourceの指紋、元ToDo/受付ToDo/実担当/実revisionを不変の関連証拠に残す。
会話の取得・配送・source検証・方向仕様記録・終了はその会話受付を使う。
正規のToDo書込、source検証、全返信の反映検証、PATCH後GETの条件は維持する。
関連記録の作成だけを実反映、担当の実読、制作完成とは呼ばない。

## 受入条件

1. 先行した通常受付を保ったまま、同一質問の現在返信だけを正規配送できる。
2. 別質問・本人・編集版・担当・停止・不明guardは拒否し、元記録を変更しない。
3. source保存/状態保存/配送が失敗しても、再実行で重複せず回復できる。
4. 再起動後のsource-verify、direction-record、closeが同じ関連を使う。
5. 元受付/原sourceの全bytesと元ToDoの所有/停止/依存を保持する。
6. 実対象では現在APIを再照合し、方向仕様だけを記録する。制作・画像確認・公開は未完了のまま。
7. 受付担当自身による同一返信の対応付けでも、取得元・担当の検証と再実行・再起動後の照合を保つ。

実対象: request `todo3684-album-release-format-v1`、元ToDo3684、受付ToDo3693、
質問1563590474374320440、返信1563590672487817279。
原投稿: https://discord.com/channels/1552944707464597524/1563590474374320440/1563590672487817279

## テストとの対応

`conversation_intake_link_test.go` の `TestConversationIntakeLink*` が受入1〜5を検証する。
通常受付の全bytes、原取得ファイル、元ToDoの担当と依存、再起動後の検証、
不一致と停止の拒否、source/状態/配送失敗後の再実行、保存証拠の変更検知を含む。
`NativeDirectionAndCloseKeepBothTasks` は既存のToDo CLIを隔離fixtureへ接続し、
非waitの元作業を保ったまま方向仕様記録とGETによる終了を検証する。
実担当のセッション受領と制作の完了を、このテストの結果へ含めない。

Redは変更前に新しい公開CLIの正常系が終了1になることで確認した。
関連回帰で見つかった既存の「配送証拠なし」fixtureは、本文削除後も残る正規の
追加返信証拠を除いていなかった。変更前のコードでも同じ失敗を再現し、
対象返信の証拠だけを1件除くよう修正した。製品の復旧条件は緩めていない。
