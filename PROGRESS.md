# 進み具合の記録

共有ToDoの `progress` は担当が確認した0〜100の整数です。稼働時間やジョブの状態から数値を推定しません。古い項目と数字が不明な項目は未記録として扱います。

`todo.exe progress <id> <値>`、`todo.exe add/start/block ... --progress <値>` で記録します。`todo.exe progress <id> --clear` は数字だけを未記録に戻します。`done` は100を記録し、完了済み項目を数字指定なしで再開・停止すると古い100を除きます。

一覧は製品ごとに、短いタイトルの次の行へバーを表示します。数字なしは「進み具合：未記録」、停止理由は横に一言です。

担当向けに次の1行を既存のshared-todo Skillと各エージェントのルールへ追加しました。

- 共有ToDoを更新するときは、確認できた完了条件の達成率（0〜100）を start/block の --progress または todo.exe progress <id> <数値> で記録する。推測で数字を作らず、不明なら未記録のままにする。done は100を記録する。

更新先はD:/CC/Skills/shared-todo/SKILL.md、C:/Users/makhmeto/.claude/CLAUDE.md、.codex/AGENTS.md、.config/opencode/AGENTS.md、.gemini/GEMINI.mdです。Skillの実体とジャンクションは変更していません。
## ToDo3129 Chrome整理の再開記録（2026-10-21 00:41 JST）

- この実行は 20261021-003555-todo-3129、branch scheduler/todo-3129-chrome。ブラウザ操作はこの担当が所有する。
- 全ウィンドウを現行 management APIで実測し、整理前5タブ・2グループ。現在も5タブ・2グループ。今回の既存タブ削除0、既存グループ削除0。
- 既存5タブは所有確認がないため保持した。ページ本文、未知の閲覧履歴、保存済み認証は読んでいない。別担当の現在必要な作業タブは一覧にない。
- 前回の専用タブ1枚・グループ1件の作成と終了検査の記録を元ToDoで確認。保存済みグループの確認と再発防止は未完了。
- main指定のliveファイルは正規の保護判定でclient_resource=trueだったため読み書きしていない。この許可済みPROGRESS.mdへ実測を保存した。原保護設定の探索・読取・コピーは行わない。
- 次は正規Computer Useで保存済みグループを確認する。拒否が再発したら同じ操作を別経路で迂回しない。既存cc-chromeの所有記録と終了処理の調査を継続する。

### ToDo3129 00:51 JSTの更新

- 正規Windows Computer Useの取得は「Computer Use was not approved to use Google Chrome」で不承認。再試行や別経路の迂回はしない。保存済みグループの実整理は未完。
- 元の登録作業場所に解決したlive候補が正規guardで保護対象だったため読み書きしていない。実測と残作業はこのPROGRESS.mdと元ToDo3129の追記へ保存した。
- 既存CLIのopen成功応答が個別closeに必要な記録UUIDを返さない不足を確認。Issue308、draft PR309で同じUUIDをrecord_idとして返す修正候補を保存。所有照合と従来のRust APIは維持する。
- Red候補8c47add、Green候補b8a35bd。rustfmtによる構文読取はexit0。Cargoの実行検査は未実施で、共有スケジューラのcheck3143に登録済み。別モデル読取レビューは3144（Opus5.5/max）へ登録済み。起動・検査・レビュー成功はまだ確認していない。
- この担当のCargo割当はなく、重いビルドやGPU起動は行っていない。実機の削除結果は既存0のまま、タブ5・開いたグループ2を保持している。
- 再開時は3143/3144の原結果を受領し、必要な修正とCI確認後にPR309をマージ・既存CLIへ配布する。実機・保存済みグループの未完条件は別に保持し、今回の小修復だけで3129を完了にしない。UIの再取得は正規の利用可否が変わったと確認できた後だけにする。
