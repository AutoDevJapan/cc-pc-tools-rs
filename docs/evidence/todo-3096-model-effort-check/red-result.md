# ToDo3096 Red検査

比較用Red 8b70dd71c400ef449d0216d0075aff6bc253c527で、指定2コマンドがどちらもコンパイル後の期待したassertにより失敗した。

- review_qualification_red: exit100、1件実行/1件失敗。自動reviewの実モデルSol、期待Opus。後続のモデル差/effortのassertには到達していない。
- cc-jobs parityの指定E2E: exit100、1件実行/1件失敗/384件は選択外。別枠への初回再試行でcatalog_modelの実値Null、期待p000002/gpt-6-luna。最初のcapacity-onceケースで止まり、後続capacity-alwaysケースは未到達。

構文エラー・資源待ちによる失敗ではない。固定HEADとtracked差分なしを実行前後で確認した。実AIは起動せず、実子プロセスのfixtureを使った。5/15/30秒の実待機を許し、fixtureの条件期限120秒を維持した。

各runのJSONに実HEAD/引数/開始・終了/exitを保存し、stdout/stderrは切らずに保存した。
本稿はRed検査だけの結果であり、Green・別モデルレビュー・CI・製品全体の完了ではない。
