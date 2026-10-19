# ToDo2965のR4修正を別モデルで再確認する（有限）

元2965（親2962）。前回の再レビュー2993（固定HEAD 772fbce、Must1件 R4-M1 ロック先が環境変数依存、Suggestion S1-S5）の結果は D:/CC/Work/unattended-operations-20261013-wt/todo-2993-windows-runner-helper-2965-m1-m2/todo-2965-rereview4-result.md （読み取りのみ）。子Codexジョブを直接起動しない。GPU、Civ6、APIキー、従量課金、Cargo、本人PC操作、実runner登録、製品source取得は使わない。

作業先 D:/CC/Work/unattended-operations-20261013-wt/todo-2965-windows-runner-2962 。比較元 772fbce98dccc10f0a3e27cebd3a84eb0e3591fa 、レビューする固定HEAD 7f83d967887520253c275d5c7d7568a4fe2af034 。`git diff 772fbce..7f83d967887520253c275d5c7d7568a4fe2af034` の tools/civ6-ci-windows/ 全差分だけを読む。最新D:/CC/AGENTS.mdを起動時に読む。

確認する点:
R4-M1: state_windows.go の programData()（SHGetKnownFolderPath、FOLDERID_ProgramData、絶対ローカルパス検査、CoTaskMemFree）と claimLockPath/lockClaim/plainEntry。環境変数に依存せず全phaseが同じfileを開くか。unsafe/ポインタ扱いの誤り、列挙順、エラー時の挙動。
S1: collect_windows.go の verifyTreeClean と countGrantedSIDNoFollow（FILE_FLAG_OPEN_REPARSE_POINT、GetSecurityInfo）。link/reparseをtarget追従せずに検査できているか、markerを書く前に必ず止まるか。
S2: state_windows.go のmarker key（SDDL "D:P(A;;KA;;;SY)(A;;KA;;;BA)(A;;KR;;;AU)"、RegCreateKeyExW/RegSetKeySecurity、REG_SZ形状検査、ownedRootPattern）。既存keyのDACL置換、親keyの継承、controllerが書けない確認（実測: 拒否）の十分さ。
S3: prepareClaimLock のICACL（folder: SY/BA (OI)(CI)F + controller (RX)、file: SY/BA F + controller (R,W)、/inheritance:r）。controllerがlock fileを開いてLockFileExできる最小権限か、削除/差替えができないか。
S4: contract.go imageRoot のcanonical再描画（space/tab引用、clean path検査、制御文字・quote・dot-segment・二重separator拒否）とテスト。
併せて README と evidence/shared-identity-fixtures-4-20261019.json が実測を超えていないか、新たな回帰（token露出、共有ACL変更、SD弱体化）が無いか。

読み取り専用。実装変更、checkout/reset、merge、deploy、状態変更、別担当の起動をしない。Must/Suggestion/Nitで仕分け、実害・根拠・対象file:line・具体修正を書く。未確認を検証済みとしない。結果はこのreview担当のworktreeのtodo-2965-rereview5-result.mdへ日本語で保存。冒頭にモデル・固定HEAD・Must件数を記録。結果を保存して共有ToDoのレビューだけを完了し、元2965・親2962を完了にしない。
