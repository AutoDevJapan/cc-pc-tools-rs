# GoDD MCP αの再開記録（ToDo2641、2026-10-17）

## 2026-10-19：既知8失敗の原製品候補を先に修正

最新job `20261019-125449-todo-2641`。同branchのHEAD f50aefa、cleanから再開。
主2903の工程分離に従い、子2737の全CI成功を待たず、親だけが直せる原Rustのbaselineを修正した。
診断配送2925はdone、候補受領票2926は主2903の手動追跡なので本担当ではdoneにしない。

GitHub APIで実owner AutoDevJapan/repo1154066087/private/admin/nonfork、PR3613/open/draft、
head/base同repo、原branchを確認。更新前headは `085b5b007cab4ebd57d6690faf6df95bb40453ef`、
tree `25ae0b09830d1fbeef079e8ff6bcc033928bc8d8`。
原診断のblob `73904294f03d8ee9bf901570649370c64cb03b23` はWindows CI出力契約のRust sourceと一意に一致した。

同原ref `codex/todo2641-http-response-pseudo-20261015` へ通常commit/PATCH force=falseで反映済み。

- 新head `0fda87c591ae5e6fd256508d94376c38e16b2f4e`
- 新tree `0bc5ff599c5105474f88c6284e984b55583c5510`
- 新blob `d4b45a01ebdf56f5053440cd760833498fa663ce`
- 差分は `godd-mcp-alpha/native/tests/integration/windows_ci_output_contract.rs` の1file、54追加/1削除。
- ci.ymlはblob `64636a181386f3212d0190be11ffe9afa9903644` のまま不変。

共通baselineのrunner判定は、元のWindows literalと、現在の明示lease/手動dispatch/原branch/
Windows job+run_id/Windows fallbackで束縛した式だけを許す閉じた集合にした。
workflowから期待値を自動生成せず、任意文字列/任意self-hosted/別OS/条件削除を許可しない。
元のassert・mutation群、13Cargo工程・SDK/toolchain・profile・失敗伝播・並列/直列/20分条件は保持。
追加の回帰2testで正常2経路とNone/別OS/lease・ref・event・job・run_id・fallbackの改変を検査する。

軽量検査はcandidateの追加const/helper/回帰2testをそのまま抽出し、原ci.ymlの実Windows scalarも
正例へ加えたもの。host rustc1.95.0で旧literal述語exit101（2failed/1passed）、修正述語exit0（3passed）。
candidate全体のrustfmt edition2021 checkも成功した。原公式SDK1.97.1によるserde_yaml integration/
既知8検査/Windows13工程/正式CIの成功とはしない。Cargo割当なしの親は重いCargoを実行していない。
RED準備の最初の補助はrustfmtの改行による置換不一致で実行前に停止し、1個の関数を一意に置換してから
上記RED/GREENを実行した。原製品のテスト再実行や元の失敗ログの書換えではない。

原RED/GREEN全文と抽出した検査source、compiler版、bytes/SHAは
`D:/CC/Tools/product-ci/evidence/todo-2641-20261019/windows-route-0fda87c` へ保持した。
詳細は [docs/todo-2641-windows-route-fix-20261019.json](docs/todo-2641-windows-route-fix-20261019.json)。

追加差分と実Windows jobを本文へ供給した有限別モデルレビュー **2928/gpt-6-luna** を登録済み。
read-only担当に保存/commit/ToDo操作を求めず、原応答は親が受領する。
specは [docs/todo-2641-windows-route-review-spec.md](docs/todo-2641-windows-route-review-spec.md)。
受付をMust0とはせず、原2737/2922の終了metadata診断も未確定として保持する。

元2737と手動票2926のnoteへ実HEAD/tree/ref/blob/差分/検査範囲/レビュー未受領を配送した。
主2903が有限配送だけを検証して2926をdoneにし、2737は2922と2926の受領後に正式CIへ進む。
2737へ親2641の製品全体doneを待つ条件を追加していない。
再開時は2928の実応答と2737の新sourceの関連検査・正式CI結果を確認する。
同じ旧headの失敗を再送せず、新headの実合格・Must0を得るまでmerge/Prd成功としない。
この区切りで終了する場合は親2641を `wait --on 2737 --on 2928` として待つ。
原18/1153/Issue1100・解析37件・旧証拠/未回収/未知の終了原因を、この有限配送で完了にしない。
以下の10/17記録は当時の履歴として保持する。

