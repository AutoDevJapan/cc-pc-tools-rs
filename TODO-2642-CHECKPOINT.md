# GoDD Press / ToDo2642 再開記録

2026-10-17。元29/2191や製品全体の完了ではない。

## 固定した対象と今回の成果

- 実GitHub所有者はAutoDevJapan、private/admin/nonfork、repo ID1386702183。
- 製品PR175: https://github.com/AutoDevJapan/GoDD-Press/pull/175 。open/draft/mergeable。
- base `4dc5c332d54d19325f122df1f0e4a52e9dd3a629`。
- candidate `b2b6d47b7f9e17617d16a9079a70de272bc3c04c`。
- 製品branch `fix/todo2642-schedule-predecessor-overflow`。
- アプリ修正は先行予約の整数あふれをchecked_addで拒否するもの。回帰48件と旧版REDは元2710の実結果を保持する。
- 現candidateのCodeQL公式run38893812340は全4言語とgateの5jobが成功。
- 親2648のGo実装はレビュー・最終CI・通常merge・配布済み。現在の2648待ちは2710/2730の回収であり、未配布を理由に同じ検査基盤を作り直さない。
- PR本文を最新の実測と未完条件へ更新した。実行記録item_35がcompletedで更新結果を返している。再送しない。

## 未完了を保つ

通常CIはcandidateで起動前に失敗している。rust-core check116948917330、junction-fixtures check116948917352のannotationを確認し、両方ともActions予算による起動拒否だった。通常run38893666794、V2 HTTPS38893667129、Windows38893666823。上限増額・同じ失敗の盲目的再実行・手動successは行わない。原因が分かっただけでCIを成功扱いしない。

元2710のnative検査はアプリSHA `a39a2b1833b3c24125a7b48d5a036376d94c662a`。candidateは.github以外に差分がないが、新SHAでnativeを再実行したとは記載しない。元の原log完全性・未確認・回収拒否を保持する。70-recordの未認証Linux cohortは別candidateの結果で、合格根拠へ混ぜない。

CodeQL指摘は `rust/hard-coded-cryptographic-value` 5件、`js/functionality-from-untrusted-source` 4件で、実害判定は未完。APIで全9件の該当artifact blobがbaseとcandidateで同一と確認した。JS4件がtests下HTMLであること、Rust5件の位置より前にtest属性があることは構造上の補足にすぎず、関数への包含・到達可能性・無害性の判定ではない。位置や識別名、SARIFコード・秘密値は出力しない。除外・dismissなし。

原SARIF/ログの恒久証拠は `D:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016` と対応manifest。今回の使い捨てAPI出力は独立した所有tempであり原資料ではない。所有ID6cfdc316ccbae481eb4bcb74026f3e29はsafe-delete plan指定の通常削除後、finish source_absentを確認済み。再取得や同temp参照を再開条件にしない。generic tempに対するscrub391はclient_resource=true/classification_unverified=trueだったため継続資料に採用せず、次のレビューはAPIのメモリ読取だけとした。新しいguard登録や代替スコープは作っていない。

## 別モデルのレビュー

共有ToDo2739へ既存OAuthのclaude-sonnet-5-5で有限reviewを登録した。起動指示は同branchの `TODO-2642-REVIEW-SPEC.md`。登録と保存specを確認したが、2026-10-17 00:42 JST時点ではtodo、実担当はまだ未起動。受付をレビュー成功としない。

対象は上記の固定head/baseの3ファイル。GitHub APIで原仕様と周辺実装を読み、製品sourceのcloneやPC旧製品フォルダ参照をしない。原応答の保存先はこのworktreeの `REVIEW-2642-ORIGINAL.md`。今後の受領を待ち、実Mustを直す。新しい同種reviewや子ジョブを直接起動しない。

