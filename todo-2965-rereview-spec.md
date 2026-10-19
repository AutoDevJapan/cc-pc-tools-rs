# ToDo2965のレビューMust修正を別モデルで再確認する（有限）

元2965（親2962）。前回レビュー2968（固定HEAD f2afd85、Must4件）の結果は todo-2965-review-result.md（D:/CC/Work/unattended-operations-20261013-wt/todo-2968-windows-runner-helper-2965/ 内、読み取りのみ）。実装担当は別モデル。子Codexジョブを直接起動しない。GPU、Civ6、APIキー、従量課金、Cargo、本人PC操作、実runner登録、製品source取得は使わない。

作業先 D:/CC/Work/unattended-operations-20261013-wt/todo-2965-windows-runner-2962 。比較元 f2afd8592deec9c92fbd430d0d74b28edcc434bd 、レビューする固定HEAD 205a782ec57834d399a2ad2e1122338c482a2271 。
読む対象は `git diff f2afd85..205a782` の tools/civ6-ci-windows/ 全差分と、取り込んだ tools/civ6-ci-contract（親の 0d1e95b をそのまま取り込み。契約自体は親の別レビュー対象でここでは読まない）。最新D:/CC/AGENTS.mdを起動時に読む。

確認する4点（前回Must）:
M1 共通contractがbranchに含まれ、helperの依存が解決される（自前workflowは削除済み。親の civ6-ci.yml が検査する）。
M2 collection_identity.go が contract.MatchRunPath を使い、bare path と path@ref の正しい形式を受け入れ、別ref/別workflowを拒否する。テストが実際に検出できるか。
M3 monitor_windows.go が、runner未割当のままcompletedになったjobで即座に失敗し、queued中も1分間隔で固定planを再確認する。identity_windows.go の phaseQueued の扱い（固有登録の有無、label集合）が正しいか。テストの強さ。
M4 execution_windows.go の defer で、stopFinite が遅延/拒否でも、SCMがSTOPPED（launcherがJob Object排出後に報告）になるまで、または deadline+2分まで gate child を生存させ（waitServiceStopped / waitStopped）、その後だけ fence を出すか。stopを繰り返していないか。取りこぼしが無いか。
併せて追加した job-success-verified event、README、evidence/profile-residue-measurement-20261019.json の主張が実測を超えていないかを確認する。新しい回帰（競合、デッドロック、token露出、共有ACL変更）が無いかも見る。

読み取り専用。実装変更、checkout/reset、merge、deploy、状態変更、別担当の起動をしない。Must/Suggestion/Nitで仕分け、実害・根拠・対象file:line・具体修正を書く。未確認を検証済みとしない。結果はこのreview担当のworktreeのtodo-2965-rereview-result.mdへ日本語で保存。冒頭にモデル・固定HEAD・Must件数を記録。結果を保存して共有ToDoのレビューだけを完了し、元2965・親2962を完了にしない。
