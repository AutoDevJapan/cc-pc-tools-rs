# 万世紀CIの定期供給を時間切れで省略しない

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/287

ToDo2962の本番受入で、PR265をマージ・専用runtimeを配備し、既存CC-DiscordAskTickへ設定を部分CAS接続した。
通常tickの実行結果はciv6_ci_queue_skipped_no_timeで、キューのtick.lockも生成されなかった。
既存の受付・scheduler・報告・再試行で50秒のcontextが尽き、最後のCI供給まで進めなかった。

既存処理の50秒を保ち、その後に同じ親context内でpin済みCI queueの15秒を与える。
pipe排出5秒を含む全体上限は70秒。既存タスクの実ExecutionTimeLimitはPT2Mで、設定は変更しない。
既存VBS・1分周期・IgnoreNew・窓非表示・lock・CLI単発配信50秒・pin検査・供給上限を保つ。
新しい監視や常駐処理、token・runner登録・製品jobは追加しない。製品workflowは供給実証まで公開しない。

受入: 通常phaseが自分の期限に達しても、未失効の親からCI queueに有効な期限を渡す。
親の取消・期限は無視しない。通常phaseの状態とerrorを保持し、子contextを解放する。
Red: TestPeriodicPhasesの未実装で失敗。Green: queue既存テストと新しい期限境界、unattended-runner race/vet。
別モデルMust解消・同HEAD正式CI・通常merge・配布後に、既存定期処理の実供給を確認する。
