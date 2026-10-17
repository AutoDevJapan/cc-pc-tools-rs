# PR148 Linux helperの独立レビュー

元2751の有限な読み取り専用レビュー。通常スケジューラの割当と既存OAuthのClaude Sonnetを使う。APIキー・課金・直接子Codex・Cargo/GPU/Civ6・製品API・ブラウザ・登録/dispatch/配布・ファイル変更・commit・ToDo状態変更・Discord送信を行わない。結果は最終返信だけに返す。保存と配送は実装担当2751が行う。

## 固定した対象

repositoryはcc-pc-tools-rs。読む場所は次の実装担当worktreeだけ。

`D:/CC/Work/unattended-operations-20261013-wt/todo-2751-pr148-linux-runner-cargo`

base `d6a46b0` → code head `0377dd0f66bfb69b58331cb497625b4f5e8b6ae5`。
module tree `066dab0b76d6194121e092f0f4c14e9c73b541b9`。

このworktreeの可変HEADを読む代わりに、`git show <固定head>:<file>`で下記全15ファイルの当該版と、`git diff <base> <head> -- tools/product-ci`の全変更を読む。checkout/restore/fetch/pushはしない。自分のスケジューラworktreeのHEADを対象と取り違えない。

- `tools/product-ci/go.mod`, `go.sum`, `main.go`
- `tools/product-ci/pr148_plan.go`, `pr148_plan_test.go`
- `tools/product-ci/pr148_finite_test.go`
- `tools/product-ci/pr148_linux_gate.go`, `pr148_linux_guest.go`
- `tools/product-ci/pr148_linux_namespace.go`, `pr148_linux_plan.go`
- `tools/product-ci/pr148_linux_probe_test.go`
- `tools/product-ci/pr148_linux_process_linux.go`, `pr148_linux_process_other.go`
- `tools/product-ci/pr148_linux_run.go`, `pr148_linux_source_test.go`

文脈として、同じheadの既存`codeql.go`, `codeql_admission.go`, `codeql_queue.go`, `source_materialization.go`, `lifecycle.go`, `process_windows.go`, `process_other.go`を読める。既存`tools/linux-runner-bridge/main.go`の有限Cargo adapter（概ね535〜730行）と`process_linux.go`も比較してよい。resident受付を拡張する提案や、他の旧root/SID/profile/cleanupの調査へ範囲を広げない。

## 仕様と規則

起動入口`D:/CC/AGENTS.md`とこのworktreeの`AGENTS.md`を読む。原specは`D:/CC/Tools/todo/specs/2751-24f2822cdcd143cf92e6dd34315a4741.md`。親の規則・原検査条件とprobe事実は`D:/CC/Work/unattended-operations-20261013-wt/todo-2736-pr148-ci/TODO-2736-CHECKPOINT.md`と同`docs/todo-2736-linux-runner-spec.md`にある。製品sourceや旧製品folderへ行かない。原資料を保持する。

今回の本体は、固定repo1392152644 / PR148 / 原75aだけを親とする新しい三workflow限定commitを、親2736が後で同branchへ非force更新した場合の有限公式runner helper。子2751は製品sourceを取得・展開・テストせず、workflow/dispatch/runner登録も行わない。実装コードにある実行関数と、実際に実行済みのfixtureを分けて評価する。

原commands/OS/timeout/失敗伝播と全scenarioを保持する。原toolchainは1.95.0、targetはscenarioが参照する`tools/target`。元75aを新HEADとして扱わず、新commitの唯一の親・三workflowの全bytes一致・他tree entry不変・明示head/treeをAPIで実証する。原PRイベントのmerge SHAとsource headを混同せず、正式受付は親が固定した同branch/full workflow_dispatchだけ。

immutable SDKは仕様記載のsha256:89988...。非root/init/readonly/no bind/no host socket/no GPUと有限CPU/RAM/PID/tmpfs。旧bridgeのserver/権限を変更しない。専用labelは仕様どおり。公式registration tokenはRAM/stdinだけ、no-default-labels/ephemeral/disableupdate。

callbackは元Linux Cargo集合だけ。repo/run/attempt/source/nonceを毎回照合し、host command/path/envを受け付けず、共有gateと専用targetを維持する。gate追加target/並列argsを照合し、直接/入れ子Cargoがpermitの外へ出ない。正常/取消しの実process群不在が確認されるまでhost gateを解放しない。異常時はnamespace停止、それも未確認ならgateを保持する復旧待ちである。この経路と「有限helper」の整合、未知create/取消し/早期失敗の回収も評価する。

正式job/run/check/原logの合格確認は親2736に残る。SDK/fixture/registration/listener exitを正式CI successとして転記するAPIを作らない。

## 実装担当が実行して確認した事実

以下は作者の実検査であり、レビュー担当自身がテストしたと書かない。

- 同じ最終code bytesでGo unit119 pass（subtest込み）/3 skip/failure0、vet0、Windows build0、Linux amd64 cross build0。
- 新namespace内の小さな合成Rust crateを、実guest→host HTTP→実host共有Cargo gate→guest supervisorで検査。正常exit0と取消しexit125が通った。子の実PID観測後、gate解放時にkernelから不在を確認した。
- workerからSDK Cargoの直接実行/読取コピー、toolchain読取、proc経由の読取、helper/plan書換えを実Landlockで拒否した。
- 最終fixtureは7.203秒。全10個の自作namespaceは停止/PID0/削除を確認済み。最終Docker inventoryは当該container0。
- 最初のJSON所有照合誤り、合成Rust文字列へのGo test混入、回収の早すぎる実行、stdin copy待ちの失敗は修正し履歴を保持。早く回収した自分のnamespaceが確実に停止/PID0/削除された後、正確な自分のcallbackだけを終了した。危険な広域回収testは除去した。
- これはSDK既存toolchainのfixtureである。原1.95.0の製品検査や公式registrationは未実行。PowerShell7.6.7の公式asset SHAと起動は親の以前のprobeで確認され、本子では再実行していない。

## 出力

日本語で、固定base/head、全変更を読んだか、読んだ15対象/既存文脈、未読範囲を明記する。Must / Suggestion / Nitを分け、具体的なfile:line、実際に起きる不具合、最小の修正案を返す。指摘なしの場合も読了範囲を示す。作者の実検査と自分の静的判定を区別する。実装・公式CI・mergeを完了扱いしない。1万字以内。
