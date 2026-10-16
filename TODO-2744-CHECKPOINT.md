# ToDo2744の正式CI検査・再開記録

2026-10-17 01:24 JST。状態は継続中。本人判断ではなく、正式ツールの配布更新ToDo2745を待つ。

## 確認済み

- 作業branch scheduler/todo-2744-ci。開始HEAD a83aa2606e7354088cd23da111709729e4c82e35。branchの変更なし。製品sourceの編集・PR作成・merge・Prd変更なし。
- repository1406818713はAPIでAutoDevJapan/private/admin/nonfork。最新main a83aa2606e7354088cd23da111709729e4c82e35、PR187は通常merge済み。
- 正式product-ci.exeの11718656bytes/SHA256 E5D2C61AC5B0001A565A68C37561B1CBDAA6931EE13B8109D83BCFCBD6A2BE45は元PR187配布receiptに一致。
- 正式toolをHidden起動し、終了を待ったinspectは実exit1。候補1285649124は旧b93固定によるfixed same-repository PR head rejected。他3候補はfixed-head-verified。固定拒否の迂回は行わない。
- 製品repo1285649124/PR1002は実ownerAutoDevJapan/private/admin/nonfork、同headRepo/baseRepo、open/drafttrue。PRheadとbranchheadは20c441df545a88f4a839d7e906b5aeb4f1708ae3。tree007b84082008ea9effd5052f63856273aabbfb17。唯一の親b93e126b172adbb29ad2081af4732f4b42f8bc95。
- compare APIは通常1commit、指定4ファイルだけと完全一致。backend/app・元workflow/scriptsは不変。
- CodeQL run38929313859はqueued。jobs117068905439/117068905768/117068905828/117068906037はすべてqueued、専用label、runner_id0。
- 通常secret run38929321895/job117068929592もqueued、専用label、runner_id0。新dispatch/rerunは0。
- 新headの正式CI成功・CodeQL analysis ID・Node成功はまだ無い。旧sourceの実結果を新headの結果に読み替えない。今回container/runner登録/候補lockの作成はいずれも0。

## 正規の修復依存

この担当は独立checkで「独立したPRの作成やマージを担当しない」という依頼末尾の制限を維持する。必要なsource修復を通常の共有ToDo2745へ--spec/--needs cargo付きで登録した。仕様はTODO-2744-PIN-REPAIR-SPEC.md。同工程は候補pin1行と検査、別モデルレビュー、必要CI、通常merge、既存tool正式配布だけを行う。製品CI実行は2744に残す。子Codexジョブ直接起動なし。

元2744はtodo wait 2744 --on 2745で待つ。元2639の2743/2744待ち、元2648の2710/2730待ち、元製品/停止/未完条件は変更しない。

## 再開後の順序

1. 2745の実成果から正式source/merge/module tree/binary hash/配置先を確認する。単なる登録・CI受付を配布済みとしない。2745が未配布でもdoneになった場合は実失敗を調べ、同じ修復の重複登録をしない。
2. D:/CC/AGENTS.md、保存spec、最新元2639指示を読み、固定20c/007bをAPI再確認する。別headやmerge-refへ追従しない。workflow byte固定条件を緩めない。
3. 現run/job/headを先に照会する。同じ候補のCodeQL・secret・Nodeは直列。正式toolのcodeql-jobで既存38929313859の4jobを処理し、公式success/4言語analysis ID/source SHAを照合する。
4. 正式secret-jobで既存38929321895を処理する。GitHub eventのmerge contextと物理checkout20c/tree007bを区別する。必要なら正式node-contractsで同20cの原script契約も確認する。旧sourceのRust/他製品/全backendを重複実行しない。
5. 全rawlogのbytes/SHA256、実source/head/tree/run/job/analysisID/provider、成功・失敗・未実行、container PID0/回収/登録不在/排他解放を保存。実害のある失敗は原因を調べ、範囲内の必要修正の根拠を親へ返す。
6. この有限CI工程だけを完了し、元2639へ正確な保存先と実結果を返す。製品PR1002のdraft解除・merge・Prdは担当しない。

## 操作証跡

今回の恒久保管先:
D:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016/57716f4a99527a8f0a4f73b742aa6a18/

inspection.private.json、fixed-source-proof.private.json、fixed-head-queued-jobs.private.json、installed-inspect.private.jsonl、installed-inspect-error.private.logとmanifestを保存。名前・生source・生logをstdout/モデル/報告へ表示しない。親owned d84c967c6eb3b9abf993ad85a20fb9b9はまだ作業中のため保持。

新temp owned57716f4a99527a8f0a4f73b742aa6a18は全証跡をSHA照合保存後、plan→別呼出し通常削除→finishで回収し、その結果を恒久保管先へ記録する。拒否なら正規quarantine。未完の元資料や他担当のデータは回収しない。
