# ToDo3755: 最初のツール前のprotocol 429停止

作業ブランチ: scheduler/todo-3755-429。親3628の清掃は複製せず、依存待ちへ変えない。

開始時の確認: 3642/3704の正規statusと該当job.jsonではOpus5.5/max、正規protocol 429、steps/tokens=0、各13分/約5時間後のrecovery cancelを確認した。手動復旧で完了したレビューと、429/404/未読の失敗を区別して保持する。旧容量raw state/logや認証ファイルは読まない。

原因: events.rsは正規errorをquotaとして保存するが、jobs.rsのsupervise_attemptは子のpollが終了するまで待ち続ける。retry_429/capacityおよび通常schedulerのquota_recoveryへ届かない。

実装方針: 既存supervisor内で最初のツール前の正規capacity/quotaエラーに有限の猶予を設ける。実ツール・通常完了・新しいturn・認証/取消を区別し、既存pause_idle_agentで凍結、元の所有/世代と未実行のtoolを再照合してから保存・排出する。quotaは無条件に別枠へ逃がさず既存schedulerの実OAuth検証へ返す。新しい常駐/監視/タスクは作らない。

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/481 。

確認済み: Red commit 6a156575（nextest 6d2e1239、停止回収のassertで失敗）。最初の9テストは132e76f3で全成功。Windowsの実controllerツリー回収と、同じspecを保つ既存quota証明/CLI/通常ToDo割当/再応答はa4d6a4dcで2/2成功。実プロセスは31.594秒でfailed/quota/drained、元の子と監督の終了を確認。旧監督を生存のままにした初回のquota結合fixtureは「source has not drained」で正しく拒否された。監督終了をcreation timeの交代で正しく表すfixtureへ直し成功し、拒否を製品成功へ書き換えていない。

現在: 変更の影響があるcc-jobs全体と、既存quotaのcc-scheduler/cc-job-policyの関連テスト563件を実行中。workspace全体は実行していない。

残る工程: 関連nextest完了/doctest/Clippy、固定候補の別モデルレビュー、必要CI、通常merge、現用ソースとの差分確認、CAS配布、配布版の有限回収と実OAuth応答の確認、親/元ToDo/Discordへの通知なし結果保存。

一時所有: 129a07b50c418d82809693dd1dc27887。safe-deleteのplan/別呼出し削除/finishで閉じる。新しいbranchへ変更していない。
