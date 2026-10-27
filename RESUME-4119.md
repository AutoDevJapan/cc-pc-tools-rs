# ToDo4119 作業再開

- 担当job: 20261027-224658-todo-4119
- branch: scheduler/todo-4119-schedulerlaunchoutcomereceipt
- base: 86fc9f06ca01a6cb8a11720fcfb5a884c5209f56
- 元4086/4091/4099の復旧・入力参照・起動・世代操作はしない。
- 専用worktreeと元登録rootはguard clear、実GitHub所有はAutoDevJapanをAPIで確認。
- 現行cmd_runはterminalにも同じkeyのIDを返し、schedulerは無条件Launched扱い。
- 仕様と再現テストを追加。Red確認、最小実装、Green、レビュー/ラバーダック、CI、
  配布provenanceと通常schedulerの自然な実観測の読戻し、管理受領JSONが残る。
- 一時所有899cd740bcab16bf4fb1d63ce65cbcde:
  C:/Users/makhmeto/AppData/Local/Temp/codex-owned-899cd740bcab16bf4fb1d63ce65cbcde
  build/log/review用。終了時はsafe-deleteのplan→別呼出の通常削除→finishで片付ける。
- 自分のToDo状態はscheduler管理。レビューが必要なら通常ToDoにspec付きで登録し、
  子jobを直接起動しない。ブランチ名を変えた場合は正規checkpointを保存する。
