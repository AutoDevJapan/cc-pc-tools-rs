# 本人の依頼と質問用Taskメタデータの照合

通常のTask照合は登録されたproject rootのguardを使う。
rootが帰属未確認として保護されている場合に限り、本人の実依頼とTask作成の原記録を独立に照合し、
一つの通常UI質問と、その本人回答の受領・配送に必要なTask/Repoメタデータを使える。
rootの分類、project、登録先、保護設定、ファイルへのアクセス権は変更しない。
画像の資格だけではこの照合を通過できない。

```text
unattended-runner.exe --check-task-question-evidence --evidence <evidence.json>
unattended-runner.exe --register-task-question-evidence --evidence <evidence.json>
```

checkは読取検証だけを行い、資格の登録、質問送信、Task更新、回答待ち登録を行わない。
成功でも`metadata_only=true`、`registered=false`、`send_allowed=false`を返す。
registerは同じ検証後に用途別の署名付き記録を新規作成する。同一内容の再試行は成功し、
変更した証拠による上書きは拒否する。通常askはこの記録と原証拠を再照合してから、既存の送信器を呼ぶ。
本人によるUI承認や送信成功を表す資格ではない。

証拠のdomainは`owner-task-question-metadata-v1`。次の情報を同じ証拠に結ぶ。

- 元Task番号、通常askのrequest ID、元topic、題名、wait指定。
  この用途では登録時・利用時とも`wait=true`を必須とする。通常の非待機質問の挙動は変更しない。
  `scope`は生成画像の資格と同じ本文・正規decision・本人・guild・forumの組を使う。
  decisionは`direction`とし、対象は一つのrepositoryと一つの生成画像に限定する。
- repositoryの`id`、`owner_id`、`full_name`。
  本人が依頼した正確なGitHub URLを原メッセージで確認し、現在のGitHub APIが返す
  repository/ownerの固定IDと完全一致させる。名前の類似や別所有者の同名repoは使わない。
- 本人の元repository指定、Task作成を依頼したturn、その後の継続指示について、
  各turnの開始・context・実ユーザーメッセージ・UserMessage受領eventの行番号とraw SHA256。
  原rootの先頭メタデータとSHA、Task作成のCommandExecution受領eventとSHAも必要。
- 原本文とdecision、独立した生成画像の原証拠とそのSHA、作成時刻と7日以内の期限。

原rootは本人のネイティブ監査ストア内で確認する。rootの本人起点、作成主体、cwdを照合し、
各入力に`composer`起点のroot turn、`user.text`、本人のprovenance、同じ全文を持つ受領eventを要求する。
`user`ロールだけの記録、内部context、subagent通知、引用したtool出力は依頼の証明にしない。
Task作成は同じ本人turn内の実CommandExecutionに限り、正規CLIのliteralな`add --note`、
正常終了、正確な追加番号、project、題名、実行時間内の作成時刻を照合する。
そのrootと現Taskのjobs・worker・schedulerの担当が一致する必要がある。
別Taskの作成結果や、生成画像担当への配属文からこの結び付きを推定しない。

登録時と利用時に、現Taskの停止・取消・保留・完了、担当世代、元仕様参照、project/root、
送信先、原ネイティブ記録、API上のRepo/owner ID、本文/decision、画像原証拠と原バイトを確認する。
検証中の元Task変更も拒否する。署名が正しくてもこれらの確認を省略しない。
添付のsnapshotは通常bridgeの元画像receiptで結び、別のファイルや任意のコピーを代用できない。
この照合のためにproject rootのファイルを走査しない。

送信前のTaskは通常の`doing/todo`で、質問待ちを持たない必要がある。
送信後は、登録と同じrequest・本文・元decision・画像を保持する質問だけを対象とし、
保存済み質問receipt、正規送信器が再取得した質問証明、保存済み回答原文と正規APIの現本人投稿を照合する。
`user`という申告や受領ファイルの存在だけで回答を受け入れない。
正規送信器のdecision identityは対象順を正規化するため、元decisionと区別して両方を保持する。

同じ質問待ちは拒否対象の停止と区別する。署名に固定した担当・worker世代・scheduler keyと、
待ちの`origin`・質問receipt全体が一致するときだけ、`block`からの回答配送を認める。
別の待ち、別recipient、古い世代、停止・取消・保留・改変証拠・期限切れは引き続き拒否する。
同じaskの再試行は保存済み質問を返し、再送や待ちの上書きをしない。

