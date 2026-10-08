# PC用Rustツールの作業規則

新しいコードはRustで書く。APIキーで課金される呼び出しは使わない。

ほかのToDoの完了だけを待って終了するときは、必ず `D:/CC/Tools/todo/todo.exe wait <自分のToDo番号> --on <待ち先の番号>` で記録する。複数なら `--on` を繰り返す。「完了待ち」とメモだけを書いたり、オーナーの判断待ち用の `block` を使ったりしない。待ち先がすべてdoneになるまで担当は自動起動されず、完了すると通常の実行枠で再開する。詳細は `D:/CC/Skills/shared-todo/SKILL.md` と [完了待ちの仕様](docs/todo-wait.md)。

Rustの検査は[ビルドとテストの規則](docs/build-speed.md)に従う。作業途中は変更したクレートと依存先の関連検査をnextestで回す。全workspaceはマージ前の最新ソースに対して1回だけ回し、doctestも確認する。同じtargetを使うCargoは順番に実行し、別worktreeの依存はsccacheで再利用する。

別モデルのレビューでMustを解消し、CIが通ったら担当がPRをマージする。実行ファイルを更新する前に、稼働中のソースとの並行変更を確認する。

`queue-config.json` は全体を書き直さず、`codex_jobs.exe update-queue-config --patch-file <JSON>` で必要な項目だけ更新する。既存の `fixed_max_running` はオーナー・司令塔の指定なので、省略やnullを含む配備用パッチでも保持する。解除は健康な自動計算値16以上を連続3回実測してから `update-queue-config --clear-fixed` を使い、CPUが目標70〜85%へ近づく測定記録も確認する。このコマンドが `queue-config.CHANGES.txt` に1行記録する。
