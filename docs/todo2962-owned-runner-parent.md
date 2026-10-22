# Windows runnerを本人用Tempの外へ置く

元ToDo2962 / PR149。runner 2.338.0の起動前の権限確認で、本人用Tempフォルダーを
列挙できずexit1になった。登録tokenを使わない専用のWindows仮想アカウントで再現した。
暗号化API（Machine DPAPI、CNGのRSA2048生成）は同じ制限付きアカウントで成功した。

[公式Program.cs](https://github.com/actions/runner/blob/v2.338.0/src/Runner.Listener/Program.cs#L82)
から呼ばれる[ValidateExecutePermission](https://github.com/actions/runner/blob/v2.338.0/src/Runner.Sdk/Util/IOUtil.cs#L397)
は、runner設置先からドライブの根まで各階層を列挙する。本人用Tempの権限を変更したり、
この確認を無効化したりしない。

新しいWindows検査の一時領域だけ、既に資格を確認した元2962の作業フォルダー直下へ
`safe-delete create --root`で作る。作成先はコードで固定し、設定や外部入力では変更できない。
返された所有記録の親が固定先と一致しなければ起動しない。旧Cドライブの記録は保持する。

同じ仮想アカウント、同じrunner asset、同じ無効な固定tokenを使い、作業フォルダー直下では
起動前確認を通り、GitHubの認証APIへ到達して期待どおり404になった。実登録や製品検査の
成功とは区別する。2件の試験とも停止・Job Objectの排出・通常collect成功を確認した。
本人用フォルダーを含む既存フォルダーのACLは変更していない。新規GPU/Civ6/クラウド利用はない。

終了回収も同じ作成先へ合わせる。元の所有記録・固定repo/source・有限台帳・停止SCM・
回収marker・公式job・全項目の不変性を確認する条件は維持する。D側の親は実測した
volume/file ID/作成時刻へ束縛する。削除希望へ移す場合は同じDドライブへ移し、
資格が無い資源や他の作成先の退避先は変えない。

## 未完了

本修正のレビュー、必要な正式CI、通常merge、同sourceからの配布と実jobでの供給・回収は未完了。
旧3jobは未割当のままGitHubの24時間待機期限で取消となった。旧予約の期限や履歴を
書き換えず、有限の受付をどう復旧するかは別に確認する。同じ取消jobへの供給を再開したり、
成功statusを作ったり、受入予算を増やしたりしない。
