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

## 確認記録

2026-10-27、Goの会話関連テストとraceが成功。現用ToDo CLIが新オプションを
拒否するRedに加え、Rust側のCASだけを外す負例も世代の取り違えを検出した。
正規候補によるGoのExplicitWait/ActualCanonical統合は成功（22.170秒）。
待機→回答→元ToDo再開、回答先行、追加回答・編集、所有・質問の不一致、依存・停止、
応答紛失、待機保存後のrunner index保存中断を合成データと実CLIで確認した。
統合前のRust95件とdoctestの結果・旧候補SHAは[確認担当の記録](todo-3997-cargo-validation.md)。
同担当の全体fmtで見つかった既存product_retryテストの書式は親で修正し、
変更した3ファイルのrustfmt確認に成功した。これは統合前の確認記録である。

mainとの統合後は4009がRust関連120件（conversation_wait 47、parity 38、
conversation_full_reply 11、product_retry 9、answered_wait 8、対象ライブラリ7）と
doctestを確認した。対象外のライブラリ26件は選択範囲から除外した。
統合候補のSHA256は5005c82b3995b6605a460e1a14db785c684626600339f9e3021bdfdf4a6cae2b、
Rustのビルド元はe31df774である。その後のGo修正でもRustソースは同じ候補を使い、
631ed63eで会話関連のrace/nativeテストとgo vetに成功した（95.812秒）。
nativeは実CLIのロックと状態遷移を検査し、CIの合成版ではGoの振り分けを常時検査する。

元の実質問でも正規APIの読取確認が成功し、本人回答0件と親の依存待ちを確認。
この段階で親の待機や原質問は変更していない。別モデルレビュー・CI・merge・
通常配布と現用CLIの最終確認・親への手順の受渡しは、実施後に記録する。
