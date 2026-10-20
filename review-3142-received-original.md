状態: 完了

レビューだけを完了しました。実装・配布・本番移行が完了したわけではありません。固定 base 6d5c46e62100a212dbf223e29dc8b0f979bdf7aa と固定 head c00289ab6a45f456d1b86c6ce1629758b8da5f46 の差分を git show/diff で読み、直すべき Must を2件見つけました。行番号は固定 head のものです。

**読んだ範囲と未読**

差分の6ファイルは全行を読みました。cc-todo の answered_wait.rs、lib.rs、conversation_wait.rs、tests/answered_wait.rs と、docs/answered-wait-3126.md、TODO-3126-CHECKPOINT.md です。利用側は cc-scheduler の common/engine/native と、cc-job-policy の todo_wait/todo_history/todo_store/cancellation を全文読みました。docs/test-build-scope.md も確認しています。

次のものは読んでいません。

- instructions.rs（再開時の定型指示）
- cc-todo の watch
- cc-jobs の pid_created の書込み
- 台帳、3126 の依頼原文、実際の 3050/3070 の行

実行したのは git の読取だけで、cargo と検査は実行していません。

**Must**

1. 本番で expected_note を固定していません（answered_wait.rs:201-220, 541）。本番で固定しているのは、原資料2件の SHA、直接依頼の本文、jobs の場所だけです。通常の block（lib.rs:1189-1204）は、主に status・note・更新時刻を書き換えます。このとき wait_on と旧メモの履歴は残ります。そのため、移行前に本人停止や新しい質問が block で入った場合、証拠 JSON の expected_note を新しいメモに書き直すと wait へ移ります。その行は 3084 の完了時に自動で解除されます。これは「別停止は無変更」と「同形 JSON の自己申告で解除しない」に反します。

   修正：production_scope に、id ごとに照合済みのメモの SHA を固定してください。できれば、無変更検査で確かめた行の state_sha256 も固定します。固定値と違う expected_note を拒否する単体テストも足してください。

2. 本人回答と今回の指示が、再開後の担当に届きません（answered_wait.rs:561-574）。担当への指示文は、定型文に仕様・owner_instruction・前回結果を足して作られます（engine.rs:1183-1247）。メモと領収記録は入りません。領収記録は todo.exe の list/history にも表示されません。既存の回答経路は owner_instruction に回答を書きます（conversation_wait.rs:1008-1046）。今回の移行では、3084 の完了で todo に戻った行（todo_wait.rs:178-208）が元の仕様だけで再開します。その結果、同じ質問の再送や旧候補の配布を招くおそれがあります。instructions.rs は未読なので、この影響には推測を含みます。

   修正：apply 時に、既存の owner_instruction を残したまま、DIRECTIVE・原資料の場所・thread/message ID を追記してください。job/key/phase/worker_receipt は変えません。done 3084 の後もこの指示が残ることを、受入検査で確かめてください。

**Suggestion**

1. 受入検査が実装から独立していない箇所があります（tests/answered_wait.rs:221-224, 243）。履歴を消す例では行も変わるため、履歴の照合を消しても、状態 SHA の不一致で拒否されて検査が通ります。:201-204 と同じように、無変更検査も失敗することを確かめてください。PID 再利用（作成時刻が違う）を受け付ける例と、pid_created のない生存 PID を拒否する例も足してください。
2. scrub391 を、todo.lock を持ったまま期限なしで実行しています（answered_wait.rs:221-228、lib.rs:1401）。scrub391 が止まると、台帳全体が使えなくなります。期限を付けるか、ロックの前に判定してロック内でパスの一致を確かめてください。
3. 担当なしの分岐（answered_wait.rs:425-430）は、scheduler.key・phase・last_job を見ていません。起動予約が残る行も通るので、key が空であることを条件にしてください。
4. 生存判定が、終了時刻が 0 であることに頼っています（answered_wait.rs:413-414）。公式文書では、実行中のプロセスの終了時刻は未定義です。作成時刻が一致したら、生存として拒否する方が安全です。

**Nit**

- transitioned_at が分単位の現地時刻です（answered_wait.rs:564）。conversation_wait.rs:1025 と同じ RFC3339 にそろえてください。
- 文書の「全行の状態SHA」（docs:34、CHECKPOINT:15）は、実際には対象行だけの SHA です（answered_wait.rs:556）。docs:28-29 の「同形 JSON を根拠にしない」は、Must 1 を直した後に正しくなります。
- 3084 が移行より先に完了すると、通常の依存解除で wait_on が消えます。すると移行できず、block のまま残ります。その場合の手順を文書に書いてください。

**確認できた点**

本人ID・質問世代・thread/message/request ID・時刻の順序は照合されています。原資料は同じ読取バイト列で SHA と内容を確かめ、重複キーも拒否します。通常の Wait/Start は変わりません。移行後に入った block は、再実行しても解除しません。失敗時は保存前に止まり、台帳を書きません。PID の照会失敗を終了と扱わず、3084 も変更しません。