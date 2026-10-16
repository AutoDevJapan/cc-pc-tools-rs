## 2026-10-17 03時台のレビュー対応（この節を優先）

ToDo2755のClaude Sonnet5.5原応答を受領した。固定code6275c09にMust6件、Suggestion12件。
原応答はevidence-private/20261017/review-2755/original.mdへ全bytes/SHA一致で保存した。
M1 pipeのSCM PID照合、M2実gate親/配分検査と入口分離、M3controller専用START/STOP/QUERYと
runtimeを読まない事前検査、M4有限ACL操作と個別の不可逆attempt/result、M5保護されたlauncher
結果と3hook/実job成功、M6実token/exit測定と旧証拠の誤読防止を実装した。
Goの通常11件成功・2件opt-in skip、vet/build/test binary作成まで成功。修正したservice処理は
まだ実機未検証であり、Must解消/再レビュー済みとはしない。
新しい制御rootはTemp/codex-owned-9bb8054a11c766478f94ee37562f1267（safe-delete所有）。
修正を実測するため、最終sourceに対応した新しいnormal/cancel各1件だけを作り、管理者権限を
落とした自分のcontroller tokenで実行する。既存3profileのDeleteProfileW拒否は再試行しない。
正式runner、製品source、Cargo、GPU/Civ6、旧2730資源は引き続き操作しない。
# ToDo2749 再開記録（2026-10-17）

作業branchはscheduler/todo-2749-pr148-windows-runner。
実装コードは6275c09へcommit/push済み。この記録以降の差分は証拠・文書だけ。
親2736のspec/checkpoint、2730の公開記録、f7acf6dの指定4ファイル、2746のJob Object
証拠を参照。実APIのPR148 head/base/branch/repo IDは依頼と一致した。
製品source/workflow/正式登録/原guard/登録manifest/共有bridge/旧profileは操作していない。
Cargo/GPU/Civ6/子Codexの直接起動も0。

## 実装・検査済み

独立Go moduleで準備・有限サービス・制限token・Job Object排出・停止・回収・
読取専用API照合・job hookを実装。Go6単体検査/vet/Windows build成功。
正常fixtureは実子PID29904を観測し、Job Object内3PIDの保持handle/active0を確認。
取消fixtureは実子PID46956を観測し、明示SCM停止で4PIDの保持handle/active0を確認。
登録/起動だけで検査成功とは書かず、全体未完をevidence.jsonに記録した。

初回失敗はサービスによる不要なオーナーTemp親の属性読み取り。管理者準備の祖先検査は
維持し、制限サービスは新所有treeだけ検査するよう修正して正常fixtureが通った。
旧資源のACLや親folderの権限を広げていない。初回失敗を保持した。
取消しの観測scriptは停止/Waitの後、PS5に無いutf8NoBOM指定で保存だけが失敗。
原サービス側の排出receiptを保存し、fixtureは繰り返していない。

TokenGroupsのDenyOnlyビット検査をfixture後に追加しGo検査は成功した。
その追加を実serviceで検証済みとはしない。成功時binaryのSHAと最新版コードを区別する。
新しいアカウントをさらに増やして同じfixtureを重ねない。

## 回収結果・保管

今回3serviceは不在、各SIDの問い合わせ可能なprocess0、追加ACE0。
各profile/hive/classesは残留。各1回の正式DeleteProfileWはWindows共有違反32で拒否。
再試行、unload、別APIでの削除は0。これは自動承認レビュー拒否ではない。
新3件だけを既存整理ToDo2381のnoteへ配送し、そのspec/状態/権限は維持した。
2381はrecovery_unverifiedであり、受付だけを回収成功にしない。

今回所有したTemp rootのID:
- 6fc78dd68e2b5d16c4844b92686bd55d: 初回起動失敗用。
- eea3b1b7f1f197c40c078507dd5dbcf3: 正常fixture。
- b1e5125d2f46f9e809ee18d76693f2f5: 取消fixture。

上記3rootは原JSON/script/logの全bytes/SHA一致を確認して恒久保存した後、
safe-delete plan→別通常削除→finish=source_absentを確認済み。profileは別資源で残る。
制御用c9d9cca272f24c790d36ca354d71ab27も追加原証拠のSHA/bytes保管後、同じ手順で回収し、finish=source_absentを確認した。

原証拠はこのmoduleのevidence-private/20261017に恒久保管し、Gitから除外している。
manifest.json（SHA256 3DB1CF6C59617E12F088101E64CA551D83F7D6B5371B38D80F3EE80FA54DC7E4）
に48件の原bytes/SHA対応とbinary指紋がある。remaining-cleanup.jsonに今回だけのSIDと
専用profile絶対pathを記録。原ログや認証ファイルを一括表示しない。
Git追跡対象のevidence.jsonは認証の無い結果概要だけ。

## 次の再開

別モデルレビューは共有ToDo2755（Claude Sonnet5.5、固定code head6275c09）。
--specで通常登録し、取消fixtureと残留の追補も保存済み。実応答はまだ受領していない。
正規の原応答・対象head/全読取fileを照合し、Must修正と必要Go検査を行う。
レビューを登録しただけでMust0とはしない。別担当を直接起動しない。

その後、実hash/必要Go検査/原応答/未達/残留を親2736のnoteへ追記配送する。
親2736の完了を待たず、元2640/880の停止・依存・未完を解除しない。
自分のToDo状態はscheduler管理。レビュー完了だけを待つ間はwait --on 2755を使う。
profile整理を待つ段階へ移るときは2381への正規waitを使い、拒否操作を再試行しない。
作業終了/待機前はscheduler checkpoint-workspaceで自分のjob/branch/実HEADを保存する。

正式runner/認証/source/SDK/shared Cargo gateの実適合と元CI成功は親統合の未達条件。
fixtureから実runner/source/正式CIの合格や再起動復帰を主張しない。本人PC操作を求めない。
