# ToDo3238：94dc4c1の有限検査

2026-10-22、既存check3238をジョブ `20261022-161917-todo-3238` で再開した。
前回ec08002の診断生成と今回の検査を区別する。旧記録と候補は保持する。

親3085の読取元は
`D:/CC/Work/unattended-operations-20261013-wt/todo-3085-capacity-decision`、
ブランチは `scheduler/todo-3085-capacity-decision`、固定HEADは
`94dc4c10a3e857e3b028c83dec675448264b8ea5`。着手時点で変更なし、push先一致を確認した。
最新の共通方針、両作業場所のAGENTS、親の最新仕様と旧0c9仕様を確認した。

既存の生成先 `codex-owned-0c7d583da6c03192db12646d4a0493d3` だけを使う。
通常のcargo_gate、sccache、最大10並列を保ち、全工程を直列で行う。

1. 新規の別checkout移行検査1件を先行する。内部の8caseを8検査と数えない。
2. 先行した1件を除くreview_workspace 4件、current_succession 1件、targeted_dispatch 9件を実行する。
3. scheduler lib 1件、targeted_cli 1件、todo subwork 4件を実行する。ここまで計21件。
4. cc-schedulerとcc-todoのClippyをall-targets、警告禁止で1回行う。
5. 固定ソースからtodoとschedulerをreleaseで1回生成し、指定の新候補へコピーする。
6. コピー元と候補のSHA256・サイズ、候補の独立したリンク数1、helpの終了を確認する。

同じ94dcの正式CIについて親がAPIと完全ログSHAを照合した受領記録は、着手時点で未確認。
今回は指定21件とClippyをnativeで実行する。旧0c9のE0061失敗、旧ec08の成功は流用しない。
実行できる文書例の追加がないことを確認し、4ライブラリのdoctest各0件の既存結果だけを再利用する。

全compile入力の指紋、各工程前後のHEAD・branch・変更なし、全stdout/stderrと結果のSHA、
実ジョブ・モデル・推論設定・割当、実終了を `.ci-work/3085-cargo-parent-source-helper-fixed.private.json`
へ保存する。途中状態と再開手順は `.ci-work/3238-parent-source-helper-fixed-*` に残す。
再開時は記録と実終了を先に確認し、成功済み工程を重複実行しない。

親ソースの編集、新ToDo、直接の子ジョブ、実dispatch、PR、マージ、配布は行わない。
共有ToDoの状態は割当元のschedulerが更新する。子からDiscordへ送信しない。
このブランチ名は変更しない。成果を自分のブランチにcommit・pushして親へ全結果を返す。
親3085・レビュー3090・製品全体の完了とは区別する。本人の対応は不要。

## 実結果

この有限検査は2026-10-22 16:25:41〜16:27:36 JSTに成功した。
正規CLIで照合した実ジョブは上記の番号で、モデルは `gpt-6.1-sol`、推論は `max`、
割当はcargo。全コマンドとrunnerの実終了コード0、全出力の回収を確認した。

| 実行した範囲 | 結果 |
| --- | --- |
| 新規の別checkout移行 | 1件成功。8通りの内部条件を1検査として確認 |
| 残りの指定parity | 14件成功。先行した1件の重複なし |
| scheduler lib / CLI / todo subwork | 1 / 1 / 4件成功 |
| 2クレートのClippy、all-targets、警告禁止 | 終了コード0。Cargo表示16.69秒 |
| todoとschedulerのrelease生成 | 同じ固定ソースと既存生成先から1回。終了コード0、表示16.01秒 |
| todo全体 / scheduler全体 / dispatch-one / dispatch-subworkのhelp | 4コマンドとも終了コード0 |

今回nativeで通った検査は重複なしの21件、失敗0件。CIの再利用は0件。
文書内コードを試すdoctestは、4ライブラリとも過去の実結果が各0件だった。
追加された文書コメント2行に実行できる例がないことを差分で確認し、その結果だけ再利用した。
runnerの補助抽出はこの2行を取りこぼしたため、独立した最終照合に正しい2行を保存した。
元の実行記録は変更していない。製品検査を繰り返す必要のある不具合ではなかった。

候補は次の2ファイル。絶対パスの共通部分は、この作業場所の `.ci-work/`。
生成終了後にコピーし、両方とも生成元とのSHA256・サイズ一致と、独立したリンク数1を確認した。

| ファイル | bytes | SHA256 |
| --- | --- | --- |
| todo-3085-parent-source-helper-fixed.exe | 7,915,008 | `7c3c04fbea7768379eaa7c0bc3a281ad4467d9bc7515955831ab50bbb6308d32` |
| scheduler-3085-parent-source-helper-fixed.exe | 6,506,496 | `57d8b409c3e42b574546675e3fbe335da1fb6b01110518862944e871bf51acd6` |

正式な実行記録は `.ci-work/3085-cargo-parent-source-helper-fixed.private.json`。
SHA256は `731586b6d3fe3c9e6d3c1dac089b3a07d4bbbf59adb4bd0bd3611c6ba15ec1b0`。
最終照合は `.ci-work/3238-parent-source-helper-fixed-final-verification.private.json`。
SHA256は `74663205ba0e9232a065485bf21ab25e9d7987c584de5ebdf0ed12828dcb8e5a`。

全11コマンドとrunnerのstdout・stderr・結果JSON、計36ファイルを照合した。
親HEAD・branch・変更なしと242入力の指紋は工程前後26時点で一致し、終了後にも再照合した。
旧候補・旧受領18ファイル、ツールとツールチェーン、生成先の所有IDも保持されている。
runnerの終了コード0、排他解放、生成先と候補の実行中プロセス0を確認した。

この3238の達成率は検証済み100。共有ToDoの状態更新はschedulerが行う。
自分のジョブ全体の終了状態は最終返信後に親が正規metadataで確認する。
正式CIの最新成功は今回受領しておらず、この結果はnative検査の成功を示す。
親3085・レビュー3090・実dispatch・最終manifest・配布・実周期の確認は親の継続範囲。
旧476件、指定外のparity、全workspace、product_reviewは実行していない。

本人の対応は不要。子からDiscordへの送信は行っていない。
親がこの結果を受領・永続化し、既存の製品別投稿へ通知・メンションなしで記録する。
候補・生成先・証拠は親の受領と次の工程に必要なため保持する。清掃失敗や判断待ちではない。
作業ブランチ名を変更していないため、workspace checkpointの対象となるブランチ変更はない。

## 再開時に残る工程

この有限検査で残るのは結果のcommit・pushと親への返却だけ。
再開時は本書と正式記録・最終照合・実終了を先に確認し、検査やビルドを重複実行しない。