## 03時台の再開とMust修正候補

最新jobは `20261017-032928-todo-2641`。同branchのHEAD `5219626`、cleanから再開した。
司令塔から原2738の実Sonnet応答を受領した。Must1は、評価用の一時runner labelをmainへ残すと
push/PR/週次解析がrunner不在で止まること。原応答を
[docs/todo-2641-review-2738-original.md](docs/todo-2641-review-2738-original.md) へ保存した。
SHA256は `CCA14CC6926E33D703FF9ADB54D5E7590D8201CB2E8FBAED95CCD2AE4740B5A3`。
原2738のdoneを製品Must0と扱わない。37件は同レビューにSARIF未供給のため全件未評価だった。

原GitHubの実owner/id/private/admin/nonfork/PR/head+base/refを再照合し、次の実修正を保存した。

- 修正commit: `012150cb853c1366b79783b25f5d825aef0c9824`
- tree: `99c3a9a454088e4ddb01a02595cfddc3d435192f`
- 同repoの保存branch: `codex/todo2641-codeql-route-fix-20261017`
- 元b194からの追加差分は2file。CodeQLのruns-on2行を元のubuntu-latestへ戻し、Specの異常系42とT17へrequest/response用pseudo-headerを逆にした拒否を追記した。
- workflowのblobは元mainと完全同一の `d1d593aad61699a96e6d2a131d8c1faea584782d`。
- 元mainから修正候補までの全差分はnativeとSpecの2fileだけ。`.github`差分0。
- nativeのblobは元b194と修正候補で一致。Rust/JS/runtimeへ追加変更なし。

原2737は新設guest準備の続行中で、固定b194の原製品CIは未実行のまま。
2753/2754/2746の実応答を待っている。旧固定の受付を壊さず検査を続けるため、
原PR3613のbranchはb194のまま保持し、候補を別の通常branchへ先に保存した。
これは新しいPRやPrd配備ではない。元PRへの適用と同最終headの検査は未完了である。
元2737の完了を修正開始の前提にせず、上記commitと追加差分レビューを先に進めた。

追加差分だけの有限Sonnetレビューを **2761** に登録した。
base/headと全2patchをspec本文へ供給し、既済HTTP修正や37件の再監査を求めていない。
read-only担当には最終返信だけを求め、保存・commit・ToDo操作を完了条件にしない。
[docs/todo-2641-must-review-spec.md](docs/todo-2641-must-review-spec.md) と
[docs/todo-2641-must-fix.json](docs/todo-2641-must-fix.json) を参照する。

レビュー指摘の対応: M1は候補で修正し適用・再確認待ち。S1はdispatchの成功をPR経路へ広げない。
S2は上記Spec2行に反映。N1は一時label自体を除去したため追加の由来コメントは不要。
N2の既存拒否code差とN3の既存UntilClose記述は今回の実行挙動を変えず、未修正として保持する。

CodeQL37件は保存SARIFのrule/件数とpath種別だけをプログラム内で照合した。
Actions27件はworkflow、JS2件はtest/fixtureを含むpath、JS4件とRust4件はその他のsource。
path名・位置・本文・値は出力していない。この区分だけで安全性やproduction除外を証明せず、
37件とも未評価のまま保持する。記録は [docs/todo-2641-findings-path-audit.json](docs/todo-2641-findings-path-audit.json)。

この未評価部分だけの有限checkを **2762** に `--needs cargo` で登録した。
原2738/2761の差分レビューや原2737の全件検査を重ねず、必要な固定SARIFと原GitHubの
RAM対応付け・信頼境界・実行可能性だけを確認する。specは
[docs/todo-2641-findings-check-spec.md](docs/todo-2641-findings-check-spec.md)。
登録を実確認済みや37件の評価完了とは扱わない。

