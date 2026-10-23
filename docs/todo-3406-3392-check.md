# ToDo3406: 回答照会の必須修正を独立してテストする

親はToDo3392、実ジョブ `20261023-102927-todo-3392`。
この担当はToDo3406、実ジョブ `20261023-103917-todo-3406`。
対象はAutoDevJapan/cc-pc-tools-rs、Issue392 / PR393。

## 結果

指定された有限なテストと配布候補のビルドを完了した。実装の追加修正はない。
Redは `babed84edc0048ac9100cc7489402de4bf3acf53`、Greenは
`490ffd47e78aa0c5f7a4c5d2f054bddd05746314` に固定した。

| 工程 | 実結果 | Cargo gateの実終了コード |
| --- | --- | --- |
| Red | コンパイル成功後に指定2件が期待どおり失敗 | 100 |
| Green | 66件成功、778件は指定範囲外 | 0 |
| doctest | 成功。対象のドキュメントテストは0件 | 0 |
| Clippy | `--all-targets -- -D warnings` が成功 | 0 |
| release | 同じ固定Greenから `discord_ask.exe` を生成 | 0 |

Redの失敗は、架空名 `ExampleClient3392` の伏せ字漏れと、未確定配送が
`not_sent` になるassertionで確認した。ビルド失敗や別原因を期待Redに数えていない。
Greenの内訳はlookup 9件、conversation_bridge 25件、decision_identity 32件。
原名簿がない場合の参照保持、Pending、API失敗、archived返信、無効mode、GET-only、
新しい画像scopeの区別を指定された関連テストで確認した。
実API、秘密情報、本物の案件名、GPUモデル、Civ6は使っていない。

## 実行したコマンド

既存の `D:/CC/Tools/codex-jobs/cargo_gate.exe` を通常のCargoラッパーとして使った。
すべて同じ専用targetと既存sccacheを使い、Red、Green、doctest、Clippy、releaseの順で
直列に実行した。起動は `UseShellExecute=false / CreateNoWindow=true`。
全workspaceと元3396のrecovery 141件は重複実行していない。

```text
cargo nextest run --locked -p cc-ask -E 'test(normal_lookup_redacts_synthetic_client_names_in_every_output_branch) | test(normal_lookup_posted_but_unverified_question_is_pending_never_not_sent)'
cargo nextest run --locked -p cc-ask -E 'test(normal_lookup_) | binary(decision_identity) | binary(conversation_bridge)'
cargo test --locked -p cc-ask --doc
cargo clippy --locked -p cc-ask --all-targets -- -D warnings
cargo build --locked --release -p cc-ask --bin discord_ask
```

[証拠の一覧](evidence/todo-3406-3392-check/evidence-index.json)に、全stdout/stderrと
実終了を記録したJSON、コマンド、時刻、HEAD、各ファイルの全SHA256を保持した。
元の全出力も `.ci-work/todo-3406-3392-check/` に残している。
テスト結果JSONは保存した全ログから抽出したもので、nextestが直接出力したJSONではない。

## 新しい配布候補

絶対パス:

```text
D:/CC/Work/unattended-operations-20261013-wt/todo-3406-rust/.ci-work/todo-3406-3392-check/candidate-490ffd47e78aa0c5f7a4c5d2f054bddd05746314/discord_ask.exe
```

サイズは12,145,152 bytes。元のビルド出力と保存先の全SHA256が一致した。

```text
f848aebecc50332e6a155d56eb07d4159014706ccb394abaf89cf9dba45ae70e
```

実ビルドHEADは上記Green、treeは `7607183cb4b64246a3ab8ceb43b4d1cc9f7a9e35`。
[入力閉包](evidence/todo-3406-3392-check/input-closure.json)には、依存する6クレートと
Cargo.lock等を含む176ファイルのGit blob ID・実bytesのSHA256を保存した。
releaseのdep-infoにある83入力すべてがこの一覧に含まれることを照合した。
外部依存の版とチェックサムは記録したCargo.lockと `--locked` に固定し、
registryのキャッシュ自体は複製していない。コンパイラーはrustc 1.95.0、sccacheは0.18.0。
入力閉包JSONのSHA256:

```text
d8e45767db97299d80c08a11a8e2feded073c34c7a99f319d0889f611d8e6a1f
```

[候補票](evidence/todo-3406-3392-check/candidate.json)は `release_eligible=false`、
`latest_max_review_verified=false`、`latest_ci_verified=false`、`deployed=false`。
最新の別モデルレビューと同じソースのCIはこの子では確認していない。
元3396のf804候補と既存141件の成功履歴は保持し、修正後の成功に流用していない。
現用への配布、親ソース、PR393、共有設定・台帳・ToDo状態は変更していない。

## 後片付けと親へ残す工程

所有ID `facf6bf26f2bff0187676ad938dfcc33` の自分のtargetだけを片付けた。
safe-deleteのplanは8,365,990,608 bytes・5,175項目を容量上限でquarantineとした。
続く正規quarantineは `QUARANTINE_CROSS_DRIVE` で変更前に終了した。
Skill所定の同ドライブ移動として、リンク・Gitソース不在・所有・絶対パス・終了を
確認し、次へ退避した。移動前後の項目数とサイズが一致し、元の場所はない。

```text
D:/削除希望/20261023-todo-3406-cargo-facf6bf2/target
```

実削除と容量回収は未実施で、この約8.37GBは保持している。元の所有票はtoolの
preflight失敗時の `owned` のままであり、手動移動を成功したtool receiptへ偽装していない。
退避先の `理由.txt` と[清掃結果](evidence/todo-3406-3392-check/cleanup-result.json)に
原因と次の手を記録した。通常の清掃担当がこの保全記録を照合して回収できる。
本人判断は不要。親はこの保持容量も依頼の結果と合わせてメンションなしで報告する。

親3392は最新レビュー・CI・マージ・配布・送信なしの実受入・回答の実読と適用を
引き続き担当する。この子の有限工程の正常完了を親3392全体の完了にしない。
作業ブランチは `scheduler/todo-3406-3392-check`。
テスト後のこのブランチへの追加は結果文書と証拠だけで、テストした実装との変更はない。
終了時の実HEADを使ったcheckpointは `.ci-work/todo-3406-3392-check/` へ保存する。
