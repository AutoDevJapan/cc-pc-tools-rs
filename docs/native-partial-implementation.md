# デスクトップ担当の部分成果を保存する

デスクトップ担当がCLIジョブを持たない場合も、元の質問と同じTaskへ、確認できた
成果の証拠を保存できる。CLIのjob IDや別担当Taskを作って既存の
`implementation-record`を通す必要はない。

この入口が保存するのは**部分成果**で、`acceptance:pending`、
`application_inferred:false`、`question_closed_get_verified:false`を返す。
質問の閉鎖、製品完成、GUIの受入、停止解除には使えない。

## 入力の確認と記録

```text
unattended-runner.exe conversation check-native-implementation --input-file <入力JSON>
unattended-runner.exe conversation native-implementation-record --input-file <同じ入力JSON>
```

入力ファイルは現在の通常の保護判定で読み取れる原ファイルとする。
拒否されたファイルをコピーしたり、登録rootや分類を変更したりして通さない。
確認コマンドは既存state・cache・質問・Taskを変更せず、記録コマンドは通常の
runnerロックと正規ToDo writerを使う。どちらも配備済み設定・元の保存先を使い、
呼出し元の任意configやcheckerは受理しない。

JSONは次のフィールドだけを持つ。重複・未知キー、空欄、曖昧なselectorを拒否する。

| フィールド | 内容 |
| --- | --- |
| `domain`, `kind` | `discord-native-partial-implementation-v1`, `native_partial_implementation` |
| `request_id`, `message_id`, `origin_todo_id`, `project` | 元の質問、現在の回答、元Task、登録project |
| `source_receipt_sha256`, `reply_sha256` | 正規APIで検証した現在のcanonical sourceとreplyのSHA256 |
| `revision` | 証拠が対象とする40桁の小文字commit |
| `parent_thread_id`, `child_thread_id` | 元Taskへ署名済みで結ばれたnative管理親と実子 |
| `assignment` | `turn_id`, `item_id`, `prompt_sha256`。親から実子への完了した`sendInput`を選ぶ |
| `verification_commands` | 1〜16件。各`turn_id`, `item_id`, `command_sha256`, `output_sha256`が実子の成功コマンドを選ぶ |
| `artifact` | `path`, `sha256`。登録root内の、通常guardが許可する原JSON成果物 |
| `evidence` | `completion`, `execution`, `origin_saved`, `identity`, `applied_at`の5文字列 |

割当は元replyに加え、元requestそのもの、または元Taskと質問の正確なIDを含み、
現在のsourceと回答本文またはreply SHAにも一致する必要がある。
成功コマンドは実際のcommand/cwd/output/終了コード・時刻と照合する。
少なくとも一つは、Windowsの正規PowerShellから実行した次のliteralな読取とし、
元artifactの完全なパスと`Path`/`Hash`のJSON出力を同bytesへ結び付ける。
変数、echo、コメント、追加statement、別pathの同名ファイルでは代用しない。

```powershell
Get-FileHash -LiteralPath '<原artifactの絶対パス>' -Algorithm SHA256 | Select-Object Path,Hash | ConvertTo-Json -Compress
```

artifactの`owner_reply`または`ownerReply`と`revision`または`main`の文字列を
元reply/revisionと照合し、併記された別名の不一致も拒否する。
コマンドの成功だけで製品の完成とはしない。
`evidence`には未達の受入条件も記す。

## 資格と更新の境界

署名済みの原native質問admission、元Taskの作成記録・所有・世代・root、
現在の質問と本人reply/source、正規app-serverの親子・割当・成功command、
通常guardで読める原artifactを毎回検証する。原質問の署名だけで一般ファイルの
保護を解除しない。停止や待ちは成果記録によって解除されず、所有tupleや仕様の
変更があれば新しい正規の資格を必要とする。

入力を用途別のimmutableな名前で保存し、Rust writerが`todo.lock`内で元rowの
snapshotと登録rootをCAS照合する。固定配備runnerの
`verify-native-implementation`を呼び、同じ入力・snapshot・source・reply・native
証拠を再検証する。この内部verifierはrunner/todoロックを取り直さず、書き込みや
通常schedulerの起動も行わない。

追記先は元Taskの`owner_native_partial_implementation_records`だけ。
従来の完全な実反映票とは別に保ち、同版の部分成果がある間は従来のapplied票へ
fallbackして質問を閉じない。その後に同じ版の正規`implementation-record`で完全な
実反映が検証された場合は、その正規票で閉鎖を判定する。
他row、仕様、メモ、停止、依存、世代、jobsは変更しない。
同じ入力の再試行・応答消失後の読戻しは同じreceiptを返す。同じreply/sourceに
異なる入力を上書きせず、競合は元データを保持して返す。

固定verifierの起動失敗・期限・不完全出力・証拠不一致では追記しない。
保存済みreceiptも読戻しのたびに再検証し、現在の証拠と結べない記録を成功としない。
