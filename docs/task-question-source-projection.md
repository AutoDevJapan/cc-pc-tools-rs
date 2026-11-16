# 質問に使う現在Taskと元入力の読取専用照合

`unattended-runner.exe --inspect-task-question-source` は、既存の通常Task providerから指定した1件のmetadataを取得する。登録済みproject root、状態、保存済み担当・世代と、元の本文・decision・比較画像の同bytesを同じ実行中に照合する。Taskの一覧、title、spec、note、raw storeは出力しない。

```text
unattended-runner.exe --inspect-task-question-source
  --todo-id 42
  --cwd D:/Work/RequestedRepository
  --body-file D:/Work/Question/question.txt
  --decision-file D:/Work/Question/decision.json
  --attach D:/Work/Question/comparison.jpg
  --body-sha256 <元本文のSHA256>
  --decision-sha256 <元decision bytesのSHA256>
  --image-sha256 <元画像のSHA256>
```

これは引数の説明用に改行した例で、実際は一つのコマンドとして渡す。本文・decision・画像をコピーしたり、このコマンド用に書き換えたりしない。`--cwd` は質問に使う実source rootであり、Taskに保存されたrootを置き換えない。callerからstore、provider、guard、実行fileの場所は指定できない。

## 取得する値と境界

- normal providerは既存の `taskQuestionReadState` と同じ固定store・exact ID選択を使う。同じIDの重複、store envelope・projectキー・選択rowの曖昧なJSONを拒否する。非対象Taskのspec/noteの意味や内部キーは調べない。
- source rootとTaskの登録rootは別に確認する。どちらかが保護対象・未確認ならmetadataを出さず固定stageで失敗する。source rootの成功をTaskや工具の資格へ貸さない。
- 元入力は普通のguardが許すplainなregular fileだけを開く。元handleを保持してサイズ・hash・名前との同一性を再確認する。本文とdecisionは各128KiB、画像は10MiB以内のPNG/JPEGで、decisionの元Taskと `direction` を照合する。画像生成の証拠には変換しない。
- metadataにはid/project/root/status、resume generation、worker job/generation、scheduler jobとsnapshot指紋だけを含む。未保存の担当値はnullで、保存済みの0や空文字とは区別する。
- 各rowではIDだけを浅く検査し、大小文字を含む複数のID、無効な型、後勝ちの対象変更を拒否する。投影する文字列は元JSONの不正UTF-8や孤立surrogateを置換せず拒否し、正常なsurrogate pairと元からあるU+FFFDは保持する。非対象Taskのspec/note内部を検査するために範囲を広げない。
- Taskは入力取得の前後で通常providerから再読する。世代、root、担当、状態や原rowが変わった場合は成功出力を作らない。2回目のTask読取後も同じ入力handleを再確認し、解放失敗時も成功にしない。
- 出力は一度だけ書き、短書込・書込エラーは非0とする。部分出力へ成功やfallbackを追記しない。利用側はexit0の完全なJSONだけを観測結果として扱う。

成功でも `metadata_only=true`、`registered=false`、`send_allowed=false`、`task_to_repository_proved=false`、`source_quiescence_proved=false` である。独立したlive sampleの一致は、継続するlease、原依頼の意味、repo所有、UI承認、実投稿や原素材の一般的な利用権を証明しない。ファイルI/OのOS内停止をcontextだけで中断できるとも保証しない。

## 通常の質問経路との関係

結果は現在のsource/Task不足を特定するための読取情報であり、資格登録やsenderの代替入力ではない。通常askは元のTask、本文、decision、画像を自身の現在の経路で再照合し、同じ要求の配送状態を確認する。rootが未確認の場合は[元Taskのmetadata用途](task-question-metadata.md)の正規originを扱い、この観測結果だけで解除しない。

この入口はGit/ghを起動せず、repo labelのprobeや工具admissionを行わない。通常ask側でrepository probeが必要な場合、実行する工具のcurrent identityと用途の正常な資格は別工程で確かめる。metadata取得を根拠に未資格の工具をhidden childや別binaryで起動しない。

Task状態・project・root・担当・保護設定・台帳を変更せず、質問送信、待機登録、通知、source/mediaの新規登録もしない。

## 検査

`TestTaskSourceProjection` 群は隔離した実storeと元入力fileを使う。正常読取、非対象Taskやtitle/spec/noteの非出力、未保存nullと保存済み0、保護拒否、duplicate、入力不一致、二度目のprovider読取での世代・root・停止・元入力変更、短書込を対照とする。世代変更は読取境界への到達後に起こし、固定sleepで窓を推定しない。

これらのfixtureを実行した結果、配備済みCLIの実取得、質問の送信・回答はそれぞれ別に記録する。
