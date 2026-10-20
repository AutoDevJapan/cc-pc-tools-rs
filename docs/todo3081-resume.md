# ToDo3081の再開記録

通常担当のSol/Max/Fastと、混雑時に同モデルの別OAuth枠を先に試す実装を進めている。
Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/277 。まだ完了ではない。

## 保存した範囲

- worktree: D:/CC/Work/unattended-operations-20261013-wt/todo-3081-fast
- branch: scheduler/todo-3081-fast。開始baseはc8d71fb。実所有AutoDevJapanをAPI確認し、作業場所のguardはfalseだった。
- 本人の正本: D:/CC/Policies/owner-model-effort-and-pool-retry-20261020.md。
  Console UIとSkillsの回答待ちは保持。source2903、3070/3050/3080は変更していない。
- 受入仕様と実測: [model-effort-pool-retry-3081.md](model-effort-pool-retry-3081.md)。
- 主な変更: cc-job-policyのprofile、cc-jobsのcapacity/pool_retry/execution/jobs/queue、
  cc-schedulerの既知providerとOpus alias、cc-askのモデル修復、対応する受入検査。
- 起動要求、native turn_context、providerの実効値を区別する。
  既に動いていた担当全体をMax/Fastへ変更したという結果ではない。

## 実行済みと未実行

1. 検査3083は固定c4eea9aの実Redを完了。
   policyはhigh/期待maxで失敗。jobsはFast引数なしで失敗。明示model/effort保持は合格。
2. 検査3087は完了。bb7cb6bのpool Redは実Sonnet/期待Solでexit100。
   871e184はmodel_effort/pool_retryが8/8合格、parityの関連28件は27合格/1失敗。
   失敗はcapacity_preserves_scope_wait_and_review_constraintsで、親も除外情報もないレビューに候補を返した点。
   f4b026d以降の早期returnで修正したが、再実行結果はまだ無い。
   完全ログは検査3087のdocs/evidence/todo-3087-cargo-check。
   4crate全体とdoctestは、関連検査の失敗により未実行だった。
3. 別モデル読取3088はc8d71fb..871e184を担当。最後の観測時は実行中。
   native turn_contextでanthropic/claude-opus-5-5 / maxを確認した。
   検査3087もnative Sol/maxを確認した。どちらも結果未受領。
4. 871e184より後に、待機中のqueue/state/ToDo照合、世代再確認、
   native観察の期限、明示effort保持、同providerの別レビュー候補を修正した。
   account selectorを使うCodexに、元モデルのcatalog metadataをjob単位で渡す処理も加えた。
   これらは3087/3088の固定候補に含まれない。追加差分の関連検査・別モデルレビューが必要。
5. OpenCodex 2.83.0の正規metadataと公開同版sourceを照合した。
   既存の公開selectorから別枠のSol/Max/priorityへのHTTP要求は200/completed。
   requested selectorとaccountLogLabelが一致し、served modelはSolだった。
   Sonnet/Opus Maxの短いOAuth要求も200/completed。品質比較ではない。
   Sonnetの既存adapterはMaxをadaptive thinkingへ写像する。今回400は出ていない。
6. 実upstream Fastとactual effortは未確認。要求priority、応答default、
   response authoritative=false / confirmation=assumedだった。
7. この担当ではCargo/GPUを起動していない。rustfmtの構文確認とgit diff --checkだけ実行した。
   正式CI・PR・マージ・release build・配布はまだ行っていない。
8. 追加検査3091を登録した。native metadata用のRed専用commitは0e48665f2aabb5c6d28c7cc13fbd4f9ad80a56d0。
   これは871e184へfixture/parityの2ファイルだけを載せた比較用refで、作業ブランチを切り替えず別indexから作った。
   3091はこのRedと、Green候補6b6e7d9を固定して検査する。catalogと実process fixture、
   4crateの関連検査・doctest・変更exampleのコンパイルを含む。登録を検証成功とは数えない。

## 次の工程

- 3091/3088の結果を、元の固定HEAD・実モデル・原ログと照合する。3087の結果は受領済み。
- 実害のある指摘とコンパイル/検査失敗を直す。871e184以降の差分も含めて関連検査を行い、
  追加差分とMust修正を別モデルへ確認する。重いCargoは割当のある正規ToDoへ登録する。
- 新しいcatalog処理は本体とfixtureの経路を検査する。通常テストではAIを呼ばない。
  exampleは直接Codexを起動する明示受入用なので、今回の担当の実行条件では起動していない。
- 合格した最終ソースでPRを作り、正式CIを確認して通常マージする。
  最新mainへ取り込むときは他担当の変更を保持し、変更で無効になった範囲だけ再確認する。
- releaseと既知の元配備先を確認し、AdminBrokerで有限CAS配布を行う。
  cc-jobs、scheduler、discord_askの並行配布とソース版を照合する。
  Consoleの正常な既存版を未承認UIの候補に置き換えない。
- 配布後に新規起動の引数・native turn_context・可能なproviderメタデータを確認する。
  旧担当や実upstream tierの未確認を、確認済みと報告しない。

## 作業用の所有記録

safe-delete所有IDは951cd46c49333fa6208f0816b1e45b09。
作業用ファイルは%LOCALAPPDATA%/Temp/codex-owned-951cd46c49333fa6208f0816b1e45b09。
正規CLIのspec、限定OAuth要求の内容を含まない結果、公開source、checkpoint.jsonを保持している。
最終HEADはその時点のgit rev-parse HEADとschedulerの保存結果で確認する。
完了後に同じ所有IDのplan→通常削除→finishを行う。拒否時はSkillのquarantine手順を使う。

scheduler.exeはGUI subsystemのため、PowerShellの単純な&実行だけではstdout取得前にshellが終わる場合があった。
作業用hidden-cli.ps1はCreateNoWindowとstdout/stderrの並行回収、WaitForExitを使う。
これを使ったcheckpointでsaved=trueを確認した。最終保存でも同じ正規CLIを待って結果を確認する。
