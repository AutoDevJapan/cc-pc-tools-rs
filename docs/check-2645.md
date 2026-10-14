# 親2636の固定ソース独立検査（ToDo2645）

## 範囲と再開

- 依頼: 固定HEADの関連検査と配備用2実行ファイルの作成のみ。親の編集・PR・マージ・正規配備は担当しない。
- 元作業先: D:/CC/Work/unattended-operations-20261013-wt/todo-2636-work
- 検査作業先: D:/CC/Work/unattended-operations-20261013-wt/todo-2645-2636
- 固定ソース: c0a47e86fde8329a50c8a382399c70ad87b8d642
- 固定tree: a14ab06369ede48a47afa3352fed8645342067a8
- origin: https://github.com/AutoDevJapan/cc-pc-tools-rs.git
- PR: https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/172
- 自分のブランチを固定コミットへfast-forwardした。元ソースは変更していない。
- 保護判定: 両作業先ともclient_resource=false。原保護名簿は未復旧。確認済み修復範囲だけを扱う。
- ジョブ: 20261015-012428-todo-2645。Cargo割当あり。GPU/Civ6起動なし。
- 自分の共有ToDo状態更新はスケジューラに任せる。

## 成果の所有と保持

- 所有ID: 5652fc6344fede6c9c6b12646b513e6f
- 成果保存先: C:/Users/makhmeto/AppData/Local/Temp/codex-owned-5652fc6344fede6c9c6b12646b513e6f
- Cargo生成先: D:/CC/Work/unattended-operations-20261013-wt/todo-2645-2636/target
- 親が配布と確認を済ませるまで成果を削除しない。後の削除はsafe-deleteのplan→通常削除→finishを使う。
- 個別コマンドの全stdout/stderrとexitは成果保存先に保持する。

## GitHub CIの確認

固定HEADの以下3実行はsuccessだった。必要な最終workspace・doctest・Clippyを含むBuild speedの成功を利用し、全件は重複実行しない。

- Build speed: https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38593696286
- Unattended readiness tools: https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38593696374
- CodeQL PC: https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38593696275

## 未完了

cc-scheduler nextest、関連fmt/Clippy、repair-guard test/vet/releaseビルド、scheduler releaseビルドを一つの順序で実行する。全コマンドのexitとソース同一性を確認し、2実行ファイルの絶対パス・SHA256を記録して親へ返す。失敗した場合はソースを変えず、原因とfile:lineを記録する。
