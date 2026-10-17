# Operationsの既存タスクによる再起動（ToDo2793）

Operationsの起動actionは、サーバーの終了まで待つ。再起動APIは既存タスクへ実行を要求し、500ms後にサーバーを終了する。IgnoreNewでは、実行要求時に現在のactionが残っているため、次の起動が取りこぼされる。

管理対象のactionを読み戻すGoの検査は、次の設定を要求する。

| サービスID | MultipleInstances | ExecutionTimeLimit |
| --- | --- | --- |
| operations | Queue（1） | PT0S |
| opencodex / godd_ao / opencodex_watchdog | IgnoreNew（2） | PT0S |

[WindowsのQueue仕様](https://learn.microsoft.com/en-us/windows/win32/taskschd/tasksettings-multipleinstances)では、現在のtask instanceの完了後に次のinstanceを開始する。既存のmutexと待受確認は維持する。Goからタスク設定を書き換える処理や再起動ループは追加しない。

## 主担当が行う配布と実機確認

1. レビューとCI後、既存の実行版を更新する。既存operationsタスクの多重起動設定だけをQueueに変えて読み戻す。actionのパスと引数、PT0S、主体、トリガーを照合する。他サービスのIgnoreNewを保持する。過去の配布スクリプトはIgnoreNewを指定するため、そのまま再実行しない。
2. **通常状態**では、実行中の既存operationsタスクがlauncherを通じて現在のserverの終了を待っていることを、task状態と親子関係で確認する。再起動要求はそのtaskに待機し、serverとlauncherが終了してから次のactionへ引き継がれる。
3. **初回移行**では、現在のserverがtask外で復旧している場合を区別する。Queueは実行中のtaskに対してのみ待機する。taskが待機状態なら要求が即実行され、launcherが既存の待受を見つけて終了しうる。旧serverは既存APIで終了させ、終了を確認してから既存taskを1回起動し、以後のserverがtaskに追跡される状態へ移す。Goによる自動移行は行わない。
4. 管理された通常状態からAPI再起動を2回確認する。各回で応答200、旧PIDの終了、新PIDの単一待受、health回復、既存taskが新serverを追跡することを記録する。初回移行時の追加起動を、通常状態の再起動成功に数えない。

旧VBS actionは移行前の定義として照合可能なまま残す。管理されたactionのoperationsにIgnoreNewが残る場合は定義不一致とする。有効・無効・実行中・キュー待ちの判定は変えない。

本変更の自動検査はtask定義の照合を対象とする。本番への配布、実タスク設定変更、実API再起動2回は主担当の統合作業で確認する。
