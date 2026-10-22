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
- 07:50の再開: 3208の実Opus5.5/maxレビューはMust0。原文と出所はTODO-3189-DELTA-REVIEW.private.json。
  a14fe15のCodeQLは成功したが、Build speedは1304中1件が失敗。原因候補のjunction経由のGit比較を
  実Windows junctionでRed再現し、既存resolveを使って修正した。次のCIで実失敗の解消を確認する。
- 3208のS1も実ロックでRed再現した。回収時だけロックを保持し、解除の前に解放すると、
  回収できなかったIDのkeyまで消えた。失敗IDを同じ回の依存解除から外す最小修正を追加した。
  他の完了した待機は解除され、次の回に元の実receiptが回収されることを確認した。
- 最終差分の影響範囲18検査はすべて成功（16件とエラー名の検証2件の結果を合わせる）。
  対象Clippy/doctestも成功。共有実行物と3126への実反映はまだ行わず、最終差分だけを再確認する。
- 09:12の再開後: 最終レビュー3218も実Opus5.5/maxでMust0。
  d22e1b1のBuild speed39512030892は1306検査すべて成功、必要CodeQLも成功した。
  PR328を通常mergeし、main58ddc7d57d501fb7b0d753d0c44df696b9063af9へ取り込んだ。
- 実3126の全条件一致と元行SHAを再確認し、正常CLIで実終了済みjob042842を履歴へ回収した。
  saved=true、同じ根拠の再実行はsaved=false/already_recorded=true。通常list/APIでも
  statuswait/wait_on3189/phasewaiting/activeJobnull、jobs4件を確認した。
  元branch/HEAD94ed755を保持。メモ・原資料・他担当を変更していない。
- 共有scheduler.exeは正式guard、元SHAb9bd54a0、元ソースの並行変更なし、既存scheduler.lockを照合し、
  同じ実行物SHA5cae04f7へ原子的に置換した。旧版backupをこの作業場所に保持した。
  配布先CLIでも回収済み受領のno-opを確認。原応答・source/hashはACCEPTANCE/DEPLOYMENT.private.json。
- 元3126へ通常noteのCASで結果と次の実担当のcheckpoint手順を追記し、正確な本文を読戻した。
  3189の担当が3126を名乗るcheckpointは実行しない。依存解除後の本人担当が保存する。
- 残件: merge後CI39520384110が1306中1件、期限付き割当の実時計依存で失敗した。
  今日09:00 JSTの固定期限到達を原因として確定し、同じ失敗をRedで再現した。
  scheduler配布と3126回収は成功済みで再実行しない。cc-todoの時刻注入を検査だけに分け、
  product_review4件、対象Clippy/doctestはGreen。仕様はdocs/product-review-test-clock-3189.md。
  本番の期限や通常CLIは変更せず、このCI修正だけを別PRでレビュー/必要CI/通常mergeする。
  現branchはscheduler/todo-3189-ci-expiry。保存済みbranchからの変更を正規checkpointで保存する。
- 一時所有ID: 27075b23a6091a25d55408958e1de949。
  証拠はprivate記録として保持。元資料と他担当の成果は片付けない。