原2738のMust1受領と修正候補、S/Nの対応をGitHub review **5552171006** にCOMMENTEDとして
元b194を指定して保存した。PR本文にも候補branchと未適用・未検査を追記し、GETの本文全一致と
元b194/draftを確認した。同じreviewや同じ本文を重複投稿しない。
元2737のnoteへも候補SHA・原PR/ref不変・旧結果保持・親待ちを作らないことを配送した。

再開時はまず2761の実応答を受領し、2737の固定b194結果と変更箇所を照合する。
2762の実害分類も受領し、必要な修正と追加差分の確認を続ける。
元PRへの適用は元branchのexpected旧SHAを再照合した通常更新だけとし、forceしない。
新headの正規CI/CodeQL・必要関連検査・Must0が揃うまでmerge/Prd成功としない。
この区切りで待つ場合は元2641に `wait --on 2737 --on 2761 --on 2762` を保存する。
この節より下の旧記録は、その時点の状態として保持する。

## 結論

PR3613のHTTP修正は保存済み。通常CIの未開始失敗と、CodeQL37件の評価が残る。
親2641は完了にしない。原18/1153の停止・依存・未完了も保持する。
本人への質問やPC操作依頼はない。Cargo/GPU/Civ6はこの親では実行していない。

## 今回の実照合

- 親worktreeのbranchは `scheduler/todo-2641-godd-mcp`。開始HEADは `a83aa2606e7354088cd23da111709729e4c82e35`。開始時clean。
- GitHub実owner/idは、製品がAutoDevJapan/GoDD-MCP-Alpha /1154066087、作業repoがAutoDevJapan/cc-pc-tools-rs /1406818713。製品はprivate/admin/nonfork。
- 原PR3613はopen、sameRepo head/base、head `b194d66524ef86330bb73ca3f22f9e5dd6ae0a07`、branch `codex/todo2641-http-response-pseudo-20261015`、base main `9ee640e763091c4688aaf6204e06f9fb1b493695`。
- 原差分はnative、Spec、CodeQL workflowの3ファイル。API compareで旧 `c53dc14f1b49f3d7d16a604f29b120de3ce8163d` から新b194への差分はCodeQL workflow1ファイル、runs-onの2追加/2削除のみ。`.github`以外の差分0。
- 原レビュー5520900843はgpt-6-luna、旧c53、Must0/Suggestion1/Nit0。Suggestion未採用の理由は既存の正常request回帰との重複。今回は新規レビュー実施済みとは主張しない。
- 新b194の公式CodeQL run38893804340は4言語とCodeQL gateがsuccess。現在のcheck APIでも確認。解析はActions27、JS6、Rust4、Python0。37件は未判定のまま。
- 通常CI38893656960はfailure。Verify check116948883594の公式annotationは `The job was not started because an Actions budget is preventing further use.`。CI Gateはfailure、Windows native check/PR tree receiptはskipped。再実行や手動successは行っていない。
- 2488/2505はdone。2648のGo基盤はPR187 merge/配布とtested/mergedの必要CI成功を配送済み。2648自身は2710/2730の回収待ち。2710/2730は既存2381待ち。既済検査や拒否された清掃を繰り返さない。

## 保存済み検査の限界

2710の旧c53に対するLinux/Windows/Rust/Node受入は元noteを保持する。
Linux2の原回収では部分70ファイルに最終manifestがなく、認証済み回収とは扱わない。
原baselineと同SHAの新しい独立cohortは別証拠としてSHA照合成功が配送されている。
新b194でnativeを再実行した結果にはしない。旧資源回収のツール拒否も元noteのまま保持する。

CodeQLの原log/SARIF/metadataは次の指定証拠に保持されている。
`D:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016/d6eb340d94b85fa2d879b467a597d459`
別SHAのmain alertをこの固定analysis結果と混ぜない。
SARIF本文・位置・識別名・秘密値をmodel/stdoutへ出さず、rule/count/hashだけ扱う。

## 新しい有限の独立工程

