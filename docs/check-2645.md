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

## 独立検査の結果

実行はCREATE_NO_WINDOWで子プロセスを起動する専用の一時PowerShellから直列に行う。Cargoは通常のcargo.cmdが使うcargo_gate.exeを経由し、専用targetとsccache（42259、30G）を指定した。

|実コマンド|exit|結果|
|---|---:|---|
|cargo nextest run --locked -p cc-scheduler|0|149件成功、skipなし|
|cargo fmt -p cc-scheduler -- --check|1|下記の書式差分|
|cargo clippy --locked -p cc-scheduler --all-targets -- -D warnings|0|関連依存・全target含め成功|
|go test -v ./...（repair-guard）|0|16件成功|
|go vet ./...（repair-guard）|0|成功|
|go build -trimpath '-ldflags=-s -w' -o <成果保存先>/scrub391.exe .|0|実行ファイル作成済み|
|cargo build --locked --release -p cc-scheduler --bin scheduler|0|実行ファイル作成済み|

検査記録の追加による差分だけを除外すると、git diff --exit-code c0a47e8 -- . ':!docs/check-2645.md'は0。別の一時indexから検査記録だけを除いて作ったtreeは固定treeと一致した。変更6ファイルとCargo.toml/Cargo.lockのSHA256はsource-sha256.jsonへ保持した。親ソースのHEAD/treeはこの時点でも一致し、親のgit statusは空だった。

初回のsource-beforeは検査記録自身の新規追加を検出してexit 1になった。検査記録だけを比較対象から除き、コード・原資料を比較し直してexit 0を得た。この検査手順の調整でソースは変更していない。

## 親へ返す修正箇所

fmt失敗を成功として扱わない。固定ソースは変更しないため、親が修正する。

- cc-scheduler/tests/parity.rs:1683: 今回追加したfs::writeの引数を複数行へ展開する。
- cc-scheduler/src/backend.rs:2,63: use項目の順序。
- cc-scheduler/src/engine.rs:3,15: use項目の順序。
- cc-scheduler/src/preparation.rs:3: use項目の順序。
- cc-scheduler/src/product_review.rs:2: use項目の順序。
- cc-job-policy/tests/support/mod.rs:3: use項目の順序。schedulerテストが参照するモジュールもfmtが確認する。

全差分はscheduler-fmt.stdout.logに保持した。通常のcargo fmt -p cc-schedulerで書式を直し、同じcheckを確認する。固定ソースのテスト・Clippy・GitHub CIは成功しており、この指摘は書式検査についてのもの。

## 配備用成果

2026-10-15 01:30:37 JSTに2実行ファイルの作成とビルド後の照合が完了した。source-afterの差分検査はexit 0。全stdout/stderrと実コマンド・cwd・開始終了日時・exitはcommands.jsonと各log、GitHub CIのstep結果はgithub-build-speed.json/github-readiness.jsonへ保存した。判定はmanifest.jsonのindependent_check_complete=true / all_checks_passed=false / deployed=falseに分けて記録した。

|成果|絶対パス|bytes|SHA256|
|---|---|---:|---|
|guard|C:/Users/makhmeto/AppData/Local/Temp/codex-owned-5652fc6344fede6c9c6b12646b513e6f/scrub391.exe|3529728|c0d9650f803df8c533e9c8d0735330302c4d455ef659013583d0435e1d8e4865|
|scheduler|C:/Users/makhmeto/AppData/Local/Temp/codex-owned-5652fc6344fede6c9c6b12646b513e6f/scheduler.exe|5612544|90317492d6e3e4b5b330d667efcf98957bb7b319c803183929b844a60eb3706e|

独立checkの担当作業は完了。親の製品作業は完了としていない。親へ上記fmt失敗の修正を返す。書式修正後の新ソースをこの固定ソースと同じものとして配布しない。親が使う実行ファイルは、実際に配布するソースと照合し、配布後の確認を済ませるまでこの成果を保持する。

正規のscrub391/schedulerは置換していない。原192項目・本人停止・原依存・履歴・原資料は変更していない。親側の別モデルレビュー、書式修正、マージ、配備、新担当2637の実処理は親の工程として残る。自分のビルドと子プロセスは完了し、ブラウザ・Docker・Civ6・GPU処理は作成していない。
