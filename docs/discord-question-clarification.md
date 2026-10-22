# 元質問への説明補足

通常の `discord_ask post --thread <質問> --body-file <固定本文>
--clarification-file <照合JSON>` から、本人が説明を求めた元質問へ説明だけを補う。
既存の通常CLI、Go conversation log、Rust recovery log の順に処理する。
質問、判断範囲、ToDoの待機、旧台帳、共有設定は変更しない。

最初は同じコマンドに `--check-only` を付け、正規GETによる照合だけを行える。
結果は `check_only:true, delivery:not_sent`。送信・送信台帳・照合台帳の更新はしない。
通常入口の既存本文コピーは照合するが、確認時には照合JSONを送信用に固定しない。
実送信時は `--check-only` を外し、同じ本文・質問・照合JSONを使う。
未作成の通常log要求でも、元質問と本人の説明要求を先に検証できる。
実送信では検証成功後だけ原入力と用途を固定し、既存の未送信log予約の再開も維持する。
先行postを失敗させて予約を作る手順は使わない。確認モードはGETだけで、未作成の
予約・本文コピー・bot記録・用途記録・lockファイルを作らない。
既存入力がある場合は完全一致を要求する。[初回の仕様](discord-first-clarification.md)を参照。

照合JSONの用途は `discord-question-clarification-v1`、purpose は
`explain_original_question`。次を固定する。

- 送信側request_id、元question_request_id、元todo_id、thread_id、question_message_id。
- question_fingerprint、question_body_sha256、bot_id、owner_id。
- reply_message_id、reply_body_sha256、reply_timestamp、reply_edited_timestamp（未編集ならnull）。
- body_sha256（通常入口の改行正規化後、送信側が先頭BOMを除いたUTF-8本文）。

全項目は必須で未知項目・重複キーを拒否する。照合JSONは私的な保存先に置く。
`recovery-replies` は既存の `owner_replies` の形式を保ったまま、
`reply_versions[返信ID].edited_timestamp` に編集日時（未編集はnull）を返す。
日時は原表記を保持する。PowerShellは `ConvertFrom-Json -DateKind String` を使う。
公開コード・Issue・PRへ実際の質問・返信・本文・費用・IDを含めない。

## 受入条件

1. 元質問の独立したquestion:0受領記録、元ToDo、原文・fingerprint、送信者、
   設定済み本人、説明要求の原文・版・日時を正規Discord GETで照合する。
   本人の説明要求は説明を求める文であり、別の判断や課金の承認とは扱わない。
2. 初回は原入力を保存する前に照合する。同じID・本文・投稿先の未送信予約も再開する。
   元intentを編集せず、別の用途付き保存記録へ固定要求全体と照合結果を保存する。
   既に送信予約のある要求へ用途を後付けしない。
3. 通知・メンション・添付・新質問・選択肢の再提示は拒否する。未知先・別本人・
   別質問・原文改変・版違いを拒否する。通常log/postの進捗を質問へ許可しない。
4. 同じ要求の再実行はGETで回復する。送信応答の消失は既存のPending/POST/GET記録で
   回復し、POSTを繰り返さない。同じ本人返信を別ID・別本文へ結び直さない。
   検証済みの照合記録と送信予約済みの部分があれば、本人返信の後の編集は
   その部分のGET回収を妨げない。未予約部分を新たに予約する直前にだけ現在の本人返信を再照合する。
   check-onlyも既に固定した部分をGETで回収可能か確認する。残部分の新規送信可否は実送信時に再照合する。
5. 説明投稿後も、そのquestionを通常logの進捗先へ昇格しない。
   原質問・本人返信・元ToDo・判断待ちは保持する。

通常CLIで新方式が無効なら説明送信は失敗する。旧送信へ切り替えない。
Go側の元入力hash、通常ID、送信側IDは照合JSONの追加で変えない。
JSONは通常入口とGoで内容SHAを含む名前へ固定コピーする。未照合のJSONを誤った場合も、
同じrequest_idのまま修正したJSONを検証できる。原要求・本文は変更しない。
元質問と本人返信へ結ぶ権限は、照合後の `recovery-clarifications.json` だけに保存する。
既に照合済みの結び付けを別JSONへ変更することは認めない。
確認結果のsource_body_sha256、body_sha256、binding_sha256で各層の読取値を照合し、
Goでは検証済みの同じバイト列だけを保存・送信する。通常Rust入口では改行を正規化した同じ本文を
固定し、既存body.txtがあるときは確認にもその固定コピーを使う。CRLFの元入力へ戻して再比較しない。
検証後に元ファイルを読み直して差し替えない。
Goは同じ保存済み本文を送信側へ渡す。送信側は各未予約部分の直前まで本人の現在版をGETで照合する。
外部の編集とDiscord POSTを原子的に固定できるとは主張しない。
質問の明示的な終了記録があるときは新しい説明部分を予約しない。Discordの自動アーカイブと
本人による停止・明示終了を混同しない。既存部分のGET回収はその記録を変更しない。

用途記録の完全Request追加後は、旧版のsenderへ単純に戻して回収を続けることはできない。
既存記録の読込互換は維持するが、配布後のrollbackは同形式を読める修復版か記録の保全を伴う
個別の復旧手順で扱う。保存済み記録を旧版に合わせて削除・編集しない。
検査は架空APIで行い、実Discordへの投稿と区別する。実送信は元の配送担当が行う。

通常postのIDは `normal-log-` に、UTF-8のコンパクトJSON配列
`["normal-log-v1","既存の作業への報告",本文SHA,スレッド,false,[]]` のSHA256を続ける。
送信側IDは `conversation-log-` に、通常IDのUTF-8バイト列のSHA256先頭32桁を続ける。
JSONの符号化は既存Rust入口のものを使い、別実装でIDや元予約を作り直さない。
照合JSON・確認モードはこのIDの計算へ追加しない。

## 配備対象の照合

既存の保護判定は通常CLIの実行物 `D:/CC/Tools/discord-ask/discord_ask.exe` を
分類できていなかった。修復repoが名指すこの実行物だけを、既存runnerと同じ
物理パス・通常ファイル・リンク不在・既存の保護除外との照合で分類する。
資格をディレクトリ、送信台帳、固定本文、認証情報、コピー先へ広げない。
元の消失した一覧や共有設定は作成・変更しない。
