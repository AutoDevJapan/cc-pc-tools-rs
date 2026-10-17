# 既存の1分受付で製品の進捗を報告する

このGo変更は、既存の `CC-DiscordAskTick` と同じrunnerで動く。
新しい常駐処理・定期タスク・OAuth権限・有料APIは追加しない。
コミット・配布・実Discordへの最初の投稿は統合担当が行う。

## 有限の報告対象を設定する

既存の `config.json` に、明示した元ToDoだけの `product_reports` を追加する。
設定がなければ製品報告を開始しない。最初は1〜2件で通知なしの実結果を
確認し、その後に必要な対象を追加する。受付の `project` は
`cc-pc-tools-rs` のままにする。

定型表示する有限対象は2639（「受託案件」だけ）、2640（万世記）、2641（GoDD MCP α）、
2642（GoDD Press）、1626（PhosGaea）、2626（司令塔）の6件。
これに加え、独立受付の `State.Requests` に正確な本人投稿と元ToDoの対応が
ある項目だけを `state_request` で指定できる。こちらは「依頼の進捗」と表示する。
任意の番号を許可しない。設定は最大16件で、最初の実受入は2705と2626の2件。
題名・表示名・本文をToDoや担当の自由記述から作らない。

正規Discord投稿を直接指定する既存方式では、以下の形を使う。
`kind` は省略または `discord_api_v10`。この方式だけが元Discord本文ハッシュを要する。

```json
{
  "todo_id": 2641,
  "project": "<元ToDoのprojectと完全一致する値>",
  "owner_source": {
    "thread_id": "<元の本人投稿のthread ID>",
    "message_id": "<元の本人投稿のmessage ID>",
    "created_at": "<APIのtimestampの文字列>",
    "body_sha256": "<APIのcontentをUTF-8でハッシュした小文字64桁>"
  }
}
```

これは配列要素の書式であり、そのまま有効にする設定ではない。
Discord方式では元の本人投稿をAPIで読み直し、設定済みのサーバー・本人ID・Bot false・
スレッド・メッセージ・作成日時・本文ハッシュを照合する。
本文を進捗報告へ転記しない。設定の構造に不備がある場合は製品報告を保留する。
対象1件の照合失敗はその製品だけを保留する。通常の受付と、独立して検証できる
既存の進捗投稿は継続する。

### 直接チャットと受付済み依頼の初回設定

`direct_owner_chat` は正直な私的受領記録であり、Discord APIの記録ではない。
`provenance` は `codex_current_user_message`、`observed_at` は受領を確認した実時刻。
受領原文は私的設定の `received_text` にだけ保持し、そのUTF-8 SHA-256を記録する。
Discordの本人投稿ID・作者を作らず、`discord_url_exists` はfalseとする。
旧承認・製品実行・費用・別質問への権限には利用しない。

初回2件の正確な設定パッチは、本文を表示せずに次へ作成してある。
`D:/CC/Work/discord-request-audit-20261017-Native/initial-product-reports.patch.private.json`。
既存configの `product_reports` だけをこの配列にする。原文をソースやログへコピーしない。

構造は次のとおり。直接チャットの原文とハッシュには、上記パッチに入っている
実受領記録の値を使用する。本人に同じ内容をDiscordへ再投稿させない。

```json
{
  "product_reports": [
    {
      "todo_id": 2705,
      "project": "cc-pc-tools-rs",
      "owner_source": {
        "kind": "state_request",
        "message_id": "1560534965228404786",
        "purpose": "status_question",
        "answer_message_id": "1560865966252888147",
        "answer_receipt_file": "D:/CC/Work/discord-request-status-2705-20261017.private.json",
        "answer_receipt_sha256": "d60185904cfbdcce43e208af5845c615e8e5b0e7ff7b8e4696debd0ff52a342e"
      }
    },
    {
      "todo_id": 2626,
      "project": "cc-pc-tools-rs",
      "owner_source": {
        "kind": "direct_owner_chat",
        "observed_at": "2026-10-17T12:28:12+09:00",
        "provenance": "codex_current_user_message",
        "received_text": "<私的パッチに保持した受領原文>",
        "body_sha256": "<同じ私的パッチのSHA-256>",
        "target_todo_ids": [2639, 2640, 2641, 2642, 1626, 2626],
        "scope": "report-and-private-reply-delivery-only",
        "discord_url_exists": false
      }
    }
  ]
}
```

