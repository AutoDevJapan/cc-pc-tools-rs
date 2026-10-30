# 有限researchの取消・PC復帰後の継続（ToDo4261）

元Issue283/611。611で完了した旧世代からの一度限りの継続を保持し、
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
   次の通常準備でそのworker自身をsourceとし、古い例外を再利用しない。
   正常終了、排出済みの回復取消、lost/transientは過去のquota分類だけで拒否しない。
3. 最新worker自身が実quota失敗したときは、現在の世代と有限retry予算を使う。
   古いsourceの例外を何度でも受付したり予算を0へ戻したりしない。
4. 別job/owner/spec/scope/世代、偽caller、排出未確認、未完依存、本人停止、
   枯渇・未確認OAuth、API鍵だけ・追加支出は引き続き拒否する。

## 完了条件

関連境界のRed/Greenと利用側テスト、別セッションのセルフレビューとラバーダック、
最終headの必須CIと通常merge、Windows既存実行版の更新を確認する。
親3080自身が通常経路で4133/3719を再開し、実native Sol/maxと同じspec/scope、
原結果とACKの前進を確認した証拠を受け取る。登録やプロセスの存在だけでは完了にしない。

## 再開情報

担当20261030-104632-todo-4261、ブランチscheduler/todo-4261-3080。
初期HEAD 951c0ff01c72f949c1ade6304c0d0a640a47c94f。
対象はpublic AutoDevJapan/cc-pc-tools-rs、repository ID1406818713をAPIで確認。
資源cargo。現在は原因再現中で、修正・テスト・レビュー・配布・親の実受入が残る。
一時物所有ID50d768ec6cc3aa27d49040483ac13ca2。原資料は親の指定範囲内で保持する。
