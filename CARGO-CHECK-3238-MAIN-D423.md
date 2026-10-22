# ToDo3238：現用の回答配送を保持した統合版の有限検査

2026-10-23、既存check3238をジョブ `20261023-005540-todo-3238` で再開した。
親3085の固定入力は `8b996cb9a6ece6160527dbaf4a4bb0534804a48d`、
ブランチは `scheduler/todo-3085-capacity-decision`。変更なしとpush先一致を確認した。
読取元は `D:/CC/Work/unattended-operations-20261013-wt/todo-3085-capacity-decision`。

旧94dcの21件成功は過去の結果として保持する。main `d423c9d` を統合した今回の
回答配送・通知処理との結合を追加確認し、古い候補を現用版へそのまま配布しない。
親ソースは3085だけが編集する。この子は実装、実dispatch、PR、マージ、配布を行わない。

## 今回の範囲と再開手順

既存の所有ID `0c7d583da6c03192db12646d4a0493d3` の生成先だけを使う。
通常のcargo_gate・sccacheと既存の最大10並列を保持し、次を直列に実行する。

1. cc-todo capacity_cycleの指定結合検査1件。
2. cc-todo conversation_waitのWindowsに適用される全件。実listとrunから件数を数える。
3. cc-ask libの指定recovery_messages 5件。
4. cc-ask commandsのlegacy等6件、attachmentsの指定1件。
5. cc-todo、cc-scheduler、cc-askのClippyをall-targets・警告禁止で1回。
6. 同じ親Cargo.tomlと生成先からtodo・schedulerをreleaseで1回生成。
7. `.ci-work/todo-3085-main-d423.exe` と `scheduler-3085-main-d423.exe` へ独立コピーし、
   全体help2種、dispatch-oneとdispatch-subworkのhelpを確認する。

親が受領した同じ8b996cbの正式CI成功証拠が先にあれば、必要な成功範囲だけ再利用する。
旧HEADのCIを今回全体の成功へ流用しない。文書内の実行例が増えていないことを確認し、
対象ライブラリのdoctest各0件の既存実結果は再利用し、今回の新規実行と数えない。

各工程前後にHEAD・branch・変更なし・入力ファイルの指紋を照合する。
完全なstdout、stderr、argv、実終了コード、実件数、SHA256、実モデルと推論設定、
cargo割当、生成先の所有照合を `.ci-work/3085-cargo-main-d423.private.json` に保存する。
release/todo.dとscheduler.dの元パス・完全SHAを保存し、今回専用の独立コピーも保持する。
旧候補・旧証拠を上書きしない。新しい生成先、Go capacity、全workspace検査を作らない。

途中状態は `.ci-work/3238-main-d423-*` に保存する。再開時は正式記録、完全出力、
runnerの実終了を確認してから残りだけ進める。失敗時は具体原因を親へ返し、
親ソースを横から直したり、成功済みの検査を盲目的に反復したりしない。

共有ToDoの状態はschedulerが更新する。子からDiscord送信や新担当起動はしない。
作業ブランチ名は変えず、この作業場所の記録だけcommit・pushして親へ返す。
親3085、レビュー3090、実dispatch、配布と製品全体の完了は今回の完了に含めない。
本人の対応は不要。親が受領・永続化し、既存製品投稿へ記録して配送を確認する。

## 実結果

未実施の工程を成功と扱わない。実行後に実測結果と記録のSHAをここへ追記する。
