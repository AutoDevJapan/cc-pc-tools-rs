# ToDo3177 再開記録

親3081 / Issue277の配布資格だけを実装中。
作業branch: scheduler/todo-3177-3081。起点main: eae2d2bd7e3e16fe3833b252b065c0ee8ee92a4b。
実所有AutoDevJapanと原source/自worktreeのguard falseを確認した。
自ToDoの状態はscheduler管理。子job・質問・Discord投稿・PR/merge/本番配布は行わない。

仕様はmodel-runtime-update-3177.md。既存readerとToDo3085の実装を読了。
指定されたtodo/Discordの保存receiptのSHAとsoftwareの対応を確認済み。
実targetの生hash/コピー/逆解析は未実施。reviewの独立配布証拠は未発見。

実Red: 2件とも実行して失敗した。専用flagがなくexit 2になり、helpにも入口がない。
実行: `go test -p 1 -count=1 -run '^TestModelUpdate(CLIHasPurposeSpecificFailure|HelpAddsPurposeWithoutChangingHelpContract)$' .`。
次は目的限定readerの実装、合成負例、関連Go test/vet、
可能なら正規readerによる現在一致の観測、commit/push、scheduler checkpoint。
新Rust候補のSHAは未生成。候補結合と正式レビュー/CI/統合/配布は親へ正確に返す。

自分の検査一時フォルダのsafe-delete所有ID: d1760e624e67279484ce82962426a1ae。
path: C:/Users/makhmeto/AppData/Local/Temp/codex-owned-d1760e624e67279484ce82962426a1ae。
終了時はplan→別呼出しの通常削除→finishを使う。拒否されたら再試行せずquarantine。
