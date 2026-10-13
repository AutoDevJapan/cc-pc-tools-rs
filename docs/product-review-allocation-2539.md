# 元2407の正規レビュー割当

2026-10-13の本人指定範囲だけを、元2407・親1626・原担当・原仕様SHA・PR24の固定headへ接続する。期限は2026-10-21 00:00 UTC。原保護一覧の復旧や一般受付の解除ではない。

`todo-review-allocation-2539.exe product-review-allocate` は事前照合だけを行う。`--apply` は正式guardの再検証後、scheduler.lock、todo.lockの順に排他し、元の全値と検証receiptを保存する。旧keyを保持し、空の資源を配列に正規化し、実際のGit所有情報へ結ぶ。本人停止・未完依存・別の受付や担当は解除しない。

割当保存直後はdispatchable=false。排他を保持したまま、通常のworktree準備で指定headの別reviewworktreeを作る。その後だけ起動可能にし、通常の資源・容量・所有照合、subwork_admission、標準run、登録・実行時のrefresh_jobを通す。未起動ならfalseへ戻す。旧resident schedulerへ未準備の起動可能な項目を渡さない。既存stateを読み、通常persistへ結果を保存する。

配備は正式guardの限定差分と、同じ元台帳・通常ロックを使う側置CLI2件（todo-review-allocation-2539.exe、codex_jobs-review-allocation-2539.exe）。共有todo/scheduler/codex_jobsの並行変更を置き換えない。正式guardは直前ハッシュを照合して旧ファイルを保持する。

同じkeyの担当登録が存在すれば再起動を拒否する。親1626は2407待ちのまま。レビューはread-onlyで、最後の正規応答だけを返す。結果ファイルの保存と元2407の完了更新は主担当が行う。登録だけを実起動やレビュー完了と報告しない。

PR152の資格・原状態保存を前提とする最小差分。CI基盤PR147/148/151/153の全件検査を重ねず、関連検査と別モデルレビュー後に最終ソースの必要CIを確認する。Sourceの統合・マージは親PR144へ返し、競合する親sourceを上書きしない。