2705の `state_request` は独立stateの当該messageにある正規source・本人ID・
元ToDo番号と、実項目のProjectを照合する。2705の本文を他製品の許可へ流用しない。
純粋な状況質問の既存回答は、上記私的記録のハッシュと、実APIのsource・作者・
回答ID・通知抑止・回答本文ハッシュを再照合する。元threadへの回答はすでに送信済み。
新Goが同じ回答を再送することはない。「当該質問への回答の完了」だけを別の
通知なし進捗へ記録し、全製品の完成とは表示しない。後からToDoの待機をdoneへ
整理しても、同じ回答完了の報告をもう一度作らない。

他の受付済み依頼では `kind: state_request` と元の `message_id` だけを指定できる。
元ToDo番号とProjectは設定に必須。本人の元投稿を実APIで再確認し、対応のない番号、
別Project、別の本人投稿は拒否する。受領記録のない任意番号の追加はできない。

1626には、元項目の正式な `product_resume_qualification.receipt` と
`scrub391.exe --verify-product-resume 1626` の新しい照合結果の両方が必要。
既存の期限は2026-10-21 00:00 UTCまでであり、延長しない。
他プロジェクトへの許可は、明示した元ToDoの報告と返信の保存だけ。
ゲームの起動・停止解除・待機解除・新しい担当世代の登録には利用しない。

## 投稿の上限と送信の復旧

通常の受付報告と製品報告を合わせて、1回の実行で送信CLIを呼ぶのは最大1回。
両方に未送信がある場合は交互に優先する。製品の確認は1回に最大2件、
最大10秒で打ち切り、保存した位置から次回に続ける。
1製品につき状態の変化があったときだけ、前回から15分以上あけて報告する。
同じ状態を時間経過だけで進捗として投稿しない。受付済み依頼の実行・長い待機は
1時間ごとに「前回と同じ状態」と明記して確認結果を返す。完了した状況質問は再送しない。

達成率は元ToDoの数値だけを使う。担当の登録・順番待ち・稼働・判断待ち・
依存待ち・完了記録を区別する。稼働は担当の `job.json` のIDと状態を読み、
PIDと作成FILETIMEの一致・プロセス生存まで確認する。PIDの再利用や読取不能を
稼働として扱わない。完了表示にはToDoの完了と担当の実行成功が必要。

送信前に固定した本文・観測値・一意のrequest IDを独立stateへ保存する。
既存の `discord_ask.exe recovery-log` だけで通知なし投稿を作り、
`recovery-messages.json` の独立したverified記録と実APIの投稿を照合する。
応答が失われても同じIDと本文で回収し、別の製品・スレッドへ送らない。
投稿先・投稿者・Bot・通知抑止・メンション不在・本文を実際に照合してから
送信済みと記録する。製品ごとの投稿を継続使用する。Discord出所には元投稿リンクを
載せ、直接チャットはDiscord本人投稿URLが存在しないことを明記する。

製品の返信元は独立stateの検証済み `progress_links` に保持する。
未配備の `progress-links.json` を空で作らない。受付が作った通常の報告も、
正確なrequest key・本人の元投稿・元ToDo・担当・verified記録・実投稿から
対応を復元できる。壊れた対応表を正常として扱う処理は追加していない。

製品の進捗投稿への本人返信は、元ToDo番号を保って既存の私的な
`steering/<todo>-<message>.json` と独立受付stateへ保存する。
段階は `product_reply_saved` であり、担当が実反映したことや製品の完了を
示さない。短いa/bも文字どおり保存し、別の旧質問への承認へ言い換えない。
通常の `todo edit --note-file` を使う担当参照の配送手順を追加した。
以下の3段階で、保存だけの状態を配送済みと混同しない。

1. `product_reply_saved`: 正規本人返信を私的ファイルへ保存。
2. `product_reply_delivered`: 元ToDoのnoteに、元番号・実担当ID・私的参照が
   通常のedit経路で届いたことを読み直して確認。担当が読んだとはまだ主張しない。
