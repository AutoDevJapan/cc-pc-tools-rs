# 親6候補・本文読取追補・比較判定の受入範囲

これは全16資料を広く取り込む元3080/Issue283の追加差分である。元3048の新4/既存10、原本・notice・backup・junctionと実配置32を保持する。この差分では中央へ新しく書き込んでいない。

PR431でmainへ受入済みの値はraw474/workflow475、新規32/統合3/除外8。今回の作業ブランチではraw480/workflow482、除外14（重複9+個別権利5）、未完73171へ反映する。新しい別モデルreview・正確なheadのCI・通常mergeを確認するまでmain受入済みとしない。

## 読取と比較の単位

[親6候補](skills-3080-sixmile-parent6-evaluation.json)は、先に親が評価した5候補と、8KBより大きい1候補である。raw5/伏字workflow1を区別し、支持57は保存27/raw全読14/伏字workflow3/走査0/保存未読10/未保存30となる。同一byteのnested本文はworkflowの再利用だけを記録し、raw再読や別primary評価には加算しない。

3指紋は公開前のprivate評価SHA563ced896eff80b425bdf49b8a315e8d26514029396e4f49a991a8b44785e535、実際の読取receipt、完了済みの原本検証から固定した。原本検証の実行を意味読取や実行許可にしない。4つの個別full MITと大きい1候補のMITは作者手順文だけへ限定し、短いMIT-0表題の通知は未成立のまま。依存・外部素材・サービスの許諾は別である。

[本文読取追補](skills-3080-primary-reading-amendments.json)は、next45でnotice-onlyだった1本文7068Bを親が新しくraw全文読取した記録である。元childのraw/workflow=false・actorなし、個別noticeと支持の担当は元のまま保持する。source集計のraw/workflowへ各1を一度だけ加え、新candidateや新supportとして加算しない。

[固定snapshot](skills-3080-sixmile-three-cohort-snapshot.json)は受入済みad8151cの旧3評価群の索引byteとGit blob3c9b26478a118f1e43e8896a3f77d9741b9fbf81を保持する。snapshotの数値は歴史値で、現在値は[source index](skills-3080-sixmile-evaluation-index.json)と[進行記録](skills-3080-progress.json)から読む。旧21/45/20と親6の読取検証を保持し、新しい全source unionが全ての実台帳行を先に照合する。

## 最終採否と別分類の比較

[個別権利5件](skills-3080-rights-decisions.json)のうち新4件は、保存済みの個別1467B通知SHA79f6d8f5b427252fa3b1c11ecdbdb6bf610b944f7530b4de78f770f38741cfaaによるサービス外保持/複製/派生/再配布の制限と、中央へコピーする許諾の不成立に限る。既読primary3件と今回の親全読1件を別々に対応させ、未読を除外へ置換しない。元1権利行は不変に保持し、公開内容の評価、別の許諾済み手順や通常のサービス利用を禁止しない。

[MCPの旧2比較](skills-3080-mcp-sibling-duplicate-evidence.json)は完了済みの固定比較を再利用する。canonicalの実配置/発見/原本と公式notice保持・PR428受入を条件にして、今回その条件を満たした出現の重複を記録する。元7重複は全fieldを不変に保つ。

旧2のうち1件はnext45の直接読取に含まれ、もう1件はこれまで直接読取の評価群に登録されていなかった。後者は全primary byteと支持の同一性が比較済みのため、comparison-onlyとして新しく明記する。新しいraw/workflow読取、個別noticeの存在、コピー許諾やhelperの安全を得たことにはしない。原86と予約86のpathは完全一致、準備80との重複も0で、誤った予約置換や二重委任が原因ではなかった。

sourceの評価済みpathは直接群92+comparison-only1で93、raw90/workflow92/伏字2/notice-only0/comparison-only1となる。支持822/保存445/raw256/伏字26/走査113/保存未読50/未保存377は4つの直接読取群だけの和である。比較のみのpackage metadataは別の証拠へ保持し、支持の読了/保存件数を推測してこの和へ混ぜない。

公式MCPの不足treeは固定commitのmetadataだけ取得し、3つのGit tree hashを再構成して支持8のmode/path/blob一致を確認した。これは新たな本文の取得ではなく、取得拒否済み647件の同じ/別経路の再試行も0。未保存scriptは未読・未実行のままで、公式noticeをミラーpublisherや外部SDK/serviceへ移していない。

## テストと未達

source/actor/支持の独立指紋、unknown bytes、raw/伏字/走査、歴史snapshot、読取追補の一度だけの加算、比較を新しい読取へ変える反例、全sourceの未知/重複/片側flag/誤採否をテストする。権利と重複の型、固定notice、primary読取証拠、canonicalとの対応、原7+1行の不変性も別に確認する。合成の将来採用正例は関係検証で、実採用/実許諾ではない。

匿名の中央保留7件は分類未確認/access不可のまま。有無・容量は未知、guard後の本文読書き0、設定/例外/改名/別path/Gitbootstrapによる回避0。新しい実配置・Codex発見・実使用をこの差分の成果には数えない。通常の正規分類条件が成立した対象だけ実反映を進め、他の評価を継続する。本人の操作・判断は不要で、全16資料と記事/647取得の保留は未完である。
