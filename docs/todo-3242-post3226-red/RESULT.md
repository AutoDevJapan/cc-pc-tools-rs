# ToDo3242の測定結果

指定の固定ソースで検査を1回実行し、コンパイル後に **2 FAIL / 2 PASS、exit 100** を確認した。この測定だけが完了した。ToDo3103の実装・レビュー・配布の完了ではない。

- source: `a1aafe73c9e0564a79dc11d67de4fea362ca6020`
- 実装の基準: `d4e1379d1302eb9277f373766fb3d51d418fbe40`。差分は追加test/specだけ。
- 実行日時: 2026-10-21 10:48:52.0847345〜10:49:38.8223110 +09:00
- コンパイル45.12秒、起動から排出完了まで46.7375765秒。
- nextest: 選択4件、実行4件、成功2件、失敗2件、対象外481件。20 binaries。実行ID `56774bb9-b9d5-4976-a883-86a458962b43`。
- 通常のcargo_gate、sccache、所有付きの専用targetを使用。要求した並列上限は10。論理コマンドは [command.txt](command.txt)、起動情報は [run.json](run.json)。

| 対象 | 実測 | 確認できた内容 |
| --- | --- | --- |
| cc-job-policy `mcp_effective_private_task_masks_every_raw_job_edge` | FAIL | 有効な受託への解決は通ったが、別主担当の関連reviewがsensitiveにならない。`product_identity.rs:290:5`。 |
| localai-web-rs `mcp_effective_contract_identity_masks_related_job_details` | FAIL | `related_todo_ids`条件のtodo詳細はErr(404)。続く関連job詳細が、Err(404)の代わりにreportを含むOkを返した。`operations_product_identity_tests.rs:735:9`。 |
| cc-job-policy `mcp_related_review_preserves_current_primary_and_dependency_priority` | PASS | reviewの主担当9、現在の受託の帰属、必要工程の順位10を保持。 |
| cc-job-policy `mcp_live_requester_dependency_exclusion_controls` | PASS | 既存5条件と追加の`terminal_requester`をすべて完走。 |

API検査は最初の`related_todo_ids`条件のjob詳細で停止した。その条件の後続collect確認と、次の`jobs`条件全体には到達していない。これらを成功・失敗の件数に足していない。直接呼び出したworker_detailの返却値を測っており、HTTP通信の追加検査はしていない。

全文の [stdout.log](stdout.log) と [stderr.log](stderr.log)、機械可読の [result.json](result.json) を保持した。コンパイルや起動の失敗ではなく、2件とも指定箇所のassert失敗である。再実行、Goの再測定、他のCargo検査、本番操作はしていない。

## 固定ソースと改行の照合

最初のgit archiveは`core.autocrlf=true`によって1,167ファイルを改行変換していた。測定前の照合で検出して却下し、コンパイルには使わなかった。元tarのSHA256は [archive.json](archive.json)、変換された各ファイルの前後SHA256は [archive-line-ending-conversions.json](archive-line-ending-conversions.json) に保持した。

共有Gitオブジェクトを`git cat-file --batch`でバイナリとして読み、生のPAX tarに保存してから展開した。[raw-archive.json](raw-archive.json) にそのSHA256と作成方法を記録した。Gitのclean/smudge・改行変換を通さず、全1,186ファイル（12,765,248 byte）の生blob SHA1、SHA256、改行数を実行前後で照合した。追加・欠落はなく、Git blobとの全バイト一致と前後不変を確認した。

全sourceの前後manifest SHA256はいずれも `9308027f93c7ab25f322bfb495b4847f1ba8af4ee8c9f6ef51f1a5c3588e24d3`。[source-invariance.json](source-invariance.json)、[source-before.jsonl](source-before.jsonl)、[source-after.jsonl](source-after.jsonl) が証拠になる。各証拠ファイルは改行変換せずGitへ保存する。

現時点では証拠のcommit/push、所有付き一時データの通常削除、schedulerの再開情報保存が残っている。親のコード・PR・共有ToDo状態は変更しない。最終応答を通常CLIのwait経由で親が受領し、受領用helper・子Codex・Discord通知は使わない。
