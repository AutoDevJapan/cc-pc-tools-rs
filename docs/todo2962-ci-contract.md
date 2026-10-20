# 万世紀の追加料金なしCI（ToDo2962）

## 2026-10-20の適用契約（下の旧OS計画を更新）

本人のWindows完成優先指示に従い、Rust matrixはWindowsだけとする。
元のWindows workspace/all-targets/doctest/release/シナリオ、nativeとliveのfmt/test/Clippyを保持する。
Linux helperのソースは保つが、Linux利用者向けの供給・移植検査・配布を今回の完了条件にしない。
CodeQLは公開repoの標準GitHub-hostedを使い、actions/rust・default query suite・remoteを保持する。
これはWindowsのアプリbuild/testと異なる静的解析で、PC側のCodeQL供給は行わない。

受入上限12件の内訳は、最初とmerge後にWindows 3件＋CodeQL 2件ずつ、移行検証用に最大2解析を予約する。
queueのPC受付上限は最初3件・merge後3件。CodeQLの実run/jobは別の受入記録で数える。
設定変更や登録を検証成功と扱わず、同じ最終HEADの実job・成果物hash・登録不在・回収を確認する。

Given 固定した同repo/現在HEADのqueued jobがある。
When 新しいrunnerを供給する。
Then 固有run/attempt/job/OSラベルに加え、古いbaseラベルだけのjobを含む全候補を集合包含で調べる。
他の未終端jobも引き受けられる場合は、予算を予約する前と登録直前に拒否する。実SDKの割当も別に照合する。

Given prepareやCargo待ちで時間を使った。
When queueの絶対実行期限を過ぎる。
Then serviceは開始せず、未開始の証明を調べる専用collectorだけを呼ぶ。
開始した場合は同じ絶対期限までのworkerと90秒の排出をgate内で待ち、その後に回収を行う。

Given Cargo枠を取れないまま期限を越えた。
When 親queueがexecute全体を終了する。
Then 生成時に同一Job Objectへ所属させたhelper/gate/子孫を終了し、保持handleとActive=0で証明する。
未確認ならfenceを維持。beginなし、またはservice_started:falseなら専用collectorが未起動の実状態を確認する。
正式job未開始・専用回収成功・証拠保存・owned root回収後だけ同じ予約をbackoffして再供給する。
予算キーは同じrun/attempt/jobの1件のままで、正式実行済みjobの自動再実行はしない。
登録credentialはgate取得後、監査と他job包含検査の後に1回だけ既存OAuthから取得する。
取得失敗の生stdout/stderrはログへ出さず、待機中にcredentialを保持しない。

CodeQLの標準runnerへの変更はHTTP202と設定再読を確認済み。解析成功と製品正式受入はまだ未確認。
以下のLinux供給・CodeQL専用ラベル・6+6件の記述は、この更新前の設計記録として保持する。

この文書は実装契約であり、登録・実行・成功の証拠ではない。
対象は実APIで確認した AutoDevJapan/civ6-banseiki（1392152644）だけ。
2026-10-19の本人追加指示により、同repoはpublicへ変更済み。
非公開フラグを受付条件にせず、元owner/ID/nonfork/adminと現在のsame-repo sourceを照合する。
外部fork PRは三workflowのruns-onで標準GitHub-hostedの元OSへ送り、本人PCへ無条件で流さない。
Windowsの同最終source/toolchain/lockfile/検査範囲のビルドをPCとcloudで二重に実施しない。
Rustの既存release build直後に、実Git HEADと生成した各binaryのSHA256/bytesを正式job logへ記録する。
同じ実行物を配布に使い、同条件のcloud buildを追加しない。
既存予算2500USDは本人承認で変更済みだが、本工程でさらに増額・再公開はしない。
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
`Verify` はrepo所有/ID/nonfork/admin、現在のbranch/PR、元run/attempt/job/labels、
source/treeと三workflowのレビュー済みbytes、CodeQLの元languages/settingsを正規APIで照合する。
helperは登録前・hook開始/終了時・終了後に同じ固定planを再確認する。
終端jobの成功判定は別で必須。listenerのexitだけでは成功としない。
`VerifyAssigned(ctx,p,uniqueRunnerName,read)` でmatrix共有のjob名と実job IDを区別し、
`RequireSuccess` で正式successを確認する。CodeQL生成eventは公表されたworkflow refを
捏造せず `workflow_ref`/`workflow_digest` を空、`workflow_sha` をsource HEADにする。
生成PRの `context_sha` は実SDKで観測したHEADまたは正規APIの現在のmerge SHAだけを
受け付ける。mainはHEADだけとする。実SDK値を未観測のまま成功や確定値と記録しない。
RESTのrun pathはbare pathと検証済みの `path@ref` の両形式を受け付ける。
current sourceが進んだ/閉じた後の回収は、freshnessを要求するVerifyから分離し、
`ObserveJob` の固定job identityと固有登録/所有namespaceの後条件で実行する。
PRのhead checkoutとmerge contextを混同しない。fork・古いsource・別run/jobを拒否する。
workflow変更を含む将来イベントはレビュー済みtemplateと一致しなければ実行せず理由を残す。

## workflowとOS

三workflowのevents/permissions/job名/OS matrix/全command/concurrencyを保持する。
元Rust workflowの `--all-targets` はdoctestを実行しないため、依頼に明示された
受入を満たす `cargo test --locked --manifest-path tools/Cargo.toml --workspace --doc`
を独立stepとして追加する。既存commandの置換や省略はしない。
Linuxは `[self-hosted, Linux, X64, civ6-ci-linux]`、
Windowsは `[self-hosted, Windows, X64, civ6-ci-windows]`。
両方に `civ6-ci-run-<run_id>-<attempt>-<job>-<OS>` を必須ラベルとして追加する。
workflowのformat式と登録側の共通JobLabelが一致し、別run・試行・job・matrix OSを取り違えない。
matrix.osのubuntu-latest/windows-latestはjob表示とOS選択に残し、hosted labelとして登録しない。
checkout refは `${{ github.event.pull_request.head.sha || github.sha }}`、認証永続化false。
旧CodeQLは `cc-product-2648-1392152644-codeql` を使うが、PR148との共有競合が残る。
この競合の解消前には登録しない。公開後の標準runner等への移行は読取確認3009の対象で、未実施。
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