3. `product_reply_applied`: 既存の `applied/<message>.json` の元番号・返信ID・
   実担当ID・revision・verificationが一致した実反映の証拠を確認。
   編集された本人返信では `receipt_sha256` も現在の私的参照と一致することを要し、
   編集前の実反映証拠を再利用しない。前の本文と編集時刻は保存する。

主が行う最小の正規手順は次のとおり。対象IDは新しい進捗投稿への本人返信IDであり、
すでに回答済みの2705のsourceを再回答するためには使わない。

```powershell
$runner = 'D:/CC/Tools/discord-ask/unattended-runner.exe'
$replyId = '<保存済みの新しい本人返信ID>'
$prepared = & $runner --prepare-steering-delivery $replyId
if ($LASTEXITCODE -ne 0) { throw 'prepare deferred' }
$plan = $prepared | ConvertFrom-Json
if ($plan.phase -ne 'prepared') { throw 'prepare receipt invalid' }
$editArgs = [string[]]$plan.edit_args
& $plan.edit_executable @editArgs
if ($LASTEXITCODE -ne 0) { throw 'normal edit deferred' }
& $runner --verify-steering-delivery $replyId
if ($LASTEXITCODE -ne 0) { throw 'delivery verification deferred' }
```

prepareは私的なnoteファイルと計画だけを作り、ToDoを変更しない。元noteを保持し、
実担当・元ToDo・指示原文の私的参照を追記する。元の待機・本人停止・担当世代・
割当てが変わった場合、verifyは配送済みとせず再準備を要求する。
noteの同時変更があればedit前に再準備する。通常CLIは元noteも履歴へ保存する。
この手順で `start`、待機解除、新しい担当起動、費用操作は呼ばない。
実担当は作業の区切りで参照を読み、既存形式の実反映証拠を残す。
verifyをもう一度実行すると、その証拠まで確認して別段階として記録する。

### 純粋な状況質問

短い純粋な状況質問は、固定した狭い判定で `status_query` に分ける。
正規の本人sourceをAPIで照合し、既知の作業・プロセスの数値情報だけで回答する。
新しい製品実行や原保護の復旧を待たず、新ToDoや追加権限も作らない。
既存の通知なし `recovery-log` と独立verified記録を使い、同じrequest IDで回収する。
状況質問に別の実行・費用指示が付く場合や、短いa/bはこの経路へ入れない。
新しいsource ACKの自動投稿は今回追加していない。2705のsourceへの一度の返信は
主が実施済みであり、古い11スレッドへの一斉投稿はしない。

## 依頼スレッドの走査

active一覧と、2フォーラムそれぞれのarchivedを確認する。archivedは
100件ずつ、`archive_timestamp` の次の位置を保存して続け、末尾で一巡を終える。
取得した件数・has_more・前後の位置・確認時刻・一巡の完了をhealthへ残す。
失敗したページのメッセージ位置を進めず、走査を試みたスレッドの巡回位置は
進めるため、先頭の失敗が後続を毎回妨げない。

依頼フォーラムのBot所有スレッドは、正確な新しい送信記録または検証済みの
元依頼との対応があるものだけを取り込む。監査では旧Botスレッドに
受付開始後の未取得本人投稿は確認されなかったため、旧スレッド全体を新しい
承認対象には広げない。作業フォーラムで元依頼との対応が不明な返信は
`origin_pending` として捕捉し、任意の新規ToDoへ登録しない。

## 配置と確認

統合担当は通常のGoビルドで既存runnerだけを更新し、既存の1分タスクから
実行する。新しい実行先・タスク・旧質問受付の有効化は不要。
実投稿の前に、少数の設定と本人投稿・元ToDoの対応を照合する。
配布後はhealthの走査範囲とエラー、独立verified記録、通知のない製品別投稿、
元依頼のリンク、再実行時の重複不在を実際に確認する。

検査は `go test -race ./...` と `go vet ./...`。実送信と既存担当の返信反映は
ローカルの検査とは別の受入工程である。
