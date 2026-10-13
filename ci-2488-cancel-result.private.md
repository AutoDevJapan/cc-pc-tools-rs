# 固定CIの確認結果（ToDo2600、親2488）

旧指定コミットの8 workflowはすべて終端となった。2成功・1失敗・5取消であり、取消修復の成功は確認できていない。現在の指定cff5c84の先行関連検査は19pass・3fail・317skip。失敗の原全文・診断を親へ配送し、残る未終端CIを同じToDoで確認する。

- 対象: PRIVATE AutoDevJapan/cc-pc-tools-rs、PR144。
- 固定SHA: `9aac8c6f1d976395c799fb8cbfdede7f158e75ce`。
- GitHubのrepo APIで実所有者AutoDevJapanとprivate=true、PR APIで開始時の同HEADを確認した。
- 確認中にHEADが `2295bdc710a410c0c637efcd5f77af40066e9d1b` へ進んだ。compare APIはahead=1、behind=0、変更は `cc-ask/src/recovery_messages.rs` の4行削除だけ。旧結果を新HEADの成功に流用しない。後段に親の更新仕様による後続確認を記録する。
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

最初の後続変更は重複部分の削除だけである。取消試験の成功は未検証。必要な関連検査の範囲は上記cc-askのcommands/resume/channel_reports/ask_dedup、取消修復のcc-jobs parityフィルタ、cc-todoのjob_references/verified_resumeであり、新固定HEADの正規CI結果で確認する。

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

旧指定SHAの報告を作業ブランチの159e52bで保存pushし、remote一致を確認した。親2488へ正式todo editで配送し、最新の既存noteが全て保持され配送本文も一致することを再読した。配送の時刻・前後の本文SHA256は `ci-2488-delivery.private.json` に保存。旧SHAの確認は完了したが、親が保存仕様を更新したため、このToDo全体は継続する。

## 親の更新仕様に従う後続確認（06:23〜06:27 JST）

自分の保存仕様に親2488が追記した「最新版HEADと自動新CIをAPIで実取得し、関連先行/新全件/docClippyを確認する」を読んで反映した。自分のspec・状態を変更せず、旧結果と後続結果を分けて保持する。

重複削除版 `2295bdc710a410c0c637efcd5f77af40066e9d1b` の8 workflowは1成功・7取消。Bookmark38469265099の両OSは成功し、Windowsのcc-ask commandsは34pass・resumeは81pass、前段Rust10pass・Node14passでfail/skip=0。Channel38469265392のLinux job115543782512は成功し、前段72passにcc-askのchannel_reports22・ask_dedup3・commands34が加わり131pass・0fail・0ignored。原全文ログで重複定義によるコンパイル失敗の解消を確認した。この版でもBuild38469265347は取消で、取消fixture・workspace/doc/Clippy成功の証拠にはならない。CodeQL38469265414は4言語とも取消、同SHA解析なし。

さらにPRの正規HEADは `787eb4df7c01b5f774c2064ff6b238b8fb8a96ff` へ進んだ。2295から1commit、変更はcc-jobsのjobs.rs/queue.rs/queue_status.rs/parity.rsだけ。API compareとGit diffを保存。取消識別の確認・作成時刻欠落時の占有保持を対象とする通常の親修復範囲である。

| 最新workflow | run | 最後に取得した状態 |
| --- | --- | --- |
| Build speed | 38470144152 | queued |
| CodeQL PC | 38470143944 | queued。ただしGo job115546812461はfailure |
| Readiness | 38470144033 | queued |
| AgentProgress | 38470144030 | success、同SHAのtest/vet/build原ログ保存 |
| RecordRecovery | 38470144025 | queued |
| Channel | 38470144110 | queued |
| Browser | 38470144083 | queued |
| Bookmark | 38470143953 | queued、Linux jobはin_progress |

最新CodeQLのGo失敗は、checkoutやautobuildより前のLinux runner開始hook `/opt/bridge/job-hook.sh` が `bridge rejected (403)` を返したもの。`ci-job-115546812461.private.log` 23行がHTTP拒否、24行がexit1。正規job logs APIで原全文1690文字を取得し、check annotationもexit1を確認。Goソースのコンパイル失敗とは数えない。403を出した内部の判定理由はこの原ログに無く、親のPC CI bridge受付・当該job admissionの確認が必要。新SHAの解析APIに4言語いずれのupload/解析も未登録。自分はCI再実行やbridge変更を行わない。

