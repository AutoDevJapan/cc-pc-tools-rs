# ToDo2623: 固定コミットのCI確認

親ToDo2488のPR144、固定コミット b9201e3a8bfd52bf90e17e2ae7a788aafcdabf48、tree 966bff6509ca39ba3f7f8a5de08467c4c2e17cc0について、2026-10-14の有限確認を完了した。

正規8workflowは全てattempt1・completed・success。21jobは20success・1skipped。スキップは対象branchを限定した任意のpackage-toolsで、必須検査の失敗ではない。PR headの全桁一致、PRIVATEと実owner AutoDevJapanを再確認した。

Buildの先行5検査は順に1、1、18、22、12件成功。全workspaceは2164件成功・0失敗・5skip（418.252秒）。docは実行成功、16summary群の試験件数0。全workspace/all-targets Clippyも正常終了した。成功の数値exitは原ログへ独立表示されないため、実行stepのsuccessと完了出力による終了0相当の確認と区別して記録した。

CodeQLの同SHA4言語解析とupload、error空/results0を照合。Browser76pass、Channel Windows157/Linux131pass、Bookmark両OS Rust10/Node14とWindows追加commands34/resume81成功。Go各jobのtest/vet/buildも成功。非verboseの個別case件数は不明で、cachedを今回の実行件数として数えない。

全8runの原全文を通常gh run view --logで取得exit0。原stdoutの全行とbytesが、実行20jobの派生全文へ漏れなく対応した。API metadata・bytes/SHA256・根拠行・原全文は、このworktreeの `.ci-work/todo-2623-b920/` に保存。結果は `result.final.private.md`、索引は `manifest.final.private.json`。私的な記録をGitへ追加しない。

初期の未終端snapshotと再開記録を保持し、全件終端を確認できたため再試行登録は不要。自分の状態更新はスケジューラに任せる。親の最新noteを保持して結果を追記し、親2488全体のmerge・配布・運用受入や原記録復旧の完了とは区別する。

本担当はsource編集、手動Cargo、CI再実行、merge、配布、GPU、Discord送信、子job直接起動を行っていない。