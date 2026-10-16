# GoDD MCP αの再開記録（ToDo2641、2026-10-17）

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
