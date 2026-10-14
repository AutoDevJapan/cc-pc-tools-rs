# 進み具合の記録

共有ToDoの `progress` は担当が確認した0〜100の整数です。稼働時間やジョブの状態から数値を推定しません。古い項目と数字が不明な項目は未記録として扱います。

`todo.exe progress <id> <値>`、`todo.exe add/start/block ... --progress <値>` で記録します。`todo.exe progress <id> --clear` は数字だけを未記録に戻します。`done` は100を記録し、完了済み項目を数字指定なしで再開・停止すると古い100を除きます。

一覧は製品ごとに、短いタイトルの次の行へバーを表示します。数字なしは「進み具合：未記録」、停止理由は横に一言です。

担当向けに次の1行を既存のshared-todo Skillと各エージェントのルールへ追加しました。

- 共有ToDoを更新するときは、確認できた完了条件の達成率（0〜100）を start/block の --progress または todo.exe progress <id> <数値> で記録する。推測で数字を作らず、不明なら未記録のままにする。done は100を記録する。

更新先はD:/CC/Skills/shared-todo/SKILL.md、C:/Users/makhmeto/.claude/CLAUDE.md、.codex/AGENTS.md、.config/opencode/AGENTS.md、.gemini/GEMINI.mdです。Skillの実体とジャンクションは変更していません。
## ToDo2632: PR169のCI確認（2026-10-15T00:26:57.9719860+09:00）

固定source cbf0a7468182c466d5e5286793346c1f2b999c9dの3workflowを照合した。全runと全14登録jobのSHA・attempt1が一致。Buildは全workspace検査中、Readiness9jobとCodeQL4jobは未終端。開始・待機・skipを成功としていない。

原メタデータ、通常gh run view --logの取得終了値1、個別jobのログ取得終了値1と原stdout bytes・SHA256を、このworktreeの .ci-work/todo-2632-cbf/ に非公開保存した。原ログ本文はまだ取得できていない。旧b920/e380の結果を流用していない。

残りは同3runの終端、全実jobの原ログとBuild件数/doc/Clippy、Readiness全9job、CodeQL4言語の同SHA解析/upload/results/errors確認。再開は同ToDo・同固定runで行う。Cargo・CI再実行・dispatch・取消・merge・配布・Discord・本体編集は行わない。親2488と原2512の完了を変更しない。
