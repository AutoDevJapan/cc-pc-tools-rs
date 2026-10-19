# ToDo2965の固定サービスID方式と停止境界を別モデルで再確認する（有限）

元2965（親2962）。前回の再レビュー2974（固定HEAD 205a782、Must3件: R1 SCM照会エラーでの早期復帰、R2 期限起点、R3 STOPPED=排出の同一視）の結果は D:/CC/Work/unattended-operations-20261013-wt/todo-2974-windows-runner-helper-2965-must/todo-2965-rereview-result.md （読み取りのみ）。子Codexジョブを直接起動しない。GPU、Civ6、APIキー、従量課金、Cargo、本人PC操作、実runner登録、製品source取得は使わない。

作業先 D:/CC/Work/unattended-operations-20261013-wt/todo-2965-windows-runner-2962 。比較元 205a782ec57834d399a2ad2e1122338c482a2271 、レビューする固定HEADは作業先の `git log` 先頭のうち、この指示を追加するcommit直前（件名 "feat(civ6-ci-windows): fixed shared service identity"）。`git diff 205a782..<そのcommit>` の tools/civ6-ci-windows/ 全差分を読む。tools/civ6-ci-contract は親main fec91d4 の無変更取り込みで対象外。最新D:/CC/AGENTS.mdを起動時に読む。

確認する点:
A. R1-R3 の解消: monitor_windows.go の holdDeadline/holdUntilDrained/realHoldOps と execution_windows.go の defer。照会エラーで早期復帰しないか、期限がサービス開始起点で起動+実行+排出猶予を覆うか、STOPPEDだけでdrained扱いにせず launcher report (job_drained, AccountSID) または口座のprocess0を確認するか、fenceが正しく出るか。stopを繰り返していないか。テスト(audit_monitor_windows_test.go)が実際に失敗を検出する強さか。
B. 固定サービスID方式(親の確定方針): contract.go の svc()/name() 分離、scm_windows.go の ensureService（busy/PID/口座process/前job root未回収の拒否、ChangeServiceConfigW で image path 付け替え、SD再設定）、previousRootSettled/imageRoot、QUERY_SERVICE_CONFIGW の読み出し（unsafeの境界、バッファ長、オフセット16の妥当性）。prepare の呼び出し順(heavy workの前)。
C. collect_windows.go: shared service を削除しない、ACE除去、recursive owner reset(Administrators)、service idle再確認、census0、receipt(service_retained/service_idle_confirmed/owner_reset/shared_profile_retained/profile counts)、collectionServiceStatus から1077要件を外した妥当性(unstarted は launcher report 無変更+artifact無しで代替)。同一SIDの次jobが前job rootに対しWRITE_DAC/ownerで入れないか。前job root削除前に次jobが準備できる窓での漏れ。
D. cleanup-profile削除、errResidue/exit 3 廃止、READMEの記述が実測(evidence/shared-identity-fixtures-20261019.json: 固定fixture IDで normal/cancel/normal、CC-CIW profile count 4 のまま)を超えていないか。実IDCC-CIW-civ6-banseiki は未実行である点の明記。
E. 新たな回帰（競合、token露出、共有ACL変更、service SDの弱体化）が無いか。

読み取り専用。実装変更、checkout/reset、merge、deploy、状態変更、別担当の起動をしない。Must/Suggestion/Nitで仕分け、実害・根拠・対象file:line・具体修正を書く。未確認を検証済みとしない。結果はこのreview担当のworktreeのtodo-2965-rereview2-result.mdへ日本語で保存。冒頭にモデル・固定HEAD・Must件数を記録。結果を保存して共有ToDoのレビューだけを完了し、元2965・親2962を完了にしない。
