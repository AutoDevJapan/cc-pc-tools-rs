# ToDo2736 再開記録

## 2026-10-17 12時台の統合結果（以下を優先）

job20261017-123327-todo-2736。9d57e63から再開し、元2640の記録、当該repo規則、最新D:/CC/AGENTS.md、原2750/2751/2749の実成果を確認した。作業branchはscheduler/todo-2736-pr148-ciのまま。製品PR148のAPIで実owner AutoDevJapan/private/nonfork/admin、同repo1392152644、open/draft/mergeable、head75a/base1100/原refを確認。専用runnerは0。製品workflow変更/source展開/runner登録/dispatch/製品Cargo検査はまだ0。GPU/Civ6/再起動/旧資源操作も0。

### 保存した実装と実検査

- Linux2751は5a656e4で完了。module tree269264365cc6b22c2bb6314722f8021f39b038afのレビュー2781/2785を受領し、実Sonnet5.5/succeeded/exit0を確認した。元5Mustの解消と実Cargo正常/取消し・Rust1.95の原4command fixtureは子の原記録に保持し、重複実行していない。
- そのPR148 Goファイルだけを72e82e0へ統合し、他製品のcandidate変更は取り込まなかった。最新mainとのmerge a0475b5でgo.mod/go.sumの競合を解消し、mainの既存依存を保持した。
- 原2750の実応答は7patch/3全文の有限範囲でMust0だった。main/verify/dispose/原workflow等は未読と明記されていたため、これを最新全コードの承認にしない。原応答と2785応答をbytes/SHA一致で自分の証拠rootに保存した。
- Suggestionの実害を直した。PR148以外のPR/mainはgithub.shaによるmerge結果のcheckoutを保つ。原job名/両OS・追加annotationがnoticeだけであること・checkout byte span・元withの有無・正確なowner/repo・safe-delete所有を確認する。
- f86e32dde58428efb4a0d63670362cafca01c5edへGitHub APIによる三workflow限定tree/単独親commit/非force ref更新を実装した。全tree比較と更新前の原候補再検証、変更前の実object/attempt保存、更新後の実new head/tree/proposal全bytes照合を行う。不確かな失敗は再試行しない。このモードはdispatch/runner/status書込をしない。製品にはまだ呼び出していない。
- 最新sourceのGo raceは196 pass/4 opt-in skip、vet/Windows build/Linux cross buildが全exit0。source race、原実装file改変、ref拒否ではbranch変更・成功receiptを拒否し、正例で原75aだけの親とforce=falseを確認した。新しいfixtureはAPI合成のみで製品を重ねて検査していない。
- Windows binary SHA256 8CD6AC0E9BFCBC24675252FCFFF4E2360A8039EB7AA4ECE3F14C1527204F0BC0、Linux binary 2032A74084DA2D86182C669BA8F26C7C699016724BD28CE785410164ACE30A26。Go原jsonl SHA256 0BC0252F4EFB0D5E0CBB0BF353DB78FF5B1A946E05FBEA70459750DE49D9EE06。
- 実read-only pr148-planは三原workflow/四未開始jobと更新案を確認してexit0。最後のowner限定追加前のbinaryによる実API確認であることを保持する。最後のowner限定は上記最新Go検査で確認した。
- helper PR189を更新しf86e32dをpush、APIのPR head一致を確認。URL https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/189 。タイトル/本文を最終差分と未達条件へ更新した。新headの正式helper CIは実行中/順番待ちであり、成功/merge/配布とはしない。

### Windows契約の未一致と次の登録

2749は497b2f1/コードda2b45eの別モデルMust0を配送済みで、自分の5profile/hive/classes回収だけを2381へ待っている。この整理を親のCI前提には戻さない。
Windows helperは元75a checkoutとpull_request merge contextだけを受け付ける。一方、レビュー済みLinux経路は三workflowだけの明示new head/treeと同branch全OS workflow_dispatchを受け付けるため、現状では両者が一致しない。共有SDK/gateに対するfiltered service tokenの実適合、原sourceの新runtimeへの有限materializer、停止/排出の実監督もWindows側の未達である。
独立統合の具体specをdocs/todo-2736-windows-integration-spec.mdへ保存した。旧2749のroot/SID/profileや共有ACL/guard/manifest/bridgeを再利用・拡張しない。新headの原実装不変と元三workflow案bytesを照合し、両OSの同dispatch契約へ合わせる有限工程だけを求める。

### 共有ToDo容量上限による未受付（判断待ちではない）

Windows統合addと最新統合レビューaddはいずれもtodo_store_too_large: shared ToDo exceeds 67108864 bytes (64 MiB)でexit1。受付番号は作られていない。元2750等を新しい成果へ読み替えず、直接子Codexや別経路で登録しない。復旧担当2786が既存doingであることを確認した。
共有ToDoを正式にwait 2736 --on 2786へ保存する呼出しも同じ容量エラーでexit1となった。waitは未保存であり、元2736のdoing/元2640のwait2736を保持する。元2640へのnote配送も容量復旧後に行う。保存した成果を先に消したり、元2640の未完を解除しない。

新統合レビューの固定本文は自分の証拠rootのreview-integration-f86e32d.private.md。base3f15e84d8051cd99e59a2bc2b7fcbeab89f0d531/headf86e32d、31全文と全差分、409292bytes、SHA256 BA39C1DE4100961FBE49981E08AF867FDA5CA5F28F26752FE573132C08045CC2。受領済みfixture事実と旧レビューの未読範囲も渡している。復旧後はこの同じ固定本文を--model claude-sonnet-5-5/--spec/--subwork-of2736/実parent-job/--subwork-kind reviewで1回登録する。Windows工程は保存spec/--needs cargo付きで1回登録する。未受付を委譲済みとしない。

次の再開は容量復旧の実結果を確認し、上の2工程の通常登録・原spec SHA照合・受付確認を行う。必要なら正規waitを保存してCargo枠を譲る。最新helper CIにfailureがあれば原因を直す。Must解消とCI成功後だけhelperを通常merge/必要配布し、PR148の三workflow非force更新→明示new head/tree→正式run/job/check/原logs→元2640配送を行う。製品PR148のmergeは元2640が担当する。2736から元2640完了への逆依存を作らない。

### 原証拠と自分の後片付け

自分の恒久証拠rootは以前作成したcodex-owned-184a58eedf4f923741f160dfe7cfd5b7。今回原binary/Go jsonl/vet/plan/二レビューの7fileを全bytes/SHA一致で保存し、integration-f86e32d-manifest.private.json（SHA256 F285DDE6795C95621D2E83D6B7891FC8F4E86F204F2734ABCA2C2B43D537C396）を保持する。原証拠と固定review本文をGitへ追加しない。
今回のbuild Temp dd785cd3a4a58ca26536ba836562d0b9は保存後plan→別通常削除→finish=source_absent。削除後も恒久7fileの全SHA/bytesを照合した。以前の継続SDK Temp f8847d61da9cfac712675b5cc26b4932は公開PowerShell asset等が次の正式統合に必要なため保持する。今回新しいcontainer/volume/service/profile/runner/tab/process/追加ACEは作成していない。

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