有限の今回確認は30分以内で区切り、最新8run・同spec・同worktree・既存成果を保持してToDo2600のwait-retryで再開する。最新SHAの関連/workspace/doc/Clippy、4言語CodeQL、残るGo検査は未終端または未確認。親のMust対応/merge/配布/運用受入は引き続き親へ残す。

## 06:41 JSTからの再開: cff5c84の実結果

途中commit `ea8200d` とgit statusを照合して再開した。未コミットの変更は前回の証拠だけで、ソース変更はなかった。最新の保存specと親の再開記録を読み、PR APIのHEADが `cff5c8421bff80ab7a0c65a471429b7279ef4cd9` と一致することを確認。787から1commit・queue_status.rs/parity.rsの2ファイル（+35/-10、+13/-0）だけの修正をcompare APIと固定Git diffで照合した。

| workflow | 同cffのrun | 06:42〜06:44取得時 |
| --- | --- | --- |
| Build speed | 38470825098 | failure、cc-jobs job115549141105失敗・package-tools skipped |
| CodeQL PC | 38470825104 | queued、Rust/Actions/JSの3job success・Go queued |
| Readiness | 38470825011 | queued、Linux unattended-runner/capacity-auditの2job success・5job queued |
| AgentProgress | 38470825052 | success、job115549134833原全文保存 |
| RecordRecovery | 38470825176 | success、両OSの原全文保存 |
| Channel | 38470825143 | queued、Linux job115549140146 success・Windows queued |
| Browser | 38470825108 | queued |
| Bookmark | 38470825145 | success、両OSの原全文保存 |

### Buildの先行関連と3件の根拠

Build run原全文 `ci-run-38470825098.private.log` とjob原全文 `ci-job-115549141105.private.log` を別々に取得した。先行は順にfixture resume **1pass/0fail/80skip**、tree取消 **1pass/0fail/338skip**、cc-todo job_references/verified_resume **18pass/0fail/0skip**。続くdependency_/cancel::/resume拒否フィルタは **22実行・19pass・3fail・317skip** で終了値1。skipは各フィルタによる除外数であり、全workspaceの集計に足さない。

| 失敗した試験（cc-jobs parity） | 固定ソース位置 | 原runログの診断 |
| --- | --- | --- |
| cancel::test_never_kills_a_process_that_reused_the_pid | parity.rs7431 | 623〜624行: unwrapがErr("live process has no recorded creation time") |
| run_validation::resume_refuses_a_lost_writer_when_its_identity_is_inaccessible_or_missing | parity.rs8254 | 648〜649行: unwrap_errがOk(0) |
| cancel::test_spawned_child_is_stopped_if_supervisor_setup_fails | parity.rs7421 | 667〜668行: supervise().unwrapがprepare write job.json: disk fullのErr |

PID再利用試験はpid/codex_pid=555、実probe=99を与える一方、保存作成時刻を与えず成功/lostを期待している。固定jobs.rs344〜354のchecked仕様は、生存PIDの保存時刻欠落を明示的なErrにする。このfixture/期待の不整合を直す必要があり、時刻欠落を終了と見なすように本体の拒否を緩めてはならない。

再開拒否試験はlost active-writerのcodex_pid=321へアクセス拒否または作成時刻欠落を与えるが、runがOk(0)を返した。どちらの反復で失敗したかはログに表示されない。固定CLI827〜837の同session拒否は `registration_jobs` の結果を使い、その入口はqueue.rs793〜803の公開index→admission_record→admission_activeである。公開indexに入る前のfixtureと、実受付までの経路を合わせて確認する必要がある。checked helperの成功だけではこの受付負例の成功を保証できない。失敗がfixtureだけか本体かをこのログだけで断定しない。

