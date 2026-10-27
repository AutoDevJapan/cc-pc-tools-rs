# 通常dispatchの候補判定を読み返す（ToDo4105）

`scheduler explain-admission --id <ID>` は、直近の通常dispatchが実際に通った
分岐を読む。診断のためのdispatch、再判定、起動、状態保存、モデル/認証検査は行わない。
通常の候補抽出・順位・容量・準備・起動の意味と頻度を変えない。

## 入出力と失敗

通常dispatch内で既に走査している行とキューへ観測を添え、scheduler領域の
`admission.json` を有界・原子的に置換する。対象指定dispatchはこの通常結果を
上書きしない。保存失敗は通常dispatchの成否を変えず、旧結果は日時と期限で区別する。
記録がない、壊れた、上限外、期限切れ、制御メタデータが変化した場合は、その状態を
返し、現在の許可や起動成功として扱わない。到達していない段階はunknownとする。

出力はID、観測/有効期限、制御メタデータのfingerprint、順位、候補/採否、固定理由、
実測した容量と除外/guardのbooleanだけ。fingerprintは本文SHAではなく、タスク制御
メタデータと登録rootに限定する。容量・他担当の状態は観測時点の事実であり、CLIの
実行時点に再検証した許可ではない。Source/spec/noteの本文、コマンドライン、認証、
保護名一覧は診断へ取り込まない。CLIはstoreの本文フィールドをデシリアライズせず
読み飛ばし、spec/prompt/Source等の別ファイルは開かない。

fingerprintの投影は通常判定と共有する項目定義から選び、通常側の既存ValueとCLIの
streamの両方を同じVisitorへ通す。通常の入力比較は元の値をそのまま使い、本文を
省略した比較へ置き換えない。診断のtitle・prompt・本文/秘密を含む値は有無のbool
だけとし、updatedやnoteの内容は含めない。未対応の制御構造では現在の照合をunknown
とする。これは本文を含めた全入力のSHAや、起動許可の証明ではない。

input_metadata_fingerprintは候補抽出時の入力、metadata_fingerprintは通常処理が
自分で書き込んだ後に期待する制御メタデータを表す。通常editがtodo.lockの中で
書き終えた行だけを後者へ反映し、外からの後続変更は一致と扱わない。quota待ち、
準備失敗、容量の変化、起動受付後も、同じ通常結果として読み返せる。
期限は記録完了から180秒、観測開始・記録完了・読取時刻を分けて返す。

全validityで最上位のキーを固定し、記録がない値はnullを返す。走査前の失敗は
incompleteと固定errorで返し、launchの受付後に紐付けが失敗した場合も準備失敗と
区別する。上限では未起動のtodo行を優先し、完了行を際限なく蓄積しない。

## 固定67acaf23の別モデルレビューと修正

ToDo4107はnative Opus5.5/maxで10差分を読了し、Must3件を返した。
自己更新の誤認、制御項目の不足、出力キーの揺れを、それぞれRedの3テストで再現した。
上の共有Visitor・元の入力比較の保持・edit後の照合・固定schemaで直し、Greenを確認する。
別行の復元済みジョブ参照と予約のnull/object変化も別のRedで再現し、本文を含めずに
扱う。最終候補の再レビュー、CI、配布、4091の実CLI受入は別に確認する。

## 受入条件とテスト

- 通常の候補順・採否・起動回数が診断の保存成功/失敗で変わらない。
- 候補前除外、依存、関連owner、時間、容量、資源、quota/準備失敗と未到達を区別する。
- CLIの結果、読み飛ばし、状態/ファイル不変を、書込不可の入力や本文の囮で確認する。
- 件数/バイト上限、原子的置換、古い/不一致/欠落/破損、出力の固定schemaを確認する。
- 変更したcc-schedulerの関連テスト、doctest、Clippy、固定headの別モデルレビューと
  CIを確認し、通常配布の実所有/ロック/source receiptを照合して反映する。
- 通常起動の観測を待ち、4091へ実CLIを使って具体的な候補/除外/保留を読み返す。
  4091等の本文、停止、依存、予約、model/account/routeは変更しない。

Windowsの置換は[ReplaceFileW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-replacefilew)を使い、
初回だけ[MoveFileExW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-movefileexw)で配置する。
