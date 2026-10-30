# 正規に再開したSkills担当の照合

元3080・Issue283の公開資料を扱う同じ3686の有限修復。
一般の保護判定、私的保護一覧、647件の取得拒否は変更しない。

`assign_worker` / `advance_resume` が保存する `start --job` の正規割当は、
`scheduler.key` が明示的なnullで、正のu64整数の `resume_generation` と、
同じ世代・現在の実ジョブを持つ `worker_receipt` を伴う。
これを通常の非空起動キーとの完全一致とは別に照合する。
null、欠落、空文字を文字列変換で同一視しない。

元のJSONから正規キーの存在・null・整数の型を保持し、3080と3686の現在の
actorだけに使う。欠落・空文字・誤キー、異なるジョブ・世代、0・負数・小数・
指数表記・文字列・u64範囲外、重複キー・大文字小文字の別名は受け入れない。
非空キーの完全一致に世代記録を新たに要求しない。
実task・プロジェクト・最新ジョブ・現在の登録・原spec・停止や質問や待機・
実cwd・branch・remote・同じbirthで生存するPIDの既存条件を保持する。
過去の作者、現在のconsumer、独立reviewの起動時の許可は混同しない。

合成metadataのテストで両actorの従来の拒否をRedとして確認し、正規割当と
負例を同じproductionの射影とpredicateで確認する。
テストから実corpus・中央Skills・原結果本文を読み書きしない。
結果の受領、新規CreateNew、forward、改訂、更新直前の再照合が共有する
actor判定を修復する。既存の個別notice・alias・prefix・unknown・実ID・
同bufferの条件は別のまま維持する。

PR618の成功済み3870/3876を凍結し、新しいセルフ4198・ラバーダック4199を
通常ToDoへ登録する。本人の既答によるLuna/max・両origin owner_requestを
この残る複数ファイルレビューに限って使い、同じ最終headの全指摘0を必要とする。
旧pairは正確な旧source・reviewed head・software SHAでの既存版の読み返しだけに
残し、新しい更新・回復には使わない。

更新前のF8は、過去の通常merge/実配布と新候補生成前の同handle実測を別々に
照合し、同じSHA/sourceの一組だけを両既存updaterに保持する。
元の9be/50c/F8のbackup・source・ID・ACL・全spec/job/nativeを保存する。
配布候補は同じmerged sourceから一度だけ作り、全raw入力・modified=false・
同headのCI・実2レビューと通常verify/applyで確認する。

親は同じ30cee5c5の元request、Creator4193・8feのspec・aac5の原結果を現在の
正規CLIで再検証し、原文全読・実native・受入後だけ元a02cの新規作成へ進む。
修復の登録・merge・Windows更新を、配置1件や指定16資料全体の完了としない。
