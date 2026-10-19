# ToDo2965のWindows有限runner helperを別モデルで確認する

元2965（親2962）の独立レビュー。実装担当はCodex。レビューは既存OAuthサブスクのClaude Sonnet 5.5で行う。子Codexジョブを直接起動しない。GPU、Civ6、APIキー、従量課金、Cargo、本人PC操作、実runner登録、製品source取得は使わない。

対象repoは実GitHub所有者AutoDevJapan/cc-pc-tools-rs。
作業先 D:/CC/Work/unattended-operations-20261013-wt/todo-2965-windows-runner-2962 。
基準 2977911 （origin/main）。レビューする固定HEAD f2afd8592deec9c92fbd430d0d74b28edcc434bd 。
読む対象は `git diff 2977911..f2afd85` のうち tools/civ6-ci-windows/ 全file（README.md、evidence/fixtures-20261019.json含む）と .github/workflows/civ6-ci-windows.yml 。差分の比較元として tools/pr148-windows-runner/ （旧監査済みhelper、変更しない）も読む。親2962の共通契約 D:/CC/Work/unattended-operations-20261013-wt/todo-2962-ci-pc-actions/docs/todo2962-ci-contract.md と同 tools/civ6-ci-contract/ （plan.go、verify.go、workflow.go）は読み取り専用で参照する。最新D:/CC/AGENTS.mdと対象sourceのAGENTS.mdを起動時に読む。保護判定が要る資源はscrub391 --guard-pathで確かめ、保護対象の中身を開かない。

目的: 万世紀AutoDevJapan/civ6-banseiki（repo ID 1392152644）のWindows 3 job（test (windows-latest)、native-driver、live-chain）を、公式ephemeral runner 1件ずつで、追加Actions料金なしに処理する有限helper。旧pr148-windows-runnerから、PR148固定の条件を共通契約cc/civ6-ci-contractのPlan.Validate/Labels/Verify/CheckEventへ置き換えた。

重点確認:
1. contract.goのPlan（contract.Planを埋め込みJSON平坦化）、validate、configureArgs（既定labelを残し civ6-ci-windows を追加）、checkEvent（PR merge contextとtested headの分離、RUNNER_NAME・HEAD_REF・BASE_REF）が共通契約と矛盾せず、fork・古いsource・別run/job・別workflowを通さないか。
2. identity_windows.goのauditWith/phaseBefore/Running/Finished、ownRunner（固有登録だけを見る。長寿命Linux/Windows runnerを無視）、checkJobのlabel集合比較、jobProgress。登録前/start/complete/終了後の再確認がmonitor_windows.go（10秒間隔、policy不一致は即停止、GitHub読取失敗は連続6回まで許容）で実際に行われるか。成功判定がlistener終了だけにならないか。
3. execution_windows.goのexecute-begin/execute-end{fence}イベント、既存のdeferred stopと組み合わせて、不確かなcrashで新workerを止める材料になるか。Cargo gate枠をJob全process drain前に解放しないか（stopFiniteとSCM stop確認の順序）。
4. collection_identity.go（run identityと固有registration不在だけを要求し、branch移動・失敗runで回収が止まらない）、collect_windows.goのcollectWithEvents、errResidue→exit 3、READMEが述べるprofile/hive/classes残留の説明（24件のロード済みhive測定）が実測と一致し、隠していないか。cleanup-profileが実planを拒否する限定が保たれているか。再試行や別経路の迂回がないか。
5. 新規テスト（contract_test.go、collection_identity_test.go、audit_monitor_windows_test.go、receipts_test.go）が実際に失敗を検出する強さか。弱い断言・実装を写しただけのテストを指摘する。
6. tokenがRAMからstdin・pipe・公式input以外へ出ないこと（argv・SCM・file・log・event出力に無い）、source/secretを既存製品folderから取らないこと、共有ACLを変えないことの維持。
7. README.mdのCLI入出力・Plan表・runtime pin・公式runner asset（v2.338.0、sha256 f48e0750...）の正確さと、実測済み/未実測の区別。fixtureの実結果（normal worker 0、cancel worker 125、Job drain）を実証済み範囲以上に主張していないか。
8. .github/workflows/civ6-ci-windows.ymlが既存product-ci.ymlを変更せず、必要十分なgo test/vet/buildだけを自前runnerで行うか。

読み取り専用でレビューする。実装変更、checkout/reset、merge、deploy、状態変更、別担当の起動をしない。Must/Suggestion/Nitで仕分け、実害・根拠・対象file:line・具体修正を書く。誤検知と思われても根拠を記載し、未確認を検証済みとしない。実Windows service・実GitHub・実runnerの挙動は未実証で、レビュー対象はソースと上記fixture証拠である。
結果はこのworktreeのtodo-2965-review-result.mdへ日本語で保存する。冒頭にモデル・固定HEAD・Must件数を記録する。Must0でも根拠を明示する。結果を保存して共有ToDoのレビューだけを完了し、元2965・親2962を完了にしない。
