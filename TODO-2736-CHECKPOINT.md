# ToDo2736 再開記録

## 2026-10-17 02時台の再開結果（以下を優先）

job20261017-015924-todo-2736。8db9ee1から再開した。以前の「OS再起動後だけ続行する」案は採用しない。Sandbox有効化・PC再起動・旧cleanupを繰り返していない。

実APIでPR148のowner/private/nonfork/admin、同head/base repo、open/draft/mergeable、75a/head、1100/base、原branch refを再確認。製品規則はAPIで取得し、原固定head/tree/3workflow digestと原4jobをGo helperでも前後照合した。原製品folder、旧検査source、保護/manifest、共有bridgeは触っていない。

tools/product-ciにread-onlyなpr148-planを実装。三workflowのYAMLを構造解析し、有限routeとcheckout head/authだけの案をRAMで生成する。他の全bytesとparsed semanticsを保つことを検証。main/別PRは元Hosted経路。実APIの4jobは未開始・runner0・steps0・予算annotationあり。LinuxのOS移行noticeも存在したため「annotationは1件だけ」という初回の誤った制約を直し、全JSON value hashを保持する。原CI logのhashとは呼ばない。製品source/workflowは未変更、登録/dispatch/Cargo/製品テストは0。

Go unit/vet、Windows buildとLinux cross buildは成功。初回実API planはnoticeのため拒否、修正後に成功。proposal_only=true/all_required_checks_success=false。実装第一保存点822b4cb25934b4c0082bdde4a9d5c686158ee607はpush済み。

既存SDK imageから新しい3個の専用containerでCPU-onlyな前提を調べた。公式runner/Rustup/Cargoはあり、PowerShellは無かった。最初の空log失敗と次の診断失敗も保持。公式公開PowerShell7.6.7のasset SHA256 6896f9e3a6b0a354bf470835a96da76d2f801cd50dcf047ad7c4563bf93bf431を照合し、新guest tmpfsへstdin供給して起動成功。imageは変更していない。最終SDK出力は502byte、SHA256 dad9c4af3aeabec8653d6cc89cebc38a0c6fa7cc7828f586325de1a9341f7b1e。全3containerは停止/PID0/除去を確認。これは正式CI・Cargo受付・製品検査成功ではない。

元2730の有限virtual serviceの実検査は既存方式の根拠として読んだ。2737/2746のVM準備とJob Object fixtureだけでは公式CIが未達である点も確認した。原SDKはreadonlyでRust1.90のため、原commandで必要1.95を入れる新tmpfs homeと共有gate接続が必要。再起動を唯一の前提にしない。

### 正式な独立工程と次の再開

- ToDo2749: 今回だけのWindows有限service/専用account/公式runner helperを実装する。先行2710のtoken/Job Object方式を調べ、旧root/SID/profileは再利用しない。製品取得/正式登録/製品実行は親が統合後に行う。
- ToDo2750: 今回planとSDK probeをClaude Sonnet5.5でread-only reviewする。原応答は未受領。Must解消前に製品workflowへ反映しない。
- ToDo2751: Linux専用ephemeral runnerと有限host Cargo gate adapterを実装し、credential-free fixtureを確認する。親の原commands/source条件を渡した。旧bridgeの許可拡張や製品検査重複はしない。

三件とも--specを付けて通常登録済み。2751は--needs cargo付き。直接子Codexを起動していない。登録時のjobs=[]を実行済みと呼ばない。親2736は三件の成果を受け、別モデル指摘を修正し、必要なhelper通常PR/CI/配備を確認してから製品同branchへの非force API commit、固定新head/tree、正式run/job/check/原logs、OSと必要全件、後片付けを確認する。製品PR148のmergeは元2640が行う。親2640完了を待つ逆依存は作らない。

今回の継続用一時rootはC:/Users/makhmeto/AppData/Local/Temp/codex-owned-f8847d61da9cfac712675b5cc26b4932、safe-delete ID同名。公開PowerShell SDKと自作binaryを置き、次の統合で再利用する。製品source/秘密は置いていない。
原receipt/logの恒久保管rootはこのworktreeのcodex-owned-184a58eedf4f923741f160dfe7cfd5b7、safe-delete ID184a58eedf4f923741f160dfe7cfd5b7。原JSON/logを全SHA/bytes一致で保管し、manifest.private.jsonを残す。ここは原証拠として消さない。既存の保護判定で未確認となる証拠rootを開かず、新しい所有記録を作った。

終了時は正規todo wait --on 2749 --on 2750 --on 2751で依存を記録する。元2640のnoteへ結果と残条件を追記配送し、元2640/880の未完を維持する。自分の最新HEADはscheduler checkpoint-workspaceで保存する。成果・待機・公式CIの成功を混同しない。

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
