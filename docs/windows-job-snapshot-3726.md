# Windowsのjob.json置換失敗（ToDo3726）

元3700/3686を継続するためのcc-jobsの修復。原ジョブ本文、認証、モデル設定、
課金、保護原本、全体queue設定は変更しない。

## 症状と確認範囲

元3700が記録した対象2件は`job.json.<pid>.tmp -> job.json`のos error5でfailed。
記録時にはpid/codex_pidが不在、属性はArchiveだった。
当時の読み手、ハンドル、ACLのどれが実際の拒否元だったかは確定していない。

起点8ed37bbeのwrite_snapshotは一時ファイル書込とrenameをそれぞれ
最大1秒/50回再試行する。進捗saveの失敗はsupervise_attemptのwork全体へ
伝わり、子の停止とfailed処理を起こす。再試行が無いという原因説明はしない。

現用exeのSHA256は`98e8279c8225e7fa553b5e1a182b4292003ff5a5793234f1e7bb963ed1c12084`、
9006592bytes。旧2458のsource sidecarは別hashであり、この実物のsource証明にはならない。
現checkoutやmtimeから同一sourceとは判断していない。
実物を専用fixtureへコピーして同じ共有保持を起こしたところ、
supervisorはexit75、子も終了し、job.jsonはrunningのまま残った。
対応する実プロセスハンドルの終了観測とos error5を確認した。
この実測は当時の全ソースを復元した証明ではない。

## 修正の契約

- 保存済みsnapshotと比較し、activity/activity_detail/last_event_at/steps/tokens/
  context_pct/context_pct_exact/rate_limit_pctだけが変わった更新を延期可能とする。
  新しい未知の項目は必須保存側になる。
- 表示更新の失敗はマスクした診断に記録し、子の監視とイベント回収を続ける。
  最新のメモリ上の値を保ち、3秒後以降にイベントが無くても再保存する。
  復旧も診断する。同じ失敗をイベントごとに連続出力しない。
- 起動・所有・キャンセル・再開ID・結果・終了状態の保存失敗は無視しない。
  終端の保存が失敗したら、終了状態と具体エラーを診断し、非成功を返す。
  元job.json、一時snapshot、イベントと結果本文を保持し、成功通知や再起動へ進まない。
- 交代の可能性がないtool完了で不要な必須saveを行わない。
  実際の交代は従来の必須保存・排他を通す。
- error5やPermissionDeniedを共有違反と決めつけない。ACL変更、権限昇格、
  非原子的な上書き、削除後再作成などの別経路は追加しない。

通常のRustのFile::openは削除共有を許す。Windows実テストでは、開いた旧ハンドルは
旧内容を読み続け、パスからは置換後の完全な内容を読めた。
削除共有を外した読み手ではos error5を再現した。元の2件の読み手の特定とは区別する。
Microsoftの[CreateFileW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew)と
[MoveFileExW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-movefileexw)の
削除共有・rename・ACLの契約を参照した。

## テスト

Windows専用のsynthetic fixtureだけを使う。実job、実API、Discord送信、認証情報は使わない。
共有解除は最初の失敗試行から明示し、期限超過は解除せずに再試行の完了を待つ。
ACL拒否は専用ファイル/フォルダのDELETE/DELETE_CHILDを拒否し、RAIIで戻す。
原資料や稼働中のジョブのACLは変更しない。

Red: 修正前の6件中、共有保持中の進捗でsupervisorが失敗する1件だけが失敗。
正常reader、共有解除、期限超過、ACL拒否、必須保存の陽性対照5件は成功。
現用exeの独立した実プロセス試験でも終了を観測した。
Green初回: 上記6件と実プロセスの継続・wait返信・終了の計7件が成功。
実プロセス試験は保存済み再開IDを確認してから読み手を保持し、診断の到達を待つ。
保持中に同じsupervisor/childの生存を確認し、解除後のDONEと両者の終了を追う。

公開Rust APIやsnapshot schemaは変えない。cc-jobsにRustの逆依存は無いが、
CLI利用側のcc-scheduler/cc-askの関連テストを含め、doc/Clippyも確認する。
全workspaceを無条件で再実行しない。
別モデルレビュー、最終headのCI、merge、実配布とそのhashは、実施後に記録する。
