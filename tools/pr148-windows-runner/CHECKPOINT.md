## 2026-10-17 最後の小差分を配送（この節を優先）

code3ba895fのレビュー2763はMust0で受領済み。追加Suggestionの小差分はda2b45eへ
commit/pushした。最新Go12pass/2opt-in skip、vet/Windows build成功。
この小差分の有限レビューはToDo2764へ--specで登録した。2764の原応答は未受領なので、
da2b45e全体をレビュー済みとは呼ばない。前2763のMust0の対象は3ba895fに限る。

今回は新service/account/profile/実Cargo/製品source/正式runnerを作っていない。
新build root f2590b9747900085d9ed16af40309dc7はbinary SHA/bytes記録後、
safe-delete plan→別通常削除→finish=source_absentを確認した。
原レビューと実build指紋はevidence-private/20261017/review-2763配下を保持。
前5profile/hive/classesは既存2381へ配送済みで、再試行や新しい削除は行っていない。

次の再開は2764の固定base/headと実応答を受領し、Mustがあればその差分だけ修正する。
親2736へ原応答/候補hash/未達を渡す。親完了は待たない。レビューだけを待つ間は
2749を2764への正規waitへ記録する。回収だけが残る段階なら2381への正規waitにする。
強制controller終了や不確定stop時のgate保持は未保証で、親統合の監督/隔離条件を保持する。
## 2026-10-17 04時台・レビュー2763受領（この節を優先）

Claude Sonnet5.5の原応答を受領し、SHA/bytes一致でreview-2763/original.mdへ保管した。
対象code3ba895fの元Must6件はソース上解消、Must0。全Go18fileとf7acf6dの4一次参照、
前回未読の2746証拠を読了。別モデル自身のgo testは11pass/2opt-in skip、vet/build成功。
この結論を正式runner/原source/共有gate/非昇格controller/元CI成功へ広げない。

追加Suggestionへ小さい補強を行った。正常に起動したserviceがまだ停止確認前のまま
controllerがエラーを返す場合は、deferで1回だけstopFiniteを完了させてから戻る。
拒否/不確定の停止を反復せず、強制終了時のgate保持までは保証しない。親の実統合では
正常stop/drainの監督と不確定runの隔離が必要。この条件を新daemon/共有gate変更で隠さない。
collectionのACL期限はcleanup_seconds（省略1800秒、60..14400秒）でartifact規模に合わせる。
hook receiptの同account改ざん耐性が無い限界と、一時的PID検証handleであることも明記。
pipeには匿名QoSを追加し、実pipe unitも同じopenRegistrationPipe経路を通す。

最新Goは12pass/2opt-in skip、vet/Windows build成功。この差分では新しいservice/accountを
作っていない。既存の正常/取消・AU測定・worker0/125証拠はcode3ba895fのまま保持。
旧3件＋新2件のprofile/hive/classes未回収記録は合計5件。再読/再削除していない。
レビュー2763の原応答と実binary hash/残件数はreview-2763-summary.jsonにも記録した。

新しい一時rootはTemp/codex-owned-f2590b9747900085d9ed16af40309dc7（safe-delete所有）。
Go build binaryだけを置いた。原指紋保管後にplan→別通常削除→finishで回収する。
次はこの小差分の有限な別モデル確認を--specで登録し、親2736へ原応答と未達を配送する。
製品source取得/正式登録/製品workflow・PR/実Cargo/旧資源の操作は引き続き0。
## 2026-10-17 修正後の最終保存（この節を優先）

コードは3ba895f、証拠/文書は9b0f205までpush済み。Goコードは3ba895f以降変更なし。
レビュー2755のMust6件へ実装修正し、再レビューを通常ToDo2763へ--specで登録した。
2763の実応答はまだ受領していないのでMust0/統合可能とは書かない。
原2755応答はSHA/bytes一致で保管し、REVIEW.mdに各指摘の採否/限界を書いた。

修正後binaryで正常worker49284/子32644、取消worker37124/子8672を実測。
AU DenyOnlyは両方true、保護結果の実worker終了は正常0/取消125、Job保持handle/active0。
実launcher PIDを明示した再open拒否/制御ファイル書込拒否も両fixtureで確認した。
非管理者controllerの作成は正規brokerでもCreateProcessWithTokenWがWindows Access denied5。
別方式の迂回は試していない。非管理者の実動を成功としない。
取消observerは明示stop0/controller終了を確認したがProcess.ExitCodeはnullでscript1。
fixtureは繰り返さず、実worker125/SCM success/保護結果とは別の限界として保存した。

今回追加2service不在、queryable process0/unreadable1、管理者追加の2境界ACE0。
profile/hive/classesは新2件残留。旧3件の共有違反32は再試行しておらず、新2件の
profile削除も未試行。既存2381のnoteへ新2件だけ追記し、spec/状態/権限は維持した。
新2root（6192f4b25e5b6a1a5cf4475ec72f5398、62233620ef55db0d7638390a4e15498a）と
制御root（9bb8054a11c766478f94ee37562f1267）は全bytes/SHA保管後、safe-deleteの
plan→別通常削除→finish=source_absentを確認した。原profileは別資源で未回収。

原証拠はevidence-private/20261017/review-fixed-fixturesに49レコードを保管した。
manifest SHA256 C6F0960661D7EE0EE893778D8067A5FA3B8F7091265F5B1B434C2EC62E4EF365。
original-execution-events.jsonlには当該ジョブの実行原応答5件、remaining-cleanup-v2.jsonに
今回だけの新SID/専用profile pathがある。review-fixed-evidence.jsonが追跡概要。
旧evidence.jsonの未実測AU・取消exit初期値は実測と区別して修正し、原本は保持した。

次は2763の原応答の対象head/全対象file/前回未読primary refsを照合してMustを直す。
親2736の完了は待たない。原製品2640/880の未完/停止/依存は解除しない。
正式runner/製品source/共有Cargo gate/原CI/SDKは親統合の未達条件であり、この子では未実行。
レビュー待ちだけで止める場合は2749を2763への正規waitへ記録する。
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
