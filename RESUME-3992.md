# ToDo3992 再開メモ

質問の再送・元InputSHA/Waitの手修正なしで、同じ質問を後から回答待ちへ移す修復。
Issue561 / PR562。元の制作担当3684は3992と3995の依存待ちであり、解除しない。

- branch scheduler/todo-3992-3684、Go実装head 5a078715c41758d3983dae0ef642f865b5652718。
- Rust: question-wait --expected-state-fileで通常ロック内の行全体CAS。
- Go: conversation wait --request-id [--check-only]、正規receiptの全質問項目と
  現担当の一致でwait=falseの回答配送もquestion-answerへ接続。元Waitはfalseのまま。
- Go TestConversationとraceが成功。現用todo.exeで新オプション未対応のRedを確認。
- Cargo担当3997から関連95テスト成功・CASだけを外す負例・候補を受領。
  新テストのfmtと検証記録をcherry-pick済み。既存product_retryテストのfmt不良も親で解消。
  `D:/CC/Work/unattended-operations-20261013-wt/todo-3997-rust-cli/target/debug/todo.exe`
  SHA256=5349D31BC53F3E7508F0F99559D1699C9CD39153936FC3041CF6014CFA4DC7FF。
  候補のcc-todo/srcは親の実装と同一。候補でExplicitWait/ActualCanonicalテスト成功、22.170秒。
- 別モデルレビュー3999を受領し、実job/native contextでOpus/maxを照合済み。
  Must2件を修正。詳細はdocs/conversation-explicit-wait-review.md。
  別質問の回答配送・継続世代・大きな行の再試行、期限後の部分成功、明示待機のwithdraw、
  aliasとcheck-onlyのファイル無変更も回帰テストに追加。修正差分の再レビューが残る。
- PRのCI、merge、既存実行版との並行変更比較と通常配布、現用CLIのcheck-only、
  親へ実結果と通常再開後の手順を渡す工程が残る。現段階で完了扱いしない。
- Go現用版はvcs.revision eaae9615182ab6cdd20b9e95675bd38b0617d90b、modified=false。
  そのcommitは実装headのancestor。更新直前に再確認し、並行配布を失わない。
- 修復候補Go CLIの実質問check-onlyはAPI照合成功・replies_verified=0・task_held。
  親3684はwait_on=[3992,3995]、owner_question_waitなしで保持した。
  本番配布後も親が依存待ちならcheck-only結果と通常再開後のwait手順だけを返し、解除しない。
- 自作tempのsafe-delete所有ID: 4a0cbf9fa690cf0a55fc37f477624331。
  specはToDoへコピー済み。PR本文等の一時ファイルを保持中。配布・受渡し完了後だけ清掃する。
- 再開jobの一時所有ID: 0f374850a35303c487c4730a1d555caf。

親の現在の原質問・画像・私的本文は親worktreeと正規senderに保持する。
公開fixtureやレビューにはコピーしていない。画像への承認・動画完成は本修復の完了条件ではない。