次は2739の原応答と固定headの一致を確認し、指摘を処理する。CodeQL9件の実害判定と、費用増額を伴わず通常CIを成立させる方法の実装・検査は残る。通常mergeはMust解消と必要CI成功の後。Prdは原ルールの別途検証・許可を保ち、mergeだけで配備や製品全体doneにしない。

## 保存と終了

このworktreeは `scheduler/todo-2642-godd-press` を保持し、製品branchと混同しない。共有ToDoのprogress50は元の4条件中2条件の記録を維持し、レビュー依頼だけで上げない。原29/2191の停止・依存・未完と権限を維持する。cargo/GPU/Civ6/本人PC操作/新費用/Discord送信なし。

レビュー完了だけを待つ区切りでは `todo.exe wait 2642 --on 2739` を記録し、状態を読み返す。commit/push後、実branch/HEADと同jobでscheduler checkpoint-workspaceを保存する。次回は元2642の最新note、元仕様、この記録、2739の実結果を読み、更新済みPR本文や成功済み検査を繰り返さない。

## 2026-10-17 04:21以降の再開（上記の待機条件を更新）

元2739はdone。実job20261017-004930-todo-2739はanthropic/claude-sonnet-5-5/succeeded/exit0で、原応答は元jobのlast_message.mdから `REVIEW-2642-ORIGINAL.md` へbyteで保存した。読み取り専用reviewerが保存できなかっただけで再レビューしない。固定head/baseの3差分でMust0を受領した。fork未確認だったレビュー側に対し、親はAPIでfalseを再確認した。

修正前のpanicはdebug/testのoverflow-checks有効時で、releaseではMAX+1がMINに戻り既存のbinding拒否になっていた。checked_addで両設定の拒否を明示化した範囲へPR題名・説明を修正し、返却本文/head不変を確認した。製品sourceは変更していない。

レビューの追加提案（監査列・ACK後の状態・DB拒否理由）は新しい実害の指摘ではないため、この回帰へ追加しない。差分外storeの加算は別経路で、今回へ混ぜない。CodeQL再実行の提案は、親が現APIで公式check116959200210(gate)/116955636194(CodeQL)の同SHA successを取得し、元ephemeral回収証拠も存在するため採用しない。旧PRイベントcancelledは保持する。判断をPR comment6223065886へ投稿成功。最初のconnector引数schemaエラーは送信前で、正常な投稿は1件だけ。重複投稿しない。

共有ToDo2765は9件の実害判定だけを担当する有限check、2766は通常公式CIを既存guest基盤で通す工程。各保存specはこのbranchの `TODO-2642-ALERT-CHECK-SPEC.md` / `TODO-2642-OFFICIAL-CI-SPEC.md`。両方--specで登録し、2766だけ--needs cargo。子ジョブ直接起動なし。2766は原2737のguest基盤を再利用し、未完なら正規wait_on2737で待つ。新しいWindows/Linux/sourcepacket実装を複製しない。親2642を依存先にしない。原2737は2746(実Windowsguest)だけが未完で待機中、2748/2753/2754は既済。原2762は別製品の37件で、今回の9件を担当しない。

2739のreview原文と親判断、PR本文更新、2765/2766の保存済みspecは成果であるが、9件の判定や通常CI成功はまだない。4条件中の検査成功とmerge条件は未達のためprogress50を維持する。次回は2765/2766の実結果を受領し、実害修正・必要な追加差分の別モデルレビュー・最終CI後の通常mergeへ進む。元29/2191の受入全体とPrdに勝手に範囲を広げない。

親の現在の依存は `todo.exe wait 2642 --on 2765 --on 2766 --progress 50` で記録する。元2739を待ち直さない。branchは不変だが、現job/実HEADのcheckpointをHidden/Wait/PassThruで保存し、実exit0と原todo.scheduler.workspace_checkpointを照合する。自分の軽量な再開ファイルだけcommit/push。新しいCargo/GPU/Civ6/guest/runner/本人PC操作/費用/Discord送信はこの親再開では行っていない。
