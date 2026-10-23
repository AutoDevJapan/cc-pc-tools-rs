# ToDo3409: 回答照会の判定項目を伏せ字から守る修正のテスト結果

親ToDo3392、親job `20261023-102927-todo-3392`、Issue392 / PR393。
この担当はToDo3409、job `20261023-112229-todo-3409`。
指定の有限なRustテストと候補作成は正常に終了した。実装の追加修正はない。
一時ビルド生成物の清掃は分類未確認で保留し、下記に別記した。

| 工程 | 実結果 | 実終了コード |
| --- | --- | --- |
| Red `eff8bb59489c76584c1813cf457439c618414f04` | コンパイル成功後、指定2件が期待どおり失敗 | 100 |
| Green `90dd0c09f34541183672f6e1afda0f74fd363b05` | 68件成功、778件は指定範囲外 | 0 |
| doctest | 成功、対象テストは0件 | 0 |
| Clippy | `--all-targets -- -D warnings` 成功 | 0 |
| release | 固定Greenから `discord_ask.exe` を生成 | 0 |

Redは`found`キーの消失と`text_redacted`未設定による`Null != true`で失敗した。
別の失敗を期待Redに数えていない。Greenの内訳はlookup 11件、conversation_bridge 25件、
decision_identity 32件。同じ専用targetと既存Cargoゲート・sccacheを使い、次を直列実行した。

```text
cargo nextest run --locked -p cc-ask -E 'test(normal_lookup_redaction_cannot_change_protocol_keys_or_delivery_values) | test(normal_lookup_marks_display_redaction_and_does_not_call_pending_text_available)'
cargo nextest run --locked -p cc-ask -E 'test(normal_lookup_) | binary(decision_identity) | binary(conversation_bridge)'
cargo test --locked -p cc-ask --doc
cargo clippy --locked -p cc-ask --all-targets -- -D warnings
cargo build --locked --release -p cc-ask --bin discord_ask
```

起動は`UseShellExecute=false / CreateNoWindow=true`。全workspaceと元3396の141件は
重複実行していない。GPU、Civ6、課金AI API、親source編集、共有runtime更新は行っていない。
[テスト結果](evidence/todo-3409-3392-check/test-results.json)は保存した原ログから集計したJSONであり、
nextestが直接出力したJSONではない。

## 候補と証拠

新しい候補の絶対パス:

```text
D:/CC/Work/unattended-operations-20261013-wt/todo-3409-rust/.ci-work/candidate-90dd0c09f34541183672f6e1afda0f74fd363b05-20261023-112229-todo-3409/discord_ask.exe
```

12,026,880 bytes。コピー元と保存先の全SHA256が一致した。

```text
44ae9022979d0bac68707775036fc1855f5b544c0620fb86d523cc999fbb3832
```

実ビルドHEADは上記Green、treeは`63e5d28efc490405aeb07d302e898d81ba1ca8ce`。
[入力閉包](evidence/todo-3409-3392-check/input-closure.json)には依存する6クレート、全workspaceの
Cargo manifest、Cargo.lock、設定を含む189ファイルのGit blob ID・実bytes・SHA256を保存した。
入力はビルド後にも不変で、releaseのdep-infoにある83入力がすべて含まれることを照合した。
registry依存の版・チェックサムはCargo.lockと`--locked`に固定し、キャッシュ自体は複写していない。
入力閉包JSONはLF・BOMなしで保存し、全SHA256は次のとおり。

```text
deadd7f76f134f24eb0b27a860358e64ae4b34d4088b7711825e4cf6bdd7dd79
```

[候補票](evidence/todo-3409-3392-check/candidate.json)は`release_eligible=false`、
`latest_max_review_verified=false`、`latest_ci_verified=false`、`deployed=false`。
候補の通常guardは`client_resource=false / classification_unverified=false`だった。
最新レビュー・CIはこの子では未確認であり、旧f848候補や既存の原票は上書きしていない。

[証拠一覧](evidence/todo-3409-3392-check/evidence-index.json)に、原stdout/stderr・実コマンド・
終了値・時刻・HEADを保存したファイルの絶対パス、サイズ、全SHA256を記録した。
原ログと候補はこのworktreeの`.ci-work/`に保持する。巨大な原ログとexeはGitへ複写しない。

## 清掃の保留と残る親の工程

通常safe-deleteで作成した所有ID`3de77355c6a4b764aed1aeb6ddb75fb7`の生成先だけが対象。
`D:/OMB/tmp/codex-owned-3de77355c6a4b764aed1aeb6ddb75fb7`は通常guardで分類未確認となった。
planの実測は8,360,100,069 bytes・5,176項目、推奨は容量を理由とするquarantine。
削除・移動は実行せず、そのまま保持した。容量回収や承認審査の拒否として報告していない。
[清掃結果](evidence/todo-3409-3392-check/cleanup-result.json)に対象・理由・容量・所有票・次の手を残した。
通常の清掃監督がこの正確な所有票とguardの適用範囲を照合して回収する。本人判断は不要。
親はこの保留も、依頼の結果と合わせて既存の話題へメンションなしで記録する。

親3392の最新レビュー・同じソースのCI・マージ・配布・実受入は別工程として未完。
この子は独立PRの作成・マージを行っていない。ブランチは`scheduler/todo-3409-3392-check`。
テスト後の追加は結果文書と証拠だけで、テストした実装への変更はない。
通常checkpointの保存・読返し結果と再開情報は`.ci-work/`へ残す。
