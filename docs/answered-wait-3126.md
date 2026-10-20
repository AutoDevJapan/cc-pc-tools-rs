# 回答済みの古いConsole待機を依存待ちへ移す

元ToDo3110 / 修復ToDo3126。通常の `wait` は本人の `block` を維持し、
`start` は未完の依存を拒否する。この両方の契約を保つ。

## 明示操作

`todo answered-wait <3050|3070> --evidence-file <JSON>` は無変更の検査である。
検査が返す `state_sha256` を指定した
`--apply --expected-state-sha256 <SHA256>` だけが移行する。
一般の本人停止を解除するコマンドではなく、今回照合済みのConsoleの2件に限定する。
待ち先は既に正規 `wait` で保存された3084を含む集合と一致しなければならない。
依存の追加・削除、担当の起動・割当、画面候補の配布は行わない。

JSONのdomainは `todo-answered-dependency-wait-v1`。以下を含む。

- `todo_id`、現在の依存と一致する `wait_on`、現在のメモと一致する `expected_note`。
- `legacy_note` と `api_reply`：それぞれ原資料の `path` と `sha256`。
  前者は元の `todo list` 保存本文、後者は3110の正規API取得による質問・原回答。
- `current_instruction`：`source` は `scheduler-user-task:3126`。
  今回の本人直接依頼の該当原文 `text` と、そのUTF-8の `sha256`。
  `scope` は `{"todo_ids":[3050,3070],"dependency_id":3084,
  "retain_current_layout":true,"deploy_old_candidates":false}`。
  APIで取得した回答と、今回の直接依頼は別の出所として保存する。

原資料は同じ読取バイト列からSHAと内容を検査する。原質問世代、本人ID、元ToDo、
thread/message/request ID、判断対象、返信時刻を照合する。
3050の質問への回答を3070へ結ぶのは、この固定した2件の依頼に限る。
元の本人待ち本文は現在のメモまたは検証済み履歴に実在しなければならない。
新しい質問待ち、別の本人停止、外部条件、未知の保護、未確認の担当状態は拒否する。

共有ToDoの正規ロック内で、全行の状態SHA、依存の実在・一意性・循環、
現在のメモ、担当の終了を再確認する。競合・不一致・読取失敗は保存前に失敗し、
ToDo原本のバイト列を変えない。一般の `wait` / `start` は変更しない。

成功時は選択した行の `status` を `block` から `wait` へ変え、更新時刻と
`answered_dependency_wait_receipt` を保存する。メモ・履歴・仕様・進捗・担当履歴・
元依存・質問と本人回答・保護情報を保持する。3084と他の行は変更しない。
証拠の受領と待機移行は、元の機能修復・実反映・配布完了を意味しない。
同じ操作の再実行は無変更とし、後から追加された停止を解除しない。

## 受入条件と検査

1. 正常CLIで無変更検査→状態SHA指定→3084への `wait` を保存する。
   元の全フィールドと他の行、原資料を保持する。
2. 通常の `wait` は `block` を保ち、未完依存がある `start` は拒否する。
3. 別本人・別質問世代・別対象・原文改変・出所欠落・原履歴欠落は無変更で拒否する。
4. 行・メモ・依存・担当が競合した場合、未知の保護・新しい質問・別停止・
   外部条件がある場合は無変更で拒否する。
5. 3084完了前は起動不可。通常の依存解除を使って再開でき、他の本人待ちは保つ。
6. 再実行は無変更であり、新しい `block` / `done` を解除しない。

実行対象はcc-todoと、共有待機を利用するcc-scheduler・cc-jobs・localai-web-rsの
関連検査、および同じ範囲のdoctest。共有設定は変更しない。
Red/Green、固定base/headの別モデルmaxレビュー、必要CI、マージ、配布source/hash、
正規CLIの実移行とreadbackを作業記録へ残す。
