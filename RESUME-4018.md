# ToDo4018 再開記録

目的: 既存hostの受領済み入力を一件ずつ再取得・比較する一般の読取API/CLI。
専用branch: scheduler/todo-4018-api-3467。ブランチ変更なし。

- 実remote/APIでAutoDevJapan/cc-pc-tools-rs、ID1406818713/publicを確認。
- cc-jobs/cc-scheduler/cc-todoのsrcを正規guardで確認し、保護対象外。
- 既存のdispatch-receiptは起動応答喪失の回収用で、この読取機能とは別。
- 並行PR557はschedulerのメモ保存、PR562は回答待ち。これらのwriterを変更しない。
- 仕様はdocs/received-input.md。実照会の私的結果は.ci-work/4018へ保持し、Gitへ追加しない。
- 一時領域の所有ID: 59b412c10969ccf5f5efe3fc37e63f35。safe-deleteのplan/finishで片付ける。

未完了: 合成テストを先に作成しRed確認、実装、対象テスト/doctest/Clippy、固定候補の
別モデルレビュー（共有ToDo --specで依頼）、CI、マージ、正式CLI配布、実照会、親への結果。
自分のToDo状態はschedulerが管理する。レビュー待ちで終了する場合はwait --onを記録する。
画像の元資料/監査/native/evidenceは開かない。新しい本人質問・画像送信・UI変更はしない。
