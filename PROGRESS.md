# 進み具合の記録

共有ToDoの `progress` は担当が確認した0〜100の整数です。稼働時間やジョブの状態から数値を推定しません。古い項目と数字が不明な項目は未記録として扱います。

`todo.exe progress <id> <値>`、`todo.exe add/start/block ... --progress <値>` で記録します。`todo.exe progress <id> --clear` は数字だけを未記録に戻します。`done` は100を記録し、完了済み項目を数字指定なしで再開・停止すると古い100を除きます。

一覧は製品ごとに、短いタイトルの次の行へバーを表示します。数字なしは「進み具合：未記録」、停止理由は横に一言です。

担当向けに次の1行を既存のshared-todo Skillと各エージェントのルールへ追加しました。

- 共有ToDoを更新するときは、確認できた完了条件の達成率（0〜100）を start/block の --progress または todo.exe progress <id> <数値> で記録する。推測で数字を作らず、不明なら未記録のままにする。done は100を記録する。

更新先はD:/CC/Skills/shared-todo/SKILL.md、C:/Users/makhmeto/.claude/CLAUDE.md、.codex/AGENTS.md、.config/opencode/AGENTS.md、.gemini/GEMINI.mdです。Skillの実体とジャンクションは変更していません。