- **2737**: 原PR3613の通常CIを追加課金なしで完走する。`--needs cargo`、親2641のcheck subworkとして登録済み。保存specの存在、dispatchable、cargo要求、親/job対応を読戻した。既存のnative受入再登録ではなく、未実行の公式CIを正常化する工程。仕様は [docs/todo-2641-ci-spec.md](docs/todo-2641-ci-spec.md)。原PRのmerge/Prdは親が担当する。
- **2738**: 新b194の3差分とCodeQL37件の静的評価を既存OAuthのClaude Sonnetへ依頼。親2641のreview subworkとして登録済み。保存spec、指定model、親/job対応を読戻した。仕様は [docs/todo-2641-review-spec.md](docs/todo-2641-review-spec.md)。親や2737の完了を待たず有限応答を返す。
- reviewの最初のaddは `anthropic/` を含むmodel表記がtodoの引数検証でValueErrorとなり、項目は作成されなかった。ソースの検証とschedulerの正規化を確認し、受理される `claude-sonnet-5-5` で2738を1件だけ登録した。審査拒否の迂回ではない。
- 子Codexジョブは直接起動していない。受付を実行成功やレビュー済みと扱わない。モデル混雑は既存のOAuth枠で同じ対象を維持して切替える。

## 再開の順序

1. D:/CC/AGENTS.md、元2641の最新note/spec、このcheckpoint、2737/2738の実応答を読む。
2. 実owner/id/PR head/refをAPIで再照合する。2737がheadを進めた場合は差分と必要な再検査・CodeQLを照合し、古い結果を新headの成功へ流用しない。
3. 2738の実害指摘を修正し、必要な再検査・差分の別モデルレビューを行う。37件の未確認を消さない。
4. 同じ最終headの必要な公式CIとCodeQLが成功しMustを解消してから、親がPR3613を通常mergeする。Issue1100と原18/1153は残条件どおりopen/未完を維持する。
5. 元のnative release/署名/4target/npm昇格規則を保ち、既存Prd反映・受入・自分の資源回収まで追う。新費用やHosted予算変更へ広げない。

待機前は実HEAD/branch/jobでscheduler checkpoint-workspaceを保存し、`todo wait 2641 --on 2737 --on 2738`で待つ。メモだけの待機やowner blockへしない。
新しい製品checkout、原製品フォルダ・保護一覧・guard・登録manifestの操作、GPU、本人PC操作、Discord送信は今回0。

## 枠を譲った後の照合とPR本文

同じsession/jobで再開し、events.jsonlのitem_34はexit0/completedであったことを先に確認した。
停止前のAPI操作・登録を繰り返していない。2737はjob `20261017-004230-todo-2737` で起動を確認した。
2738は保存済みspecを持つtodoであり、起動や実応答はまだ確認していない。
原PR3613の本文を現状に更新し、旧native検査・現CodeQL成功・現通常CI失敗・37件未評価を明記した。
過去の失敗runとレビューの範囲を保持し、PATCH後のGETで本文の全一致とdraftを確認した。
非秘密の監査記録は [docs/todo-2641-audit.json](docs/todo-2641-audit.json) に保存した。

checkpointの保存結果は、最初の直接呼出しでは出力が空で、ToDo原本にも記録が無かった。
このexeはWindows GUI subsystemのため、PowerShellからの直接呼出しだけで完了判定をしない。
同じ正規コマンドを `Start-Process -WindowStyle Hidden -Wait -PassThru` と出力リダイレクトで実行し、
実exit0、saved=true、launched=0、原本のworkspace_checkpoint.requestを確認した。
以後もこの形式で実完了と同job/branch/HEADの保存を照合する。審査拒否や別経路への迂回ではない。

## 自分の一時資源

safe-delete所有ID `9315e029733a03edf587aa08e7c56175`。
原GitHubから取得した指示とworkflowの参照だけで、製品checkoutや実行資源はない。
非秘密の監査を保存した後、planで401416bytes/6entryを確認し、別呼び出しの通常削除がexit0。
finishでsource_absentを確認して回収した。原指示とworkflowは固定GitHub sourceに保持される。
原証拠や他担当の資源は触らない。
