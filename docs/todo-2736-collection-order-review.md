# 将来の新規サービスの権限回収順序を単一ファイルで確認する

元2736の有限read-onlyレビュー。既存OAuth Haiku5.5のみ。単一code fileを読み、
他file/AGENTS/履歴/元資源/認証/原logs/ネットワーク/Git/build/test/編集/ToDo操作/子jobは禁止。
読むfileは D:/CC/Work/unattended-operations-20261013-wt/todo-2736-pr148-ci/tools/pr148-windows-runner/collect_windows.go
だけ。親が固定head e05efa9410846a077ae6d61dedb6faba6804d4e7と作業コピー一致を照合する。
元base5411cac009326b69bc725fc7ff6e7163b64297ffの同fileからの今回差分だけを判定する。
比較元ではservice削除/handle close→ACL解除/DACL0確認→必要controller復元だった。

今回差分: stopped/PID0、元launcher drain又は厳密unstarted条件、remote照合とSID/censusを
通した後、service handleを保持。root/runtime2grant解除→実DACLでSIDのAllow0確認→
認証なしfixture/unstartedだけcontroller Full復元→service-delete journal/実DeleteService→
handle close→実service不在/profile/hive確認。ACL失敗はservice削除前に返りdeferでhandleを閉じる。
失敗/不明mutationを再実行するcollectionActionの制限は変更しない。原5051に再適用しない。

確認済みの原失敗: 新collector1077のpreflight通過後service-delete OK=true、続くroot ACLが
exit1332 ERROR_NONE_MAPPED、元SIDの名前変換失敗、root/runtime明示Allow各1件が残った。
旧rootは保持し、同mutation/別APIによる再試行0。元journalは未変更。
新しい認証なしfixtureの実prepare/executeはexit0。候補collector実行でroot/runtime両ACL解除、
controller復元、service削除の全journal OK=true、実DACL0/実service不在/queryable process0。
そのfixtureはprofile/hive/classesが残りcollectorはerrResidueとしてexit1（ACL失敗ではない）。
別cleanup-profileの初回DeleteProfileWは共有違反32、再試行せず保持中。製品正式成功ではない。
元sourceと隔離copyの変更file以外の同一性を確認し、関連Windows Go tests/vet/buildはexit0。
これらを全回収/全process不在/製品CI成功と読み替えない。

固定前提: p.Root/runtimeだけの既存new-owned資源。SIDは元serviceSIDのまま、無関係な
アカウントの許可は触らない。checkUnstartedFilesはworker開始/auth不存在をLstatのみで検証、
checkUnstartedJobは同run/attempt/job/labelのqueued/steps0/runner無し、auditCollectionは
同repo/source/run/attemptと対象runner登録なしを確認。普通collectは保護された実launcher
reportとSCM終了結果を照合する。これら依存は今回不変で、追加file読取なしの契約とする。

実害Must、改善Suggestion、Nitに分け、行/根拠/最小修正案を書く。
特にACL失敗時にserviceが削除されないこと、全returnのhandle排出、失敗journalの禁止維持、
元root再試行や許可範囲の拡大が無いことを確認する。読んだ範囲・実検査0を最終応答に明記。
sourceを読めない時も別file探索や代替レビュー作成はせず、その具体的エラーだけ返す。