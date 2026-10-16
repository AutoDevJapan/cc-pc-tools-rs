# GoDD Press 通常CIの再開記録（ToDo2766）

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
