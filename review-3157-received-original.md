状態: 完了

レビューだけが完了し、実装や本番移行はまだです。固定 base c00289ab6a45f456d1b86c6ce1629758b8da5f46 と固定 head 639d9b358ac6264d70432b47642b2167d15ec151 の差分を git から読みました。前回の Must 2件は直っていて、新しい Must はありません。行番号は固定 head のもので、tests は cc-todo/tests/answered_wait.rs です。

**Must（新規なし）：前回2件の確認**

- Must1（別の停止を解除しうる）：本番では、メモの SHA（answered_wait.rs:245）と対象行全体の状態 SHA（:201-229, :568-570）を固定しました。後から block が入ると行が変わるので、無変更で拒否されます。3084 が先に完了すると wait_on が消えるので（todo_wait.rs:168）、この場合も拒否されます。
- Must2（指示が担当に届かない）：apply で既存の owner_instruction の後ろに追記します（answered_wait.rs:609-639）。依存解除は job・key・selection を消しますが、指示は残します（todo_wait.rs:191-198）。prepare は新しい起動鍵で prompt を作り、指示を入れます（engine.rs:1157, :1219-1224）。tests:330-368 と parity.rs:4912 の新テストもこの経路を確かめています。

前回の Suggestion と Nit も反映されています。対象は次のとおりです。

- 無変更検査での拒否
- PID 再利用の例
- guard の10秒期限と子孫の停止
- 起動予約の拒否
- 終了時刻に頼らない照合
- 時刻の書式と文書

**Suggestion**

1. 元の質問文と判断の対象が担当に届きません（answered_wait.rs:610-623）。届くのは回答本文・ID・資料のパスだけです。資料は別の作業場所の .ci-work にあり（CHECKPOINT:6）、片付けで消えるおそれがあります。本人方針では、回答済みの判断を担当へ返すときに元質問本文も渡し、a/b は元質問の意味で読みます。また 3070 には、3050 の質問への回答だという説明がありません。
   修正：api["original_question"] と decision_identity（todo 3050・direction・console-first-view）を指示に直接入れてください。他の判断の承認ではないことも書いてください。tests:350-354 には元質問文の確認を足してください。
2. updated の書式を UTC の RFC3339 に変えています（answered_wait.rs:627, :638）。前回レビューとフィクスチャ（tests:57-58）を見ると、ほかの経路は「2026-10-20 23:59」形式です。前回の Nit は領収時刻だけが対象でした。updated の利用側は読んでいないので、影響は確かめていません。
   修正：updated は crate::now() に戻し、RFC3339 は transitioned_at だけにしてください。

**Nit**

- tests:371-380 は key と phase を同時に変えるため、どちらか一方の判定を消しても検査が通ります。key だけ・phase だけ・last_job だけを変える3例に分けてください。
- CHECKPOINT:32 の「残り」に「変更後の関連検査」が残っていますが、30-31行では済んでいます。

**読んだ範囲と未読**

次のものを読みました。

- 差分6ファイルの全行
- head の answered_wait.rs 全体
- tests の1-502行
- engine.rs の prepare（1101-1400行）
- instructions.rs と todo_wait.rs
- cc-ask の run_hidden_json・run_hidden_captured と process_guard.rs

次のものは読んでいません。

- lib.rs（crate::now とロック）
- conversation_wait.rs と tick_runtime.rs
- cc-jobs が pid_created を書き込む部分
- parity.rs の Fixture
- 実台帳と原資料

**実行した検査**

実行したのは git の読み取りだけで、cargo・テスト・CLI は実行していません。そのため、226件成功などの結果と実物での無変更検査の結果は確かめていません。