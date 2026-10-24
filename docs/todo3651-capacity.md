# 共有ToDoの上限到達時に旧履歴を保管して保存する（3651）

## 仕様と受入条件

通常の書込みは共有のtodo.lockを保持して、変更後のスナップショットを保存する。
UTF-8・LF・1文字インデントでの64MiB上限は変更しない。上限を超える場合に限り、
大きいnote_historyから既存のnote-history-v1形式へ保管する。上限の1/16を空きの
目標とし、目標に届かなくても上限内なら保存できる。履歴以外の情報を容量対策で
削除・変更しない。小さい履歴を保管して参照の方が大きくなる処理は行わない。

保管原文・日時・未知フィールド・順序・重複を完全に保持する。保管先は既存の
ACLとimmutable publicationを使い、読み返したSHA256と内容の一致を確認してから
スナップショットへ参照を保存する。既存参照が壊れている場合はエラーを返す。
保管先の失敗、スナップショットの保存失敗、再試行でも元のストアを保持する。
保存前のクラッシュでできた未参照の同内容セグメントは再試行時に再利用する。

通常保存のCLI、scheduler、jobs、回答受領、Consoleのwriterを同じ契約にそろえる。
明示的なrestore-historyは上限内へ完全に復元できる場合だけ保存し、復元直後の
自動再保管を成功として返さない。historyでのページ読み出しは従来どおり使える。

テストは合成データだけを使い、内部のテスト用上限で通常追記の失敗→成功を確認する。
元履歴の全ページ・復元、現在の全フィールド、並行更新、保存失敗後の再試行と
immutableセグメントの同一性を照合する。実ストアのコピーや試験用の増殖は行わない。

## 原因と対象

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/457

- cc-job-policy/src/todo_store.rsは整形後の上限チェックだけで書込みを拒否していた。
- cc-todoとcc-jobsの一部はこの共通writerを使う。
- schedulerの4経路は独立した事前チェックとwrite_jsonを使う。
- cc-askのatomic_jsonとrecovery_followupにも別の上限チェックがある。
- Consoleはcc_todo::save経由。既存のarchive-historyは通常保存から呼ばれない。

元2641・2737の製品・PR・状態・権限は変更しない。2444は同担当と確認できず保持。
2026-10-25、正規listの対象projectのactive titleをプロセス内で照合し、同種担当は
3651だけだった。モデルへ本文や他製品の題名は出していない。

## 再開記録

- 作業: scheduler/todo-3651-todo-64mib、job 20261025-013135-todo-3651。
- 開始HEAD: 3da96aee。実remote/APIの所有者AutoDevJapan、repo ID1406818713。
- guardはclient_resource=false / access_allowed=true。
- 自分のToDoはschedulerが状態を管理する。独立レビューは共有ToDoの--specで依頼する。
- 一時所有ID: 98007c6a298536f4056526b3bad1fb3d。
- 未完: Red/Green、関連writer回帰、レビュー、CI/merge、PC実行版反映と通常edit、元2641配送。

実測・変更版・テスト・PR・配布の結果を以下へ追記する。記録だけで実反映済みとしない。

### 実装と最初のテスト

- d93ae92aで通常更新が小さい上限を超えてTooLargeになるRedを確認（exit100）。
- 同じ再現は修正後に成功。関連する共通writerの単体テスト10件も成功。
- 自動保管の計画中はファイルを作らない。上限内へ収まる計画だけ公開する。
- スナップショット保存前の失敗・再試行、全履歴の順序/重複/未知フィールド、
  複数セグメントのページ取得と明示復元、壊れた参照、ID重複を小さい上限で確認した。
- 6クレート（cc-job-policy/cc-todo/cc-ask/cc-scheduler/cc-jobs/localai-web-rs）の
  関連テスト2561件を実行中。共有writerとエラー型の利用側を含むためこの範囲を選んだ。
- CLIの64MiB境界と並行CASのテストは合成データだけを使う。実ストアのコピーはない。
- 全件ログは一時所有領域のrelated-nextest.logへ保持。通常の終了記録で結果を追記する。
