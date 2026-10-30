# ToDo4142：既存レビューの再評価と正規の保護判定

元3387の有限工程。対象は `AutoDevJapan/cc-pc-tools-rs`（repository ID
1406818713、公開）の `reevaluate-owner-wait` 入口だけ。
元3684の制作、元3387全体、既存4023のレビュー実施・再開・完了はこの工程の完了に含めない。
実4023/3992/3684の状態、依存、原spec、model、所有は操作しない。
別モデルのレビュー、ラバーダック、CI、統合、共有実行物の配布、実再評価は親が担当する。

## 原因と確認した範囲

開始HEADは `86fc9f06ca01a6cb8a11720fcfb5a884c5209f56`。
親指定の `2344cb493996194029afcac55c956a642f70f702` と、この修復の関連ソースに差分はなかった。
旧 `cc-todo/src/recovery.rs:19-22` は対象行を選ぶ前に旧保護一覧を直接読み、
読取エラーを一律に `client guard unavailable` へ変換していた。
そのため、対象別の正規判定が現在許可でも、この入口はその判定へ進めなかった。
一覧の不存在、ACL、内容、復元可能性は調査していない。
原一覧・registry・認証情報・別ファイルの原job prompt・SDK dataは参照、hash、copyしていない。
この表現はjob.json内部のprompt値を除外して読む保証ではない。
参照job.jsonは上限1MiBで本体全体を読み、全体をJSONとしてparseするため、
内部にprompt等があれば読取・parseの対象に含まれる。

Redでは、旧一覧への読取要求だけを拒むテスト用Hostを使い、
正常対象の期待 `Ok([1])` に対して `Err("client guard unavailable")` になることを確認した。
旧一覧の実物を開いて障害を再現したのではない。
テストの架空データを実sourceの資格や本人回答として登録していない。

## 修復と保存条件

入口の公開シグネチャとCLIは維持し、処理を `recovery/owner_wait.rs` に分離した。
既存 `received_input::SystemHost` の正規 `scrub391 --guard-path` をそのまま使う。
同実装は窓を出さず、四つのbool、整合性、重複JSONキー、終了コード、上限と期限を確認する。
`ok=true/access_allowed=true/client_resource=false/classification_unverified=false`
以外を許可へ読み替えない。
旧一覧を空の一覧へ変換する処理、別guard、HTTP reader、資格登録は追加していない。

選択行の登録済みproject rootと、正確に参照された実jobのcwdを判定する。
後続のToDo4246では、選択行とowner行の非空scheduler.worktreeも同じsourceとして判定する。
参照jobのID、対象ToDoとの関係、project、存在するregistered_cwdの整合、process identityを確認する。
登録ルートの補完とGit所有の入力投影には、既存の `work_owner::registered_job/from_job` を使う。
これにより、登録フォルダと祖先Gitルートが異なる正常ケースを維持する。
root/cwdや所有の根拠が欠ける場合を、許可済みのowner資源と推測しない。
bulkの `Backend::jobs/jobs_from` は全jobを列挙するため使わず、Hostの有界読取で選択job.jsonだけを読む。
`received_input` の全入力取得はspec/promptまで読むため、この工程には使わない。

guardと既存 `work_owner::reevaluate_todos_if` の提案作成は共有lockの外で行う。
変更提案があるときは判定と所有投影をもう一度確認し、通常のjob state lockからToDo lockの順で取得する。
最後のToDo lockは即時取得とし、ほかのwriterを長く待ちながらjobを占有しない。
保存時に選択行・登録root・参照owner行、実jobの元bytes、fresh process snapshotを再照合する。
ToDo4246では初回の共有ToDo lock待機・source取得が終わってから単一の45秒期限を始める。
実scope開始から45秒以上、時計の逆行、不明時刻、変更、busyは保存せず拒否する。
保存は最新storeの選択行だけへ反映し、別itemの並行編集を保つ。
外部guardの失効とGit変更を跨ぐ全サービス共通の原子性を新しく保証するものではない。

本当の本人質問、停止、完了、延期、別の未完了依存、live/queued worker、
未確認分類、保護対象、guardエラー、source欠落・変化では保存も解除も行わない。
通常の自動所有edgeの根拠がない状態を、この入口で新しい本人回答や技術待ち解除へ変えない。
PR/branchによる本当の完了待ちと、元3992/3995の通常の依存解放は既存policyのまま。

## テストと残る工程

以下はToDo4142当時の履歴であり、ToDo4246の最終sourceの成功件数には加算しない。
M1の修正・新しいテストと未対応の前提は [ToDo4246の証拠](todo-4246-owner-wait-holds.md) を参照する。

- Red：正常対象の1件が旧一覧への依存で失敗。
- 最初の関連テストでは、質問を含まない「要判断」をblockと期待した1件が失敗した。
  既存 `result_state` は具体的な質問がない結果を継続として扱うため、テストを具体的なa/bの質問へ修正した。
  この結果文の本体実装は変更していない。
- Green：最終の入口13テストが成功。拒否・保持ケースでは保存callbackの呼出0とstore bytes不変を両方確認した。
  guard中に通常のToDo lockを取得できること、別itemの編集の保存、job lock競合とliveness競合も確認した。
- 登録フォルダの互換調整前に `cc-todo` 全311テストが成功。
  その後の影響はこの入口だけであり、上記13テストを再実行した。
- 既存 `cc-job-policy` の所有・依存・停止関連21テストが成功。
- scheduler利用側、doctest、Clippy、候補生成、checkpointの結果は最終の追記で確定する。

作業の完全な出力と実行条件は、このworktreeの `todo-4142-results.private.json` に保存する。
再開情報とcheckpointは `todo-4142-resume.private.json` を正本とする。
この工程では共有ToDoの状態更新、runner/CI起動、子AI job、PR作成、マージ、共有exe置換、
実4023への再評価、本人への質問・再メンションを行わない。

## 親が行う実4023の再評価

出所はこの依頼の保存済み仕様と既存CLI `todo.exe reevaluate-owner-wait --help`、
`cc-todo/src/lib.rs` の `ReevaluateOwnerWait` 分岐。
本担当は実4023のsourceや原job promptを再読していない。

1. 同じ最終ソースの別モデルレビュー・ラバーダック・必要CIを親が確認し、通常手順で統合する。
2. 親が稼働版・並行writer・CASを照合して共有todo実行物を配布する。
   未審査の候補は `release_eligible=false` のまま扱う。
3. 通常の `todo list --all` から既存4023の登録rootと参照を確認し、現在の正規guardで資格を確認する。
   旧一覧や原promptを開いたり、別root/copyへ置き換えたりしない。
4. 配布済みの通常 `D:/CC/Tools/todo/todo.exe reevaluate-owner-wait 4023` を1回実行する。
   このコマンドは既存の自動所有edgeを再評価するだけで、新workerを起動しない。
5. `released` と対象行・既存jobの結果を通常経路で読み返す。
   空のreleased、本当の本人待ち、M0未判定、source未確認を成功解除やレビュー完了と解釈しない。
   原依存は通常の完了以外で解除しない。
