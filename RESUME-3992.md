# ToDo3992 再開メモ

質問の再送・元InputSHA/Waitの手修正なしで、同じ質問を後から回答待ちへ移す修復。
Issue561 / PR562。元の制作担当3684は3992と3995の依存待ちであり、解除しない。

- branch scheduler/todo-3992-3684、実装head 012c7cb1755a69cf007e1ea66297d1639bce1866。
- Rust: question-wait --expected-state-fileで通常ロック内の行全体CAS。
- Go: conversation wait --request-id [--check-only]、正規receiptの全質問項目と
  現担当の一致でwait=falseの回答配送もquestion-answerへ接続。元Waitはfalseのまま。
- Go TestConversationとraceが成功。現用todo.exeで新オプション未対応のRedを確認。
- Cargo担当3997は固定Rust commit cca9689975b361664749fbad0afc4b832938b03cの
  関連テスト・CLI候補ビルドを処理中。候補を受領したら
  CC_CONVERSATION_TODO_BINARYで新規ExplicitWaitと既存ActualCanonicalテストを実行。
- 別モデル固定差分レビュー3999をOpus/max指定で登録。未受領。Mustが残れば修正と再確認。
- PRのCI、merge、既存実行版との並行変更比較と通常配布、現用CLIのcheck-only、
  親へ実結果と通常再開後の手順を渡す工程が残る。現段階で完了扱いしない。
- Go現用版はvcs.revision eaae9615182ab6cdd20b9e95675bd38b0617d90b、modified=false。
  そのcommitは実装headのancestor。更新直前に再確認し、並行配布を失わない。
- 自作tempのsafe-delete所有ID: 4a0cbf9fa690cf0a55fc37f477624331。
  specはToDoへコピー済み。PR本文等の一時ファイルを保持中。配布・受渡し完了後だけ清掃する。

親の現在の原質問・画像・私的本文は親worktreeと正規senderに保持する。
公開fixtureやレビューにはコピーしていない。画像への承認・動画完成は本修復の完了条件ではない。
