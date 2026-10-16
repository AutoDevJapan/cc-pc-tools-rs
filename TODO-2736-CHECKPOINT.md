# ToDo2736 再開記録

2026-10-17。PR148の通常CIを追加費用なしで回復するための実行環境調査を行った。CIの再実行、workflow変更、GitHubへの書き込み、runner登録、Cargo実行はまだ行っていない。

## 完了した確認

- `D:/CC/AGENTS.md`、共有ToDo/日本語報告/safe-deleteのSkill、当該pc-tools worktreeの`AGENTS.md`を確認した。
- 元2640の`docs/todo-2640-checkpoint.md`と`docs/todo-2640-official-ci-spec.md`を確認した。製品repo上にある規則はGitHub APIから取得した`documents/repository-rules.md`で、製品repoのAGENTS.md/CLAUDE.mdは元記録どおり対象headに存在しない。
- GitHub APIで実所有者AutoDevJapan、repo ID 1392152644、private、non-fork、admin権限、PR148のopen/draft/mergeable、head SHA `75a4051305b6f47c70ddb59a8d2bf9015666f12c`、base SHA `1100676d1ed421794c252fb4b15ed3d954e21a5b`、同一head repo/base repo、実branch refを再照合した。開始時点で指定と一致。
- 指定された既存証拠rootは`client_resource=true`のため開かず、作業前に与えられた原証拠の要約だけを利用する。
- 専用runnerの基盤を確認した。`tools/product-ci`にはNode/CodeQL/secret/advanced用の限定runner経路があり、PR148の通常Rust/native/live-chain用実行モードはない。`tools/linux-runner-bridge`の常駐runnerは別repo専用で、今回のrunner用途へ拡張しない。
- ホストのHyper-Vは有効だがWindows Sandbox機能は当初無効だった。既存CC-AdminBrokerで追加費用なしに機能を有効化し、DISMでState=Enabledを確認した。窓口の結果はexit=0、RestartNeeded=true。再起動はまだ実施していない。Docker DesktopはLinux amd64 engineのみ。

## 未完了と再開手順

必要な通常CI4件（Rust/Linux、Rust/Windows、native-driver/Windows、live-chain/Windows）の正式成功とrun/job/log証拠配送は未達。PR148のsourceやworkflowは変更していない。

次は、PC上の並行作業を中断しない時機を確認してOSを再起動し、Sandboxの起動可能性を確認する。再起動後にSandbox実行環境が確保できた後、Goの独立した有限helperとPR148内の必要最小限workflow変更を設計し、別モデルレビュー、共有Cargo gateを用いた実検査、実runner/run/job/check/log証拠の確認、原2640への結果配送を続ける。成功statusの書き込みや既存resident bridgeの権限拡張は行わない。

管理者スクリプト用の一時フォルダはsafe-delete plan→通常削除→finishでsource_absentを確認した。共有ToDo2736には60分後の技術再確認を`wait-retry`で記録済み。再起動やrunner登録はまだ行っていない。

現worktreeは`cc-pc-tools-rs`の`scheduler/todo-2736-pr148-ci` branchで、製品repoのbranch/headではない。作業treeはこの記録以外clean。再開前にPR148と元2640の状態を再照合する。
