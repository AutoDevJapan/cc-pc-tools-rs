# GoDD Press 通常CIの再開記録（ToDo2766）

## 2026-10-21 再開: 2737完了を受領し、Pressの準備差分を保存

この節が最新。通常CIはまだ未実行・未成功で、元2766は未完了。本人判断は不要。

- 今回のjobは20261021-094646-todo-2766。元ToDo2766はdoing、jobsとscheduler.jobに同jobが正規に紐付いている。前回のprepared/jobs空というcheckpoint拒否条件は今回の原記録では解消。台帳を直接変更していない。
- 2737はdone。元の最終7b8dbb6/checkpointと履歴noteを読み、実Windows13工程、最終Gate、同sourceの公式run39201344542 attempt9 success、レビュー3230のMust0、sourceproof/VM/runner/route回収を受領。これはPressのCI成功ではない。
- 2737の既存5 moduleとその履歴を自分のbranchへmergeして再利用した。新しいVM/SDK/sourcepacket実装や実行担当を複製していない。merge commit fe19223。READMEの競合マーカーを取り除いたf01265dの後に自分の差分を追加。
- 準備差分8a686793f397b1fa0d473e4b673332990228621d/tree55f79020fd1a22dcd9188df9a46d7e18894f06e9をcommit/push、remote ref一致を確認した。Product=press-2766と3 workflowの明示pinだけを既存sourcepacketへ追加。空profileのMCP αを保ち、元repo/owner/private/admin/nonfork/Draft PR175/元ref/baseを検査する。物理Git照合は共用。guest側のPress admissionはまだ接続していない。
- `press-ci-outline`は読取専用。元3workflowの完全Git blob/hashと、元PR/head/base/ref/treeを前後照合し、local-checks10 + V2 HTTPS2 + hidden1の13 jobを返す。4 integration shardと両OSを保持する。source命令やworkflow bytesをホストへ保存しない。出力のRunnerAdmission/OfficialCISuccessはfalse。
- 実APIで製品repo1386702183/AutoDevJapan/private/admin/nonfork、PR175 open/Draft/同head+base repo/ref、元b2 head/4dc baseを再照合した。原AGENTS全文と全workflowをRAM読取し、CLAUDE.mdは404。修復repo1406818713は現在public/admin/nonfork。製品sourceやAPI書込要求を修復repoへ混ぜていない。

### 検査と初回失敗

- sourcepacket全moduleのWindows raceは57.438sでパッケージok。追加3workflowの公開合成Gitを実Materialize/VerifyCheckoutし、正確なhead/tree/all blobs、改変blob拒否、別workflow pin拒否を確認。直接起動した最初のGo processは終了コード取得がnullだった事実を保持し、exit0を作らない。完全stdoutのokと実終了を確認済み。source vetは別プロセスでexit0。
- product-ci全module raceはmain1.622s/normalci1.066s/pressci1.066s、wrapperの実exit0。最初のvetはmerge後の型aliasに古いunkeyed literal9箇所が残り失敗。main/main_test/merged_jobの値を変えずkeyedへ修正しvet実exit0。型記法だけの変更で同じ成功検査を重複しない。
- 利用側ci-guest race14.360s/exit0、Windows runner race7.483s/exit0。どれも元製品Rustやguestの実受入ではない。
- 実APIのpress-ci-outlineはexit0、3元Git blobと13 job、dispatch true/false/false、SourceRevalidated=true、RunnerAdmission=false、OfficialCISuccess=false。git diff --check成功。
- 完全な原stdout/stderr/実exit/元workflow digest/merge観測/pushを、同worktreeの無視対象evidence-2766-20261021.private.jsonへ28recordとしてbyte/base64保存し、全bytes/SHAを読み返し一致。bundle SHA256 B648C581FEE067D56F98927C87A27CC2AF9153414811E4F3ACA2C5246EBCAAE5。元原ログ/旧cleanup拒否は変更していない。

### レビュー3246と次の手順

固定base f01265d40618dc45a0e9747a60c5ef4ecc62b61d → head8a686793の12file全文/局所を、別モデルの読取レビュー3246へ通常add --specで登録した。指示はTODO-2766-PREPARATION-REVIEW-SPEC.md。model claude-sonnet-5-5/effort max、dispatchable true、保存spec全文一致を確認。確認時はtodo/jobs空で、まだMust0の受領ではない。最初の`anthropic/`を含む--modelはCLIのモデル名文字規則でValueError・未登録だった。slash無しの正規aliasはschedulerがanthropic/へ正規化する実装を確認して登録した。承認審査の拒否や迂回ではない。

