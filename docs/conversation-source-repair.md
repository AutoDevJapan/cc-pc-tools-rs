# 終了済み一般受付と質問回答の競合を1件だけ復旧する（ToDo3853）

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/513

## 契約

`conversation source-repair --request-id ID --message-id ID --actor-todo-id N
--intake-todo-id N --source-sha256 SHA --body-sha256 SHA --revision HEAD`
は通常runner lease内の有限な保守入口とする。自動探索・全件移管は行わない。
通常captureのforeign-association拒否と既存source-linkの契約は保持する。

正規senderの現在の質問、版、decision、元ToDoと、Discord APIから新たに取得した
本人投稿のID/thread/author/作成/編集/本文hash/参照/添付を独立に照合する。
`source-sha256` は通常受付の投稿版（ordinaryVersion）のhashとする。新入口は
保護未確認の一般受付原ファイルを開かず、存在・通常ファイル・size/更新日時だけを
保存する。登録済み受付の投稿版と実job・仕様・prompt・現在APIを独立に照合する。
原文のコピーで新しい資格を作らない。既存source-linkのSHA契約は変更しない。

保守担当の登録・実job・仕様・実行prompt・作業場所を確認する。元製品の登録先と
保守担当の登録先は別でよいが、それぞれ通常の保護判定を通す。一般受付は保守基盤
の登録先に属し、正常終了した実jobと起動キーを持ち、元ToDoだけへの依存待ちで
なければならない。実行中・予約中・所有不明の受付は拒否する。

一般受付、原ファイル、一般受付ToDoの仕様・履歴・wait・jobを保つ。専用captureと
不変のauditを追加し、canonicalが永続化された後に状態を公開する。異なるcanonical
は置換しない。元製品には既存question-answer/全文instructionの配送専用経路を使う。
`question-answer --delivery-only --preserve-question-wait` は元の質問待ちを保ち、
担当が終了済みでもjob・世代・停止・依存を変更せず、作業を実行可能にしない。
受領記録に`preserved_wait=true`を保持する。同版の再呼出しは配送を重複させない。
保存・配送・実セッションの読取・実反映・製品完成は別の事実とする。
クラッシュ後に保守担当のjobが変わっても、同じToDoの新しい正規jobを独立に検証し、
元の関連記録のjobが履歴にあり終了済みの場合だけ再開する。最初の関連記録は書き換えず、
今回のcallerと最初の関連付け担当は別のmetadataとして返す。

## 受入条件

- 合成投稿で通常captureの拒否と新しい有限入口のRed/Greenを確認する。
- 別受付、別質問、owner/thread/本文/編集の違い、実行中/予約中の一般受付、Source
  不成立、canonical衝突では元データとToDoを変更しない。
- 本文全文、message type、添付メタデータを保ち、メディア取得は既存経路へ任せる。
- canonical/audit/state/配送の途中失敗と応答紛失後に同じ入力で再開できる。
- 再起動後のsource-verifyと既に配送済みの再呼出しを確認する。
- 本文・編集時刻が同じでも添付metadataが変われば、通常巡回でも元captureの再利用を拒否する。
- 無関係なforeign-association拒否を弱めず、製品担当を起動しない。
- 元担当が作業中でも終了済みでも、元の待機・担当・世代を保ち、再起動しない。

## 実行中に変わった元担当の状態

実データの変更前に読み返したところ、元2913のjobは00:39にsucceededで終了していた。
既存の通常answerは終了済みownerを退役させるため、そのまま実行していない。
隔離した実ToDo CLIで終了済みownerが退役するRedを確認し、上の配送専用モードを
追加した。Cargoを割り当てた独立した確認担当でRustの関連テストと配布用ビルド、
新しい実ToDo CLIを使ったGoの隔離テストを確認する。原質問の待機が残っても、
本人未回答として質問を再送しない。実セッションの読取と実反映は別に追う。

## 今回の実対象

request `normal-ask-841de42d0cb0c1a0bf2345d0e22f7a092097a055260a9d8c743b1f5a3b7e8d6f`、
message `1563926324979175525`、元2913、一般受付3852、保守担当3853に限定する。
実対象の本文・秘密・添付はこの文書や公開PRへ出さない。
