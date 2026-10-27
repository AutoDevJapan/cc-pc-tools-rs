# 起動返却と実ジョブの照合（ToDo4119）

Namespace: `SchedulerLaunchOutcomeReceipt`。既存の通常schedulerだけが起動する。
元4086/4091/4099、完了4105/4117の状態・入力・世代は変更しない。

## 実装前の確認

比較元: `86fc9f06ca01a6cb8a11720fcfb5a884c5209f56`。
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
