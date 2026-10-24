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
  context_pct/context_pct_exact/rate_limit_pctと、実行中の結果・エラー・使用量の
  射影（last_message/turn_completed/last_error/last_failure_kind/capacity_error/
  rate_limit_times/execution_observed）だけが変わった更新を延期可能とする。
  新しい未知の項目は必須保存側になる。
- 表示更新の失敗はマスクした診断に記録し、子の監視とイベント回収を続ける。
  最新のメモリ上の値を保ち、3秒後以降にイベントが無くても再保存する。
  復旧も診断する。同じ失敗をイベントごとに連続出力しない。
- 起動・所有・キャンセル・再開ID・終了状態と、交代/終端での結果の保存失敗は無視しない。
  終端の保存が失敗したら、終了状態と具体エラーを診断し、非成功を返す。
  元job.json、一時snapshot、イベントと結果本文を保持し、成功通知や再起動へ進まない。
- 交代の可能性がないtool完了で不要な必須saveを行わない。
  イベント有無にかかわらず、実際の交代は必須保存・排他を通す。
- 進捗を書き直す前にstate.lockを取り、現在の所有・実行世代と再開IDを照合する。
  最新snapshotへ自分の変更だけを合わせ、待ち時間中のキャンセルや他writerの
  記録を古い値へ戻さない。所有/停止/終端が更新されていれば古い書込を中止し、
  自分の子を後始末して、更新後の世代を保護する。
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
さらに、開始イベントの保存完了まで待つことで、再開IDと進捗の保存を競合させない。
共有解除後も子には新イベントや終了を許さず、3秒後の再保存と進捗の復旧を
確認してから終了させる。この追加試験も成功した。

公開Rust APIやsnapshot schemaは変えない。cc-jobsにRustの逆依存は無いが、
CLI利用側のcc-scheduler/cc-askの関連テストを含め、doc/Clippyも確認する。
全workspaceを無条件で再実行しない。
別モデルレビュー、最終headのCI、merge、実配布とそのhashは、実施後に記録する。

関連1694件は初回1688件成功、6件失敗、3件skip。失敗2件はテスト用フォルダを
このリポジトリ内へ置いたためプロジェクト名の推論へ影響したものだった。
4件は長い一時パスからtempfileの保存先がWindowsのパス長制限を超えた。
所有確認済みのリポジトリ外フォルダで2件が成功し、同じフォルダのWindows短縮名
（長い名前とFile IDの一致を確認）で残り4件も成功した。
テストのアサーションや製品コードを変更せず、実行環境を訂正した。
対象3ライブラリのdoc（各0件）とClippyは成功した。
結果のハッシュは[検証記録](evidence/windows-job-snapshot-3726.json)に保持する。

## 最初のレビューと追加のRed

3729は固定76fb8af0を読了し、Must1、Suggestion3、Nitを返した。
実jobと対応nativeはGrok4.6/xhigh、原応答SHA256は
`f1dcca9794eb4806babdf062a61e0ce1f67a09962576d22e74b7d61d861622d5`。
これはAIのレビュー意見であり、本人の方針変更ではない。

Mustのうち、実行中のlast_message等の保存失敗が正常終了をfailedへ変える点は
追加テストでRedを確認して修正した。既知の実行中の結果射影は延期し、drainして
実exitとturn.completedを確認してから、終端を必須保存する。
再開IDまで保存失敗を無視する提案は、依頼の「再開IDの永続化を無視しない」に
従い採用しない。再開IDと未知の制御項目は必須のままで、追加レビューへ根拠を渡す。

また、待ち時間中のcancelledを古い進捗でfailedへ戻すRedを独立に確認した。
排他・最新状態確認・差分の適用で修正した。元の停止・終端・別の所有者を
保存後に上書きしない対照も確認した。
無イベント交代の保存、無イベント復旧の実テスト、ACL復元の明示確認、
共有モードの定数名を追加した。未計測のclone最適化は今回の修復へ追加しない。
3733は実入力を照合すると旧293612d6のnative差分だけを読んでいたため、
その範囲で読了・Must0として記録した。新04a42020の確認へは流用しない。
3739は通常選択でOpus/maxになり、quota/429・読取0件で正規recovery終了した。
同じ固定差分を実Grok4.6/xhighの3743が読了し、Must1を返した。
原応答SHA256は`68929fe9031e08d256e21c9c88cedfbdbad68f4944fa50ef8f29374bb4bf1176`。

3743は、再開IDだけがdiskで変わる場合に、監視中の中止と終端の照合基準が
違っていた点を指摘した。同じPID・異なるthread_id/resume_sessionの更新を
終端がfailedで上書きするRedと、diskが自分と同じ新thread_idへ更新済みでも
failedへするRedを確認した。
最後に保存できたsnapshotを終端まで保持し、監視中と終端で同じ所有と再開IDの
照合を使う。再開IDはdiskが基準値か自分の値と同じ場合だけ受け入れる。
異なる値を保存した制御者の状態は上書きしない。
この修正後、5種類の制御更新と一致する再開IDを含む新回帰10件が成功した。
追加差分は固定候補で再レビューし、最終HEADのCI成功までマージしない。
