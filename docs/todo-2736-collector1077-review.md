# 未起動サービスの回収判定を単一ファイルで確認する

既存OAuth Haiku5.5による有限read-onlyレビュー。読むcode fileは以下の1件だけ。
ほかのfile/履歴/AGENTS/証拠/製品/source/credentialを追加で読まない。Git・API・テスト・build・
Cargo・Docker・GPU・Civ6・編集・保存・commit・ToDo操作・子ジョブは禁止。最終応答だけに返す。
規則: OAuthサブスクのみ、秘密値を読まない/表示しない、結果は日本語。必要な文脈は全て下記。

対象 D:/CC/Work/unattended-operations-20261013-wt/todo-2736-pr148-ci/tools/pr148-windows-runner/collect_windows.go
固定base d9bb6c6387fa1835e299ea70fd9829b7f3da1e31
固定head 6c72729c7e03119c5d2cbccf0f895a442d32dd5d
元worktreeのこの絶対pathを読む。自分の子worktreeの旧fileで置き換えない。
head照合は親が行う。自分でGitを起動して確認する必要はない。

## 今回の追加差分

1. collectModeのunstarted分岐で、SCM code 0に加えて1077だけを許す。
2. p.Fixtureが空の時のauditCollectionをopenServiceより前へ移し、serviceが既に消えていても
   remote照合を省略しない。普通collectのlauncher drain条件と全action journalを保持する。

## 確認済みの事実と既確認の依存契約

- 元65592b4全差分はSonnet review2853でMust0/実検査0。親が固定source/作業コピー一致を照合。
  今回はその後の上記2点だけ。namespaceやOAuth環境修復を再レビューしない。
- 実collect-unstartedはservice code!=0の検証で停止し、削除/ACL action attemptは0件。
  対象は新規所有root5051だけ。sc queryexはSTOPPED/PID0/WIN32_EXIT_CODE1077を返した。
  Microsoft仕様は1077を、前回boot以降にサービスの起動試行が無い場合の値と定義する。
  https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-scmr/cf94d915-b4e1-40e5-872b-a9cb3ad09b46
- checkUnstartedFilesはworker最初の副作用started.jsonを含むworker/hook/auth fileの不存在と、
  初期launcher reportが厳密に2バイト{}であることを要求する。auth本文は読まない。
- checkUnstartedJobは同run/attempt/job/labelのqueued、runner無し、steps0を要求する。
- auditCollectionは同じ固定repo/PR/head/tree/run/attemptとrunner登録0を要求する。
- collectionActionは変更前のintentと結果を保存し、失敗/不明な同mutationを繰り返さない。
- collectの既存条件は停止/PID0、同SID queryable process0、保護されたlauncher drain report。
  unstartedだけは上記の別条件を使う。profile削除はせず、既存SID/旧資源は扱わない。
- cleanupでcontrollerへ付けるFullは、auth不存在を確認したこの新root/runtimeだけのcleanup用。
  prepare後のRXをそのまま復元するという意味ではない。READMEの説明は親が修正済み。
- 同じ普通collect/別API/guard解除で今回の検証を迂回していない。実再実行は今回の受入後だけ。
  非0の失敗コードを一般的に許す変更にはしない。

## 判定

実害のあるMust、改善Suggestion、軽微Nitに分け、位置・根拠・最小修正案を返す。
特に1077許可がunstarted以外へ広がらないこと、ほかの前提が保持されること、
削除済みserviceの経路にもremote照合が入ることを、この1fileだけで確認する。
読んだ範囲と実検査0を明記する。読めない場合も追加file探索や代替レビュー依頼をしない。
