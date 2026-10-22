# 未割当のまま期限取消になった受付の復旧

元ToDo2962、[Issue383](https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/383)。
PR149の初回Windows 3件はrunner ID 0・steps空のまま24時間で取消となった。
CodeQL 2解析は成功し、Windowsの製品検査は0件である。作成したjobの件数と、
実際に実行した製品検査の件数を区別して記録する。

同じ取消jobへ供給せず、受付期限・延期回数・元台帳・失敗結果を消さない。
修復を通常merge・配布し、実供給の準備が整ってから、担当が各Windows workflowの
次の正式attemptを1回だけ正規に要求する。CodeQLは同じHEADの有効な成功を再利用する。
queue自身はGitHubへの書込、rerun、成功statusの作成を行わない。

`replace-expired --config <固定設定> --entry <旧run/1/job> --replacement <同run/2/新job>`
は次を確認して、通常tickが使う予約だけを記録する。

- 元PR149・Windows・initialの既存枠であり、元予約は24時間を経過している。
- 旧公式jobはcompleted/cancelled、APIの待機時刻から24時間以上経過、runner ID 0、
  runner名空、steps空。source/run/attempt/job名/全ラベルが元Planと一致する。
- 旧資源を作った場合は、元safe-delete記録のclose・元root不在・SCM停止・
  正式登録不在・privileged collection markerとprocess censusを再照合する。
  作成していない場合は、元entryにcreate intent・PID・helper・root等が一切ない。
- 次の公式jobは同じrunのattempt 2だけ。元Planのsource/tree/PR/workflow/名前を保ち、
  共通contractで現在の所有・head・固定workflow bytes・実jobを再検証する。
  新jobもqueued・未割当・steps空である。外部観測は回収確認の前後に新しく取得する。

旧entry全体と取消API・次Plan・回収証拠を`replacement-2.json`へ排他的に保存し、
SHA256を台帳へ記録する。元entryの期限・延期・PID・回収情報と元resultは保持し、
旧stateだけをrefusedへ閉じ、新keyへの関係を記録する。元entryの変更前の全体は
証拠に残る。保存途中からの再開は同一証拠だけを再利用し、上書きしない。

initial/normalの元予算配列は減らさず、増やさず、元keyを保持する。
初回3枠それぞれに差替えは1回まで。差替えからさらに差替えることはできない。
新しい正式jobにも既存の24時間・最大6延期・Windows 1件・Cargo gateを適用する。
本人が認めた初回6・マージ後6の計12実行上限を維持し、登録・作成・実行・成功を
別々に集計する。実行済みの失敗、別HEAD、回収不明をこの経路へ流さない。

## 作業場所と終了回収

Windows runnerは元2962 worktree直下に新しい所有領域を作る。
`/codex-owned-*/`をGitの除外へ追加し、資格情報をcommit対象へ混ぜない。
この作業場所は継続CIの依存であり、稼働中や継続供給中にgit cleanやworktree削除で
片付けない。将来移す場合は供給元の切替と静止・残資源回収を先に確認する。

D側のqualified CI領域でplanがsize/attributes退避を要求した場合も、通常の
root資格・元記録・停止・全項目不変の確認を経て、同じDドライブへ退避する。
旧C領域のsize/attributes経路と、未知領域・別の拒否理由の扱いは広げない。
Windows親のvolume/file ID/作成時刻は実供給直前に実測値と照合する。
MAX_PATHを含む実製品受入、次回通常eventの供給と回収はまだ未検証である。
