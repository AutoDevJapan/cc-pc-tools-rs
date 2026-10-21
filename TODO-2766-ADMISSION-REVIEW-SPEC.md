# GoDD Pressの検査受付と統合コミット照合を確認する（元2766）

共有ToDoから割り当てられた別モデルの読取レビューです。修正・commit・ToDo変更・子ジョブ起動はせず、日本語の最終返信へ結果を返してください。元2766/親2642を依存先にしないでください。

## 固定対象

- repo1406818713 / AutoDevJapan/cc-pc-tools-rs。作業資料は D:/CC/Work/unattended-operations-20261013-wt/todo-2766-godd-press-ci-2642。guardでclient_resource=falseを再確認済み。原製品のPCフォルダは対象外。
- base1784c0d85e99151e46b3b4c439915cc6e0fcb0ba → head68ac605c6520262d60b7ff26d0467f0717c0fd70 / tree72cc7c66b29c1ff22a3905a90bc01e6e690cdd38。
- `git diff --name-only <base> <head>` の32ファイルを固定headのGit blobで全文読了する。現在のworktree HEADへ追従しない。
- 対象は REVIEW-2766-PREPARATION-3246.md、tools/ci-source-2737 の README.md/profile.go/profile_test.go/sourcepacket.go/verify.go/merge_test.go/remote.go/signed_payload_test.go、tools/product-ci の README.md/go.mod/main.go/pressci/{outline.go,route.go,admission.go,admission_test.go,source_inspect.go}、tools/ci-guest-2737 の host_windows.go/job_contract.go/job_linux.go/job_windows.go/runner_windows.go/windows_job_windows.go/job_policy.go/job_policy_windows_test.go/timeout_windows.go/timeout_windows_test.go、tools/windows-ci-runner-2737 の contract.go/guest_windows.go/identity_windows.go/press_test.go/press_windows_test.go。
- 必要な追加局所: normalci/{identity.go,admission.go,policy.go,lease_policy.go}、sourcepacket の既存 Materialize/Decode/strict JSON/secureRoot/metadata snapshot、ci-guest の canonical Cargo gate/VM cleanup/Windows clone stage/実行物同版検査、Windows HostSession/GuestSession/worker環境。変更していない基盤の全面再監査を求めないが、今回変えた入口がその保護を実際に使うかは確認する。

## 仕様と未実施の区切り

元2737の完成済みguest/SDK/runnerを再利用する。新しいVM実装や実行担当を複製しない。元MCP αの空profile、3役割、label、timeout、通常API、権限、cleanupを維持する。

Pressはprivate AutoDevJapan/repo1386702183/Draft PR175/固定ref fix/todo2642-schedule-predecessor-overflow/元PR API base4dc5c332d54d19325f122df1f0e4a52e9dd3a629のみ。候補headは明示pinし、物理実行headと混同しない。

- Source Pinにpull_head_sha/merge_base_shaを追加。PR実行ではhead_shaが実merge commit、追加2値が候補と現在main。両live ref、PR merge SHA、commit/treeと正確な2親順をAPIとguest物理Gitで検証。空MCP profileは追加merge値を拒否。
- 元3workflowの全bytesは b2b6d47b7f9e17617d16a9079a70de272bc3c04c に固定。PrepareRoutesは完全blob一致後にruns-on scalarだけ変更し、その他のbyteは不変。role/run/attempt label、専用変数 CC_PRODUCT_2766_CI の正確なnonce値を要求する。元events/permission/steps/gates/timeoutsは書き換えない。
- local-checksの元10jobは候補branchの元dispatch、HTTPS2とWindows hidden1は元PR event。全4shard/両OSの13roleを固定する。job表示名・SHA/run/attempt/runner/name/label/OS/timeoutを束縛する。候補treeは3つのroute blob以外に変更を認めない。実merge treeを候補のtreeへ読み替えない。
- Windows/Linux両controllerは既存の共有Cargo gate/専用target/上限/直列条件/intent/単一job/hook前後の物理proof/取消し/VMと登録とlock回収を維持。動的なのは閉じたPressの役割・API送信先・固定checkout path・元timeoutに必要な有限の外側時間。hostの任意実行物/command/path/secretを入力で選ばせない。
- Windowsは2つの元Windows roleだけを追加。SYSTEM/LocalService/private proof/worker Git/ACL境界は維持し、checkout ownerの固定path1箇所だけPressへ切り替える。
- source CLIはRAMでFetch/検証しmetadataだけを返す。host Materialize、source body出力、route公開、dispatch、runner登録、公式status successはしない。ArchiveBlobIDsVerified/CommitIDVerifiedと、まだfalseのPhysicalCheckoutVerified/RunnerAdmission/OfficialCISuccessを分ける。

**未実施を保つ:** この差分のlive利用、route公開、dispatch、実guest/公式job、SDKへのPress用Chrome/Rustup/Rust1.95の準備、全通常CI、親製品merge/配布は未実施。現在mainは元4dcより進んでlocal-checks自体を変えている。全route更新がGitHubで競合せずmergeできるかは未検証。後続はまずこの32ファイルをレビューし、実害を直して、元原workflowを変えずに実行できるsource/routeとSDKを照合する。上記の未実施を完了主張にしない一方、後続全体をこの読取レビューの完了条件に追加しない。

## 確認済みの実結果

