# ToDo3227 限定比較検査

指定の1コマンドを1回だけ実行し、失敗済みのSolへ戻る不具合を再現した。2件中1件が想定したassertで失敗し、作者履歴の陽性対照1件は合格した。
検査プロセスの実終了コードは100。コンパイル失敗0件、fixture失敗0件。398件は選択外で実行していない。

対象はAutoDevJapan/cc-pc-tools-rsの比較ref `scheduler/todo-3081-review-recovery-red`、固定HEAD `4b2cf1da5321600fe5443c272191de9e7372edea`。
製品ベースは`027b7ecce88cff154bcd617aa0e7f24be6ac1d3f`。差分はテスト2ファイルの23行追加・2行削除だけで、製品とfixtureには手を加えていない。

```text
cargo nextest run --locked -p cc-jobs --test parity -E 'test(review_model_recovery_excludes_the_failed_model_only_for_current_routing) | test(review_after_writer_exit_uses_durable_model_history)' --no-fail-fast --status-level all
```

| ケース | 実結果 | 到達した箇所 |
|---|---|---|
| `review_model_recovery_excludes_the_failed_model_only_for_current_routing` | 実Red | CLI登録と保存済みジョブの取得を通過し、parity.rs:2085:9で失敗 |
| `review_after_writer_exit_uses_durable_model_history` | 合格（陽性対照） | 全assertを通過 |

Redの記録は`review registration routed straight back to the model that just failed`。
最初のassertが失敗したため、保存モデルは`same_model(saved.model, "gpt-6.1-sol")`でSolと同じと判定されている。保存文字列そのものはログに表示されていない。
後続のOpus除外・effort max・作者履歴・除外履歴・model_recovery_fromの5つのassert（2087〜2091行）は未到達であり、合格と扱わない。

実行日時は2026-10-21 09:46:43.4563222〜09:47:22.2308886 JST。全体38.775秒、コンパイル34.13秒、検査実行の集計0.052秒。
割当cargoで共有ゲートをCREATE_NO_WINDOW付きで利用し、所有記録のある専用targetと既存sccacheを使った。ビルド・検査の要求上限は10、共有ゲート設定のjobs_per_buildは8。
プロセスの子を観測した時点では既に終了していたため、実際の並列数を実測したとは扱わない。

完全ログはstdout.txt（0 byte）とstderr.txt（59,988 byte）。Cargoと検査の出力はstderr.txtにある。
SHA256はstdoutが`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`、stderrが`bb59ce2f62ae455200cd65dcc64fdee3cc8c6a0e0ca580ce825401fae8ce95d0`。
このフォルダの.gitattributesで改行変換を無効にし、原ログのバイト列を保つ。

所有生成物1,923,350,442 byte・1,906項目は、planが指定したsize理由のquarantineで削除希望フォルダへ退避済み。元の場所の不在と退避先の存在を確認した。削除や空き容量回収とは扱わない。完了記録はcleanup-receipt.jsonに保存した。
この結果は元ToDo3081/Issue277の限定比較検査であり、製品全体の完成、修正後のGreen、別モデルレビュー、配布の完了を示さない。
主担当のフォルダや状態・担当・依存は変更しない。比較refをmainへ統合せず、PR作成、マージ、追加検査、実AI要求、Discord通知は行わない。
