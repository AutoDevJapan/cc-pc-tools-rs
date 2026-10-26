# ToDo3997: question-wait のRust候補検証

親3992の独立したCargo確認。実装元は
`cca9689975b361664749fbad0afc4b832938b03c`。
自分の `scheduler/todo-3997-rust-cli` に通常mergeし、
`93fc91f3929375b66478816efd54b4e5d0117035` へ取り込んだ。
元の作業ブランチと他担当のworktree、元3684の待機・本文・画像・監査は変更しない。
本番配布、独立PR、レビューの再委譲は対象外。

## 実行条件と残作業

- 作業場所と元登録場所の `scrub391 --guard-path` は
  `client_resource=false`、`access_allowed=true`。
- AGENTS、変更範囲のテスト方針、共有Cargoゲートの規則を確認。
- 同じworktreeのtargetを使い、共有 `cargo_gate.exe` 経由で直列実行する。
  起動は `CreateNoWindow=true`、`UseShellExecute=false`。
- 対象ファイルのrustfmt初回確認は、新しいテスト内だけの書式差で失敗した。
  該当ファイルを整え、再確認する。
- `conversation_wait`、`conversation_full_reply` と、共通CLIおよびEdit公開面の
  `parity` を選ぶ。対象外workspace全件へ広げない。
- CAS比較を外した負例でも新テストが失敗することを確認し、必ず元に戻す。
  完了後は候補ソースで関連テスト・ライブラリdoctestを実行する。
- テストで作られる同じtargetのtodo.exeを保持し、必要な場合だけbin buildを行う。
  候補のSHA256、元HEAD、使用target、件数、時間を親へ返す。
- 自分の共有ToDoの状態はscheduler管理。親や元3684の完了は待たない。

## 確認結果（2026-10-27 JST）

テストと実行ファイルのビルド元HEADは
`5f501d16802e0f7141954c3eada1f38c19a38053`。
このHEADの `cc-todo/src` は実装元 `cca9689975b361664749fbad0afc4b832938b03c`
と差がない。子側の変更は新テストの書式とこの検証記録だけ。

| 確認 | 結果 | コンパイル等を含む実時間 |
| --- | --- | --- |
| 変更2ファイルのrustfmt --check（edition 2024、skip_children=true） | 成功 | 0.328秒 |
| CAS負例：question-waitだけsnapshot比較を外した同じ新テスト | 1件が想定どおり失敗 | 56.624秒 |
| nextest：conversation_wait / conversation_full_reply / parity | 46 + 11 + 38 = 95件成功、skip 0 | 54.246秒 |
| cargo test --locked -p cc-todo --doc | 成功、doctest 0件 | 3.564秒 |
| cargo fmt --package cc-todo -- --check --config skip_children=true | 対象外の既存ファイルに書式差 | 2.208秒 |

nextestのテスト実行部分は42.948秒、コンパイルは7.70秒。
いずれも共有ゲートから同じ生成先へ直列実行し、通常の並列上限を維持した。
全workspace検査、独立build、release buildは追加していない。

### CASを検出する根拠

負例は、新しいCLI引数を残したまま `question-wait` の行比較だけを外した。
旧CLIが未知のオプションを拒否した結果とは区別する。
新テストは `accepted changed resume_generation` で実際に失敗した。
負例終了時に元のソースをbyte単位で復元し、Git差分なしとSHA256
`705B5EFB0786A374C4B37137F739467C543712DE54F52EEADB124A54D59E4344`
を確認してから候補を再コンパイルした。

候補では、世代・worker receipt・scheduler・status・依存・本人停止・noteの
7項目それぞれの競合を `conversation_state_changed` で拒否し、ストアの全byteを保持。
変更前の同じ行なら登録でき、古いsnapshotでの再実行は拒否し、現在の待機行を
渡した再実行はストアを書き直さない。既存の実OSロック競合と回答の保持の検査も成功。
ソース上でも通常のtodo.lock取得と最新ストア読込の後に比較されることを確認した。

### 親3992が使う実行ファイル

- 絶対パス:
  `D:/CC/Work/unattended-operations-20261013-wt/todo-3997-rust-cli/target/debug/todo.exe`
- SHA256: `5349D31BC53F3E7508F0F99559D1699C9CD39153936FC3041CF6014CFA4DC7FF`
- サイズ: 15,929,856 byte。debug候補、build元HEADは上記 `5f501d16802e0f7141954c3eada1f38c19a38053`。
- 使用target:
  `D:/CC/Work/unattended-operations-20261013-wt/todo-3997-rust-cli/target`
- 候補自体のguardは `client_resource=false`、`access_allowed=true`。
  実ファイルで `question-wait --help` の `--expected-state-file` 公開を確認。
- Go側の `CC_CONVERSATION_TODO_BINARY` にこのパスを設定できる。
  テストで生成・実行された同じ候補を保持し、本番のtodo.exeは置換していない。

### 残る懸念と引き継ぎ

パッケージ全体のfmtは `cc-todo/tests/product_retry.rs` の既存書式だけで失敗した。
このファイルのblobは、取り込み前 `99d5fcb2`、実装元 `cca96899`、候補HEADの3版とも
`5691dee50b5fc03a624d003953630180d2f8c72a` で一致する。
依頼の対象外なので変更せず、親へ差分と原因を返す。変更対象の2ファイルはfmt成功。
今回の候補テスト・doctestに未解消の失敗はない。

最初の `checkpoint-workspace` は `workspace checkpoint failed TimeoutError`。
実装の該当エラーはscheduler.lock取得の30秒タイムアウトであり、branch/HEAD照合の
拒否とは区別する。作業ブランチ名は変更していない。終了前に現HEADで正規保存を
有限に再試行し、その成否は生成先の `validation-3997-checkpoint-*` に保存する。
ロックの解除や他担当の停止で迂回しない。

完全なstdout/stderr、コマンド・HEAD・時間・終了コードのJSON、JUnit、候補receiptは
同じtarget直下の `validation-3997-*` として保持する。
所有付き一時記録（safe-delete ID `64327a02c7b53f981d6deeecc9fe667e`）は必要な証拠を
保持先へ移した後に正規のplanで片付け、実行ファイルとtargetは親の受入のため残す。
別モデルレビュー・CI・本番merge・現用CLIの並行変更照合と配布は親3992の担当。
