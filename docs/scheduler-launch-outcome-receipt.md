# 起動返却と実ジョブの照合（ToDo4119）

Namespace: `SchedulerLaunchOutcomeReceipt`。既存の通常schedulerだけが起動する。
元4086/4091/4099、完了4105/4117の状態・入力・世代は変更しない。

## 実装前の確認

比較元: `86fc9f06ca01a6cb8a11720fcfb5a884c5209f56`。
Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/604 。
`cc-jobs::Context::cmd_run` は同じlaunch_keyの保存済みIDを返し、terminalでも
新しいsupervisorを生成しない。通常schedulerはIDだけを受け、無条件にlinkして
`Launched`と記録し後続候補を打ち切っていた。既存admission診断には返却job IDがない。
`dispatch_receipt` は別の再試行復旧契約であり、この診断でapplyしない。

## 契約

- 既存のrun/launch-keyの排他・原子保存・一度性を使い、任意のtyped出力を追加する。
  従来の呼出元にはIDを返す。同じkeyの再返却は`ExistingJobNoAction`である。
- 新しいjob登録とsupervisorの発行は`Queued`。この関数は実workerの実行開始を
  観測していないので`NewExecution`を返さない。既存jobのpending=trueは保存済みの
  予約状態であり、この呼出が再起動を要求した証拠にはしない。
- job ID/key/実保存世代/所有の指紋と、呼出元のID/key/実保存世代/試行番号を保持する。
  不明はunknown/null。control metadata fingerprintをspec本文SHAとは呼ばない。
- 既存terminalかつpending=falseのNoActionをテストで再現した場合に限り、その
  分岐で新規起動の記録と打切りを除く。後続のすべての通常gateを再実行する。
  queued/live/pendingの予約を二重に消費・解放せず、未知の返却は保守的に止める。
- receiptは既存scheduler保存場所の固定上限の原子置換。長文note/Source/spec/argv/envを
  格納しない。保存失敗は未記録として表示し、実行成功の証拠を作らない。
- 読取は保存済みreceiptと既存の本文を除いたmetadata projectionだけを使う。
  launch/cancel/retry/モデル・認証照会やSource読取を実行しない。
  cycleは明示IDで対応させ、古いcycleへ新しい結果を推測で結び付けない。

## Given / When / ThenとRed / Green

1. 同じkeyの古いterminal job / 通常runを再依頼 / 同じID、NoAction、supervisor増分0。
2. 同じ条件と後続の正常候補 / 通常dispatch / NoActionをLaunchedとせず後続が全gateを通る。
3. 既存live/queued/pendingと新規登録 / 通常返却 / 予約と新規登録を区別し重複処理しない。
4. owner/key/世代/inputの差異と未知出力 / 診断 / mismatchまたはunknownで証拠を捏造しない。
5. 欠落/破損/期限切れ/上限/並行保存/保存失敗 / bounded reader / 固定分類、実行の副作用0。

Red/Greenの実コマンドと結果、最終headのレビュー2件・CI・配布provenance・
通常schedulerから自然に得た限定読戻しは作業記録と管理受領JSONへ残す。
登録だけで完了とせず、未観測の実機受入は未完了とする。

## 保存形式と読取

`codex_jobs run --launch-outcome` は共有Rust型`launch_outcome::Outcome`をJSONで返す。
この関数が発行するeffectは`existing_job_no_action`、`queued`、`accepted`のみ。
旧IDだけの返却は`unknown`。jobのqueued/running/terminalと、再起動/rebootの
pendingは別の保存状態であり、この呼出がRestartRequested/NewExecutionを行ったとはしない。

`scheduler explain-launch-outcome --id <ID>` は新しい実行を伴わずに読める。
既存のscheduler保存先の`launch-outcomes.json`へschema 1、最新128対象、256KiB以内、
24時間の期限で保存する。通常の走査へ新しい全件走査を足さず、返却があった対象だけ
置き換える。専用の短い排他と既存admissionの原子置換を共用する。
古いsnapshotより前の一時ファイルをcrash後の正本へ昇格させない。

receiptは明示的なcycle ID・receipt ID・target ID・launch_key・保存世代/試行番号と
返却job IDを持ち、所有tupleは本文を含まない指紋で保持する。
admissionのentryにも同じcycle/receipt/job/effectを結ぶ。
古いcycleに新しい値を付け足さず、近い時刻や番号だけで対応を推定しない。
Root/jobへのlink成功と返却成功は別で、返却直後に未linkのreceiptを先に保存する。
副記録が失敗しても通常のRoot/job操作は続け、`launch_receipt_recorded=false`を返す。
起動受付後のjob読取などが失敗した場合はエラーを保持し、同じcycleで次を起動しない。

通常prepareで既に読んだspec/dispatch、通常registrationで既に読んだdispatch/保存する
receivedのバイトからだけSHAを保存する。過去jobの未記録SHAはnullのままとし、
replayと診断のためにSource/spec/promptを開かない。
`input_binding`は保存済みの通常dispatchと返却jobのSHAの照合であり、
診断時点のSource本文が同一であることを証明しない。
`current_source_bytes_checked=false`と照合範囲を常に返す。
世代・key・control入力変更、所有不一致、保存済み入力の不一致、未知、欠落、破損、
期限切れを別の分類で示す。

## 検証の途中記録

Red: commit `22d2a96f`の`test(terminal_same_key_no_action)`は2件とも意図した理由で失敗。
元行がdoingへ変わり、Launchedが記録され、後続が進まないことを実測した。
同じ条件を含む最初のGreenは12件合格。追加の関連1084件は1083件合格、
受付後のエラーを成功として隠す1件を検出し、通常の失敗返却を保持するよう修正した。
この段階の実行は最終HEADのCI成功や実配布成功の証明には使わない。
固定した最終HEADのテスト・別セッション2件・全指摘・CI・配布と限定実読戻しは
PRと管理受領JSONで照合する。
