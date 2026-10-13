# 固定CIの確認結果（ToDo2600、親2488）

指定コミットの8 workflowはすべて終端となった。2成功・1失敗・5取消であり、取消修復の成功は確認できていない。この有限な確認工程と、親の修復・マージ・配布・運用受入を区別する。

- 対象: PRIVATE AutoDevJapan/cc-pc-tools-rs、PR144。
- 固定SHA: `9aac8c6f1d976395c799fb8cbfdede7f158e75ce`。
- GitHubのrepo APIで実所有者AutoDevJapanとprivate=true、PR APIで開始時の同HEADを確認した。
- 確認中にHEADが `2295bdc710a410c0c637efcd5f77af40066e9d1b` へ進んだ。compare APIはahead=1、behind=0、変更は `cc-ask/src/recovery_messages.rs` の4行削除だけ。新HEADのCIはこの工程の対象外であり、旧結果をその成功に流用しない。
- 正規gh OAuthと既存ゲートを使用。CI再実行・手動Cargo・source編集・merge・配布・Discord送信・GPU利用を行っていない。

| workflow | run | 結論 | job内訳 |
| --- | --- | --- | --- |
| Build speed | 38468005319 | cancelled | 2取消 |
| CodeQL PC | 38468005305 | cancelled | 2成功・2取消 |
| Unattended readiness tools | 38468005311 | cancelled | 3成功・4取消 |
| Agent progress tools | 38468005398 | success | 1成功 |
| Original record recovery staging | 38468005306 | cancelled | 1成功・1取消 |
| Channel reports | 38468005401 | cancelled | Linux失敗・Windows取消 |
| ChatGPT browser fixtures | 38468005314 | success | 1成功 |
| Bookmark queue | 38468005303 | failure | Linux成功・Windows失敗 |

21 jobは9成功・2失敗・10取消。全run/jobの固定SHA・attempt=1・終端statusをAPI保存結果で照合した。失敗jobをworkflow全体の取消で隠さない。runは `https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/<run番号>` で参照できる。

## 確定した失敗原因

固定ソースの `Event` に `OriginCorrected { request: Box<Request> }` が662行と670行の2箇所にある。これはAPIのCIログとGitの固定blobの両方で確認したコンパイル失敗である。

1. Bookmark Windows job115539608359: `cargo test --locked -p cc-ask --test commands --test resume` で `error[E0428]: the name OriginCorrected is defined multiple times`。662行が前の定義、670行が再定義。終了値1。原ログ `ci-job-115539608359.private.log` の778行以降に診断を保存。
2. Channel Linux job115539608716: `cargo test --locked -p cc-ask --test channel_reports --test commands --test ask_dedup` で同じE0428と、derive由来のE0004を4件出して終了値101。原ログ `ci-job-115539608716.private.log` に全文を保存。

親の最新変更は重複部分の削除だけである。新HEADのコンパイル成功や取消試験の成功は、この工程では未検証。必要な関連検査の範囲は上記cc-askのcommands/resume/channel_reports/ask_dedup、取消修復のcc-jobs parityフィルタ、cc-todoのjob_references/verified_resumeであり、親が新固定HEADの正規CI結果で確認する。

## 取消・全workspace・doc・Clippy

Buildのcc-jobs job115539608794は待機中から取消となり、正規ログ取得は `log not found`。package-tools job115543790279も取消。このSHAの先行関連・workspace・doc・all-targets Clippyの実結果は存在を確認できず、件数はnull（未実行/未確認）とする。

固定workflowでは先行関連として、fixture resume、tree cancellation、cc-todoのjob_references/verified_resume、cc-jobsの `dependency_` / `cancel::` / `resume_refuses_a_lost_writer_when_its_identity_is_inaccessible_or_missing` を確認してからworkspace/doc/Clippyへ進む。いずれもこのSHAではpass/fail/skipを報告できない。