- 前回レビュー3246: 実job20261021-110245-todo-3246 / succeeded/exit0、対応rollout turn_contextはanthropic/claude-sonnet-5-5/max。raw response SHA B2F518173E58730DA45EA16D40FD9DAD95FE56BD7922232C8EE42A1BD9026B69、task_completeとlast_message一致。固定旧8aの12全文読了/Must0/Suggestion3/Nit3。未読 secure_files* / source_materialization / normal_ci_admission本体は未読のまま保持。この新68acをレビュー済みとはしない。
- Suggestion1はEventsキー一覧、Suggestion2は不足した拒否行、Nitはdoc/usage/Repositoryのjson非出力へ反映。Suggestion3の一律Press拒否は、今回の有限接続と両立しないため、unknown/partial/cross-product/cross-OS/role/run/attempt拒否とbindingの実再検証へ置き換えた。
- source全module Windows race初回72.908sで新しい公開fixture書換えだけが失敗。Git loose objectはReadOnlyだったのでテスト所有の公開fixtureだけchmodしてfocused race4.176s/exit0。残りの検査は初回有効結果を保持し、テスト補助だけの変更で全件を重複しない。
- 新merge親pinの回帰: 元8aのverify.goだけをGo overlayで使うと実FAIL/exit1「wrong physical merge-parent pin accepted」。修正版はmerge/signature focused race4.172s/exit0。別のcommit改変negative controlは旧版でも拒否できたので、それを新しい欠陥のREDとはしない。原試行log/実exitを保持。
- real source Fetch初回はcommit段階failure（当時はAPIとpayloadを細分しておらず、初回の厳密な単独原因を捏造しない）。独立して実APIのPGP unsigned payload+signatureをRAMで再構成し、signatureのfinal LFを保持したものだけ実Git OID f3a7041に一致、末尾LFを落とす元方式は不一致と確認。実timezoneは+0000で旧対応範囲内だった。
- 同じfinal-continuationを持つ公開合成fixtureは、元8aのoriginalPayload関数だけのoverlayで実FAIL/exit1、修正版GREEN。署名付きAPI payloadを正確に使っても実OID一致が必要。新diagnosticはcommit_api固定分類/commit_identity/commit_payloadを区別し、raw error/bodyは出さない。
- 修正後、実Press merge f3a704132f286d58eee2c16b819bc85cd4565c37/tree a0c5944f8fc69b78db78afd525d2eab104fe3319、候補b2、main2da7d4af1dd9911eaff970fdfe08b60531732eb6をRAM Fetchしてexit0。archive49,306,964bytes/SHA0326e36cc480c5fc6f29ac32f6c386a52eecbf249b03f8879ce7702de78a8a73、2812 entries/2315 blobs。host checkout無し。初版metadataのArchiveGitObjectsVerifiedという広い名前を、実際に確認したArchiveBlobIDsVerified/CommitIDVerifiedへ訂正し、treeの物理Git成功やguest成功は主張しない。
- product-ci全module race: main1.320s/normalci1.037s/pressci1.435s exit0。新たな原13role、全非route bytes不変、candidate/merge/owner/ref/nonce/workflow/run/attempt/runner/partial inventory拒否を公開API fixtureで検査。最新版pressci focusedもexit0。
- 利用側ci-guest全module race14.528s/exit0、新13role focused1.038s/exit0、timeout focused exit0。Windows runner全module race8.771s/exit0、新2role/ACL差分1箇所 focused1.029s/exit0。
- source/product-ci/ci-guest/windows-runner全module vet exit0。Linux helperのGOOS=linux/amd64 compile-only exit0。実Linuxで新差分のテストやguestは実行していない。dirty版のcompile確認物はlive用にせず回収する。
- API追加診断の1回はTLS timeout後にPowerShell補助がnullから値を作りかけたため、そのreceiptを無効として保持し、ErrorActionPreference=Stop/exit照合後に有限の読取1回で上記の実SHA一致を測定した。無効なfalse値を判定に使わない。
- 原CodeQL run38893812340のb2での成功を保持し、再実行0。元通常CI3runのbudget failureも保持。原head/PR/variableへ書込み0。新VM/runner/container/Cargo/GPU/Civ6/APIキーAI/新費用/本人PC操作/Discord送信0。

## 重点観点

期待値とauthorityの混同、candidateとmergeの読み替え、署名/親/全blobの同一性、未知profileがαへ流れる経路、matrixの取り違え、job/run/attempt/runnerのすり替え、元workflowのbyte改変、cleanupの別repo化、期限の元意味、SYSTEM/workerのpathとACL、host sourceやsecretの出力/保存を確認する。

GoテストのAPI fixtureは受付の正負検査で、公式CIやOS/SDKの実受入の代用ではない。登録/起動/ソース照合/公式success/配布を区別する。

## 一次資料と規則

このworktreeのAGENTS.md、D:/CC/AGENTS.md、D:/CC/Policies/owner-agent-conduct-20261021.md。通常の技術判断を本人のPC操作/新承認待ちへ置き換えない。原製品PC folder/名簿/保護一覧/guard/manifest/認証値は読まない・変更しない。実Source/VM/CIの新取得や実行は不要。

GitHub公式のevents-that-trigger-workflows#pull_request / workflow-syntax#jobsjob_idtimeout-minutes と actions/runner の v2.338.0 stock hook仕様を必要範囲だけ参照可。技術的な不足は親2766へ具体的に返し、本人へ直接送信しない。

## 返却

全指定対象を読んで指摘を返したら、先頭は「状態: 完了」、次段落に読取レビューだけの完了と書く。固定base/head/tree、全文読了ファイル/追加局所/未読、Must/Suggestion/Nitとfile:line・具体的修正、実行した検査と0/未実施を返す。Mustがあっても読取を終え、修正は2766が進める。結果ファイル保存、commit、ToDo更新、子ジョブ、Discord送信は不要。実modelは親がjobと対応turn_contextで照合するので自己推測しない。最終返信3000字以内。
