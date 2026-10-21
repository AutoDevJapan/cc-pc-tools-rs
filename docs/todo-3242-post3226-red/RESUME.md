# ToDo3242: 固定ソースの追加非表示検査

ToDo3103 / Issue289 / PR302 の追加4件を、修正前の固定ソース a1aafe73c9e0564a79dc11d67de4fea362ca6020 で一度だけ測定する。

## 再開時に最初に確認すること

- `run.json` / `run-start.json` / `stdout.log` / `stderr.log` の有無を確認する。開始済みならCargoを再実行しない。
- 生のGit archiveを所有付きの一時フォルダーに展開する。親の作業中ソースは使わない。
- 全ファイルのGit blobとSHA256、改行数を実行前後で照合し、同じソースだった証拠を残す。
- 指定コマンドだけを通常のCargo共有ゲートとsccacheで実行する。GPU、Civ6、全件検査、Clippy、release、本番操作は禁止。
- 実結果・終了コード・全文stdout/stderrを保存して、この作業ブランチへcommit/pushする。
- 所有付き一時データをplan → 別呼出しの通常削除 → finishで片付ける。拒否なら再試行せずquarantineする。
- 実HEADとCC_JOB_IDでscheduler checkpoint-workspaceを保存する。親のコード・PR・ToDo状態は変更しない。

現時点: 保護判定、GitHub所有者/OAuth、固定コミットを確認済み。最初のgit archiveで1,167ファイルの改行変換を検出したため、Git blobから生のtarを作り直した。1,186ファイル（12,765,248 byte）の全バイトがGitと一致。指定検査は未実行。結果保存・push・後片付けは未完了。

検査の測定だけを担当する。修正・配布の完了とは扱わない。通常CLIの最終応答で親へ返し、受領用helperや子Codex、Discord通知は起動しない。
