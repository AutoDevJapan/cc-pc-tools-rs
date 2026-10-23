# 方向回答の仕様記録と質問終了（Issue325）

方向を仕様へ保存した担当と、機能を実装する元担当を分ける。方向の記録は
機能実装・UI画像の承認・Prd反映・元ToDo全体の完了の証拠にならない。

## 入力と範囲

正常な `conversation direction-record` は元質問request ID、現在の返信message ID、
確認したsource SHA、実担当ToDo、ソースrevisionを受け取る。jobは実行環境の
`CC_JOB_ID` を用い、任意の他jobを指定する引数は設けない。正常な質問indexと
Discord APIから質問世代・設定済み本人・返信の原文と編集版を照合する。
既存の方向の質問だけを扱い、新規送信やメンションはしない。

実担当の保存済み仕様と、そのjobの実際の起動promptには同じrequest・質問・返信の
識別子がすべて明示されていることを求める。同projectという理由だけで別担当を
採用しない。実jobのToDo所属、現在の担当割当、worktree、本人停止も照合する。
元ToDoとは異なる実担当ToDoを使う。
保護判定は元作業と実担当の登録rootへ行い、そのproject/rootと両行のsnapshotを
Rust CLIが同じToDo lock内で再検証する。未知の保護範囲は保留する。

## 保存と読戻し

Rustの `direction-record` は元のnoteを保持して、元質問と本人回答の原文・出所・
日時・編集版・取得版SHAを、方向だけの仕様追記として保存する。元のspecファイルは
変更しない。これは元ToDoのnoteに保存する仕様追記であり、配送用の参照だけとは
区別する。原文と担当の説明を混ぜず、機能未完了・画像承認ではない旨を付ける。

同じ原子的な保存で、元行に `owner_direction_records` を作る。記録にはactor、origin、
destination、scope、仕様追記の本文とbytesのhashを保持する。書込み直後は
`--verify-only --verify-note` で記録とnoteの実内容を照合する。
行の状態・job・依存・停止・進み具合・元specは変更しない。
応答を失った場合は既存記録を読戻し、追記や履歴を重複作成しない。
`note_after_sha256` は実書込み時のnote全体の監査用hashであり、後日の読戻し条件ではない。
直後のnote読戻しが競合で失敗した場合も、再実行は既存の永続記録を確認し、追記し直さない。

行全体の一致は書込み時のCAS条件とし、保存済みの事実を通常の担当交代・spec更新・
依存解消・jobの終了や後日のキャンセルで無効にしない。その後の読戻しは
`owner_direction_records` の元入力・仕様追記の本文/hash、保存した両project/root、
実jobのToDo所属・cwdと元promptのhashを照合する。通常のjob終了、block/start、doneや
editで現在のnoteが置き換わっても、原子的に保存した仕様追記と実担当の事実を保つ。
再開時に古いnoteを復元しない。保存元specのhashは記録時の証拠として保持する。

最初にその回答版の質問を閉じる時は `--check-holds` も付け、元作業の新しい停止
（blockへの変更、停止/判断待ち条件の追加変更）や依存先の追加があれば保留する。
停止の解除や既存依存の解消後は同じ記録で再開する。work_owner_wait_onは数値を保つ。
既存blockはresume_generation・scheduler.question・停止理由の本文hashで識別し、
正常な方向記録の追記だけを本文hashから除く。同じ元作業への2件目の記録は新blockではない。

## 終了の検証

通常の機能applied票の元担当job条件は保持する。方向の票は別kindとし、
実際に保存された元ToDoの方向記録を再照合した場合だけ質問終了に使う。
方向の票をapplied JSONへコピーしても通常の機能反映として受け付けない。
fresh API、全返信、現在の返信/source SHA、終了前後の再確認、archive PATCHの
応答喪失からのGETによる再開を維持する。
既にarchive GETを確認した同一proof・同一受領票集合では、後続作業の停止を
過去の質問終了の失敗へ変更しない。API、返信編集版、source、方向記録、project/rootは
引き続き再照合する。新規/編集された返信や受領票の変更は初回の終了検証として扱う。
終了の予約phaseが一時的にpendingになっても、過去のGET確認日時と同一VerifiedScopeを
使って既確認の版を識別する。既確認の方向票の再照合でコマンド等が一時失敗した時は
観測エラーを返してverifiedを保ち、記録の変化だったと推定しない。

GoとRust実行物を使う統合検査の実行根拠は、この担当が最新Rust binaryを指定した
Windowsでの実検査である。現行CIはこのbinary指定を渡さないため、CI成功だけでは
その統合検査を実行したと主張しない。CI結果とこの実受入結果を別に保持する。

進捗は既存の別の無通知投稿へ「仕様へ記録、機能は未完了」と報告する。
質問本文と本人回答は編集・削除しない。元ToDoや依存先をdoneにしない。

## 受入条件

- 正式な別担当が原文を仕様追記し、正常読戻しと質問のarchive GETを確認できる。
- 元担当の偽装、foreign job/project、保存仕様やpromptに対象のないjob、未知scopeを拒否する。
- guard後のproject/root変更、同noteでの新block/依存/担当変更はCASで拒否し、元行を保つ。
- 古い返信、編集後の返信、source SHA相違、未保存の仕様、配送だけでは終了しない。
- 機能appliedや画像承認へ昇格せず、元の停止・依存・未完コード/Prdを保持する。
- 送信応答を失っても二重追記せず、再開で実保存と終了GETを確認できる。

Red/Green、実際のmodel/effort、読取範囲、CI、配布source/hash、対象2件の実操作は
担当の再開記録とPRへ追記する。登録・検査fixtureの成功を実回答の記録済みと扱わない。
