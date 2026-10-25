# 作成済みの公開Skillを使うために不足している証拠

元3686の新差分。新規配置の成功と、その後の実体の同一性を分ける。
旧PR476/497の修復・配布、元3080の従来32件の限定使用成功は保持する。
本書は新しい読取資格・例外台帳・一般分類の緩和を実装したものではない。

## 確認した事実

- `skillsOperation` は、正規writer、本人scope、マージされたmanifest、既取得の
  固定source/license、別モデルレビュー、不在targetを検証してCreateNewを行う。
- `skillsCreate` は実ファイルを排他で作成し、同じhandleから出力bytes/hashを
  読み返す。成功と作成件数は有効な実績であり、作成失敗へ戻さない。
- その戻り値は件数と理由だけである。CLIの成功結果にはrequest/manifest/sourceの
  hashと件数があるが、作成時のvolume/file IDや変更世代を保存していない。
- 今回の作成後readbackも、同じhandleの実体一致を一時比較するが、保存するのは
  bytes/hashとregular/nonreparse/singlelinkのboolである。元のfile IDは残らない。
- 元jobのcanonical nativeには実CreateNew呼出しと対応するtool結果がある。
  requestと成功wrapperのhashは対応するが、ここにも中央ファイルIDはない。
- 通常のguardは28instructionを分類未確認で保留したまま。実clientであるとの
  断定、自動承認レビューによる拒否、原保護台帳の復旧とは区別する。

## 現在のhashだけを根拠にできない理由

`TestSkills3080CreatedResourceProvenanceGap` は本物の合成CreateNewを実行する。
完了後に元ファイルを保持したまま別名へ動かし、元のpathへ同じbytesの別ファイルを
CreateNewする。size/hashに加え、creation/last-writeの時刻も同じにできる。
過去の成功件数・固定manifest・現在のbytes・通常metadataは全て一致するが、
作成時と現在のfile IDは異なる。保存したboolの`accepted:true`を追加しても変わらない。

これは実対象で置換が起きたという主張ではない。保存済みの証拠では、その反例を
排除できないという限界である。今初めて取得したIDを、過去の作成IDとしては扱えない。
公開sourceの固定bytesを別pathへコピーして使うことも、この不足の修復にはしない。

## Windows側での回収確認

中央本文を開かない有限なGo probeで、既存CC-AdminBrokerから
`FSCTL_QUERY_USN_JOURNAL`を実行した。Dの変更ジャーナルはinactiveだった。
対象ファイルのopen・本文read・容量測定に進む前に終了した。
ジャーナルの新規作成・有効化・広域走査はしていない。

`FSCTL_READ_FILE_USN_DATA`が仮に成功しても、返るTimeStamp/Reasonは有効ではない。
最後のUSNだけを作成時刻の証明へ読み替えない。
[MicrosoftのAPI仕様](https://learn.microsoft.com/en-us/windows/win32/api/winioctl/ni-winioctl-fsctl_read_file_usn_data)
と[ジャーナル照会の仕様](https://learn.microsoft.com/en-us/windows/win32/api/winioctl/ni-winioctl-fsctl_query_usn_journal)
を確認した。通常のファイル時刻だけでも不足する。

## 最小の実装修復案と未達

この案は未実装・未配布であり、既存14の利用資格を発行しない。

1. 正規CreateNewの全出力成功後、まだ保持中の作成handleから各実体のvolume/file ID、
   実path、bytes/hashを取得する。rootと物理親の同一性も同時に保持する。
   `TestSkills3080CreationIdentityIsAvailableBeforeHandlesClose` は、この時点なら
   ID取得と置換排他が可能なことを合成fixtureで示す。
2. その証拠を、元3080・実writer/job/保存spec、固定公開manifest/source/license、
   内容レビュー、実行した正規ソフトのsource/hash、CreateNewの全件完了と結ぶ。
   任意JSONの自己申告や、後から作ったcopyの`accepted:true`を信頼しない。
   canonicalな実実行記録と照合できる発行先・形式まで実装/レビューが必要である。
3. 消費側は現在の正規actorと承認された操作を再照合し、固定manifestのexact対象だけを
   非reparse/singlelinkで開き、実体IDとbytes/hashを同じ保持handleで検証・消費する。
   検証結果を後続の別openの許可票にしない。元client設定・カード/秘密・旧protectedを
   常に優先し、通常guardのunknownを解除しない。
4. 既存unknown、未実作成、他writer/job/spec、変わったmanifest/receipt/current bytes、
   他path、reparse/hardlink、コピーされた受入票、旧protectedの負例を実証する。
   この案の正例は作成時証拠がある新規出力だけ。旧14への後付け発行は正例にしない。

現在の84出力については、独立した作成時IDの証拠がまだない。元のunknownを新規出力へ
成り代わらせる反例を排除できないため、新たな正規読取CLIを配布して利用成功とはしない。
元の保留・原本・既存9・取得拒否647・junction・通常guard・原台帳を保持する。
本人へ同じ権利判断や通常操作の許可を再質問せず、不足した証拠とこの最小案を親へ返す。

この文書と合成テストの完了は、元16資料・新14の使用・Windows修復版の配布完了を
意味しない。実対象の後付け証明やCreateNewの再実行も行わない。
