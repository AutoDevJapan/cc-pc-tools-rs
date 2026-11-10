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

登録時と新規送信前に、現Taskの停止・取消・保留・完了、担当世代、元仕様参照、project/root、
送信先、原ネイティブ記録、API上のRepo/owner ID、本文/decision、画像原証拠と原バイトを確認する。
検証中の元Task変更も拒否する。署名が正しくてもこれらの確認を省略しない。
添付のsnapshotは通常bridgeの元画像receiptで結び、別のファイルや任意のコピーを代用できない。
この照合のためにproject rootのファイルを走査しない。

送信前のTaskは通常の`doing/todo`で、質問待ちを持たない必要がある。
送信後は、登録と同じrequest・本文・元decision・画像を保持する質問だけを対象とし、
保存済み質問receipt、正規送信器が再取得した質問証明、保存済み回答原文と正規APIの現本人投稿を照合する。
`user`という申告や受領ファイルの存在だけで回答を受け入れない。
正規送信器のdecision identityは対象順を正規化するため、元decisionと区別して両方を保持する。

期限は新規送信と原素材の利用を制限する。送信前の検証結果を用途別に署名し、通常送信器による
本文・画像hashの検証と質問のAPI読戻し後、実際の質問receipt・保持入力・登録を一つの署名付き
admissionへ結ぶ。送信前の記録だけでは、送信済み質問の資格にならない。
質問の作成時刻は元資格の有効期間内に限る。期限後に新しい送信資格を作り直さない。
既にadmitされた同じ質問への遅延回答・追記・編集・再配送は、この不変のadmissionと認証済み回答を使う。
原素材の再読込や再資格化は行わず、保持した元decision・本文・画像hashと質問receiptを照合する。
登録・原証拠のメタデータ・指定ネイティブ記録・Repo ID・現Task/担当/待ち・送信先の検証は続ける。
原証拠や登録の変更、admission欠落/改ざん、別質問・別本文・別画像は拒否する。
原画像ファイルの存続を、既に送った同じ画像への本人回答を受け取る条件にはしない。

admissionと質問receiptの保存後、indexを`posted`へ公開する前に中断しても、同じ質問を回復する。
残った`reserved`入力、保存済みreceipt、通常送信器のread-only API証明から候補を組み立て、
署名済みの完全な質問identityと一致させる。元登録・原入力・現Task/世代/停止/他の保留を再確認してから
indexへ公開し、既存の待機・回答経路を続ける。この回復で`recovery-ask`は呼ばない。
送信前stageや署名欠落から投稿済みを推定せず、質問やscopeの不一致も回復対象にしない。

新規送信の期限は、時間のかかる素材検証後にも確認し、通常送信器へ`--send-before`で渡す。
送信器は期限を元intentへ保持する。再試行で指定を省いても期限は消えず、別の期限へ延長できない。
request ID、表示fingerprint、本文、decision、画像のバイト列は期限metadataにより変わらない。
新しい質問を作る`question:0`の予約前とPOST直前に確認し、HTTPの待機・再試行にも残り時間を適用する。
wall-clock値の採取前に単調時計の起点を取り、残り時間をその起点に固定した期限へ変換する。
値の採取後に処理が止まっても予算を取り直さず、POST呼出し直前と各HTTP試行で同じ期限を確認する。
この期限は通常コマンドの時間予算として扱い、定期tick用の短い通信制限・429待機抑止・出力を有効にしない。
明確な429拒否は、`retry_after`が同じ残り時間に収まる場合だけ既存の有限回数で再試行する。
外側に実際の定期tick期限がある場合は、その制約と最短の残り時間を保つ。
既に開始した同じ質問の後続部分や通知は従来のreceiptへ結び、POST結果が不明な場合は既存journalからGETで照合し、再POSTしない。
期限内に作成された質問の読戻しが期限後になっても、同じ質問を待機へ進める。
実際の作成時刻が期限外、または完全なposted admissionが無い場合は、資格を推定せず未完の境界を保つ。

同じ質問待ちは拒否対象の停止と区別する。署名に固定した担当・worker世代・scheduler keyと、
待ちの`origin`・質問receipt全体が一致するときだけ、`block`からの回答配送を認める。
別の待ち、別recipient、古い世代、停止・取消・保留・改変証拠、期限切れの新規送信は引き続き拒否する。
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
仕様やreceiptの改変は拒否する。記録は最大256件で、欠けた連続性を推定で補わない。
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
scope/inputのJSON値は、キー順・文字列・配列順・scalarの型を保持して照合する。
数値は余分なfloat変換をせず、十進の係数と指数へ正規化する。`1700000000`、`1700000000.0`、
`1.7e9`は同じ数値で、文字列やnullとは異なる。64bit整数の丸め、HTML escapeの追加、
Unicode separatorの表記差を持ち込まない。元のrequest・本文・decision・署名のhash方式は変更しない。
最終sample後の変更は再準備へ延期し、その後の競合も既存launcherの登録・queue実行受付が
`todo.lock`内で再照合する。実行受付は、その版をrunningとして確定する線形化点であり、
実workerの受領・処理完了ではない。受付後の訂正は保留入力として残し、実行中の別actorへ注入しない。
物理spawnまでの時間差もこの境界の後に含む。R後の保留保存だけでは、そのattemptが訂正を受領・反映したとは扱わない。
prepared jobではTask正本の欠落・破損も受付待ちにし、元Task/所有/入力/promptの照合なしにrunningへ進めない。
正本の復旧後は通常の受付から再開する。一般manual jobへTask正本の条件を追加しない。
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
