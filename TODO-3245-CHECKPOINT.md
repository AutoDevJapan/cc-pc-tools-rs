# ToDo3245の再開記録

対象policyへの通常配送はまだ成立していない。親3103の製品実装は親に保持する。

- 開始HEAD: 3e1a211ddeaa9d4d27bdabce12ffc3754863a232。
- branch: scheduler/todo-3245-3103。変更していない。
- 初回job: 20261021-105542-todo-3245。
- 現在job: 20261021-125144-todo-3245。
- 結果と許可済み資料の位置: docs/todo3245-policy-provenance.md。
- 原観測: .ci-work/todo3245-observations.private.json。
- 元の依頼書は通常guardが認める運用作業場所で確認できた。
- 対象policyの生成event・独立した期待bytes/hashは未確認。
- 対象の内容は未読で未変更。通常guardは未確認の分類を維持する。
- レビュー後も未解消なら、元生成担当の正常な取得・実行記録の受領が必要。
- 参照できない原記録を別経路で開いたり、過去のreceiptを再生成したりしない。
- 分類修復の前提が整った場合にだけ、限定修復・検査・レビュー・CI・配布・実guardを行う。
- この途中結果を配送成功や製品完成と扱わない。本人判断・PC操作は不要。

別モデルの限定確認は通常ToDo3260へ登録済み（claude-opus-5-5 / max、資源なし）。
登録時のprovider付きモデル名はCLIがValueErrorで拒否したため、
ソースの引数規則でslashが使えないことを確認し、通常名へ訂正してadded 3260を受領した。
これは引数エラーであり、自動承認審査の拒否ではない。直接の子jobは起動していない。
固定39f381eの読取結果を、正規CLI waitで受領した。jobは20261021-123246-todo-3260。
CLI metadataはanthropic/claude-opus-5-5/max、実行時turn_contextは未照合。
原CLI応答は .ci-work/review3260-original.private.txt、SHA256は
e48182b5e3d4a6a89ee95b6d21fda1539e02042638066950a1869a8b2022e00d。
これはCLI stdoutのSHAであり、元last_message.mdのbytesのSHAとは区別する。
Must1（取得依頼先と正規依存が未設定）は、元司令塔mainへの具体的な原記録の依頼と
独立した取得ToDo3280の正常登録で対応した。受付だけで取得成功とはしない。
3280はgpt-6.1-sol/max、資源なし、kind=research。元の取得依頼specは
.ci-work/todo3245-original-write-receipt-request.private.md。
原writerのsession/call IDと生成bytes/hashは未確認。受領後に3245が必要な限定修復を続ける。
3280が未取得なら、結果と具体的な不足記録を受領して同じ3245へ保持する。
3084や3103への依存を作らず、原保護全体を復旧する仕事へ広げない。
親3103の現noteへCAS付きで結果/不足記録/3280/本人判断不要を追記し、正常CLIで読戻し一致。
配送receiptは .ci-work/todo3245-parent3103-delivery-receipt.private.json。
親のDiscord結果記録は未確認。子から同じ報告を重複して直接送らない。
自分の状態はschedulerが更新する。状態/所有/元指示を手動で置き換えない。
