# ToDo3751 再開情報

- 担当: 20261025-070552-todo-3751、元3277の依頼。
- 開始HEAD: 2e173041612ec7ae4736ab3bd04293fa7bc27492。
- 実owner: gh repo viewでAutoDevJapan/cc-pc-tools-rsを確認。
- 元1171はrecovery_unverified、3277はwait_on=[3751]。両方の状態を保持する。
- 正規guardの3277 receiptは元3638/実装3650、spec SHA 7d526ccfc04dae7df0b9a486a52e1feabaec566378cbfc093353b85684b94d2d。
- 元1171の復元仕様SHA: ae91e338c9d49ec5444c2582963498926e41a40ce0916d3fa240eba42bb41f34。
- 3277実jobは20261025-063120-todo-3277、現在succeeded。実cwdは保存scheduler.worktreeと一致。
- 原因: Rust橋渡しがscheduler-item/jobを渡さず、Goは元rowのrecovery_unverifiedで登録前に拒否する。
- 残り: 回帰テスト、実装修復、別モデルレビュー、CI、merge、現用版との照合と配布、固定質問の通常CLI配送、元3277へ結果。
- 所有済み一時領域: a9123966bdd042a795ceabc93f16c41c（safe-delete）。
- 固定要求: normal-ask-9620ba9f12adf45e1a01ee67010dafe616750231a1625ffcca0c7ed470a6a862。
- 本文/decisionは依頼にある私的な原ファイルを使用し、公開repoへ複製しない。

## 実装候補とテスト（2026-10-25）

- Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/478 。
- Red: TestConversationActorCLIRequiresCompletePairが、担当入力を受け付けない理由で失敗。
- Green: Go会話テスト全件、go vet、正規ToDo CLIの新ビルドを使うactor結合テストが成功。
- 結合テストで、元row不変更、依存待ち中の全文配送と重複抑止、依存完了後の質問待ちと回答解除、ロック前の元本人停止競合の拒否を確認。
- 別project/actor/source/root/spec/job、本人停止、同要求の本文/decision変更を拒否。
- 同actorの通常の次jobでも元の質問・元送信jobの証跡を保持。実反映の記録後だけ質問を閉じる。
- Rust nextest（cc-ask/cc-todo、1171件）は実行中。doctest/Clippy、別モデルレビュー、CI、配布と実配送はこれから。
- 初回Rustコンパイルはserdeの直接依存がないため失敗。既存の厳密な文字列envelope検証へ修正し、依存追加なしで再コンパイル成功。
- Goの既存固定エラーテストは出力互換性の差で失敗。未知の失敗は従来出力を保ち、確認できる固定段階だけ追加する形へ直して該当テスト成功。
- cargo fmtによる対象外ファイルの書式変更は除去した。

## 指示文までの配送確認

- PR: https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/479 。
- 最初の候補26a27dbaでRust1171件成功、doctest成功（対象0件）、Clippy -D warnings成功。
- 追加照合で依存待ち中のメモ保存だけでは再開プロンプトに全文が入らないことを発見。有限actor scopeでの正規editに、同じロック内の指示文更新を追加。
- 追加後: cc-todoの関連48件成功。新ビルドの正規ToDo CLIを使うactor結合テスト全件で、指示文の全文と編集回答の置換まで成功。
- 別モデルレビューToDo3754を正規subworkで登録。現在は通常スケジューラの起動待ち。依頼時のslash付きモデル名はCLIの入力検証に拒否されたため、slashなしのモデル名で登録した。正規審査拒否ではない。
- statusでbusyを確認。共有scheduler/runnerを停止せず、ロックも削除していない。CIと残る関連確認を続ける。
