# ToDo3238: 親3085の固定版を検査する

この担当は検査と未配布の実行ファイル候補の生成だけを行う。
親ソース、親ブランチ、配布済みの実行ファイルは変更しない。

- 対象: AutoDevJapan/cc-pc-tools-rs (repository ID 1406818713)
- 親ToDo: 3085 / この検査のToDo: 3238
- 実行ジョブ: 20261021-100648-todo-3238（割り当て資源: cargo）
- 親ソース: D:/CC/Work/unattended-operations-20261013-wt/todo-3085-capacity-decision
- 固定ブランチ: scheduler/todo-3085-capacity-decision
- 固定HEAD: 3a9910d7f0ceb9dbdc178ccaa6391b26256b2dda
- この担当のブランチ: scheduler/todo-3238-todo（開始時HEAD: 3e1a211ddeaa9d4d27bdabce12ffc3754863a232）
- 生成先: codex-owned-0c7d583da6c03192db12646d4a0493d3（この作業場所内、safe-delete所有IDも同じ）

開始時に親のHEAD・ブランチ・変更なしを照合した。
正規のscrub391判定は親とこの作業場所の両方でok=true、client_resource=false、classification_unverified=false。
GitHub APIで所有者とrepository IDを確認した。

## 実行する検査

通常のCargo共有ゲートを使い、同じ生成先で次の順に実行する。
各コマンドに親のCargo.tomlをmanifest-pathとして渡す。
既存nextest設定の直列指定とignored検査を維持し、新しいskipは加えない。

1. cargo nextest run --locked -p cc-todo -p cc-scheduler
2. cargo test --locked -p cc-todo -p cc-scheduler --doc
3. cargo clippy --locked -p cc-todo --all-targets -- -D warnings
4. cargo build --locked --release -p cc-todo --bin todo

全workspace検査は実施しない。公開CLIとscheduler境界の統合を対象にする。
releaseビルドは親が取り込む配布候補の生成に必要な1回だけ行う。

## 証拠と再開

標準出力・標準エラー・各終了結果はこの作業場所の `.ci-work/3238-*` に全件保存する。
実行中の検査、子プロセス、完了済み検査は `.ci-work/3238-live-status.private.json` に残す。
再開時はそこにあるプロセスと各検査の結果を照合し、実行中または成功済みの同じ検査を重ねない。
親ソースが変わっていたら追従せず、差異を記録して親へ返す。

全検査成功後、release実行ファイルを独立コピーし、次を確認する。

- `.ci-work/todo-3085-integrated.exe` のSHA256がrelease出力と一致する。
- fsutil hardlink listで候補の現在のリンク数が1である。
- 親ソースのHEAD・ブランチ・変更なしが実行前と一致する。
- `.ci-work/3085-cargo-receipt.private.json` にコマンド、結果、件数、出力証拠、候補情報、production_changed=falseを保存する。

候補とreceiptは親3085が読み返すまで保持する。この担当は配布しない。
最終レビューとマージは親3085/既存3090の範囲に残す。
