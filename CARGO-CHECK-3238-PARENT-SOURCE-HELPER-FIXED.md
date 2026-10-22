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

## 再開時に残る工程

指定21件、Clippy、候補生成とhelp、完全記録の最終照合、結果のcommit・push。
