# ToDo3414: 回答照会の誤案内と判断範囲の保護を確認した結果

親3392、親job `20261023-102927-todo-3392`、[Issue392](https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/392) / [PR393](https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/393)。
指定の固定候補のテストと新しい候補作成は成功した。追加の実装修正はない。
この担当は3414、job `20261023-120331-todo-3414`。親sourceの編集、PR作成・マージ、共有実行版の更新は行っていない。

| 工程 | 実結果 | 終了コード |
| --- | --- | --- |
| Red `10fffdbd2bb0ce2690e166a9bfff0861d39753a6` | コンパイル成功後、指定2件のassertionが失敗 | 100 |
| Green `138d2fd805447ff2e1285310aaf89ea39ba2b529` | 70件成功（lookup13、bridge25、identity32）、778件は対象外 | 0 |
| doctest | 成功、対象0件 | 0 |
| Clippy | 全ターゲット、`-D warnings`で成功 | 0 |
| release | 固定Greenから新しい実行ファイルを生成 | 0 |

Redは、本文もthreadもURLもない場合の`text_redacted`と、判断識別子が伏せ字で変わる場合の`text_available`が、どちらもtrueを返すassertionで失敗した。別の障害を期待Redへ数えていない。
Greenには、原名簿が欠けた送信待ちの質問を返信取得へ誤誘導しないテストも含む。
本文・添付だけ・mentionだけの返信、編集版、異なるscope・画像版、回答の存在と承認の区別も指定の関連テストで確認した。

同じ所有target、通常のCargoゲート、既存sccacheを使い、次を直列実行した。
すべての子コマンドを`CREATE_NO_WINDOW`で起動し、原stdout・stderr・実引数・時刻・HEAD・終了コードを`.ci-work/`へ保存した。

```text
cargo nextest run --locked -p cc-ask -E 'test(normal_lookup_missing_question_does_not_claim_a_hidden_body_or_thread) | test(normal_lookup_masked_decision_kind_or_resource_uses_original_reply_reference)'
cargo nextest run --locked -p cc-ask -E 'test(normal_lookup_) | binary(decision_identity) | binary(conversation_bridge)'
cargo test --locked -p cc-ask --doc
cargo clippy --locked -p cc-ask --all-targets -- -D warnings
cargo build --locked --release -p cc-ask --bin discord_ask
```

全workspaceテスト、旧3409候補の上書き、GPU・Civ6・実AIの利用はない。
[テスト結果](evidence/todo-3414-3392-check/test-results.json)は原stderrから集計したもので、nextestが直接生成したJSONではない。

新しい候補の絶対パスは次のとおり。

```text
D:/CC/Work/unattended-operations-20261013-wt/todo-3414-rust/.ci-work/candidate-138d2fd805447ff2e1285310aaf89ea39ba2b529-20261023-120331-todo-3414/discord_ask.exe
```

12,027,392 bytes。コピー元と候補のSHA256は`dff5f38dd96740734d2290191f357aef598ca58a114b6b8a0b03fbb6a20a129b`で一致した。
実ビルドtreeは`ed62c16d131f02ffcf36c5711d76484d50d4859d`。
[入力閉包](evidence/todo-3414-3392-check/input-closure.json)は依存する6クレートの全追跡ファイル、全Cargo manifest/lock、追跡された設定を含む195入力のGit blob・実bytes・SHA256を記録する。195入力はビルド前後で不変で、release dep-infoの自作コード83入力をすべて含む。
閉包JSONの全SHA256は`a8240f8936c61b03690d3d3f90eb36fce27dec84b9d4684d9c8aa4fd6a8e34dd`。
registry依存の版とチェックサムはCargo.lockと`--locked`で固定した。registryキャッシュやOS全体を独立に再検証したという意味ではない。

[候補票](evidence/todo-3414-3392-check/candidate.json)に全SHA、サイズ、実HEAD/tree、ツール版・hash、通常guardの成功を保存した。
`release_eligible:false`、最新レビュー・CI未確認、未配布を維持する。親は3411のCI修復を別に受領し、差分と入力閉包を照合してからレビュー・必要CI・マージ・配布・配布後の実受入を完了する。この子は3411を統合していない。
[ログ一覧](evidence/todo-3414-3392-check/evidence-index.json)には原ログ・必要receiptの絶対パス、サイズ、全SHA256を記録し、原ログとexeはGitへ入れない。

所有scratch `9975d44bed860ed1b6bc8f25c81e902d`の不要生成物の清掃結果は、後続の同ブランチの記録へ追記する。候補・原ログ・必要receiptは配布まで保持する。
