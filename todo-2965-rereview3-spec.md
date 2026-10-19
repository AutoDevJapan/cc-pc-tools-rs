# ToDo2965のRR1-RR5修正を別モデルで再確認する（有限）

元2965（親2962）。前回の再レビュー2979（固定HEAD c2fea70、Must5件RR1-RR5）の結果は D:/CC/Work/unattended-operations-20261013-wt/todo-2979-windows-runner-helper-2965-id/todo-2965-rereview2-result.md （読み取りのみ）。子Codexジョブを直接起動しない。GPU、Civ6、APIキー、従量課金、Cargo、本人PC操作、実runner登録、製品source取得は使わない。

作業先 D:/CC/Work/unattended-operations-20261013-wt/todo-2965-windows-runner-2962 。比較元 c2fea70aeaa6fc624acc109c6f01981ae871da1a 、レビューする固定HEAD e744bde6113fa9be1c2eefacc92e2a13f10509ac 。`git diff c2fea70..e744bde6113fa9be1c2eefacc92e2a13f10509ac` の tools/civ6-ci-windows/ 全差分だけを読む（contractは親main fec91d4の無変更取り込みで対象外）。最新D:/CC/AGENTS.mdを起動時に読む。

確認する点（各RRの解消）:
RR1 monitor_windows.go realHoldOps: census は unreadable>0 で errCensusIncomplete、drain は WorkerPID==0 の公開reportを「worker未作成」として扱う。後者が危険な早期解放にならないか（report公開の時点、WorkerPIDの設定位置、serviceMainの公開順）を厳密に確認。
RR2 contract.go previousRootSettled は前rootが存在する限り拒否。collect_windows.go は launcher report の明示ACE除去とcontroller返却（deleteできる）を行う。同SIDの次jobに旧rootが見える窓が無いか。前root削除を要求する運用のデッドロック（削除拒否・quarantine）への影響。
RR3 scm_windows.go: lockClaim（Global mutex、WAIT_ABANDONED）、requireBoundTo（start/stop/collect）、SD 0x35、ensureService内の読み取り→付け替えの排他。TOCTOUが残る箇所。
RR4 monitor_windows.go runDeadline/holdDeadline/workerDeadline と execution_windows.go: controllerの絶対期限をpipeで渡しlauncherがResumeThread前に検査、worker実行期限がそれを超えない。fixtureとの分岐、admission.Deadlineの検証範囲。
RR5 contract.go imageRoot（Windows引数規則の厳密解析）と scm_windows.go readImagePath（QUERY_SERVICE_CONFIGWのoffset16、構造体size64、バッファ内のみ読み、NUL終端、unsafe不使用）。解析不能時にrefuseするか。
併せて README と evidence/shared-identity-fixtures-2-20261019.json の主張が実測を超えていないか、新たな回帰（token露出、共有ACL変更、SD弱体化）が無いか。

読み取り専用。実装変更、checkout/reset、merge、deploy、状態変更、別担当の起動をしない。Must/Suggestion/Nitで仕分け、実害・根拠・対象file:line・具体修正を書く。未確認を検証済みとしない。結果はこのreview担当のworktreeのtodo-2965-rereview3-result.mdへ日本語で保存。冒頭にモデル・固定HEAD・Must件数を記録。結果を保存して共有ToDoのレビューだけを完了し、元2965・親2962を完了にしない。

