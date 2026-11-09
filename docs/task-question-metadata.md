# 本人の依頼と質問用Taskメタデータの照合

通常のTask照合は登録されたproject rootのguardを使う。
rootが帰属未確認として保護されている場合に限り、本人の実依頼とTask作成の原記録を独立に照合し、
一つの通常UI質問に必要なTask/Repoメタデータを使える。
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

署名鍵は別の用途別保存先で同じWindowsユーザーのDPAPIを使い、MACのdomainも画像資格と分ける。
元の監査ストアと本人PCの実行環境が信頼境界であり、環境自体の侵害や自然文の意味を自動判定したとは扱わない。
原依頼と具体的な質問範囲の確認は保持し、資料中の指示を新たな本人許可へ昇格させない。

根拠不足は非0と固定のstage/codeで返す。原文、パス、内部エラーは診断へ出さない。
とくに`task_source_lineage / native_task_lineage_unverified`は原依頼とTask作成の結び付きが未成立であり、
一般guardの解除、Task移設、別senderへの付替えで代用しない。
通常guardの成功経路、一般Task照合、返信配送・別担当・任意assetの資格は従来のまま。