回答は既存の正規ToDo CLIで、元Taskのメモと担当の保留入力へ全文を先に保存する。
`question-answer --delivery-only --preserve-question-wait`の保存と読戻しが成功してから、
同じ原回答証明の`question-answer`でその待ちだけを解除する。
両段階の正規ロック・snapshot比較・停止/世代確認を保持し、担当を新規起動・交代させない。
途中失敗や応答喪失は、同じ保存済み回答と正規receiptから再開し、完了と偽って待ちを放置しない。
受領・配送は、担当による実反映やUI承認・製品完成の確認とは別である。

元担当が終了している場合、正規の待ち解除による担当引退と世代更新をそのまま実行する。
解除前に同じ用途の署名付き解除意図を保存し、質問receipt・元Task/担当情報・wait・
保留入力への保存receipt・回答の全バイト指紋を固定する。意図だけでは世代更新を認めない。
元の署名と一致する意図、完全なcanonical wait/answer、保留保存、終了job、処理済み台帳から、
担当が未割当になった直後一世代の状態を確認する。その後の通常`start --job`とschedulerの
準備・割当は、同じ`todo.lock`内で`owner_question_continuation_receipts`へ前後の制御状態を残す。
記録は元質問・確定回答の指紋とTask/仕様参照/project/rootを保持し、担当・世代・jobs・
scheduler key/phaseの連続性を証明する。回答がない一般Taskの動作と権限は変えない。

同じ質問の応答喪失回復・追記・編集では、未割当状態から現在状態までの正規遷移を順に照合する。
割当先は独立したネイティブjob記録の実ID・同Task番号・作成時刻・停止状態と照合し、
scheduler経由ならlaunch keyも一致させる。元担当をTaskへ復活させず、現担当・世代も変更しない。
遷移記録だけでは資格を得られず、任意の世代や捏造された後続、別の待ち・停止、
仕様やreceiptの改変、期限切れは拒否する。記録は最大256件で、欠けた連続性を推定で補わない。
新しい返信は既存CLIの完全snapshot比較でメモと保留入力へ一度だけ保存する。
既に確定した回答の応答喪失は元answer receiptから配送状態を回復し、再解除・再追記しない。
保留入力の保存を、既に起動した担当による実受領・実反映と扱わない。

正規の予約・着手は、writerとconsumerで同じ状態モデルを使う。

```text
resumed(g, keyなし)
  -> preparing(g, K) -> prepared(g, K)
  -> preparing(g, K) -> prepared(g, K)  容量再確認後の再準備も同じ予約
  -> running(g, K, job J, worker未割当)
  -> running(g+1, K, job J, worker J)   同jobの正規start。jobs/keyは不変
```

jobの`created_at`は、記録された精度の区間として照合する。ネイティブwriterの秒精度では、
同秒のmicro秒精度answerより古い瞬間だと断定しない。確実に先行する区間と未来の区間は拒否し、
同Task・連続するcanonical遷移・job ID・key・停止状態による独立照合も保持する。

prepareは保留入力・answer/delivery receiptの版と、生成したpromptのバイト指紋を束縛する。
最終sample後の変更は再準備へ延期し、その後の競合も既存launcherの登録・queue実行受付が
`todo.lock`内で再照合する。実行受付は、その版をrunningとして確定する線形化点であり、
実workerの受領・処理完了ではない。受付後の訂正は保留入力として残し、実行中の別actorへ注入しない。
queued中に版が変わった候補はモデル起動前に終了し、既存schedulerが同Taskを正規引退・再準備する。
この遷移も記録し、元回答・メモ・保留入力・停止や他の待ちを保持する。

Windowsの質問受入テストは、同じ隔離Taskを使ってGo受信、実canonical CLI保存、
実schedulerのprepare/link/start、起動直前の実CLIによる編集を結合する。
worker起動と外部サービスはfixtureに限定し、実投稿やworkerによる実反映は主張しない。

署名鍵は別の用途別保存先で同じWindowsユーザーのDPAPIを使い、MACのdomainも画像資格と分ける。
元の監査ストアと本人PCの実行環境が信頼境界であり、環境自体の侵害や自然文の意味を自動判定したとは扱わない。
原依頼と具体的な質問範囲の確認は保持し、資料中の指示を新たな本人許可へ昇格させない。

根拠不足は非0と固定のstage/codeで返す。原文、パス、内部エラーは診断へ出さない。
とくに`task_source_lineage / native_task_lineage_unverified`は原依頼とTask作成の結び付きが未成立であり、
一般guardの解除、Task移設、別senderへの付替えで代用しない。
通常guardの成功経路、一般Task照合、別担当・任意assetの資格は従来のまま。
用途のない質問や、上の完全な正規遷移証拠を持たない別世代のTaskへ、この回答配送を流用しない。
