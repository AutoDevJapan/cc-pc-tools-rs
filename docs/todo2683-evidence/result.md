# ToDo2683 固定ソース検査結果

状態: 完了。指定された独立検査の有限な範囲を完了した。製品の配備・自動再開の完了ではない。

## ソースの同一性

- 検査・ビルドHEAD: `a722fea2e28ff0a9081ce3e5701da414d4882f9d`
- 検査・ビルドtree: `2d598998342712049fd2eea4d0923ee01b8c5b4d`
- `git fetch origin scheduler/todo-2675-work`: exit 0、FETCH_HEADは上記固定HEAD。
- 開始HEAD `c56510c7aef2fdfcce8844391db8869aecbf19cb` は、固定対象にないmainの13ファイルの追加差分を持っていた。
- 自分のブランチへ正規mergeしたコミット: `c4b96796c15e775672ccb43cf564efc362e7ae40`、競合なし。
  2親は開始HEADと固定HEAD。mergeのtreeは `51804a7545a52054f8f4d59cd879039200b8a5c0`。
- 上のmerge状態は検査対象と混同しない。自分のworktreeをdetached HEADの固定対象へ切り替えて全検査を行った。
  release後も固定HEADに対する追跡ファイル全体の差分は空、exit 0。
- 最後に元の作業ブランチへ戻し、検査結果だけを追加する。この証跡コミットのHEAD/treeをビルド元と混同しない。
- 前回2676の31cbf3dの成功結果は今回へ流用していない。原2675と他担当のworktreeを編集していない。

## 実行と結果

共通実行: `D:/CC/Tools/codex-jobs/cargo_gate.exe`。CreateNoWindow=trueで起動。
生成先: `D:/CC/Work/unattended-operations-20261013-wt/todo-2683-work/target`。
sccache: 配備済みの実行ファイル・既存30GBキャッシュ・port 42259を使った。
nextestの既存設定をそのまま複製し、JUnitの出力設定だけを加えた。複製内容はnextest-config.toml。
nextest各コマンドには `--config-file docs/runner-nextest-todo2683.private.txt` を付けた。
既存の直列実行条件を維持し、共有ゲートが各コマンドの並列数を決めた。

| コマンド | exit | 結果 | 全出力の接頭辞 |
|---|---:|---|---|
| `cargo nextest run --locked -p cc-job-policy -p cc-scheduler -p cc-todo --no-fail-fast` | 0 | 345件成功、0 skip。policy43、scheduler153、todo149 | nextest |
| `cargo clippy --locked -p cc-job-policy -p cc-scheduler -p cc-todo --all-targets -- -D warnings` | 0 | 対象3クレート、警告0。テストは実行しない | clippy |
| `cargo nextest run --locked -p cc-scheduler --test parity -E 'test(=repair_step_releases_scheduler_completion_and_catches_up_without_duplicate_launch)'`（mutation） | 100 | 1件失敗、152件対象外。期待した原因を検出 | mutation |
| 同上（復元直後の古い成果物） | 100 | 1件失敗。古いmtimeでmutation版を再利用。下記原因を修正 | restored-stale |
| 同上（復元と再コンパイル確認後） | 0 | 1件成功、152件対象外 | restored |
| `cargo build --locked --release -p cc-scheduler --bin scheduler` | 0 | release実行ファイル1件生成。実行はしていない | release |

ログの絶対ディレクトリ:
`D:/CC/Work/unattended-operations-20261013-wt/todo-2683-work/docs/todo2683-evidence/`
各接頭辞の `.stdout.log` と `.stderr.log` に切り詰めない全出力、`.json` に開始終了時刻・exit・コマンド・HEAD/treeを保存。
nextest系は `.junit.xml` も保存。manifest.jsonに証跡ファイルのSHA256を記録。

## 追加検査6件

nextest.junit.xmlとソース内の名前を照合し、以下すべて実行・成功を確認した。

- `todo_wait::tests::scoped_release_preserves_unverified_rows_and_missing_edges`
- `repair_dependency_release_preserves_prerequisites_scope_and_ownership`
- `repair_dependency_release_keeps_normal_capacity_gate`
- `repair_dependency_release_admits_unstarted_waits_and_caches_paths_per_tick`
- `repair_dependency_release_rechecks_concurrent_stop_dependency_and_project_changes`
- `repair_step_releases_scheduler_completion_and_catches_up_without_duplicate_launch`

## 修正前の原因の実証と復元

自分のengine.rsのreconcile_repairs末尾だけを `self.release_repair_waits()` から `Ok(())` へ変更した。
mutation.diff.logに1行だけの差分を保持。指定の検査はparity.rs:1717で、解除済みIDの期待値[1]に対し実値Nullとなり失敗した。

元ファイルをバイト単位でバックアップし、finallyで復元した。SHA256は復元前後とも
`FEE6ECB96406A3CF16E5CDE86009D4D911B96AFCDDC7B7E8994D7BA0B97EB147`。
正規Git blobも固定対象の `414bb977acee35e980bc6b57670c9422bfe360bf` と一致した。

復元の初回はコピーが元の古いmtimeを保ったため、Cargoがmutation成果物を再利用しexit100。
この失敗の全出力もrestored-staleとして残した。コンパイル出力がなく0.20秒でFinishedとなったことを確認した。
ソースの更新時刻だけを進めて再実行し、cc-scheduler/cc-todoの再コンパイルと1件成功を確認した。
同一バイトの固定ソースへ戻したので、最初の345件成功とClippy結果は有効。無関係なコード修正はない。

## release成果物

- 絶対パス: `D:/CC/Work/unattended-operations-20261013-wt/todo-2683-work/target/release/scheduler.exe`
- SHA256: `4D48DE17A1D9D8172357BFDE4F1A213BD07BE20B31B04967F3B514A70919BC09`
- サイズ: 5,646,848 bytes
- ビルド元HEAD/treeは上記固定対象。artifact.jsonにビルド終了後の照合を保存。
- 親が使う候補として生成先を保持する。一時バックアップはsafe-delete planの通常削除を別呼出しで実行し、finishはsource_absentを確認。
  所有ID `6d0a79eb0559721af0b8cfa2f2b9fbdf`。ブラウザ・Docker・GPUモデル・子Codexを起動していない。

## 未検査と主担当の残作業

全workspace・doctest・PR正規CI・別モデルレビュー・merge後のソース・配備・本番scheduler起動・実ToDoの自然な解除/担当再開はこの独立担当では未検査。
別モデル確認は別の独立担当2684の結果と合わせ、全workspaceは親のPRの正規CIで1回だけ行う。
ここでは新しいPRを作らず、配備や本番の状態変更も行っていない。元2675の完了を意味しない。
