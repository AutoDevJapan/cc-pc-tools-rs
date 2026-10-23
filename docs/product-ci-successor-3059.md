# 同じ製品作業の次の修正をテストする

Issue267 / ToDo3059。元の995/939の成功と旧4候補を保持する。
2026-10-23に正規取得した本人回答aは、同じ製品作業3056で製品担当が確定した
今後の修正PRをrepository1285649124のテスト対象へ加える判断である。
別案件・別repo・任意callerのPR指定へ広げない。追加費用や本人のPC操作は不要。

新しい`current-pr-successor-qualify/inspect/job`は、既存995専用runtimeや有限batchと
別の入口とする。実行版に付随する`successor-input-3059.json`は、正規本人回答の
元ファイルと、製品担当のcanonical handoffへの参照だけを持つ。本人回答の位置と
byte hashはレビュー済みsourceへ固定する。handoffは既存の正式locatorのrootから
`filepath.Join(root, ".ci-work", "current-ci-successor-handoff-3056.private.json")`として
導く。rootのGit originを確認し、locatorのhandoffもこの場所と一致しなければ拒否する。
受け手のworktreeや、別の場所へコピーしたhandoffは使えない。locator自身には
対象追加や保存場所の変更を認める権限はない。
原回答の本人・元質問・元ToDo・aの意味と同じscopeを照合する。

製品担当は`.ci-work/current-ci-successor-handoff-3056.private.json`を発行する。
version=2、それ以外は既存runtimeのsource-bound schemaを用いる。
todo_id3059/origin_todo_id3056/repo1285649124は固定する。PR番号は製品担当が
原レビューと原受入を完了した同じ版のもの。head/tree/base/run/attempt/jobs/kindと
原出力の参照/hashを一度だけ読み、同じbytesを既存parser/plannerへ渡す。
元995以外のPRでも、parentと検証済みowner scopeをbindingに保持する。

受入条件:

1. 異なる2つのPR、異なるhead、secretとCodeQLを再コンパイルせず固定できる。
2. owner回答未取得/別位置/改変、元作業の停止、別parent/repo、元レビューの別PRや
   別head、role交換、未分類/保護された原記録をtoken/Docker/登録前に拒否する。
3. live APIで元source/workflow/run/attempt/jobを照合し、guestで同じbindingを受理する。
   その後のrunner割当・queue排他・source/auth hook・全owned回収を変えない。
4. 旧3拒否入力、元4候補、995の有限originと939の有限batchを保持する。
5. 実job成功はfixtureや入力資格と区別する。製品担当が実際に必要とする次の正式検査で
   一度だけ実行し、同headの正式結果・原loghash・upload・回収を照合して配送する。
   成功済みの検査をこの機能の受入のためだけに再発火しない。

元ToDo/specの位置・hashと明示停止の照合は既存runtimeと共通する。
この新経路は、修復担当3059がdoneになっても、元の製品作業3056がtodo/doing/waitで
続いている間は利用できる。3056のdoneや両方の明示停止では拒否する。3059が未完なら
従来どおりdispatchableが必要である。旧v1 runtimeの期間条件は変えない。
PR995/939も、新しい親の確定記録と同じ要件を満たせば番号だけでは除外しない。
merge済みや成功済みのjobを再発火することは認めず、openとqueuedの既存条件を保つ。
新しい保存先や原出力の読取にも通常guardを使う。元の未分類データをコピーして
資格を作ったり、原の製品source/CSV/workflowをこの担当で変更したりしない。
原出力は各4MiB、secret workflow digestとCodeQL matrixは従来の照合を維持する。

これは今回の実装仕様であり、新経路の配布・実job成功を主張する文書ではない。