固定ソースをさらに追うと、Test::newは受付indexを作らず、Test::putはjob.jsonの保存だけを行う。Context::save/write_jsonもindexを更新しない。一方、通常のCLIはtrack_queued_job（CLI986、queue869〜892）で登録を公開する。今回の負例はtrack_queued_jobを通さず、欠落indexを読んだregistration_jobsが空配列を返す経路に一致する。したがって今回の直接原因は、負例fixtureが実際の公開受付経路を再現していないことだと固定ソースから推定できる。index公開を与えた上で拒否・supervisor0・原record不変の負例を実検証する必要がある。これは静的な経路照合であり、手動Cargoによる修復確認を行ったものではない。

停止補助試験の `disk full` は、parity.rs7420の `save_failure=true` とFake runtimeの6125〜6126行で注入する `io::Error::other("disk full")` に由来する。実ドライブの空き容量不足ではない。superviseの保存失敗後の戻り値・子停止・最終状態を一緒に整合させる必要がある。

`dependency_fixture_does_not_read_its_callers_plugin_configuration` は同cffの原runログ673行でPASS。cancel終了値/stdout/stderrは成功時に出力されないため、前版のOS根因を確定したとはしない。workspace/doc/all-targets Clippyは先行失敗後で未実行、件数nullを維持する。前版の2144pass/1fail/5skipを流用しない。

3件の具体診断・関連検査範囲は親2488へ正式todo editで配送済み。既存note全体の保持と配送本文を再読し `ci-2488-delivery-cff-triage.private.json` に照合記録を保存した。修正・CI rerun・Cargo・レビュー・merge・配布は本checkで行わない。

### 同cffのその他の実結果

- Bookmark: 両OSともRust10pass・Node14pass・fail/skip=0。Windowsのcc-ask commands34pass・resume81passも成功。原全文job115549139002/115549139573を保存。
- RecordRecovery: job115549135151（Windows）と115549135653（Linux）は各30個のPASS表示（親試験28＋subtest2）、FAIL/SKIP表示0、race test/vet/buildがsuccess。WindowsはGoのcached結果なので新規の全ケース再実行と数えない。
- AgentProgress: job115549134833は2Go toolのtest/vet/build成功。非verboseのため個別test件数は不明。
- ReadinessのLinux unattended-runner/capacity-auditはAPI上success。他の5jobはqueued。完了したjobも原全文取得とコマンドの実結果確認が残る。
- CodeQL: Rust解析1947113490、Actions1947123829、JS/TS1947134954は同cff全桁/ref/categoriesが一致し、error空・results_count=0。Actions/JSの原ログはSuccessfully uploaded resultsを確認。Rust jobはAPI上success、原全文取得は未完。Goはqueued、解析なし。前版787のGo hook403を同cffの失敗へ移さない。

GitHubが返した正規API応答・全ての取得原ログ・固定ソース読取結果を保持する。未終端分と原ログ取得が終わるまでこのcheckは未完了であり、必要なら同ToDoをwait-retryで再開する。

### 06:50 JSTの追加確認

CodeQL38470825104とChannel38470825143はsuccessへ進んだ。CodeQLは4言語のjob successと原全文を取得し、Rust/Actions/JS/GoそれぞれのSuccessfully uploaded resultsを確認した。同cff全桁とpull refを照合した解析は順に1947113490/1947123829/1947134954/1947167455で、すべてerror空・results_count=0。原ログのupload行は各jobファイルの971/815/1529/5453行。

Channelの両OSもsuccess原全文を取得し、Windowsは157pass・0fail・0ignored、Linuxは131pass・0fail・0ignoredを確認した。OSごとの対象差があるため、片方の値をもう片方へ流用しない。

残りはBrowser38470825108（job115549423875、in_progress）とReadiness38470825011（Linux3job success・Windows4job queued）。Readinessの完了3jobは115549140340/115549140366/115549140599。原ログAPIは制御文字の出力制限で取得できないため、workflow終端後の正規run viewログ取得が残る。前回拒否された追加flagを再試行せず、既存ゲートも変更しない。

今回も30分以内の区切りで、同じspec・作業場・8run・保存結果のままwait-retryする。Source失敗3件の修復は親だけが行う。本人判断待ちや独立監視jobを作らず、全8件の終端・原ログと必要結果が揃うまで有限確認を続ける。
