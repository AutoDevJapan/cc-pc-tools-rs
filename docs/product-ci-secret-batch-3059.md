# 元3059の確定2件を取り消さず検査する

既存Issue267に属する追加修復。元995専用の固定条件は元の担当依頼と実装が
設けた技術上の境界であり、製品全体への本人の停止指示とは扱わない。
今回の正規本人回答は独立した元の質問と対象範囲に結び付けて検証した。
本人の原文・Discord ID・私的な証拠の場所はこの公開文書へ複製しない。

対象は元repository 1285649124のPR995/head cbd6888とPR939/head f1c6e235のみ。
995の既存の独立入力資格は維持する。939はその資格を流用せず、今回の本人回答、
固定source/workflow、独立した別モデルレビューと読取受入から別に資格を得る。
元の拒否済み入力や一般の保護分類を変更しない。

Given 同じラベルを要求する確定2件が待機しているとき、1回限りの隔離runnerは
GitHubが実際に割り当てた方だけを実行する。開始前と終了後のhookは実run/attempt、
context、実runner ID/name、source HEAD/treeと認証削除を照合する。
同じ登録で2件を実行せず、1件ごとに既存の回収を確認し、残る1件へ進む。

別run/job、別head、変更済みworkflow、欠落・編集済み証拠、曖昧な割当は拒否する。
2件以外へのqueue除外を残し、jobの取消・再実行、製品source/CSV/workflowの変更、
製品のmerge/Prd反映、新費用は行わない。検査起動と正式successは別に記録する。
既存の4候補、995単独経路、CodeQL4件の成功は維持し再実行しない。

参考: [GitHubの割当](https://docs.github.com/en/actions/reference/runners/self-hosted-runners)、
[開始・終了hook](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/run-scripts)、
[実jobのREST API](https://docs.github.com/en/rest/actions/workflow-jobs#get-a-job-for-a-workflow-run)。
