# 有限researchの取消・PC復帰後の継続（ToDo4261）

Issue644、元Issue283/611。611で完了した旧世代からの一度限りの継続を保持し、
その後の担当自身の終了を通常経路へ戻す。元3080の依頼全体は未完了。

## 再現した記録

- 4133の現在世代6のworker receiptは20261029-174543を指すが、
  依存完了後のlast_jobは旧20261028-204450のまま。
  最新workerは回復目的の取消で、過去のquota分類が残る。
  現在のcontinuation requestも旧sourceから再受付されている。
- 3719の現在世代5の最新workerは20261028-171959でlost/transient。
  taskに残るcontinuation requestは世代3の旧source/旧親を指す。
- 原spec、原返信、旧proof、親edge、使用済みretry履歴は変更しない。
  CLIの受付、実準備、実起動、原結果とACKの前進を別々に確認する。

## Given / When / Then

1. 現担当と過去の担当が異なる状態で依存を待つ。
   依存が完了したとき、現担当のIDをlast_jobへ保持し、停止と残る依存は維持する。
   既に失われたpointerは現在世代のworker receiptと実jobから照合する。
2. 同じ所有・spec・scopeの現在世代の有限workerが終了し、古いrequestが残る。
   明示受付は実親のCLIだけで行う。通常schedulerの後続再開では、現在の実親記録と
   そのworker自身の結果から次の契約を生成し、古い例外を再利用しない。
   自動生成の印と保持した旧requestの指紋を結び、本人や親の新しいCLI指示と偽らない。
   正常終了、排出済みの回復取消、lost/transientは過去のquota分類だけで拒否しない。
3. 最新worker自身が実quota失敗したときは、現在の世代と有限retry予算を使う。
   古いsourceの例外を何度でも受付したり予算を0へ戻したりしない。
   後の通常workerが旧例外を持たない場合も、現在receiptと最新の実quota失敗を照合し、
   古いrequestだけを通常のquota再試行の妨げにしない。旧quota capsuleは再利用しない。
   実quota失敗後に依存待ちが解除された場合は、新しい依存世代の契約に、従来の
   quota retry計画を結ぶ。使用済み回数と履歴に加えて、今回の再試行1回も記録する。
4. 別job/owner/spec/scope/世代、偽caller、排出未確認、未完依存、本人停止、
   枯渇・未確認OAuth、API鍵だけ・追加支出は引き続き拒否する。

担当IDの優先順位を変えるのは有限subworkだけ。通常ToDoは元の選び方を保持する。
最新workerの所有・世代・固定specと排出を確認した場合だけ、通常prepareの保存時に
古いlast_jobを修復し、元の値も別の記録へ保持する。原workspace receiptは変更しない。

## 現在の実親が行う明示受付

以下は依頼時点の値。実行前に現在のtask・最新job・両世代・spec SHAを照合する。
この修復担当が親のCC_JOB_IDへ書き換えて実行しない。受付だけでは再開成功としない。

```powershell
todo.exe continue-subwork 4133 --source-job 20261029-174543-todo-4133 --parent-job-id $env:CC_JOB_ID --resume-generation 6 --dependency-generation 11 --spec-sha256 9336a02f41b267d04f4f406815ff048938055a11ea4582f8514d79708881c9ff
todo.exe continue-subwork 3719 --source-job 20261028-171959-todo-3719 --parent-job-id $env:CC_JOB_ID --resume-generation 5 --dependency-generation 1 --spec-sha256 4f6fe32f62db56bed2acb4d378147ff952ad06230243fb45f7674f3158313fc3
```

新しい契約は受付時のsource receiptも保持し、Start後の次の1世代で同じ値を再照合する。
旧proofは書き換えず、元からreceiptを持たない旧形式の検証も保持する。

## 完了条件

関連境界のRed/Greenと利用側テスト、別セッションのセルフレビューとラバーダック、
最終headの必須CIと通常merge、Windows既存実行版の更新を確認する。
親3080自身が通常経路で4133/3719を再開し、実native Sol/maxと同じspec/scope、
原結果とACKの前進を確認した証拠を受け取る。登録やプロセスの存在だけでは完了にしない。

## 再開情報

担当20261030-104632-todo-4261、ブランチscheduler/todo-4261-3080。
初期HEAD 951c0ff01c72f949c1ade6304c0d0a640a47c94f。
対象はpublic AutoDevJapan/cc-pc-tools-rs、repository ID1406818713をAPIで確認。
資源cargo。CLIと依存待ちのRed 3件、scheduler境界のRed 1件を修正前に確認。
修正後のCLI・依存待ち14件は成功した。関連検査、レビュー・配布・親の実受入が残る。
配布前の実dispatch-one 3719 --waitはscheduler busyであり、実準備・新拒否の証拠ではない。
一時物所有ID50d768ec6cc3aa27d49040483ac13ca2。原資料は親の指定範囲内で保持する。
最新の作業位置と残作業は、この作業先のREPAIR4261-STATE.private.jsonにも保存する。

fbc61508の両別セッションレビューで、通常ToDoへの波及と、さらに後の有限workerが
古いrequestに妨げられるMustを受領した。各原応答をcanonical last_messageと照合し、
旧headの結果と最新候補の確認を区別する。通常prepare・登録・Start・最終queueで
予算と原結果を保持する検査を追加し、全指摘への対応と固定候補の再確認を追う。
