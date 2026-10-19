# ToDo 2987 再開記録

対象は非公開の AutoDevJapan/GoDD-Press の既存CIのみ。公開・素材取得・追加課金・GPU利用は禁止。

- GitHub APIで所有者AutoDevJapan、private=true、main `4dc5c332d54d19325f122df1f0e4a52e9dd3a629` を確認。
- 凍結workflow3件と現在のworkflow一覧を照合。通常CIの最終成功はrun 38212791526。10ジョブ合計5308秒（個別ジョブの開始/終了差。請求分数とは区別）。
- rust-coreは1089秒、統合4分割は609/989/1107/768秒。独立ツール、書式/静的検査、Chrome受入が先行条件なしで走る。
- 新規一時取得先と未確認の作業サブディレクトリはguardで未確認/保護扱い。そこでソースは利用しない。拒否資源の複製・別経路参照は行わず、依頼で明示されたGitHub固定workflowと確認済みdocs領域で独立工程を進める。
- 元のソース・資料は保持。ローカルの重いCargoは未実行（資源割当なし）。
- 必須条件: 変更判定の実装と回帰検査、別モデルレビュー、指摘修正、最新候補CI成功、通常merge、効果の実測。請求減は設定だけで断定しない。

次: 補助ツールと本体が読む入力を確認し、軽い検査を先行させる実workflow差分を作る。GitHubへの書込は元private repoの専用branch/PRだけ。公開された運用repoへprivate sourceやworkflow本文をコミットしない。

## 2026-10-19 候補保存

- private repoの作業branch `scheduler/todo-2987-ci` へGitHub Git Data APIで4ファイルをコミット。固定候補 `9f5a18bb182ac57006744bb5d6d194e1485fc919`、PR https://github.com/AutoDevJapan/GoDD-Press/pull/176。
- 確認済み作業資源で作った候補payloadは `docs/todo2987-patch.private.json`。公開運用repoへコミットせず保持。元workflow本文・製品source・素材を公開しない。
- 変更判定6検査、実Git merge/revert/rename/delete/missing-base、rustfmt、actionlint 1.7.12構造検査は成功。
- 変更判定は独立bridge/signing以外の全pathで全件を選択。signingの本体依存を保持。
- 同じrunner/targetで本体lib/CLI/examples/doc/build・統合4分割・Chrome受入を実行。library/Clippy/docはworkspaceを含む。冷起動専用runnerとrelease launcherは維持。
- 別モデル読取reviewはToDo2991（通常scheduler受付）。初回model表記にslashを含めた登録はCLIのモデル名検証で失敗し未登録。その後claude-sonnet-5-5で1件登録済み。
- 初回CI: local-checks 39245437660、CodeQL 39245437659、Windows fixtures 39245437673、V2 HTTPS 39245437702、Windows Chrome 39245437718。受付と成功を混同しない。まだ再実行していない。
- 親2957へ候補・検証済み・未完了を追記。親依存や既存メモを削除しない。

次: ToDo2991の原応答と実モデルを照合して指摘対応。各CIを固定IDで確認し、失敗は該当ログから原因修正する。最新candidate/mainを再照合し、全必要CIと別モデルMust解消後だけ通常merge。実行回数/runner分数/請求増分は成功後に測定。
