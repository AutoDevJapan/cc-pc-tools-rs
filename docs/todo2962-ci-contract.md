# 万世紀の追加料金なしCI（ToDo2962）

この文書は実装契約であり、登録・実行・成功の証拠ではない。
対象は実APIで確認した AutoDevJapan/civ6-banseiki（1392152644）だけ。
PR149の元HEADは `f3e26f45682ac04b8a9bf07821ce5b96d83ce012`、treeは
`a45e137db144ba41db75cd0ba30be63500efb905`、baseは
`1100676d1ed421794c252fb4b15ed3d954e21a5b`。

## 分担と固定境界

- 親2962: `tools/civ6-ci-contract`、対象三workflowの候補生成・差分照合・別モデル・公開、統合と受入。
- Linux担当: 新規 `tools/civ6-ci-linux` だけ。旧product-ci/2736と稼働runnerを編集しない。
- Windows担当: 新規 `tools/civ6-ci-windows` だけ。旧pr148-windows-runner/2736と稼働runnerを編集しない。
- キュー担当: 新規 `tools/civ6-ci-queue` と既存 `unattended-runner` への有限tick接続だけ。
- いずれも新しい製品名ではなく、内部の実装ディレクトリ名。新コードはGo。

既存監査済みhelperを参照・必要な実装を再利用できるが、旧固定候補の受付を広げない。
各担当は既存sourceに未反映の修復を巻き戻さず、新規module内に必要な共有処理を収める。
共通Go moduleは `cc/civ6-ci-contract`、replaceは `../civ6-ci-contract`。
親が共通仕様を実装し、インターフェース変更は保存して配送する。

## 共通Plan（秘密なし）

`Plan` は次のJSON tagを持つ。Go名は通常の大文字化（RepoID、RunID、JobID、ContextSHA等）。

```
version:1, repo_id:1392152644,
head:<actual tested commit>, tree:<its actual tree>,
event:pull_request|push|workflow_dispatch|dynamic,
branch:<actual source branch>, pr:<same-repo PR number, main=0>,
context_sha:<actual GITHUB_SHA; PR merge context is separate>,
workflow_sha:<actual workflow commit>, workflow_ref:<actual GITHUB_WORKFLOW_REF>,
workflow:<.github/workflows/rust.yml|native-driver.yml|live-chain.yml|dynamic/github-code-scanning/codeql>,
workflow_digest:<actual immutable bytes SHA256; dynamic has empty digest>,
run_id:<official run>, attempt:<official attempt>, job_id:<official job>,
job:<GITHUB_JOB>, job_name:<official display name>
```

親は `Plan.Validate() error`、`Labels(Plan) []string`、
`Verify(context.Context,Plan,ReadAPI) error`、
`CheckEvent(Plan,map[string]string,[]byte) error` を提供する。
`ReadAPI` は `func(context.Context,string,any) error`。
`Verify` はrepo所有/private/nonfork/admin、現在のbranch/PR、元run/attempt/job/labels、
source/treeと三workflowのレビュー済みbytes、CodeQLの元languages/settingsを正規APIで照合する。
helperは登録前・hook開始/終了時・終了後に同じ固定planを再確認する。
終端jobの成功判定は別で必須。listenerのexitだけでは成功としない。
PRのhead checkoutとmerge contextを混同しない。fork・古いsource・別run/jobを拒否する。
workflow変更を含む将来イベントはレビュー済みtemplateと一致しなければ実行せず理由を残す。

## workflowとOS

三workflowのevents/permissions/job名/OS matrix/全command/concurrencyを保持する。
Linuxは `[self-hosted, Linux, X64, civ6-ci-linux]`、
Windowsは `[self-hosted, Windows, X64, civ6-ci-windows]`。
matrix.osのubuntu-latest/windows-latestはjob表示とOS選択に残し、hosted labelとして登録しない。
checkout refは `${{ github.event.pull_request.head.sha || github.sha }}`、認証永続化false。
CodeQLは既に存在する `cc-product-2648-1392152644-codeql` を維持する。
default setupはactions/rust、configured/default suiteのまま。契約・予算・保護は変えない。

## 実行と資源

Linux1/Windows1、各jobは公式ephemeral runner。正規OAuthの登録tokenはRAM→stdinだけ。
既存Linux27/Windows28を停止・再登録・ラベル変更しない。Docker socket/host bind/
privileged/GPU禁止。既存credential-free immutable SDKを独立namespaceで使う。
VMの実MemAvailableとhost空きを測り、必要なcgroup予算と予約が入る時だけ登録する。
Rustは既存Cargo gateで実枠を保持して実行し、同じtarget排他とCPU/RAM/並列上限を維持。
Windowsは新専用virtual service account/filtered token/Job Object、共有SDKはRXだけ。
必要なadmin操作は既存CC-AdminBrokerだけ。CREATE_NO_WINDOW、本人操作不要。
既存実装の実OOM、SCM1077、profile residue等の失敗証拠も読む。

## キューと回収

既存無人tickが対象repoのqueued jobを有限走査し、同じrun/attempt/jobを永続台帳で一度だけ受付。
OSごとに1worker。重い実行をtick本体で待たず、既存Cargo gate配下の固定workerだけを起動。
固定配置したレビュー済みbinaryだけを呼び、任意callback/commandを入力から受けない。
hookは実際に割り当てられたjob identityを照合する。失敗を自動rerunしない。
crash/timeout/不確かな作成・停止・回収は同一記録を残し、別workerの新規登録を止める。
終了後にowned namespace/Jobの実PID0、固有登録の不在、service/ACL/profileを確認し、
原logとSHA/bytesを保って所有tempをsafe-delete手順で回収する。拒否は迂回しない。
古い資源や原名簿、未知repoの包括許可を追加しない。

## 有限受入

最初のPR149は正式job最大6（Rust Linux/Windows、native、live、CodeQL actions/rust）。
merge後の通常push経路も最大6。同sourceを不用意にrerunしない。合計12の受付予算を永続保存。
以後の継続運用は新しい通常eventを同じ一度限りのrun/attempt/job受付へ流す。
PR149成功・別モデルMust0・通常merge・merge後の再供給と回収をそれぞれ実測して記録する。
設定やfixtureだけで完了を主張しない。親2957/集約2955への循環waitは禁止。
