# 最初のツール前に終了しないprotocolエラー

元3628の読取レビュー3642/3704で、Opus5.5/maxが正規errorとして
`Reconnecting... 1/5 (exceeded retry limit, last status: 429 Too Many Requests)`
を返したまま、ツール0・token0で約13分/約5時間残った。正規tail/statusと
該当sessionのturn_contextを照合した。過去のrecovery cancel、404、初回未読と、
後のGrokによる実読了は別結果のまま保持する。

原因は既存supervise_attemptが子CLIの終了を待ち、その後にしか429/capacityの
復旧判定へ進まないことだった。既存supervisor内に次の限定処理を加える。

- 正規error/turn.failedのquotaまたはcapacityを受け、実操作がまだ始まって
  いなければ、単調時計で30秒の猶予を置く。同じreconnect通知は期限を延長しない。
- item本文の429、通常の通信通知、認証、取消、正常完了は回収の根拠にしない。
  既知/未知の操作開始・完了を観測した試行は保護する。steps=0だけでは判断しない。
- 既存pause_idle_agentでcontrollerを凍結し、未知の実行子がいないこと、現在の試行の
  完全なevents、元job/作成時刻/所有/仕様/世代とToDoの停止・待ちを再確認する。
  凍結記録は既存yield-paused.jsonを使い、監督消失時も既存の回復処理へつなぐ。
- stop失敗では既存keep_pausedを使い、部分停止したcontrollerをDropで再開しない。
  停止意図は呼出側にも保存し、根だけが終了してもdrainedにしない。未確認の同世代の
  collectionは既存quota資格でも拒否し、原失敗と保留を保持する。凍結後のstate lockは
  即時取得を使う。
- 実際のowner_question_wait/decision_waitはstatusがdoingのままでも拒否する。
  正常なnull/未設定や、過去世代のcollection記録は互換を保つ。
- 配布中の旧schedulerが作るv1の復旧票の指紋は変更しない。collection記録は票の
  指紋へ追加せず、受信時のeligible_sourceが現在の実sourceを読み直して未完collectionを
  拒否する。旧票との一致、drainedの受入、stoppingへ変わった場合の拒否を同じ結合テストで確認する。
- 排出前にprotocol_stallのstoppingを厳密保存し、既存の保持ハンドルで子ツリーの
  終了を確認する。readerを排出し、最終状態と同じ厳密保存でdrainedを確定する。
  停止後の中間保存失敗で回収の印を失ったり再試行へ流れたりしない。失敗や不明を
  排出成功にしない。
- capacityは既存の有限な同モデル別OAuth枠/別モデル処理へ戻す。quota回収では
  同じ要求を無条件に繰り返さず、元の失敗を通常schedulerへ返す。既存のquota_recoveryが
  同じToDo・spec本文指紋・世代・読取権限・実装者と別モデル・元子の終了と、
  更新済みの実OAuth枠の枯渇/健康を検証してから次のreviewを準備する。

新しい監視、常駐、タスク、アカウントの一括切替、APIキー、追加支出は使わない。
明示モデル指定や固定契約、未検証の旧spec、本人停止・質問・依存を解除しない。
対応できない条件は元の通常の復旧/技術待ちに保持し、受付やモデル名だけを成功としない。

テストは仮想時計と同期可能なfixtureで、実障害のRed、30秒の境界、reconnect、
ゼロ完了ステップの実行中tool、凍結中の新tool、所有/世代/待ちの変更、取消/認証/
完了、子排出後のcapacity再開と既存quotaのspecを維持した再開を確認する。
Windows実プロセス、別モデルレビュー、CI、merge、配布と実応答は別に結果を記録する。
