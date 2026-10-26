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

作業中。次は書式修正、CAS負例、候補テスト、候補実ファイルの照合。