1. 正規wait2766 --on3246でレビューだけを待つ。親2642やdone2737を依存に入れない。3246の実job/対応turn_context/固定base/head/tree/全読了/未読を照合し、原応答を保存してMustを直す。子Codexを直接起動しない。
2. その後、既存guest controller/Windows backendへPress固有の有限admissionと元workflowの最小routeだけを接続する。現13 jobを一つも省かない。元2workflowはPR専用でdispatchが無い。原event/checkoutを保つなら、公式PR merge refの実sourceを候補と区別して物理固定する必要がある。元dispatch専用MCP bindingやlabel変更だけで受付成功にしない。
3. 今回の追加観測: PR API baseは元4dcのままだが、実heads/mainは2da7d4af1dd9911eaff970fdfe08b60531732eb6。refs/pull/175/mergeとPR merge_commit_shaはf3a704132f286d58eee2c16b819bc85cd4565c37、parentsは2da7d4aとb2、tree a0c5944f8fc69b78db78afd525d2eab104fe3319で元candidate treeと異なる。元4dc→mainはahead6/file5。これを同sourceへ読み替えない。outlineは元固定候補の読取結果であり、最新mainとの統合検査を保証しない。次の実行前に元head、現在base ref、実merge commit/treeとworkflowの全bytesを再照合し、実run/job/attempt/SHAの受入条件を追加する。無断でfeature sourceや親branchを更新しない。
4. 既存SDKがPressのChrome/Node/Rust1.95と必要なOS/CPU/RAM/元timeoutを満たすか、source-freeの既存仕様を確認する。足りないものは新しい使い捨てguestだけへ有限に追加し、原SDK/host/GPU/profile/auth/socketに広げない。既存cargo_gate/専用target/sccache/直列条件を保つ。
5. route/head更新が必要なら元PR/branchへexpected旧SHA/forcefalseの最小変更だけを行う。その新headで必要な公式CodeQL/native結果を実取得する。元b2 CodeQL5job成功はそのまま保持し、head不変の間に根拠なく再実行しない。元9件のalert判定は2765の範囲。
6. 通常CIの全必要公式job、原ログbytes/SHA、source固定、失敗原因と修正後再検査、取消し/回収/lock解放を区別して親へ返す。親PRのmerge/配布/全製品doneは本工程では行わない。

### 回収と再開保存

新safe-delete temp85bf6d016d4f513bbdda56ebeb6fc35bは、終了した自分のGo検査だけに使用。原記録を上記bundleへ保存/照合後、planの通常削除を別呼出しで実行しfinish(source_absent)を確認。6305bytes/29entries。空き容量の前後差は測っていない。新しいVM/runner/container/ブラウザ/GPU/Civ6/Cargo/Discord/本人PC操作/新費用は今回なし。旧2710/2648/2730/2737や他担当の未回収は触っていない。

この再開文書とレビュー指示を自分のbranchへcommit/pushし、実job/branch/実HEADでcheckpointをHidden/Wait/PassThru保存する。要求・実exit・原todo.scheduler.workspace_checkpointの読戻しはcheckpoint-2766-20261021-*.private.jsonに保持する。共有ToDoの状態は正規waitを除きschedulerに任せ、noteだけ通常editでこの再開情報を残す。確認できない進捗率を作らない。

2026-10-17 04:51 JST。親2642の独立した有限check。通常CIは未実行・未成功。

## 今回確認したこと

- 起動入口の `D:/CC/AGENTS.md`、本worktreeの `AGENTS.md`、共有ToDoと日本語報告のSkillを読んだ。本worktree直下にCLAUDE.mdはない。
- 親2642の最新ToDo本文、`TODO-2642-CHECKPOINT.md`、`TODO-2642-OFFICIAL-CI-SPEC.md` を読んだ。原資料は変更していない。
- 共有ToDoの原記録で2737はwait、未完依存は2746。親の最新記録では2748/2753/2754は既済だが、2737自身の基盤統合・公式CI完了を意味しない。
- 本仕様の最初の条件は「2737が未完なら2766を2737の完了待ちにする」。同じVM、Windows補助、sourcepacket実装、レビュー担当を追加しない。
- 親2642は2766の結果を待っている。2766の依存先に2642を入れない。

## 今回の区切り

