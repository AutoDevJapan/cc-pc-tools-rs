# ToDo 2987 再開記録

対象は非公開の AutoDevJapan/GoDD-Press の既存CIのみ。公開・素材取得・追加課金・GPU利用は禁止。

- GitHub APIで所有者AutoDevJapan、private=true、main `4dc5c332d54d19325f122df1f0e4a52e9dd3a629` を確認。
- 凍結workflow3件と現在のworkflow一覧を照合。通常CIの最終成功はrun 38212791526。10ジョブ合計5308秒（個別ジョブの開始/終了差。請求分数とは区別）。
- rust-coreは1089秒、統合4分割は609/989/1107/768秒。独立ツール、書式/静的検査、Chrome受入が先行条件なしで走る。
- 新規一時取得先と未確認の作業サブディレクトリはguardで未確認/保護扱い。そこでソースは利用しない。拒否資源の複製・別経路参照は行わず、依頼で明示されたGitHub固定workflowと確認済みdocs領域で独立工程を進める。
- 元のソース・資料は保持。ローカルの重いCargoは未実行（資源割当なし）。
- 必須条件: 変更判定の実装と回帰検査、別モデルレビュー、指摘修正、最新候補CI成功、通常merge、効果の実測。請求減は設定だけで断定しない。

次: 補助ツールと本体が読む入力を確認し、軽い検査を先行させる実workflow差分を作る。GitHubへの書込は元private repoの専用branch/PRだけ。公開された運用repoへprivate sourceやworkflow本文をコミットしない。
