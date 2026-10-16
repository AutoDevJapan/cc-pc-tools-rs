# ToDo2744の正式CI検査・再開記録

2026-10-17 02:14 JST。配布更新ToDo2745が完了し、同branch・前回commit bd84c59から再開。指定headの既存公式CodeQLを直列処理中。

## 今回の進行

- 元2745のsource PR188は通常merge済み。APIで実AutoDevJapan/private/admin/nonfork、merge fa594cec7bee92bffa3fbd8ab815e3c03a189efc、PRhead504a547410908046fbea89e4f4b83ca595ca0e43を確認した。別モデル2747のMust0と必要CI成功の配送を保持。
- 正式配布receipt D:/CC/Tools/product-ci/install-pr188.private.jsonを読み、Windows実binary SHA25626E07700E61277F4FB93E83A9A6C1CF165A90D75C84AFB0A98C463A4D91D1B66とLinux実helper SHA2561359CD984DBC43E629D400031DFD5ED4CCA824EB2F2C341566E5F596B4987D07を照合した。
- Linux正式helperはD:/CC/Tools/product-ci/product-ci-linux-fa594ce。配置後inspect成功は元2745の結果であり、製品CI成功ではない。
- 製品APIを再照合し、固定20c/同branch/PRhead/open/drafttrue/同repo/所有条件を維持。既存CodeQL38929313859の4jobとsecret38929321895の1jobは開始前に全queuedだった。
- 1件目117068905768は公式success。check供給元github-actions/app15368、同20c。Python analysis1969012602も同20c/error空/results0。公式全文115803bytes/SHA2562608EBB50C5CD2323B73E8B645AF419867C4BE82D9BC618BCC99B92604DB66DEを保持した。
- 1件目の実checkout20c/tree007b・認証除去・source_tree_verified・tool exit0・kernelPID0/tree_gone・container_removed・登録不在を含むlock_releasedを正規toolで確認。所有54c374ae270e85080665a6eeb095fb1dの原receipt/log/checkout/contextを恒久保管先の同IDへ全SHA照合保存し、plan→別通常削除→finish source_absentで回収済み。
- 現在2件目の正式codeql-jobをHidden実行中。host tool PID48732、開始02:15台、所有root C:/Users/makhmeto/AppData/Local/Temp/codex-owned-dea5e039d164eb7830e602a16a7afda7。ジョブ117068905439が公式in_progress、runner_id36。コード・正式toolは変更していない。
- 集約用所有root C:/Users/makhmeto/AppData/Local/Temp/codex-owned-9c9448c8d6dd1c91e0b46f4cc6438d2cに配布receipt/固定sourceとmerge context/解析設定/公式checkと全文logを保存中。Save-OfficialJob.ps1は今回だけの非表示read-only取得用であり、CI実行は正式product-ci以外を使わない。未完のためこのrootを削除しない。
- クラッシュ再開時は当該PIDの生成時刻と実receipt/container/公式run/jobを確認し、同候補の生存toolを重複起動しない。未完の所有rootを削除しない。今回のjob終了後は実namespace回収と登録不在/lock解放を確認してから次の1件を開始する。

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
