# ToDo2744の正式CI検査結果

2026-10-17 02:29 JST。有限な独立検査は成功。製品PR1002はopen/draftのまま、マージ・Prd反映は親2639に残る。

## 固定した対象と正式実行機

- repo1285649124/PR1002、head20c441df545a88f4a839d7e906b5aeb4f1708ae3、tree007b84082008ea9effd5052f63856273aabbfb17。唯一の親b93e126b172adbb29ad2081af4732f4b42f8bc95。
- APIでAutoDevJapan/private/admin/nonfork、同head/base repo、branchhead/PRhead一致を開始・終了時に確認。別head/merge-refへ追従していない。
- 修復sourceはrepo1406818713/PR188、PRhead504a547410908046fbea89e4f4b83ca595ca0e43、通常merge fa594cec7bee92bffa3fbd8ab815e3c03a189efc、module tree2eec8c5f2831fe695d5c604c566c675fe9e6cac0。元2745が必要CI・別モデル2747 Must0・正式配布を担当した。
- 正式Windows binary SHA25626E07700E61277F4FB93E83A9A6C1CF165A90D75C84AFB0A98C463A4D91D1B66。正式Linux helperはD:/CC/Tools/product-ci/product-ci-linux-fa594ce、SHA2561359CD984DBC43E629D400031DFD5ED4CCA824EB2F2C341566E5F596B4987D07。両実物を配布receiptと照合した。
- この担当は製品source・元workflowを変更せず、独立PRの作成/mergeを行わず、正式product-ciだけで既存queued jobを直列処理した。新dispatch/rerun/手動success書込は0。

## 公式CodeQL

run38929313859、event dynamic、同20c、attempt1はcompleted/success。4jobともgithub-actions/app15368の同headチェックがsuccess。

| 言語 | job/check ID | analysis ID | 指摘 | 全文bytes | 全文SHA256 |
|---|---|---|---|---|---|
| Python | 117068905768 | 1969012602 | 0 | 115803 | 2608EBB50C5CD2323B73E8B645AF419867C4BE82D9BC618BCC99B92604DB66DE |
| JavaScript/TypeScript | 117068905439 | 1969032775 | 0 | 325682 | 5BD755E5951E4DF68C5D975939510BDF6957F44AA55867AC5DA841FA2F9475D0 |
| Rust | 117068905828 | 1969048286 | 0 | 117476 | 69E66CE361F87FBD61258270BF89CF0A5265CCA4A0C73A8ED2D27562C08C8030 |
| Actions | 117068906037 | 1969065988 | 0 | 86440 | 587AE6813BA93B947F37AA9E9E48C3FE3AE3B8554B6C437DBA94E4D8DE5EF02B |

全analysisは同20c/error空、CodeQL2.27.2。公式実行全体zipは244142bytes、SHAは集約rootのcodeql-run-38929313859-loghash.private.jsonに保存。全zip entryをbyte/hashでmanifest化し、4jobの全文をそのまま抽出した。最初の2jobは個別API取得の全文とも全SHA一致。

Rust jobの個別log GETは2回失敗し、その原失敗出力を保持した。公式run全体のlog GETは成功し、同Rust jobの全文を取得できた。必須testでも個別log GETの失敗後に公式run全体zipから全文を保存した。CIの再実行や検査結果の偽装はしていない。失敗した取得試行もmanifestに残す。

## 通常の必須test・Node

- 通常test run38929321895/job117068929592は公式completed/success、github-actions/app15368、同20c。secret-workflowのbyte固定条件は不変。GitHub eventのmerge contextと実checkout20c/tree007bを分け、完了hookが物理sourceと認証除去を確認した。
- 通常test全文31179bytes/SHA256F6054A71B91D7BE401F22743AEB5ADDB9A31503096C29EF77878F3C22051C30E、no leaks foundを確認。公式zip16170bytes/SHA2563E314980DE2FCA7C148701095CD8463268AE49B85B34A6263A183FF476AD901C。
- Nodeは正式node-contractsで同20c/tree007bを検査し185passed/0failed/0skip。全文36634bytes/SHA256dd194bbda8821df2e80944f87e41f87c1470e1e22c54a0bfb5f28122c77ea256。旧b93のNode結果ではない。
- 最終API照合で同20cの全6checkがcompleted/success。上記Actions供給5checkに加え、CodeQL集約check117098970172はgithub-advanced-security/app57789でsuccess。

## 回収と保持

- 新container6件はすべて正規toolの完了receiptでsource_tree_verified/exit0/kernelPID0/tree_gone/container_removed/lock_releasedを確認。公式runner5件の同repo登録不在をtoolで確認し、終了後の追加API照合でも所有instance名6件の一致0。候補lockも実ファイル不在。
- CodeQL設定の前後bytes/SHAは一致。元言語/queries/threat/runner設定を変更していない。共有SDK/Node image、常駐runner、他担当containerは不操作。
- 6件の実行用owned rootは全証跡を恒久保管先へSHA照合コピーした後、各plan→別通常削除→finish source_absentで回収済み。集約rootも保管後に同じ手順で回収し、最後の結果を恒久保管先へ残す。
- 原2639 owned d84c967c6eb3b9abf993ad85a20fb9b9と原資料は不操作。元1636/2016/2274、他3製品、元2648/2710/2730/2381の状態・依存・未完を変更していない。
- 旧956のbackend2236/残3結合、b93の旧CI、親の新50passedはそれぞれ原sourceの証拠として保持。この担当が新20cで全backend/Rust native/他3製品の検査を重複したとは記録しない。親の別モデル2743と製品全体の受入/merge/Prdは親2639が追う。

## 親2639へ返す正規保存先

基準: D:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016/

- 集約結果・公式全文log/zip・解析ID・check供給元・最終source/回収照合・全hash・親への配送: 9c9448c8d6dd1c91e0b46f4cc6438d2c/。final-delivery.private.jsonとmanifest-final.private.jsonを最初に読む。
- Pythonの原tool/checkout/context/回収: 54c374ae270e85080665a6eeb095fb1d/
- JavaScript/TypeScript: dea5e039d164eb7830e602a16a7afda7/
- Rust: fd753f320ed12a59ee28b31fa97e097c/
- Actions: 7c7dfc5c817d5d4e26efb08de0c0b44a/
- 通常test: 433129977360d6e1dfc86cdaf86084a3/
- Node: 4ab00fea15d86a04916fb4f1d4caa9bf/
- 初回の旧tool拒否・修復依存登録: 57716f4a99527a8f0a4f73b742aa6a18/

rawlog/source/顧客名をstdout・会話・報告へ表示しない。各JSONの確認済み値とhashだけを返す。製品PRのdraft解除・merge・Prdをこの独立checkの完了へ含めない。作業branchはscheduler/todo-2744-ciのまま。最終commit/headは正式workspace checkpointと集約証拠に保存する。
