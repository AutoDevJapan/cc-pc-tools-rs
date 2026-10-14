# ToDo2628の再開記録

## 完了した調査

- 起動時のD:/CC/AGENTS.md、専用sourceのAGENTS.mdを読み直した。CLAUDE.mdはsource内にない。
- remoteとGitHub APIでAutoDevJapan/cc-pc-tools-rsを確認した。
- 最新origin/mainとこのworktreeの開始HEADは0b703782a81e5c262f771caa849e766872591b2e。
- 原sourceはtracked変更なし。他担当の未追跡成果は保持した。
- 修復sourceと元登録sourceはguard=false。稼働CLIはclassification_unverified=true/client_resource=true。保護設定は変更しない。
- 旧board()が履歴を含むitemsとgroupsを複製することを確認した。

## 実装中

localai-web-rsの新規有限取得API、CLI、意味のある回帰検査とdocs/todo-listing-2628.mdを追加。
他クレート、原記録、モデル切替、共有キューは編集していない。
最初の関連検査はAPIクエリの挿入位置誤りでcompile失敗。正しいHTTP入口へ修正した。
関連nextest実成功: run caf4bd34-25b8-4358-8d94-f5cb9b7a9bb5、6/6成功。
追加の最大ページ予算検査とCLI失敗分類を含めて再検査する。

検査生成先の所有記録: f49c14cb8bc76f0d4e97563ee358396e。
path: C:/Users/makhmeto/AppData/Local/Temp/codex-owned-f49c14cb8bc76f0d4e97563ee358396e。
生成先はこの配下target。safe-deleteのplan/別呼出し削除/finishを使う。

## 残る工程

1. 関連nextest、実応答形式と原bytes保持検査を確認し、必要な修正を保存。
2. 固定HEADの別モデルレビューをspec付き共有ToDoへ登録。Mustを直す。
3. PRを作成して既存Build speed CIのworkspace/doc/Clippy実成功を確認。固定HEADでmerge。
4. 稼働対象の正規guard確認、並行source確認、bytes/hash照合、CLIと必要なサーバー更新。
5. 実CLI終了0/ok=trueと原台帳不変を確認してreceiptを保存。元2626へ正確な成果を渡す。
6. 自分の検証生成物を回収。ブラウザとGPU/Civ6は使用していない。

自分の共有ToDo状態はスケジューラが管理する。別レビューToDoだけを待つときはtodo wait 2628 --on <レビュー番号>を記録する。
