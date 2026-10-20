# ToDo3189 再開記録

- 対象は元3126の実担当連結とcheckpointだけ。3050/3070の移行は完了済みで再実行しない。
- 基準main: cb26d3112d537e2f602fbed07108673517746898。
- 自分のbranch: scheduler/todo-3189-3126。実担当: 20261021-052342-todo-3189。
- 最新指示、AGENTS、実所有者AutoDevJapan、対象2作業場所の正式guardを確認した。
- 2026-10-21 05:25台: 元3126は正規APIでwait、wait_on=[3189]、phase=prepared、activeJob=null。
  正常statusの前担当20261021-042842-todo-3126はsucceeded。
  通常作業のためsubwork/subwork_spec/assigned_needsは無い。自分3189は正常連結済みでrunning。
- 確認したコード: 一般reconcileは原保護台帳を先に要求する。
  修復巡回のadopt_repair_dispatchesはsubworkのある通常以外の独立作業に限定される。
  まだ元行の起動世代と保存promptの照合は未確認。raw台帳の表示・編集はしない。
- 次: 対象1件の読取診断で起動世代/spec/資源/実promptを照合し、最小修正のRed/Green、
  別モデルmaxレビュー、CI、通常merge、source/hash照合、正規CLI実検証を進める。
  元3126の待機を保持して実履歴だけを回収し、終了済み担当をrunningへ偽造しない。
- Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/327。
- Red実行: 通常起動の回収検査が履歴への未連結で失敗。読取診断の検査は成功した。
- 最初のGreen: 新規5検査が成功。実Git上のbranch照合、不一致と停止、同時メモ更新、
  正規Start後の新担当によるcheckpointと旧担当拒否、原ファイルの保持を含む。
  追加した停止/依存境界と関連処理はこれから検査する。Clippyは一度成功。
- 元3126の読取診断: job20261021-042842-todo-3126と起動世代、正式guard、
  実branch、spec全原文、資源、元の登録場所、保存promptがすべて一致した。
  source state SHA=09d99e74b07d47fe0a7c58dac0d8b8b9bc072d17f3a90754cee7641223dbdf0b。
  この段階では実台帳の回収は未実施。phase=prepared/status=waitを保持している。
- 配布対象scheduler.exeの正式guardはclient_resource=false。原保護台帳とguardは変更しない。
- 2026-10-21 06:52の再開: 3198は正常終了。実ジョブと対応sessionでOpus5.5/max、Must0を確認。
  PR328のe846181向けBuild speed/CodeQLは成功。原文SHAと実行出所はTODO-3189-REVIEW.private.json。
- 改善提案のうち、phase・安価な照合の優先・項目ごとの失敗隔離・不適格applyを修正する。
  旧候補で追加3検査をRed確認した。最初の修正後8検査は成功し、最終CAS/linked worktree/新keyも確認する。
  詳細はdocs/dispatch-receipt-review-3189.md。追加差分だけを別モデルmaxレビューへ渡す。
- 今回の実担当20261021-065242-todo-3189も未連結だった。初回レビュー/CI済みe846181の
  正規release CLIで、全条件一致・元行SHA指定の対象1件だけを連結した（saved=true）。
  元3126の回収や共有scheduler.exeの反映はまだ行っていない。
- 追加修正後: 影響範囲の22検査が成功。新規9検査には実linked worktree、通常prepareの新key、
  実ロック競合の項目隔離、最終guard中の行競合、正常結果処理の途中phaseと開始時刻を含む。
  変更のない範囲は初回203検査の有効結果を保持する。最新mainのcc-ask修正も保持して統合する。
- 一時所有ID: 27075b23a6091a25d55408958e1de949。
  証拠はprivate記録として保持。元資料と他担当の成果は片付けない。