前の `1e9cd8c` の2144pass・1fail・5skipは今回の結果に含めない。`dependency_fixture_does_not_read_its_callers_plugin_configuration` のparent取消終了値/stdout/stderrは今回のBuildが始まらなかったため取得できておらず、そのOS根因とSonnet2591 Must1の解消は未確認のまま親へ残す。

## 実行済み検査

- Browser job115539608352: nextest Summaryは76実行・76pass・0fail・0skip。
- Bookmark Linux job115539608008: Rust10pass・0fail・0ignored、Node14pass・0fail・0skip。Windows job115539608359でも同じ前段は通過し、その後cc-askのコンパイルで失敗。
- Channel Linux job115539608716: cc-channel-watchは9pass、cc-chromeは21pass、cc-job-policyは42pass、計72pass・0fail・0ignored。その後のcc-ask試験はコンパイル前停止であり、テスト失敗件数として数えない。
- AgentProgress job115539608711: agent-progressとcodex-progress-launchのGo test（各1 package ok）、vet、buildを実行しjob success。Goの個別テスト件数は非verboseのため不明。
- RecordRecovery Windows job115539609517: `go test -race -v ./...` で30pass・0fail・0skip、vet/buildを実行しjob success。Linux job115539609094は取消でログなし。

Readinessの7 jobは以下のとおり。

| job | 対象 | 結果・根拠 |
| --- | --- | --- |
| 115539608578 | repair-guard | test 1 package ok・vet・build、success |
| 115539609003 | Linux unattended-runner | race test 1 package ok・vet・build、success |
| 115539609055 | Windows capacity-audit-3h | race test 1 package ok(cached)・vet・build、success |
| 115539608991 | Windows unattended-runner | race test 1 package ok(cached)、vet/build入口のログあり、job cancelled。全job成功とはしない |
| 115539608969 | Linux resident-guard | cancelled、ログなし |
| 115539609037 | Windows resident-guard | cancelled、ログなし |
| 115539609088 | Linux capacity-audit-3h | cancelled、ログなし |

非verboseのGo jobはテストケースのpass/fail/skip件数を推測しない。キャッシュ再利用を新しいケースの再実行として数えない。

## 同固定SHAのCodeQL

| 言語 | job | 結果 | 解析ID・upload/error |
| --- | --- | --- | --- |
| JavaScript/TypeScript | 115539608338 | success | 1946920077、Successfully uploaded results、error空、results_count=0 |
| Actions | 115539608593 | success | 1946954871、Successfully uploaded results、error空、results_count=0 |
| Rust | 115539608634 | cancelled | 短い取消ログあり。解析APIにこのSHAの解析なし、upload未確認 |
| Go | 115539608802 | cancelled | ログなし。解析APIにこのSHAの解析なし、upload未確認 |

解析APIではcommit_sha全桁・ref=`refs/pull/144/head`・categoryを照合した。CodeQL4言語の成功やRust/Goのerror空を主張しない。

## 保存した証拠と取得制約

このworktreeに `ci-run-*.private.json`、`ci-jobs-*.private.json`、PR開始/終了・compare・固定SHA解析のJSON、固定ソース/Build workflowの読取結果、および取得できた全job原ログを保持する。`ci-2488-cancel-result.private.json` に各証拠のSHA256・bytes・関連ログ行番号、ログなしの取得結果を記録する。原全文ログとprivate JSONはGitへ追加しない。

jobログAPIは制御文字の出力制限、追加オプションは既存ゲートの未知flag拒否で利用できなかった。ゲートを変更せず通常 `gh run view --job --log` で完了runのログを取得した。HTTP502/504、TLS timeoutは有限に再取得し、取消jobのログ不在は正規CLIの `log not found` 原文を保持する。これを試験失敗や成功へ読み替えない。

この有限工程の受入は結果・原因・未検証範囲の保存と親2488への正式配送。親のMust対応・新HEADの必要CI・merge・稼働版反映・運用受入は未完了として保持する。
