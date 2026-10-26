# 質問回復と既存の回答担当を同時に保持する

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/489

候補b082d16cの最終レビュー4002はMust 0だったが、待機中にmainと配布済みの
会話処理が更新された。候補をそのまま配布すると既存の回答担当処理を失うため、
mainのcf7ed154を統合し、旧候補の実行ファイルは配布に使わない。

## 統合の条件

- 会話の保存形式は既存のactor、wait_deferredと、新しいrecovered_intake_todo_idを保持する。
- 公開出力はactorと回復受付のそれぞれの既存メタデータを返す。
- 毎分の再試行は既存のactorを引き継ぎ、回復済みの通常受付について元作業への質問待ちを再作成しない。
- 生きた質問待ち、同質問の回答済み、別受付による回復の拒否を維持する。
- 同一プロジェクト、actor付き質問、別プロジェクトの通常受付の経路を混同しない。
- 統合後のRust writerとGo runnerを同じソースから確認し、稼働中の実行ファイルを比較して更新する。
  既に配布された並行修正を古い候補で上書きしない。

競合はconversation_cli.goの公開出力、conversation_delivery.goの再試行、
conversation_store.goの保存形式の3か所で、どちらか一方を削除せず両方を残した。
Rust側は既存mainの変更とordinary_assignmentのモジュール登録をともに保持した。

## CI中断の原因と確認

旧候補のUnattended readiness tools実行40371623266では他の8ジョブが成功し、
repair-guardだけが10分のジョブ期限で中断された。
注釈の理由は `The job has exceeded the maximum execution time of 10m0s` であり、
テストの失敗と区別する。

同じ問題はmainのb0fef3c1で修復済みだった。
全テストを省略せず、ジョブ25分・Go 20分の有限上限とverbose出力を設定し、
結果キャッシュ入力の高コストなハッシュ処理を-count=1で省いている。
この修復を重複実装せず統合した。

親の旧候補に対する追加診断は `go test -p 2 -parallel 2 -json -count=1 -timeout 12m ./...`
で終了0、本体332.938秒・コマンド336.838秒だった。
repair-guardとそのローカル依存のソースはこのmain統合で変更されていない。
この結果を統合後の会話処理や本番の終了確認の成功とは扱わない。

## 残る受入確認

統合後のGo関連回帰、更新後Rust writerを使う結合テスト、統合差分の別モデルレビュー、
最終ソースのCI成功を確認する。
その後にマージ・両実行版更新・正規の質問回復と原本の結合・実反映記録・
通知なし結果配送と読戻し・archive後GETを行う。
元の実操作と未完了の元作業を保持し、本人の情報入力と質問を繰り返さない。
