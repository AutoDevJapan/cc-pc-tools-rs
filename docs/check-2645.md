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

## 検査結果（途中保存）

実行はCREATE_NO_WINDOWで子プロセスを起動する専用の一時PowerShellから直列に行う。Cargoは通常のcargo.cmdが使うcargo_gate.exeを経由し、専用targetとsccache（42259、30G）を指定した。

|実コマンド|exit|結果|
|---|---:|---|
|cargo nextest run --locked -p cc-scheduler|0|149件成功、skipなし|
|cargo fmt -p cc-scheduler -- --check|1|下記の書式差分|
|cargo clippy --locked -p cc-scheduler --all-targets -- -D warnings|0|関連依存・全target含め成功|
|go test -v ./...（repair-guard）|0|16件成功|
|go vet ./...（repair-guard）|0|成功|
|go build -trimpath '-ldflags=-s -w' -o <成果保存先>/scrub391.exe .|0|実行ファイル作成済み|
|cargo build --locked --release -p cc-scheduler --bin scheduler|未確認|実行中|

検査記録の追加による差分だけを除外すると、git diff --exit-code c0a47e8 -- . ':!docs/check-2645.md'は0。別の一時indexから検査記録だけを除いて作ったtreeは固定treeと一致した。変更6ファイルとCargo.toml/Cargo.lockのSHA256はsource-sha256.jsonへ保持した。親ソースのHEAD/treeはこの時点でも一致し、親のgit statusは空だった。

初回のsource-beforeは検査記録自身の新規追加を検出してexit 1になった。検査記録だけを比較対象から除き、コード・原資料を比較し直してexit 0を得た。この検査手順の調整でソースは変更していない。

## 親へ返す修正箇所

fmt失敗を成功として扱わない。固定ソースは変更しないため、親が修正する。

- cc-scheduler/tests/parity.rs:1680: 今回追加したfs::writeの引数を複数行へ展開する。rustfmtの差分では1683までの呼出しが対象。
- cc-scheduler/src/backend.rs:2,63: use項目の順序。
- cc-scheduler/src/engine.rs:3,15: use項目の順序。
- cc-scheduler/src/preparation.rs:3: use項目の順序。
- cc-scheduler/src/product_review.rs:2: use項目の順序。
- cc-job-policy/tests/support/mod.rs:3: use項目の順序。schedulerテストが参照するモジュールもfmtが確認する。

全差分はscheduler-fmt.stdout.logに保持した。通常のcargo fmt -p cc-schedulerで書式を直し、同じcheckを確認する。固定ソースのテスト・Clippy・GitHub CIは成功しており、この指摘は書式検査についてのもの。

## 未完了

scheduler release完了・ビルド後のソース照合・2実行ファイルの絶対パスとSHA256記録が残る。失敗したfmt以外の検査を繰り返さない。完了後に独立検査結果を親へ返し、成果は配布確認まで保持する。
