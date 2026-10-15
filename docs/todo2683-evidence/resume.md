# ToDo2683 再開記録

対象は a722fea2e28ff0a9081ce3e5701da414d4882f9d、tree 2d598998342712049fd2eea4d0923ee01b8c5b4d。
開始HEAD c56510c7aef2fdfcce8844391db8869aecbf19cb には固定対象にないmainの差分があった。
作業ブランチ scheduler/todo-2683-work には正規merge c4b96796c15e775672ccb43cf564efc362e7ae40 を保存。
そのmergeのtree 51804a7545a52054f8f4d59cd879039200b8a5c0 は固定対象と異なる。
実検査はこのworktreeのみをdetached HEAD a722feaに切り替え、完全一致するソースで行う。
原2675や他担当のworktreeは編集しない。自分のToDo状態はスケジューラが更新する。

完了: 指示読取、guard false、remote所有者確認、fetch、正規merge、固定HEADへ切替。
残り: 関連nextest、Clippy、追加6検査の実行確認、mutation失敗、正確な復元と成功、release、結果保存。
ログ: 本ディレクトリ。生成先: このworktreeの target。
一時領域所有ID: 6d0a79eb0559721af0b8cfa2f2b9fbdf（Temp/codex-owned-同ID）。
ブラウザ/GPU/本番配備/全workspace検査は使用しない。
復元時はmutation前の自分のengine.rsバックアップを使いSHA256とGit blobを照合する。

関連検査: 345/345成功（exit0）、追加6件をJUnitでも確認。Clippy exit0。
mutation: 1件失敗/exit100、期待した依存解除レポートNullにより失敗。
復元の初回: コピーが古い更新時刻を保ち、Cargoがmutation版を再利用してexit100。
この初回の全出力はrestored-stale.*として保持。ソース不一致ではない。
更新時刻だけ進め、再コンパイル後の復元検査1件成功/exit0。元バイト列/正規Git blob一致。
残り: 固定HEADのrelease、成果物hash、ログmanifest、作業ブランチへの結果commit/push、一時領域片付け。

最終: 固定HEADで関連345件/Clippy/release成功。mutation exit100、正確な復元と再コンパイル後はexit0。
成果物・全出力・未検査範囲はresult.md、artifact.json、各stageのjson/log/JUnitに保存。
一時領域はplan→別呼出しの通常削除→finish source_absentを確認済み。
検査結果は作業ブランチへ保存する。配備などの本番工程は原2675の範囲であり本担当の完了条件外。固定ソースの検査を再実行する必要はない。
