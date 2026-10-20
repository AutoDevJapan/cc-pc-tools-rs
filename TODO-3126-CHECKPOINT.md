# ToDo3126 再開記録

- 作業: 回答済みのConsole旧本人待ち3050/3070を、正規CLIで既存3084依存待ちへ移す。
- Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/305
- 作業ブランチ: scheduler/todo-3126-console。基準main: 6d5c46e62100a212dbf223e29dc8b0f979bdf7aa。
- 原証拠: 依頼が指定したtodo-2903-qualified-release/.ci-workの3110各資料を保持。
  本人ID・元質問・原回答は照合済み。再質問せず、旧UI候補を配布しない。
- 仕様: docs/answered-wait-3126.md。正常CLIのATDDを追加。
- Red: `cargo nextest run --locked -p cc-todo --test answered_wait -E 'test(answered_wait_moves_only_verified_manual_hold_to_existing_dependency)'`。
  1件が `unrecognized subcommand 'answered-wait'` で失敗した。既存コードは未変更。
- 2026-10-21 00:20台の観測: 3050/3070はblockとwait_on3084を保持。
  3084は3103/3121の技術依存待ち。本人待ちへ戻さず、他担当の状態を変更しない。
- 残り: 実装、Greenと利用側/doctest、別モデルmaxレビューを共有ToDo --specで依頼、
  Must修正・CI・通常merge、並行配布確認・ソース/hash照合、実3050/3070の正規移行とreadback。
- 画像sender/履歴readのPR296は編集しない。raw台帳編集・guard緩和・applied/jobの捏造はしない。
- 一時所有ID: 7a11b876847b64f5e8444bfa0f54070e。
  C:/Users/makhmeto/AppData/Local/Temp/codex-owned-7a11b876847b64f5e8444bfa0f54070e。
  red.logと窓を出さないcargo-hidden.ps1を保持。完了後safe-deleteのplan/通常削除/finishで整理する。
- 自分の共有ToDo状態はschedulerが更新する。レビュー等の別ToDoだけ待つときは正規wait --onを保存する。
