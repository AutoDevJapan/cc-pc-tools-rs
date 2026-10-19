# ToDo2965のM1/M2修正を別モデルで再確認する（有限）

元2965（親2962）。前回の再レビュー2984（固定HEAD e744bde、Must2件 M1 移動保管されたrootの隔離、M2 start/stop/collectの排他）の結果は D:/CC/Work/unattended-operations-20261013-wt/todo-2984-windows-runner-helper-2965-rr1-r/todo-2965-rereview3-result.md （読み取りのみ）。子Codexジョブを直接起動しない。GPU、Civ6、APIキー、従量課金、Cargo、本人PC操作、実runner登録、製品source取得は使わない。

作業先 D:/CC/Work/unattended-operations-20261013-wt/todo-2965-windows-runner-2962 。比較元 e744bde6113fa9be1c2eefacc92e2a13f10509ac 、レビューする固定HEAD 772fbce98dccc10f0a3e27cebd3a84eb0e3591fa 。`git diff e744bde..772fbce98dccc10f0a3e27cebd3a84eb0e3591fa` の tools/civ6-ci-windows/ 全差分だけを読む。最新D:/CC/AGENTS.mdを起動時に読む。

確認する点:
M1: collect_windows.go の tree-acl-reset（icacls <root>\* /reset /T /C /L）、owner-reset、verifyTreeClean（全entryでサービスSIDの付与が無いことの再帰検査、link非追従）、markCollected（HKLM\SOFTWARE\CC\civ6-ci-windows\<service>\CollectedRoot を最後に書く）と state_windows.go、contract.go previousRootSettled（marker一致のみ許可。元パスの有無に依存しない）。移動保管・保持されたrootでも次jobの同SIDから開けないか。reparse/link・使用中ファイル・icaclsの失敗がmarkerを書かずに止まるか。markerの偽造や上書き（controllerが書けないか、keyのACL）。
M2: state_windows.go の claim lock（ProgramData配下のfileに対する排他LockFileEx、ACL、OPEN_EXISTING）、scm_windows.go の startFinite（lock、requireBoundTo、collected root拒否）・stopFinite（check-and-issueのみlock、待機はlock外）・ensureService（lock下で全判定）・collect全体のlock。照合後に別操作が割り込める隙、lockのデッドロック・保持時間（stop待機がcollectを妨げない）、controllerと管理者phaseの権限差。
併せて: imageRootの正規形比較(canonical re-render)、README、evidence/shared-identity-fixtures-3-20261019.json の主張が実測を超えていないか、新たな回帰（token露出、共有ACL変更、SDの弱体化、lock fileの権限過大）が無いか。

読み取り専用。実装変更、checkout/reset、merge、deploy、状態変更、別担当の起動をしない。Must/Suggestion/Nitで仕分け、実害・根拠・対象file:line・具体修正を書く。未確認を検証済みとしない。結果はこのreview担当のworktreeのtodo-2965-rereview4-result.mdへ日本語で保存。冒頭にモデル・固定HEAD・Must件数を記録。結果を保存して共有ToDoのレビューだけを完了し、元2965・親2962を完了にしない。
