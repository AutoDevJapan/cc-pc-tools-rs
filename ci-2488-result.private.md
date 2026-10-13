# ToDo2593: 親2488の固定版CI実結果

固定版の8workflowを実検証し、成功7件・失敗1件を確認した。結果検証は完了。親の修復・マージ・配布は未完了。

最終確認: 10/14/2026 02:23:43
固定HEAD / checkout sourceSHA: 1e9cd8c90deec542d28d671d09fd1b9955d8f2ee
Git tree（GitHub commit API実取得）: ef282f643578d5949b91a7042c97fbef302735a7
PR: https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/144
PR head ref: codex/unattended-readiness-20261013 / refs/pull/144/head
repo: AutoDevJapan/cc-pc-tools-rs、PRIVATE（repo APIで実確認）

| Workflow | Run / 全文ログ入口 | 結果 |
|---|---|---|
| Build speed | [38433222034](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222034) | failure |
| CodeQL PC | [38433222531](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222531) | success |
| Unattended readiness tools | [38433222500](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222500) | success |
| Agent progress tools | [38433222594](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222594) | success |
| Original record recovery staging | [38433222286](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222286) | success |
| Channel reports | [38433222393](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222393) | success |
| ChatGPT browser fixtures | [38433222360](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222360) | success |
| Bookmark queue | [38433222089](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222089) | success |

全21jobは19success/1failure/1skipped。実行した20jobのcheckout実行行で固定sourceSHAを確認。各workflowとjobのhead_shaも固定HEADと一致。skippedのpackage-toolsは枝条件外であり、配布済みとは数えない。

| Job | ID / 原ログ | 結果 | runner |
|---|---|---|---|
| Build speed / cc-jobs | [115420510423](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222034/job/115420510423) | failure | cc-pc-tools-win-2510 |
| Build speed / package-tools | [115428469215](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222034/job/115428469215) | skipped |  |
| CodeQL PC / analyze (rust, none) | [115420500560](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222531/job/115420500560) | success | cc-pc-tools-linux |
| CodeQL PC / analyze (actions, none) | [115420500899](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222531/job/115420500899) | success | cc-pc-tools-linux |
| CodeQL PC / analyze (javascript-typescript, none) | [115420500958](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222531/job/115420500958) | success | cc-pc-tools-linux |
| CodeQL PC / analyze (go, autobuild) | [115420501027](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222531/job/115420501027) | success | cc-pc-tools-linux |
| Unattended readiness tools / go (Linux, capacity-audit-3h) | [115420503485](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222500/job/115420503485) | success | cc-pc-tools-linux |
| Unattended readiness tools / go (Linux, resident-guard) | [115420503835](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222500/job/115420503835) | success | cc-pc-tools-linux |
| Unattended readiness tools / repair-guard | [115420503837](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222500/job/115420503837) | success | cc-pc-tools-win-2510 |
| Unattended readiness tools / go (Windows, unattended-runner) | [115420503867](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222500/job/115420503867) | success | cc-pc-tools-win-2510 |
| Unattended readiness tools / go (Windows, resident-guard) | [115420503987](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222500/job/115420503987) | success | cc-pc-tools-win-2510 |
| Unattended readiness tools / go (Linux, unattended-runner) | [115420504096](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222500/job/115420504096) | success | cc-pc-tools-linux |
| Unattended readiness tools / go (Windows, capacity-audit-3h) | [115420504123](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222500/job/115420504123) | success | cc-pc-tools-win-2510 |
| Agent progress tools / windows | [115420556175](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222594/job/115420556175) | success | cc-pc-tools-win-2510 |
| Original record recovery staging / test (Windows, self-hosted, Windows, X64, cc-pc-tools-windows) | [115420535553](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222286/job/115420535553) | success | cc-pc-tools-win-2510 |
| Original record recovery staging / test (Linux, self-hosted, Linux, X64, cc-pc-tools-linux) | [115420536057](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222286/job/115420536057) | success | cc-pc-tools-linux |
| Channel reports / verify (Linux, self-hosted, Linux, X64, cc-pc-tools-linux) | [115420504831](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222393/job/115420504831) | success | cc-pc-tools-linux |
| Channel reports / verify (Windows, self-hosted, Windows, X64, cc-pc-tools-windows) | [115420505340](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222393/job/115420505340) | success | cc-pc-tools-win-2510 |
| ChatGPT browser fixtures / fixtures | [115420501448](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222360/job/115420501448) | success | cc-pc-tools-win-2510 |
| Bookmark queue / test (Linux, self-hosted, Linux, X64, cc-pc-tools-linux) | [115420499007](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222089/job/115420499007) | success | cc-pc-tools-linux |
| Bookmark queue / test (Windows, self-hosted, Windows, X64, cc-pc-tools-windows) | [115420499433](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222089/job/115420499433) | success | cc-pc-tools-win-2510 |

