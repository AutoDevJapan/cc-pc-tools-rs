# ToDo3143: Chrome記録UUIDの有限なCargo検査

元ToDo3129 / Issue308 / PR309に対する独立した検査。製品ソースは編集しない。
Chrome実機・GPU・APIキー・稼働バイナリ配布・親のブランチ変更は対象外。

## 開始と復帰先

- 担当job: `20261021-012447-todo-3143`
- 作業場所: `D:/CC/Work/unattended-operations-20261013-wt/todo-3143-chrome-uuid`
- 開始branch / 復帰先: `scheduler/todo-3143-chrome-uuid`
- 開始HEAD: `6d5c46e62100a212dbf223e29dc8b0f979bdf7aa`
- 開始時の `git status --porcelain=v1`: 空
- origin: `https://github.com/AutoDevJapan/cc-pc-tools-rs.git`
- 元作業場所とこの作業場所のclient guard: `client_resource=false`
- 一時資源所有ID: `75259ac2510ddf81bcba5cf5ec8be57a`
- 一時資源: `C:/Users/makhmeto/AppData/Local/Temp/codex-owned-75259ac2510ddf81bcba5cf5ec8be57a`

## 実行結果

自分のworktreeだけを固定Red・Greenへ順にdetachした。
全工程を `D:/CC/Tools/codex-jobs/cargo_gate.exe` 経由、`CreateNoWindow=true` で実行した。
同じ所有付き `target` とsccacheを利用し、各コマンドは順に1回ずつ実行した。
テストのTEMP/TMPも所有付きフォルダー内へ向けた。

| 工程 | 固定ソースSHA | 終了コード | 結果 |
|---|---|---:|---|
| Red | `8c47add0e2563eddac6f55eb005bb55435ef1b16` | 100 | コンパイル成功。指定assertで1件失敗、43件選択外 |
| Green | `d2389dabc6213f835f3f02108988b0c84fd80743` | 0 | 同じ検査が1件成功、43件選択外 |
| 既存の失敗経路 | 同Green | 0 | 永続化失敗時の2件が成功、42件選択外 |
| releaseビルド | 同Green | 0 | `owner_chrome.exe` の生成成功 |

Red・Greenのコマンドは共通。いずれも生成先を明示した。

```text
cargo nextest run --locked -p cc-chrome -E 'test(open_receipt_selects_exact_owned_record_for_phase_completion)'
```

Redの失敗理由は厳密に `open response must return the persisted record UUID`。
`native.rs:820:53` のexpectで失敗しており、コンパイル失敗ではない。
Greenは戻り値のUUIDの構文、永続化ファイルと戻り値の同一性、記録済み所有情報、
選択したtab=42だけの終了、別のtab=99の保持、終了履歴と理由までassertを通過した。
Greenのjob fixtureは `{"id":"receipt-job","status":"running"}`（`native.rs:816`）。
UUID付きの模擬pipeへ直接接続する `open_on` とtempfileだけを使い、実機Chromeは使っていない。

`persist_or_close` の戻り型変更が、その関数を利用するproxyの失敗経路にも影響するため、
次の既存検査2件だけを追加した。前者は記録不能時の終了と成功応答の抑止、後者は
終了にも失敗した場合の緊急記録の保持と次回回収をassertしている。

```text
cargo nextest run --locked -p cc-chrome -E 'test(proxy_rolls_back_unrecordable_creation_and_returns_error_instead_of_success) | test(failed_rollback_keeps_emergency_record_for_the_next_sweep)'
```

依頼に従って全workspace検査は追加していない。

```text
cargo build --locked --release -p cc-chrome --bin owner_chrome
```

Greenのreleaseビルドは1回だけ実施。生成物をコピーし、元とコピーのSHA256一致を確認した。

- 保持した生成物: `D:/CC/Work/unattended-operations-20261013-wt/todo-3143-chrome-uuid/codex-owned-e76938c1c757a795dde35c3a5744ba22/owner_chrome.exe`
- SHA256: `2409C01E2BAE582EE87C8615B767C97F4B53F1443AC6EF545593709C0A425AB1`
- サイズ: 1,406,464 byte
- 保持先の所有ID: `e76938c1c757a795dde35c3a5744ba22`。親が利用する検査成果物なので削除対象外。
- 元の生成先: 一時資源内の `target/release/owner_chrome.exe`。不要な依存生成物とともに回収する。

機械可読のコマンド・時刻・終了コード・ソースSHA・生成物情報は
`todo-3143-cargo-results.json`、各工程の完全なstdout/stderrは
`todo-3143-cargo-{red,green,existing,release}.txt` に保存した。

## 復帰と残りの保存手順

`git switch scheduler/todo-3143-chrome-uuid` で開始branchへ復帰済み。
開始HEADに対して製品ソース・Cargo.toml・Cargo.lockの差分なしを確認した。
検査記録だけをこのブランチへcommitし、作業場所がcleanなことを確認する。
自分のid=3143 / job / expected_branch=開始branch / branch=復帰branch / 実HEADで
`scheduler checkpoint-workspace` を実行し、保存成功を確認する。
成果物と必要なログは保持済み。不要な一時資源はsafe-deleteのplan→別呼び出しで削除→finishで片付ける。

## 状態

指定された検査とビルドは達成した。検査記録のcommit、不要物回収、checkpoint保存を残す。
これは検査だけの達成であり、親3129の実機整理・別モデルレビュー・CI成功・マージ・配布を完了扱いしない。
