# 正式runnerの容量・設定場所・未起動資源回収の有限レビュー

複数fileのため既存OAuth Sonnetでread-only reviewする。元2797/2832/2840/2844はdoneで再実行しない。
編集、保存、ToDo操作、commit/push、外部通信、実検査/build/Cargo/Docker/GPU/Civ6、
認証・秘密・製品source・旧profile読取、子ジョブは禁止。結果は最終応答に返す。

元worktree D:/CC/Work/unattended-operations-20261013-wt/todo-2736-pr148-ci
固定base c9ab54693bbe5efd0f4c9d62e38bc544e47fc955
固定head 65592b450de09d0fc2d196e6dfbf7988e3e9e917
規則はD:/CC/AGENTS.mdと元worktree/AGENTS.md。
git起動不可なら元worktreeの絶対pathを読み、固定head未照合と明記する。親が原応答と実path、
固定head/作業コピーを照合する。子worktreeの古いfileを元headの代用にしない。

## 限定読取範囲

- tools/product-ci/pr148_linux_namespace.go と pr148_memory_budget_test.go
- tools/product-ci/pr148_linux_gate.go: pr148HostEnv/pr148TranslateGate
- tools/product-ci/pr148_linux_guest.go: pr148ExecChildのcaps
- tools/product-ci/pr148_linux_execution_policy.go: pr148StepEnvironmentと元envの読取・合成
- tools/product-ci/pr148_finite_test.go、pr148_review_regression_test.go: 変更したcap/flag検査
- tools/product-ci/pr148_linux_run.go: 新Resourcesメタデータだけ
- tools/pr148-windows-runner/gate_windows.go と gate_env_windows_test.go
- tools/pr148-windows-runner/collect_unstarted.go、collect_unstarted_test.go、collect_windows.go
- tools/pr148-windows-runner/scm_windows.go: collect-unstartedのdispatch追加とenvFor
- 関係する既存checkJob/checkCollectionRemote、Plan/checkControl、service/launcher receiptの関数だけ
- 両READMEの今回追記。ほかのcode、広い履歴、証拠root、原API/log/sourceは読まない。

## 原因・修復と実測

1. 正式Linux job117740936941は原head03c7cc6/treea6851bdでcheckout/Rust/fmt成功、
   Rust/Luaリンクでsignal9。memory.max=8589934592、peak同値、oom=1/oom_kill=1。
   2並列でもtoolchain/tmpfs成果物/linkerの合計が8 GiBに達した。失敗と原logを保持。
   namespace停止PID0/除去、登録不在、lock解放は実成功。これをCI成功にしない。
2. 最初の16 GiB案b0f3は単体検査/buildまで行ったが、Docker VM総RAMの実測
   16764764160bytes（15.61 GiB）で親preflightが拒否。実namespace作成/製品再検査は0。
   最新はこの専用profileだけ10 GiB/MemorySwap同値、CPU2/PID512/readonly/no GPU等を保持。
   共通CodeQLの8 GiB validatorは変更せず、PR148側で10 GiB/no-swapを先に検査してcopyだけ適応。
   host/test/guestは1並列を要求・検証。gateの実割当を偽装しない。
   lldは元logで-fuse-ld=lldを確認。元Rust flagsを保ってlinker threads=1だけを追記する。
   debug情報・feature・テスト対象を減らさず、原Cargo command/workflow/sourceは変更しない。
   正式実行前にVM/hostの新しい空き確認が必要。現在ほかの共有containerは停止しない。
3. Windowsの新root5051はprepare exit0、source1283objects/1292tracked、固定head/treeを照合。
   executeはgate childの最初のrepo GETでexit4、worker/service開始・runner登録前に失敗。
   同じ固定repo GETを同じcontrollerのsanitized envで実測し、APPDATA無しexit4、
   APPDATA有りexit0/repo1392152644。トークン環境変数は元々無し。設定/credential fileを読まず、
   ログイン・認証方法・scopeを切り替えていない。host controllerに既存AppData/gh/XDG設定場所
   だけを保持し、token変数を除外。workerのHOME/AppDataは専用rootのままという修復。
4. prepare成功後・worker開始前の中断は通常collectのlauncher drain reportがまだ存在しない。
   新collect-unstartedは別の限定条件を持つ。停止/PID0、queryable同SID process0、worker/hook/
   auth成果物の不在、初期reportが厳密に{}、同一queued job/steps0/runner無し、既存のremote
   collection照合を要求する。auth fileは存在確認だけ。正常collectのdrain条件は保持。
   既存action journalを使い、削除済みserviceの再開でもunstarted条件を先に再確認する。
   service ACE除去後、この新root/runtimeの元controller grantだけを戻す。profile削除はしない。
   拒否/不明なmutationは繰り返さない。回収やCI成功を捏造するreportは作らない。

現時点のGo raceはPR148関連68pass/4skip（最新flag追加で後続検査を行う）、
Windows module97pass/2skip、両module vet/build成功。これらは製品正式成功ではない。
10 GiBのkernel fixture、実collect-unstarted、修正版Windows正式実行はまだ未受入。
以前の16 GiB artifactを最新sourceとして実行しない。親が最新検査を別に受領する。

一次資料（閲覧不要、外部送信禁止）:
- GitHub CLI config discovery: https://cli.github.com/manual/gh_help_environment
- Rust link-arg: https://doc.rust-lang.org/rustc/codegen-options/index.html
- LLVM lld threads: https://github.com/llvm/llvm-project/blob/main/lld/docs/ld.lld.1

## 判定

実害があるMustを優先し、位置・根拠・最小修正案を返す。特にunstarted回収が既にworkerを
起動した資源へ進まないこと、controller設定がworkerへ漏れないこと、10 GiBの適応で他の
kernel条件を無効化しないこと、元Rust flagsを保持することを確認する。実測していない
製品受入をMust0へ読み替えない。読取対象、固定head照合の可否、実検査0を明記する。