Build job115420510423/step9の全文Summary: 2145 tests run / 2144 passed / 1 failed / 5 skipped / 394.949秒。集計行を抽出・解析してJSONの数値と照合済み。doc(step10)と全workspace/alltargetsClippy(step11)はskipped。doc件数はnull（実行されていない）。
失敗: cc-jobs::parity end_to_end::dependency_fixture_does_not_read_its_callers_plugin_configuration。固定sourceのcc-jobs/tests/parity.rs:10597で e.cli(&["cancel", &parent], "").status.success() のassertion失敗。
観測: 子fixture成功、親稼働、別rootがrunning_limit待ちまでは成功。親cancelが非成功。cancelのstderr/exit値がassertionから出力されず内部根因は未確定。flakyや無関係と決めつけず、dependency_contractと取消処理・fixture寿命を親が調べ、取消結果の診断を取得する必要がある。プロセス識別・取消の安全条件は緩めない。
関連検査の必要範囲: cc-jobs::parityの当該依存fixture、real_supervisors_start_a_dependency_while_the_root_limit_is_full、test_cancel_kills_the_whole_treeと取消/保持ハンドル関連を先に確認。source修正と影響する最新workspace/doc/Clippyの検査は親担当。検証担当は手動Cargoとrerunを実行していない。
先行step8: job_references/verified_resumeの18passed/0failed/0skipped。孤立worktree・process-tree先行は各1passed。
Go: Windows/Linux各3moduleの全6jobでtest -race/vet/build成功。Windows repair-guardもtest/vet/build成功。ログcheckout SHAも同固定HEAD。Goのテスト個数は非verbose出力から推定していない。
Browser: 76passed/0failed/0skipped、関連Clippy成功。これはBuildでskippedの全workspaceClippyの代替ではない。
CodeQL: 4jobと4解析を実照合。全解析commit_shaは固定HEAD、refはrefs/pull/144/head、error空、results_count=0。全4jobでSuccessfully uploaded results/Analysis upload status is completeを確認。

| CodeQL | 解析API | rules_count | results_count |
|---|---|---:|---:|
| /language:javascript-typescript | [1945179842](https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/code-scanning/analyses/1945179842) | 87 | 0 |
| /language:actions | [1945161626](https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/code-scanning/analyses/1945161626) | 17 | 0 |
| /language:go | [1945153206](https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/code-scanning/analyses/1945153206) | 34 | 0 |
| /language:rust | [1945143684](https://api.github.com/repos/AutoDevJapan/cc-pc-tools-rs/code-scanning/analyses/1945143684) | 26 | 0 |

証拠: ci-2488-result.private.jsonは正規APIのrun/job/step、原ログURL、checkout照合と抽出ログSHA256を保持。*.acceptance.private.logは全文ログから選んだ非秘密受入行。build-2488-failure-context.private.logに失敗周辺の診断だけ保存。全ログは各job URLおよびJSONのjob_log_apiで親が取得可能。秘密・認証値を保存するためのログ全文ダンプは作成していない。
旧3581928の2143passed/1failed/5skippedは親の原仕様/checkpointの記録として保持。今回の集計と混ぜず旧failureを成功へ上書きしない。
本担当はsource編集、CI rerun、新全件、手動Cargo、PR作成/merge、runtime配布、担当停止、Discord送信をしていない。CI確認は02:27:27 JSTの30分期限より前に全件終端したためwait-retryは不要。
親2488の既存noteを保持して最終結果をCLI editで追記する。自分のToDoは通常のスケジューラ完了処理へ戻す。再開が必要ならこのJSONと同8run IDが原資料。親の失敗修正・関連検査・別モデル確認・merge・配布を本担当の完了へ拡大しない。
