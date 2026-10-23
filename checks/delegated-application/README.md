# 別担当による回答反映の独立チェック

検査対象は `bf5fa8d8a0ba8a782eac5c13c04a28dc4a318719` の既存終了処理。
これは修復の実装や配布ではない。独立checkの指定に従い、本体のソースを変更せず、
PRの作成・マージ・実行ファイルの更新・実質問の終了は行っていない。
実際の質問、本人返信、運用上の識別子、保護記録はこの資料に含めない。

## 確認した不足

1. **Must: 別担当の実反映を正直に記録する通常入口がない。**
   `conversation_close.go` の `conversationReplySourceMessage` は
   `conversation_reply_saved` または元ToDo未割当で原証拠の読取り前に戻る。
   `conversationCloseReply` は反映票のToDoとjobを元ToDoだけに限定する。
   架空の別担当を通常のToDo形状で登録しても、その担当の反映票は拒否された。
   元作業の取消を保持した配送参照、実担当、現在返信版、原証拠の照合を、
   元担当の再開を伴わない通常入口で結ぶ必要がある。

2. **Must: 新しい入口では現在の実job所有を実記録で検証する必要がある。**
   現在の実装は反映票のjob文字列が元ToDoのjobsにあることだけを確認する。
   fixtureで対応するjob記録を一切作らなくても、元ToDoの古いjob名を指定した
   旧形式票は受理され、PATCH後のGETを経て終了した。
   この観測は旧経路の検証範囲を示すもので、実データへの操作や外部からの攻撃の実証ではない。
   別担当を許可する修正をToDo番号の一致条件の削除だけで済ませてはいけない。
   実担当の登録・project/root・現在job・元依頼への配送参照を実記録から照合する。

`conversation_cli.go` の通常コマンド一覧も確認した。
既存の `direction-record` は仕様の記録であり、機能の実反映完了を示すものではない。
それを新しい実反映票の代用にしたり、元のa/bの意味を変更したりしない。
旧形式票の手書き、jobの偽装追加、個別ToDoやファイルのallowlistは修復にならない。

## テスト結果

`characterization_test.go` は本体とは別のフォルダーに置き、Go overlayで検査時だけ
`unattended-runner/independent_delegated_application_test.go` として読み込む。
このフォルダーはGo moduleではなく、通常の製品テストへ意図的なRedを追加していない。
タスク42・43、job、反映票、HTTPサーバーはすべて架空のfixtureである。

| 対象 | 結果 | 分かること |
| --- | --- | --- |
| 現在動作4件と関連する既存テスト | `-race`で親テスト30件成功、subtest56件成功 | 既存終了処理と今回の観測を再現できた |
| 正直な別担当の票を受理する条件 | Red、反映票エラー | 現在のvalidatorは別担当に未対応 |
| 実jobを持たない古いjob名の票を拒否する条件 | Red、終了してしまう | 現在のvalidatorは実job記録を確認しない |
| 取消済み元作業の保持 | fixtureのToDoファイルをバイト比較し一致 | 取消・待ち・依存・元jobsはテストで変更していない |
| saved段階に原証拠が存在する対照 | pending、原証拠SHAは返らない | inspectionの空の原証拠一覧だけではファイル不存在を証明できない |

既存の成功テストには、全現在返信の実反映、同本文の編集、別本人/別質問、
元decisionの変更、source/反映票の不一致、PATCH前後の変更、応答消失後の再実行、
GETによる終了確認、手動再オープン、保存失敗時の保持が含まれる。
関連テストのGo package実行時間は3.845秒、コマンド全体は8.226秒だった。
意図したRedは2件とも失敗し、Go package実行時間は0.082秒だった。
ビルド失敗や既存テストの失敗を成功扱いにしていない。

再現時は、次の2つの絶対パスを対応させたGo overlay JSONを作る。
左は本体フォルダー直下の架空のtestファイル、右はこのフォルダーのtestファイル。

```json
{"Replace":{"<checkout>/unattended-runner/independent_delegated_application_test.go":"<checkout>/checks/delegated-application/characterization_test.go"}}
```

`unattended-runner` を作業場所にして実行する。

```text
go test -overlay <overlay.json> -race -count=1 -v -run "^(TestIndependentCheckCurrent|TestConversationClose|TestConversationRepliesInspection|TestConversationRepliesOutput|TestConversationRepliesReadOnly)" .
go test -overlay <overlay.json> -count=1 -v -run "^TestIndependentCheckAcceptanceRed" .
```

2つ目はこの固定ソースでexit 1になることを確認した、修復前の再現である。
旧validator形状の票を使った限定的なprobeであり、新しい書込みprotocolの
正規入口を通す受入テストや、そのGreen結果の代わりにはならない。

## 修復後に必要な受入

- 通常入口で、元依頼・現在返信版・canonical source・実担当と実job・revision・
  実検証を結んだ変更不能の記録を作り、同じ入口から読み返す。
- 原文、日時、本人、質問、編集版、原証拠SHA、actorの所有が違えば拒否する。
  元sourceが無い場合や相違する場合は別世代の正規再取得を保存し、原SHAを上書きしない。
- 元作業の取消・停止・依存・質問待ち・a/bの意味・jobsを保持する。
  受領、委任、ToDo完了を実反映と見なさない。
- 実説明を固定本文で送った担当が、そのAPI読戻し後に実反映とcloseを実行する。
  全現在返信を照合し、再実行・応答消失・PATCH後のGETを確認する。
- 別モデルmaxレビュー、必要CI、通常merge、最新main/source閉包・並行writer・
  現物hash/ACL・共通lockを照合した必要exeの更新、通常入口での実受入を行う。

このチェックでは新しい入口、Green、CI、merge、配布、実質問のcloseは未実施。
実装担当は以上の未達条件を保持して修復を続ける。
