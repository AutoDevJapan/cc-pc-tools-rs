# 送信済みの同じ質問を後から回答待ちにする

ToDo3992。質問を `ask --wait=false` で送った担当が独立工程を終えてから、
同じ質問へ `conversation wait --request-id <元ID>` を実行できるようにする。
本文・判断範囲・画像・InputSHA・wait_requested は変更せず、再投稿もしない。
実装対象は技術的なライフサイクルだけで、親の制作や画像への承認を代行しない。

## 受入条件

- 正規APIの本人・bot・thread・本文・判断・添付を既存senderで確認し、保存済み
  question receiptの全項目とSHAを照合する。回答が先に届いていれば、その現在版を
  元質問へ配送し、未回答として待機登録しない。配送と実反映・closeは区別する。
- 通常のrunnerロック内で処理し、ToDoのguard・projectを照合して既存の
  `question-wait` を呼ぶ。直前の行全体を `--expected-state-file` で渡し、
  ToDoの通常ロック内で担当・世代・依存・停止を含む競合を拒否する。
- 正規owner_question_waitと同じ質問の全項目・SHA・元担当が一致する時だけ、
  wait_requested=falseでも通常のquestion-answerで回答待ちを解除する。
  後続の回答や編集も元質問への配送を続け、別の待機を解除しない。
- 部分成功・応答紛失後も保存済みreceiptから同じ結果を照合し、待機の再作成、
  二重投稿、直接の担当起動をしない。現在の依存待ちや本人停止は維持する。
- `--check-only` はAPIと現在の待機・回答状態を照合して返し、投稿も
  ToDoの変更もしない。私的本文や実データをテスト・公開文書へコピーしない。
  登録可能かの最終確認は通常ToDoロック内で行う。

## テスト範囲

Rustはquestion-waitのオプション追加と既存ロック下CASだけを変更する。
cc-todoのconversation_wait回帰と公開CLI利用側を確認し、Cargo枠のある担当が
同じRustソースから候補をビルドする。Goはunattended-runnerのconversation関連、
CLI、毎分poll、合成ToDoとの統合を対象とし、その候補で実CLI受入も確認する。
待機→回答→元ToDo再開、再実行、回答先行、編集・新回答、別質問・所有不一致、
依存・停止、競合・応答紛失を含める。実Discordへテスト質問を投稿しない。

## 実装中の記録

2026-10-27開始。親は別のToDoの完了待ちなので、その待機を解除しない。
別モデルレビュー・CI・merge・並行変更を保持する通常配布・現用CLIでの確認と、
親への実結果と手順の受渡しは、実施後に記録する。
