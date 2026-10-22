# 待機担当がいる場合の再開情報保存

元ToDo3360、親3110、[Issue384](https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/384)。対象はcc-schedulerのcheckpoint-workspaceだけとする。
同じ作業場所を使うqueued担当が、現在の所有担当の終了を正規に待っている場合にも、
現行版は競合として拒否する。実行状態や待機理由の自由文から安全性を推定しない。

## 入出力と保存条件

入力は既存のid/job/expected_branch/branch/headを維持する。所有、元の保存branch、
実Gitルート・共通管理領域・branch・HEAD、既存guard、行全体のCASを照合する。
成功時はその行の再開branchとreceiptだけを保存する。依存、本人停止、note、worker、
元資料、Gitのファイルやrefを変更せず、起動・取消・移動を行わない。

競合確認と保存は、通常のジョブ受付と共通の.queue.lockの同じbyte範囲を保持して行う。
遅いguardとGitのroot/commonの確認はロック前に行う。最後のbranch/HEADの照合は
最新の実状態を保つためロック内に残し、他の担当が受付から作業を始めることを防ぐ。
queued担当を例外にできるのは、次の全条件を確認した場合に限る。

- 元行の現担当であるstarting/runningのsourceが生存している。
- queued担当には実行過程（codex_pid）、誕生時刻が不明または不一致の過程、
  監督配下の作業の可能性がある子過程、所有生存印、再起動予約、取消、読取不能印がない。
  生存している監督過程（pid）だけは、その誕生時刻をpid_createdと照合した場合に許す。
- 監督の子は、名前がconhost.exe（大小文字を区別しない）、誕生時刻が既知で監督以後、
  自分に現在の子や誕生不明の子がないものだけを補助過程として許す。
  それ以外の子、未知の誕生時刻、補助過程の下の実行は拒否する。
  codex_pidが生存していれば名前がconhostでも拒否する。名前だけで実行権を与えない。
- 正規queue_wait_reasonがwork_ownerで、そのowner_job_id/owner_todo_idがsourceと元行に一致する。
- cwdと保存済みwork_ownerの作業場所が両担当と元行で同じ実パスであり、repositoryとgit_commonも一致する。
- queued担当のtodo_idは未登録または同じ元行である。別ToDoや別cwdの競合を免除しない。
- 追加のwork_wait_evidenceがある場合はowner全体とcandidate_identityも現在の保存所有情報と一致する。
  矛盾した証拠を無視しない。

それ以外のqueued、starting、running、生存過程を持つ終了担当は従来どおり拒否する。
待機担当を削除・取消して保存を通す手段は追加しない。

## 読取確認と失敗

同じ入力に--checkを付け、保存前と同じ検証を読取だけで行えるようにする。
出力はeligible、固定reason、比較の真偽、件数、元番号だけとする。共有ToDoの本文、
他案件の名前・パス、job本文、認証情報は出力しない。検査は保存、配送、実反映を意味しない。
通常保存の拒否にも固定の具体的reasonを返し、匿名のInputRejectedと区別する。

## 受入条件

1. 正規の待機担当が存在しても現所有担当の保存が成功する。元行の他の値とGitの途中成果は同じ。
2. 待機証拠の欠落・矛盾、別owner/ToDo/cwd、starting/running、実行過程や不明な過程、予約を拒否する。
3. 元branch、HEAD、所有、guard、途中のnote/依存/本人停止の変更を拒否する。
4. --checkは保存せず、拒否条件と比較結果を示す。通常保存の結果は改めて読み返す。
5. 共通queue lockでジョブ受付と排他する。テストで同じbyteの競合を確認する。

Windowsの実過程を使う受入は、CREATE_NO_WINDOWで起動した有限の所有console過程と
そのconhostの誕生・親子を通常native snapshotで読み、隔離したToDo/backendへ渡して
inspectと保存を確かめる。実本番のToDo/jobを編集して正例を作るものではない。
所有過程の終了とconhostの消滅も確認する。起動flagはMicrosoftの
[Process Creation Flags](https://learn.microsoft.com/en-us/windows/win32/procthread/process-creation-flags)を参照する。

変更前の正規待機の受入をRedにし、最小実装後にGreenを記録する。
対象はcc-schedulerとその既存結合検査・doctest・Clippy。共有ポリシーは変更しない。
別モデルMaxレビューと同headの必要CIを確認して通常mergeし、現配布版の並行sourceを
保持した実行物を配布する。親のjobを偽装せず、親の正常保存は親の次の実jobが行う。
