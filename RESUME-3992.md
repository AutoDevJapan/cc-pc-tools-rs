# ToDo3992 再開メモ

質問の再送・元InputSHA/Waitの手修正なしで、同じ質問を後から回答待ちへ移す修復。
Issue561 / PR562。元の制作担当3684は3992と3995の依存待ちであり、解除しない。

- branch scheduler/todo-3992-3684、初期Go実装headは5a078715c41758d3983dae0ef642f865b5652718。
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
- 再開後の統合commitはe31df774（origin/main461e0ab5との4競合を両方保持して解消）。
  4008から初回Must解消と新Must（大きな行のwithdraw）を受領し、その修正と
  CIで動く合成版の回帰、段階を戻さない監査、commit後の期限/状態の応答を追加。
  詳細と原応答SHA/実モデルはdocs/conversation-explicit-wait-review.md。
- Cargo/統合読取4009をOpus/maxの実job/native contextで受領済み。
  候補 D:/CC/Work/unattended-operations-20261013-wt/todo-4009-cli/target/debug/todo.exe
  SHA256=5005c82b3995b6605a460e1a14db785c684626600339f9e3021bdfdf4a6cae2b。
  Rust実装は統合e31df774から追加変更なし。この候補を今後のGo native確認に使う。
- 631ed63eで新候補を使うGo会話関連race/nativeとvetが成功（95.812秒）。
  4009のMust（Actorかつwait=falseの完了済み経路）は同commitの1行修正で対応済み。
  4009の4つの再現ケースを正式なconversation_actor_audit_test.goに取り込み、race成功。
  後続の変更は固定診断とテスト・文書のみ。最終差分の別モデル再確認を登録する。
- e31df774以後のGo追加差分の最終別モデルレビューとCI・merge・配布が残る。
  再開tempのdeploy.ps1は2実行物だけをrunner.lock内で差し替え、前後SHAを照合する
  配布用の準備。構文確認のみで未実行。Go候補e31版は旧候補なので、その後の修正を含む
  最終ソースから作り直す。稼働Goのb0fef3c1はe31に含まれることを確認済み。
- 4016のe31→ba9最終差分レビューを受領し、実Opus/maxとMust0を確認した。
  付随の実害（配送成功後も古いtask_statusを返す）を1ファイルで修正し、
  関連3ケースを合成/nativeのraceで確認した。追加の単独コード確認と最終CIが残る。
  旧temp4a0はplan→通常削除→finish(source_absent)済み。
  最新の実行情報・配布候補・配布計画はgitignore対象todo3992-resume.private.jsonを参照。

親の現在の原質問・画像・私的本文は親worktreeと正規senderに保持する。
公開fixtureやレビューにはコピーしていない。画像への承認・動画完成は本修復の完了条件ではない。
