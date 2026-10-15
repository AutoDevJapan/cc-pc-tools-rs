# ToDo2675の固定ソースに対する独立検査（ToDo2676）

2026-10-15、依頼された有限の検査はすべて成功した。コード修正は不要。
本記録は独立検査の結果であり、元2675の配備・実自動再開の完了ではない。

## 検査したソースと実行条件

- リポジトリ: `https://github.com/AutoDevJapan/cc-pc-tools-rs.git`
- 作業場所: `D:/CC/Work/unattended-operations-20261013-wt/todo-2676-work`
- 作業ブランチ: `scheduler/todo-2676-work`
- `git fetch origin scheduler/todo-2675-work` 後、`git merge --ff-only 31cbf3d` が成功（いずれもexit 0）。
- 検査・ビルド時HEAD: `31cbf3dbfd5417f6d478b476a5bfc35edc65f11b`
- 検査・ビルド時tree: `c399b55d1bde5d058d1a6ec4566056cfecd3f158`
- 競合なし。新しいmainや他担当の変更は混ぜていない。検査終了時も追跡済みソースの差分は0。
- 本記録とevidenceを保存する後続コミットは検査資料だけを追加する。上記のビルドHEADと区別する。
- すべてのCargoは `D:/CC/Tools/codex-jobs/cargo_gate.exe` を直接経由した。
- `CARGO_TARGET_DIR` はこのworktree内の所有記録付き専用フォルダーの `target` に設定した。
- テスト、Clippy、修正前後の再現、release buildを直列に実行した。
- nextestにはJUnit出力用の `--tool-config-file todo2676:<作業場所>/evidence/todo-2676/nextest-junit.toml` のみ追加した。
  元の `.config/nextest.toml` のプロセス検査の排他設定は保った。

## 結果

| コマンド（cargoは共有ゲート経由） | exit | 実行結果 | 全出力ログ（作業場所からの相対パス） |
|---|---:|---|---|
| `cargo nextest run --locked -p cc-job-policy -p cc-scheduler -p cc-todo --no-fail-fast` | 0 | 344件成功、失敗0、skip 0 | `evidence/todo-2676/nextest.log` |
| `cargo clippy --locked -p cc-job-policy -p cc-scheduler -p cc-todo --all-targets -- -D warnings` | 0 | 指定3クレートの全targetを確認、警告0 | `evidence/todo-2676/clippy.log` |
| 一時変更後の `cargo nextest run --locked -p cc-scheduler --test parity repair_step_releases_scheduler_completion_and_catches_up_without_duplicate_launch` | 100（期待どおり） | 1件失敗、151件はフィルター対象外 | `evidence/todo-2676/mutation-red.log` |
| 正確な復元後の上記同一コマンド | 0 | 1件成功、151件はフィルター対象外 | `evidence/todo-2676/restored-green.log` |
| `cargo build --locked --release -p cc-scheduler --bin scheduler` | 0 | releaseの実行ファイル1本を生成 | `evidence/todo-2676/release.log` |

ログの絶対パスは、例えば初回全出力なら
`D:/CC/Work/unattended-operations-20261013-wt/todo-2676-work/evidence/todo-2676/nextest.log`。
各ログに対応する `.meta.txt` にはコマンド・開始終了時刻・exitを保存した。
初回JUnitは `nextest-junit.xml`、再現と復元後はそれぞれ `mutation-red-junit.xml` と
`restored-green-junit.xml` に別保存した。初回のクレート別内訳は43 / 152 / 149件。

## 追加された検査の実行確認

以下の5件が初回344件の中で実行され、すべて成功した。
JUnitから抽出した名前と所要時間を `evidence/todo-2676/added-tests.json` に保存した。

- `todo_wait::tests::scoped_release_preserves_unverified_rows_and_requires_numeric_done_edges`
- `repair_dependency_release_keeps_normal_capacity_gate`
- `repair_dependency_release_rechecks_concurrent_stop_dependency_and_project_changes`
- `repair_dependency_release_preserves_prerequisites_scope_and_ownership`
- `repair_step_releases_scheduler_completion_and_catches_up_without_duplicate_launch`

## 修正前の原因の実証

自分の `cc-scheduler/src/engine.rs` だけを変更した。
`reconcile_repairs` 末尾の `self.release_repair_waits()` が正確に1か所あることを確認し、
その1行だけを `Ok(())` に一時置換した。差分は `evidence/todo-2676/mutation.diff`。
指定テストは `parity.rs:1712` で失敗し、依存解除されたIDの期待値 `[1]` に対して実値が `Null` だった。

事前保存したファイルのバイト列で同ファイルを復元した。
復元前の原本と復元後のSHA256はどちらも
`C299BCCAF908B3D1E737907EEE6E5337267C9F789B0F2CF1C174AD2CD2FDFF43`。
Git差分も0。復元後は再コンパイルされ、同じ指定テストが成功した。
他担当のworktreeおよび無関係な変更には触れていない。

## 親担当へ渡す実行ファイル

- 保持するexeの絶対パス:
  `D:/CC/Work/unattended-operations-20261013-wt/todo-2676-work/evidence/todo-2676/scheduler.exe`
- SHA256: `A20710406C68D21AE3A4173972AEE76E101A48C7CEE8BDC2BBF9F6D07370D20D`
- サイズ: 5,634,048 bytes。
- ビルドしたHEAD/treeは上記の固定値。
- 元の生成先から保持先へコピーし、双方のSHA256一致を確認した。
  元の生成先と保持先は `evidence/todo-2676/artifact.json` に記録した。
- exeはGitへ追加しない。実行・配備は行っていない。
- 不要な専用ビルド生成物の後片付け結果は `evidence/todo-2676/cleanup.json` に保存する。

専用生成物3,975,865,116 bytesは、サイズ上限による退避指定に従って
`D:/削除希望/20261015-cc-pc-tools-rs-todo-2676-cargo-target-7b703bce/cargo-output` へ移した。
削除・容量回収は未実施。補助処理は別ドライブへの退避を事前に拒否したため、
Skillに従って範囲・所有・リンク不在を確認し、同じDドライブ内への移動と結果確認を行った。
実行ファイルは上記の保持先に存在する。原生成先の履歴はartifact.jsonに残した。
後片付けのDiscord無通知記録はexit 1で失敗した。
`QuestionStateRecoveryRequired`（質問台帳と本人回答の原記録の復旧待ち）のため投稿されていない。
別経路の送信や再質問は行わず、失敗出力を `cleanup-discord.log` に保持した。

## 未検査・親担当に残る作業

全workspaceのnextestとdoctestは依頼どおり実行していない。親の正規CIで行う。
別モデルレビューは独立したToDo2677の担当範囲であり、本検査では実施していない。
PR作成・mainへのマージ・稼働schedulerの配備・本番起動・自然な依存完了後の実担当再開は未実施。
それらは元2675が担当する。実ToDoや原保護設定を変更して検査していない。
不具合や必要なコード修正は、この検査では見つからなかった。
