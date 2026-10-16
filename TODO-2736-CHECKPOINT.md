# ToDo2736 再開記録

2026-10-17。PR148の通常CIを追加費用なしで回復するための実行環境調査を行った。CIの再実行、workflow変更、GitHubへの書き込み、runner登録、Cargo実行はまだ行っていない。

## 完了した確認

- `D:/CC/AGENTS.md`、共有ToDo/日本語報告/safe-deleteのSkill、当該pc-tools worktreeの`AGENTS.md`を確認した。
- 元2640の`docs/todo-2640-checkpoint.md`と`docs/todo-2640-official-ci-spec.md`を確認した。製品repo上にある規則はGitHub APIから取得した`documents/repository-rules.md`で、製品repoのAGENTS.md/CLAUDE.mdは元記録どおり対象headに存在しない。
- GitHub APIで実所有者AutoDevJapan、repo ID 1392152644、private、non-fork、admin権限、PR148のopen/draft/mergeable、head SHA `75a4051305b6f47c70ddb59a8d2bf9015666f12c`、base SHA `1100676d1ed421794c252fb4b15ed3d954e21a5b`、同一head repo/base repo、実branch refを再照合した。開始時点で指定と一致。
- 指定された既存証拠rootは`client_resource=true`のため開かず、作業前に与えられた原証拠の要約だけを利用する。
- 専用runnerの基盤を確認した。`tools/product-ci`にはNode/CodeQL/secret/advanced用の限定runner経路があり、PR148の通常Rust/native/live-chain用実行モードはない。`tools/linux-runner-bridge`の常駐runnerは別repo専用で、今回のrunner用途へ拡張しない。
- ホストのHyper-Vは有効だがWindows Sandbox機能は無効。Docker DesktopはLinux amd64 engineのみで、Windows runnerを隔離する既存資源は確認できていない。

## 未完了と再開手順

必要な通常CI4件（Rust/Linux、Rust/Windows、native-driver/Windows、live-chain/Windows）の正式成功とrun/job/log証拠配送は未達。PR148のsourceやworkflowは変更していない。

再開時は、他作業を中断させないWindows隔離環境の利用可能性を調べる。Windows Sandboxを有効にする場合は再起動が必要になる可能性があり、現時点ではPC上の並行作業への影響を避けて保留している。Windows隔離実行環境が確保できた後、Goの独立した有限helperとPR148内の必要最小限workflow変更を設計し、別モデルレビュー、共有Cargo gateを用いた実検査、実runner/run/job/check/logの確認、原2640への結果配送を続ける。成功statusの書き込みや既存resident bridgeの権限拡張は行わない。

現worktreeは`cc-pc-tools-rs`の`scheduler/todo-2736-pr148-ci` branchで、製品repoのbranch/headではない。作業treeはこの記録以外clean。再開前にPR148と元2640の状態を再照合する。
