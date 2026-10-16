# GoDD MCP αの通常CIを追加課金なしで再開する

元ToDo2641、元の登録対象18/1153。2026-10-15の本人依頼は、GitHub上の原リポジトリから価値のある未完工程を一つ実装し、検査・別モデルレビュー・通常PRへ進めること。これはPR3613の通常CIだけを進める有限の独立工程であり、製品全体や原18/1153を完了にしない。

## 固定対象と確認済みの状態

- 実API確認: AutoDevJapan/GoDD-MCP-Alpha、repo_id1154066087、private/admin/nonfork、head/baseは同repo。
- PR3613はopen。head `b194d66524ef86330bb73ca3f22f9e5dd6ae0a07`、tree `f7eea82c96449b7749f98dd6c4b0344b64f1d651`、branch `codex/todo2641-http-response-pseudo-20261015`、base main `9ee640e763091c4688aaf6204e06f9fb1b493695`。
- HTTP/2・HTTP/3応答のrequest専用pseudo-header拒否は実装済み。nativeとSpecの差分に対するgpt-6-lunaのレビュー5520900843はMust0。レビュー対象は旧head `c53dc14f1b49f3d7d16a604f29b120de3ce8163d`。
- ToDo2710の独立Linux/Windows/Rust/Node受入は旧headのもの。新headで再実行済みとは扱わない。新headはCodeQLのruns-on scalar2箇所だけを追加変更した。
- 新headのCodeQL公式run38893804340は4言語と最終gateの5jobがsuccess。解析37件は未判定のまま。通常CI38893656960はfailure。check116948883594の公式annotationは `The job was not started because an Actions budget is preventing further use.`。CI Gateもfailure、Windows native checkとPR tree receiptはskipped。手動success statusを作らない。
- 2648のGo基盤はPR187通常merge・正規配布・tested/mergedの必要CI成功まで配送済み。2648/2710/2730の未完は自身の旧資源回収であり、再実行や削除の迂回をしない。2488/2505はdone。旧検査の再登録ではなく、未実行の公式CIを完走する工程。

## 実行範囲

1. 起動時にD:/CC/AGENTS.md、元2641の最新note/spec、このrepoと原GitHubのAGENTS.md/CLAUDE.mdを読む。取得前・実行前後に実owner/id/private/admin/nonfork/同repo/同PR/head/refを照合し、進んだheadを黙って採用しない。
2. 原製品フォルダは開かない。原保護一覧・guard・owner-work登録・登録manifestの作成や変更は禁止。既存の有限product-ciと独立した使い捨て検査資源を使う。原GitHubソースだけをRAM/専用guestで扱い、ホストへ製品checkoutを作らない。
3. 既存の公式CIを追加課金なしで実行する。必要なら原ci.ymlのrunner routeと、それを実行する独立Go処理の最小差分を実装する。受付のsource固定、実物HEAD/tree、checkout認証除去、単一jobの所有、終了/取消時のprocess/登録/lock回収を維持する。既存共用runnerや他担当資源を変更しない。workflowの検査・条件・権限・OS・タイムアウト・必須gate・secret境界を削らない。Docker parityをskipしない。Windowsの原13工程/profile/直列条件を保つ。
4. このToDoに割り当てられたcargo資源と共有Cargoゲート/専用target/sccacheだけを使う。GPU/Civ6は禁止。Rustの最終workspace/関連crate/doctestと原workflowの検査を維持する。原native証拠を同SHAの公式CI成功へ読み替えない。初回の失敗と修正後結果を両方保持する。
5. 新しいCI実装差分は別モデルの有限レビューを共有ToDoへ--specで依頼しMustを直す。子Codexジョブを直接起動しない。原PRのheadを書き換える必要がある場合は同branchへのexpected旧SHA/force=falseの通常更新に限定し、差分・新head・必要検査・CodeQL再解析要否を親2641へ配送する。別の原製品PRや機能追加は作らない。cc-pc-tools-rsの実装が必要なら通常PR・別モデル・CI・merge・既存Go配布まで担当する。
6. 原PR3613のmergeとPrd反映は親2641が担当する。この独立工程は通常CIと必要な実行基盤の差分を返すまで。親を待たず、自分の確認範囲を終えて返す。CodeQL37件の静的確認は別の独立レビューへ渡しており、同じ確認を新規登録しない。

## 証拠と完了条件

公式run/check/job/同SHA/物理tree・全工程・原log hash・失敗/未実行/cleanupを区別し、親2641のnoteへ配送する。原log/SARIFには識別名が含まれ得るため本文・位置・秘密値をstdout/modelへ出さない。数値ID・rule/count/hashと必要な非秘密の結果だけを返す。既存証拠はD:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016/d6eb340d94b85fa2d879b467a597d459。原2710の証拠と回収拒否は元noteのまま保持する。

新規tempはsafe-delete create/plan/別呼び出しの通常削除/finish。拒否はquarantineの正規経路だけを使い、再拒否を迂回しない。本人PC操作、Terminal/UAC、追加費用、Hosted予算変更、APIキーAI、GPU、Discordへの技術質問は禁止。お金/権利/新方向など本人だけの判断が新たに必要なときだけ元回答を照合してDiscordへ1回質問する。作業branchの成果をcommit/pushし、終了前に自身のscheduler checkpointを保存する。他ToDoだけを待つならwait --onで記録する。
