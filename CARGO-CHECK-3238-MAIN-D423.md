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

この有限検査は2026-10-23 01:01:59〜01:04:36 JSTに成功した。
正規CLIの現在ジョブ照合では `gpt-6.1-sol`、推論 `max`、割当cargoだった。
Fastの記録はnullであり、有効と断定していない。全コマンドとrunnerの実終了コード0、
全stdout・stderrの回収を確認した。Terminalや管理者確認の窓は開いていない。

| 実行した範囲 | 実測結果 |
| --- | --- |
| cc-todo capacity_cycleの指定結合検査 | 1件成功 |
| cc-todo conversation_waitのWindows向け全件 | 実listとrunがともに40件、全件成功 |
| cc-ask libの指定recovery_messages | 5件成功 |
| cc-ask commandsの指定legacy等 | 6件成功 |
| cc-ask attachmentsの指定検査 | 1件成功 |
| 3クレートのClippy、all-targets、警告禁止 | 終了コード0。Cargo表示11.67秒 |
| todoとschedulerのrelease生成 | 固定ソースと既存生成先で1回、終了コード0。表示38.13秒 |
| todo全体 / scheduler全体 / dispatch-one / dispatch-subworkのhelp | 4コマンドとも終了コード0 |

今回nativeで実行したのは53件、失敗0件。実listの選択名と実PASS名・summaryも一致した。
helperや内部caseは件数に加えていない。旧94dcの21件は今回再実行していない。
同HEADの正式CI成功証拠を実行前に受領しておらず、今回のCI再利用は0件。
旧94dcの正式CI17checkを今回HEAD全体の成功に読み替えていない。

doctestは既存の4ライブラリ各0件の成功出力をSHA・サイズまで再照合した。
基準版からの追加文書コメント10行に実行例がないことを確認し、その実結果を再利用した。
今回の新規doctest実行は0回。既存fixture/mockだけを使用し、実Discord送信は行っていない。

候補の共通ディレクトリはこの作業場所の `.ci-work/`。
両方とも実release終了後の独立コピーで、生成元とのSHA256・サイズ一致、リンク数1、
help確認後の同一SHAを確認した。未配布であり、共有schedulerも変更していない。

| 候補 | bytes | SHA256 |
| --- | --- | --- |
| todo-3085-main-d423.exe | 7,951,360 | `f8710b516d3ecdd37564d7a1ab978c8689ab1bdd2ffaec4150d0e4bed4c0b983` |
| scheduler-3085-main-d423.exe | 6,509,568 | `1e51858986407ae76dc7ada8d86ce940c70107ae5c5c1bdab76dc140985cfe13` |

生成先の `release/todo.d` と `release/scheduler.d` の完全ファイルも保存した。
次のコピーは元ファイルと同bytes・同SHAで、両方とも独立したリンク数1。
正式記録には元の絶対パス、生成HEAD、build終了コードも保持している。

| `.ci-work/`内の依存ファイル一覧コピー | bytes | SHA256 |
| --- | --- | --- |
| todo-3085-main-d423.dep-info.d | 11,702 | `cbe8ce9ad12646bf15fa7ef996db5b916986b06503fbc778492cd84dafa1f0e2` |
| scheduler-3085-main-d423.dep-info.d | 10,186 | `9d5432bdaf1ac0d2ae9db17c434ebe2c08c3ffa3372d66c5ad83771e579ed214` |

正式記録は `.ci-work/3085-cargo-main-d423.private.json`。
SHA256は `0b4ff4cfa5860489ddd51f05d8fddb243bdc95864d6420c1903d2aa9a2b9a4cf`。
最終照合は `.ci-work/3238-main-d423-final-verification.private.json`。
SHA256は `24f4143966e249b2345047a7e694660e9f4790895f5c4f1f182ed37387d9489e`。

全16コマンド（list5回、検査run5回、Clippy、release、help4回）の完全出力と結果を保存した。
最終照合では83ファイルのSHA・サイズを確認した。この数には過去の保存証拠やツールも含み、
今回の新規検査数ではない。入力243ファイルの指紋は工程前後36時点と終了後に一致した。
旧候補・旧正式記録21ファイル、生成先の所有ID、ツールとコンパイラも保持されている。
基準の旧receipt SHA `731586b6d3fe3c9e6d3c1dac089b3a07d4bbbf59adb4bd0bd3611c6ba15ec1b0`
も不変だった。runnerの終了、排他解放、生成先と候補の実行中プロセス0を確認した。

指定の有限検査だけを完了した。親3085・レビュー3090・実dispatch・正式CIの最終判定・
本番への反映と配布・Discord配送は親の継続範囲であり、この結果では完了にしていない。
旧476件、指定外parity、product_review、全workspace、Go capacityの生成は実行していない。
成果と生成先は親が受領して次工程に使うため保持する。清掃の失敗や本人判断待ちはない。
ブランチ名は変更しておらず、workspace checkpointが必要になるブランチ変更はない。

## 終了時と再開時

検査・候補生成・証拠照合は完了済み。自分のブランチに本記録をcommit・pushし、
返却時の実HEADとpush先一致を `.ci-work/3238-main-d423-completion.private.json` に保存する。
自分のジョブ全体の終了状態は最終返信後に親が正規metadataで確認する。
再開時は正式記録・最終照合・runner実終了・completion記録とgitの状態を先に確認する。
未完なら記録の保存・push・親への返却だけを補い、成功済み検査を再実行しない。
共有ToDoの状態はschedulerが更新し、親が結果を永続化して既存製品投稿へ配送・読返しを行う。
本人の対応は不要。子からDiscord送信や新担当起動は行っていない。