この記録だけを自分の作業ブランチ `scheduler/todo-2766-godd-press-ci-2642` へcommit/pushする。共有ToDo2766には正規 `todo.exe wait 2766 --on 2737` を記録する。進捗は推測して付けない。scheduler checkpointは実job・実branch・実HEADで保存し、終了コードと原記録の一致を確認する。実行結果は同worktreeの無視対象private JSONへ保存する。

製品ソース・workflow・PR・runnerへ今回の変更はない。Cargo/GPU/Civ6/VM/container/ブラウザ/Discord送信/本人PC操作は行っていない。新しい一時フォルダを作っておらず、回収する一時資源はない。古い回収拒否資源も触っていない。

## 保存結果と再開情報の照合不足

最初の記録をcommit `a2b6d908b19b82028e894fc069b2ba2871ba6686` としてpushし、リモートrefと実HEADの一致を確認した。修復リポジトリは実APIでAutoDevJapan/private/admin/nonfork、ID1406818713を確認した。独立PRの作成やmergeはしていない。

2026-10-17 04:52 JST、実job `20261017-044530-todo-2766` と同じ実branch/HEADの要求で、指定のHidden/Wait/PassThruによりcheckpointを1回実行した。実プロセスは終了済み、exit1、`workspace checkpoint failed InputRejected`。原ToDoのcheckpointはnullであり、保存成功とはしない。これはschedulerの入力・所有照合による拒否で、自動承認レビューの拒否ではない。

有限の読取調査で、原job.jsonはrunning/todo_id2766/cwd一致だが、原ToDo2766はtodo、scheduler.phase=prepared、scheduler.job=null、jobs=[]だった。保存branchと実branchは一致する。`cc-scheduler/src/workspace_checkpoint.rs` のsource選択は当該jobが原ToDoのjobsにあることを要求するため、この欠落で拒否される。詳細な要求・exit・原記録抜粋は本worktreeの `checkpoint-2766-*.private.json` に保存した。

ジョブ紐付けの偽装、原台帳の直接書換え、startによる自己紐付け、ブランチ切替、同一失敗の再試行はしない。変更したのは自分の再開文書だけで、branch自体は切り替えていない。正規waitで2737依存を保持し、次の正規再開時にジョブ紐付けとworkspace checkpointを再照合する。本人判断は不要。元ToDoのnoteにもこの不足と次の処置を残す。

## 2737完了後の再開

1. 起動入口の運用指示、親2642の最新note/spec、2737の完了記録・レビュー・実guest・source固定・通常CI結果を読む。原GitHubのAGENTS.md/CLAUDE.mdとworkflow全工程は製品操作前に読む（今回は未取得）。
2. 実GitHub APIでrepo ID1386702183 / AutoDevJapan / private / admin / nonfork、PR175のopen/draft、同head+base repo、refを照合する。保存候補が進んでいれば、旧結果を新候補へ流用せず範囲を照合する。
3. 保存対象はbranch `fix/todo2642-schedule-predecessor-overflow`、head `b2b6d47b7f9e17617d16a9079a70de272bc3c04c`、tree `dac21168dfdb55b007da5c06f36c05e66de0a9f7`、base `4dc5c332d54d19325f122df1f0e4a52e9dd3a629`。これらは親からの引継ぎ値であり、本担当が今回API再取得した値ではない。
4. 必要なPress固有の有限admission/workflowだけを既存基盤へ追加する。原工程・4 integration shard・V2 HTTPS両OS・Windows hidden-fixturesを省かず、固定sourceと単一jobの公式結果、原ログbytes/SHA、取消しと回収まで親へ返す。
5. 元CodeQL公式run38893812340の5job成功と、通常CI runs38893666794/38893667129/38893666823のActions予算による起動前失敗を区別する。成功済みCodeQLの再実行や失敗runの盲目再試行、手動successはしない。headを更新した場合の必要な新結果は別途実取得する。
6. 原2710のa39a2b1 native成功をb2候補での再実行へ読み替えない。9件の実害判定は別担当2765の範囲。親の最新結果を受領し、alert修正/dismissへ範囲を広げない。

製品checkoutはホストへ作らず、製品sourceはRAM/所有する隔離guestだけで扱う。原製品folder/名簿/保護一覧/guard/登録manifestを読まない・変更しない。既存profile/auth/hostsource/socket/GPUへの到達を許可しない。APIキーAI・追加費用・Hosted予算/契約/scope増加は禁止のまま。親PRのDraft解除・merge・配布・製品全体done、原29/2191の停止/依存/未完の変更は本担当の範囲に含まれない。
