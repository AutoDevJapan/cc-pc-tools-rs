# 今後の正規作成から、現在の公開bytesの消費へ結ぶ

元3686の実装仕様。本人の限定a、元3080、既取得の公開出典、個別notice、
固定manifestと内容レビューの条件を保持する。既存14/28instructionの保留、
旧unknown9、取得拒否647、原保護台帳・設定は変更しない。過去の正規作成や
従来32件の使用成功も無効にしない。

## 保証を分ける

|区分|この実装が検証するもの|
|---|---|
|元の正規作成|登録された3080の実writer/job、保存specを検証した正規CLIの実呼出しと返却結果|
|公開内容identity|通常merge済みmanifest、既取得source/noticeの独立固定bytes、内容レビューと出力bytes|
|現在の消費|固定pathの非reparse/singlelink handle、現在の128-bit file IDとvolume、同じhandleから読み返す同じbuffer|
|過去からの物理連続性|未証明。file ID一致も、削除後のID再利用や履歴の連続性を証明しない|
|現在のOS上の書込者|未証明。原writerを現在のOS writerと呼ばない|

作成時IDはこのforward経路の実装上の照合材料であり、本人が新たに指定した禁止や
全既存出力の利用条件ではない。USNを作成・有効化・走査せず、inactiveを理由に
将来の公開内容確認全体を禁止しない。過去14には証拠を後付けせず、保留を維持する。
[FileIdInfo](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_id_info)
と[file IDの制限](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/ns-fileapi-by_handle_file_information)
に従い、現在のhandleの識別と長期の連続性を区別する。

## 作成時の発行

`--create-skills3080-forward <requestの絶対path>` は、従来のCreateNewと同じ
本人scope、現writer、merge済みpublic manifest、個別notice、実モデルを確認した
内容レビュー、不在・物理親・保護の条件を再実行する。対象は最大128出力。
全件作成・Sync・同handle読戻しの成功後、まだ保持中のhandleから次を発行する。

- `domain:skills-3080-created-v2`、原writer、request SHA、manifestのcommit/path/SHA、review
- 実行した既設Windows版のSHAとcleanなmerged source revision
- rootと全対象物理親のvolume/128-bit file ID
- 各出力の正確なtarget、size/hash、作成handleで観測したvolume/128-bit file ID

途中失敗には証拠を発行しない。作成件数・保留を残し、同じ対象へCreateNewを
繰り返さない。従来の`--create-skills3080`は旧契約を保ち、新証拠を発行しない。

## 真正な発行元との対応

任意receiptのpathやJSONを消費requestに渡す方式にはしない。
正規のcanonical native sessionを、実job metadataのthread/start/end/cwdへ照合し、
指定した1つのcall IDについて実呼出しと実tool応答を選ぶ。現在のjobのappend logは
最初に観測した長さまで、同じhandleで読む。ほかのbodyや全履歴は返さない。

新規作成のpreflight結果の`forward_native_invocation`を、Functions execのコードへ
そのまま渡す。コードは次の1式だけであり、JSON内のfieldも固定する。

```javascript
text(await tools.exec_command({"cmd":"& 'D:/CC/Tools/client-records/scrub391.exe' --create-skills3080-forward '<requestの絶対path>'","workdir":"<writer.workspace>","login":false,"shell":"powershell","yield_time_ms":30000,"max_output_tokens":30000}));
```

作成は本人Chromeや別のAPI経由で行わず、この正規CLIを1回だけ呼ぶ。
PowerShellのprofile・環境上書き・script file・echo・任意JS・別EXE・別path・
別APIの応答copyを発行元として受け入れない。nativeのmodel/user message内の
自己申告JSONも受け入れない。tool応答の成功、完了件数、原request/manifest、
新証拠の全fieldを確認する。実行中sessionの返却やtruncationは未受領のまま保持し、
再CreateNewしない。必要なら元の同callについて受領形式を修復する。

信頼境界は既存の正規job登録、canonical native記録、review/CI/通常更新済みの
既設CLIである。これら自体の改ざんへ対する署名・秘密鍵・恒久権限は追加しない。
発行時と消費時のruntimeは区別する。現在のwriterによる消費でも、登録済みの
read-only consumerと同じ検証を使い、原native発行元のSHA/revisionが現在の実行物と
一致するか、独立に受入済みの厳密なsoftware baselineの1組に一致することを要求する。
原request・writer・manifest・reviewと全作成時IDも引き続き照合し、発行記録のproducerを
現在値へ書き換えない。未登録SHA、別revision、旧v1/未発行receiptは対象外である。
これはソフト更新後の正規作成済み出力の消費であり、新しい作成や一般分類へは使わない。

## 消費requestと実消費

`--consume-skills3080 <requestの絶対path>` のrequestは、共通の
`domain:skills-3080-operation-v1`、現在の実`writer`、同じ
`manifest_commit/path/sha256`、元の`review`を持ち、さらに次を持つ。

```json
{
  "consume": {
    "creator": {"task": 3080, "job": "<正規作成job>", "spec_sha256": "<作成時spec SHA>", "workspace": "<元3080の作業場所>"},
    "creation_request": "<実作成に使った元requestの絶対path>",
    "creation_request_sha256": "<元requestのSHA>",
    "native_call_id": "<正規作成のcall ID>",
    "indices": [0]
  }
}
```

`creator`は過去の登録済みauthorで、共通writerは現在の生存する3080である。
同一jobの後続処理でも、正規に継続した後続jobでもよい。別ToDo、架空・未来の世代、
別writer/job/spec、別manifest/review/requestを受け入れない。原requestを同じ
保持bytesでSHA確認・解析し、nativeの実呼出しと結ぶ。親の現在specで過去のspecを
上書きしたことにはしない。

選択はmanifestのindexだけで、最大32出力・合計64KiB。対象pathはcallerが指定しない。
現在の保護を先に確認し、root/全物理親を非reparseのhandleで保持してIDを照合する。
各対象はregular/singlelinkの同handleを保持し、本文を読む前に作成時観測IDと
現在IDを比較する。置換ID・reparse/hardlink・別root/親は本文を読まず拒否する。
同じhandleからsize/hashと固定公開bytesを比較し、全選択の成功後だけ、実際に読んだ
bufferを`files[].content_utf8`として返す。manifestの別copyを使用したことにはしない。
返却直前に現在writer/本人停止も再照合する。

消費結果は`same_read_buffer_consumed:true`、`creation_id_matches_current:true`、
`historical_physical_continuity_proven:false`、`current_os_writer:unproved`を区別する。
`producer`は原native発行元、`runtime_producer`は今回の実行物であり、
`original_producer_revalidated:true`はその厳密な再照合を示す。単独のboolは資格にならない。
通常guardのboolや後続の別openの許可票には転用しない。使用者は返却したbufferを
そのまま用い、pathを再度開いて利用したとは報告しない。

## テスト・配布・受領の区別

合成テストは本物のCreateNew/handle/file ID/読戻しを使う。元物を保持したままの
同bytes置換、変更bytes、hardlink、別root/親、protected優先、発行前、別writer/job/
spec/manifest/review/producer、偽native/echo、別path、時刻窓、未登録世代を拒否する。
通常guard、旧CreateNew、source/notice・647拒否の関連テストも保持する。

合成テスト成功、別モデルレビュー、正確head CI、通常merge、既設Windows更新、
親3080による新しい公開出力の実作成/消費は別の段階である。3686は親のwriterを
自称して中央へ配置しない。配布と実CLIの結果は私的な配布記録と元ToDoへ返す。
この文書だけで配布・親の実使用・元16資料全体を完了とはしない。
