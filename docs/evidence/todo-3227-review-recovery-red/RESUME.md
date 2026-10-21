# ToDo3227 再開記録

固定比較ソース4b2cf1da5321600fe5443c272191de9e7372edeaで、指定されたcc-jobs/parityの2件を1コマンド・1回だけ実行する。
製品ベースは027b7ecce88cff154bcd617aa0e7f24be6ac1d3f。製品とfixtureは変更しない。
自分のbranchはscheduler/todo-3227-review-recovery-check、jobは20261021-094050-todo-3227。
作業開始時のscheduler checkpointは実HEAD4b2cf1dで保存済み（共有状態を読み返して照合）。

実行前。capture-once.ps1はrun-intent.jsonを排他的に作り、同じ実行を重ねない。
中断した場合、run-intent.json、run-metadata.json、stdout.txt/stderr.txt、PID/作成時刻を先に確認する。
意図記録があれば、結果が不明でも検査を再実行しない。原ログを保持して原因を調べる。

所有一時フォルダのIDは0611463fdafba59587d653031ed6fedb。
ビルド生成先と試験中の一時データだけをこの所有フォルダに置く。原ログはこの証拠フォルダへ残す。
検査終了後、到達assert/未到達、陽性対照、コンパイル/fixture失敗を区別してresult.mdへ書く。
完全stdout/stderr、実exit、source/時刻/資源、SHA256を保存し、このbranchだけcommit/pushする。
生成物はsafe-deleteのplanが返す手順で片付ける。比較refをmainへ統合せず、PRも作らない。
最後に自分のjob・実branch/headでscheduler checkpointを保存する。
自分と親のToDoの状態・担当・依存は変更せず、完了判定はスケジューラに任せる。
