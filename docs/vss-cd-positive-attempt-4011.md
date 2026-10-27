# 4011：通常VSSの排他中に、正の50GiBだけでC→Dを試す候補

C→CでMinDiffを3200MiBへ増やしても07:25に25が再発したため、その自己設定は元へ戻した。
過去のpointと今回の2世代は復旧していない。現在0件を見ただけで、既存データや新しいpointを
消せる操作はしない。以下は保存先の分離を実際に試すための限定候補であり、実適用はまだしていない。

通常VSSの空集合の診断は、最初のStart=S_OK、2つ目=IN_PROGRESSを実取得した。
Abortの非ゼロ成功値EVENT_S_NOSUBSCRIBERSを診断側が未確認とした原票は保持し、
6種類のHRESULTのRed→Greenで分類を直した。10:58の別の単一Start/Abort/Releaseは正常に
完了し、通常の要求を再び受け付けることを確認した。snapshot/pointは作成していない。

## 候補の境界

`tools/vss-4011/cd-attempt/main_windows.go`は次の3モードだけを持つ。

- `--contender`：通常の単一の空集合を要求して結果を返す。子processや設定操作はない。
- `--inspect-cd <preflight.ps1>`：自分の空集合を保持し、別processのcontenderがIN_PROGRESSで
  拒否されることと、保持中のWindows匿名metadataを読むだけ。resize分岐へ入らない。
- `--attempt-cd <preflight.ps1>`：同じ排他と事前条件がすべて成立した場合だけ、既存の
  `vssadmin.exe resize shadowstorage /for=C: /on=D: /maxsize=53687091200`を1回実行する。

書込モードは別モデルで手順とsourceのデータ保持を確認してから、既存CC-AdminBrokerで実行する。
新製品/配布CLI/常駐/taskではなく、この4011の一時的な診断・是正工程である。
APIキー/新費用/重Cargo/GPU/Civ6を使わず、UI/UAC/Terminalを出さない。

## 排他とデータ保持

1. 同じOS threadでCOMを初期化し、no-writers contextで通常のStartSnapshotSetを成功させる。
2. 自分の実行物の別processをCREATE_NO_WINDOWで起動する。contenderのStartがIN_PROGRESS、
   所有set0/Abort0/Release1で終わった場合だけ先へ進む。timeoutや違う値なら書込0で終える。
3. hashを照合して書換え/削除を禁止したhandleで保持するpreflight.ps1を実行し、全取得成功、
   全snapshot0、復元point0、既存関連付け1=C→C/50GiB/使用割当0を確認する。
4. C/DがNTFS・異なる実NVMe、DがHealthy/Online/非boot/system/書込可能で、Dの他VSS依存が
   ないこと、C空き200GiB以上/D空き50+200GiB以上、MS provider1、明示保護停止なし、MinDiff未設定を確認する。
5. inspectはここで書込0のまま終わる。attemptだけが前記の**正の50GiB**のコマンドを1回呼ぶ。
   自分の空集合は、子processと前後の取得が終わるまで保持する。途中に期限だけで解除するleaseはない。
6. 例外/非0/timeoutを成功や無変更と決めず、保持中に同じ匿名metadataを読み、実際の後状態を保存する。
   関連付け1=C→D/50GiBとsnapshot/point0を取得した場合だけ「関連付けの反映」と記録する。
7. 最後に自分の空集合だけをAbortし、Release→CoUninitializeする。別のbackupや全worker、
   VSS/SystemProtectionを止めない。未確認の後始末を成功にせず、通常の可用性を別に照合する。

この排他はWindowsの通常の作成APIのものであり、二度読みによる0件判定だけを使うものではない。
異なるprovider/保護設定/必要なpointがあれば、原状態を保持して書込前に終える。
既存C→Cがあるときの正のresizeは未実証なので、成功を前提にしない。拒否・関連付けが複数・
未確認の状態でも、0への縮小、Delete、旧関連付け削除、保護停止で切替を強制しない。
無限再試行や別経路での強制も行わない。元C→Cを空の原設定へ作り直さない。

## 期限・出力・再開

子は自分が起動した単一processだけを管理し、contender10s、各metadata20s、resize20sを上限とする。
全ての子の期限を、COM初期化より前から計測する共通の70秒以内に収める。
外側の90秒期限より前に子を排出し、COMが遅れた分を子の起動期限へ上乗せしない。
stdout/stderrを並行回収して各64KiBで打ち切り、wait未確認や打切りは先へ進む条件にしない。
preflightの子は組込みのWindows読取cmdletだけ、contenderの子にはさらに子を起動する機能がない。
原CLIの文章/Volume GUID/Hardware serialを出さず、終了コード・captureのhash・数値HRESULTだけを保存する。
Windowsのサービスへ送った要求の結果が不明な場合は、後状態と未確定を残し、同じ書込を再試行しない。

実行前には同worktree/os-v2へCreateNew/WriteThroughの固定requestを保存し、resultも追記専用とする。
`cd-attempt/run-once.ps1`が実Git HEADとソースの未変更を確認し、共通関数・Go・preflightの
固定hashと読取共有handleを照合した同じsourceから新しい実行物をビルドする。
review番号・実source head・source/実行物hashを含む固定票を作り、実行物を読取専用共有で
固定して、CREATE_NO_WINDOW/非同期回収/90秒期限で起動する。既存の実行物を渡して流用しない。
inspectとattemptは違う工程の別票にし、元のempty-set-probe/availability/A/rollback票は上書きしない。
登録・起動・排他・コマンド実行・関連付けの実反映・復元point保持をそれぞれ区別して返す。

関連付けが成功してもsnapshot0のままでは修復完了にしない。必要な通常の一度きりの機能確認で
SystemRestoreの新しい2世代が保持されること、50GiB・C/D/E空き・既存機能・夜間の保持を確認する。
その機能確認は原A/Bの再試行ではなく、設定が異なる新しい是正工程の固定票で行い、point作成loopを作らない。
現在のcandidateにpoint作成は実装していないため、登録/設定だけで全体完了としない。
OS再起動や障害復元は未検証のまま区別し、本人のPC操作を依存にしない。

## レビュー4092の指摘対応と現在の適用条件

4092は固定ecc24406の5対象と共通票関数を読了し、Must0と修正案を返した。
実Opus5.5/max/native_turn_context、再開実job212901のsucceeded/exit0/18stepsと原回答を照合した。
初回job195832のfailedを成功へ数えず、最終原回答6934字/SHA8D1C5B5B670928D7B9B3512A69C5DC53185688F1C362B51D4B8A0930E324CB85を保持する。

- S1：固定resizeの出力だけを対象に、Volume GUID等を除いた先頭4KiBをprivate票のbase64と
  分類語で保持する。内容を公開文書やコンソールへ生出力せず、拒否理由をhashだけで失わない。
- S2：実HEAD/clean sourceと固定hashを照合し、その読み取りhandleを保持したsourceから新exeへ
  buildして実hashを票へ保存する。common.ps1も先に照合してから読み込む。
- S3：後のmetadata取得から自己Abortまでの間にも、同じCOM threadの別objectのStartが
  IN_PROGRESSになることを記録する。誤ってS_OKになればその自分の空集合だけをAbort/Releaseし、
  保持が継続したとは扱わない。Inspectでもこの後段を確認する。
- S4：関連付けが複数でも最大8行のC/D/otherと使用/割当/上限、列挙完了の有無を保存する。
- S5：run-onceのMode Inspectを別固定票で先に通し、同じbuild/起動/回収経路を確認する。
  既存snapshotがあればNoWriteで終わることも実結果として残す。失敗票を消して再試行しない。
- S6：timeoutの場合はサービス側の要求が後で反映する可能性があるため、親の終了後に読み直した
  状態を正とし、未確定を無変更・成功へ読み替えない。
- S7：CLIの単位の解釈に依存せず、単位なしの正確な53,687,091,200バイトを指定する。

軽微な指摘も反映し、Goの書込モード自身が固定request/byte hash/result不在を確認して
native-start票をCreateNewで消費する。既に消費した票では直接の再呼出しも実行しない。
Used/CFree/NTFS/DReadyの拒否テストを追加し、結果票の保存失敗でも正常Brokerの標準出力へ
実結果を先に残す。保存成功と標準出力への出力は区別する。

21:52の再開時には本担当が作っていないC snapshot3件・SystemRestore point264/265が存在した。
C→C/使用13,415,874,560B/割当13,943,291,904B/上限50GiB、MinDiff未設定であり、
この候補の現用の適用条件は満たさない。これらを切替のために削除せず、書込を実行しない。
新しい保存元の数値metadataとして記録し、再発で失ったpointが復活したとは扱わない。
実空きC429,004,951,552/D2,077,182,341,120/E858,052,489,216B、正常照会complete/errors0。
固定の夜間確認までに同じsnapshotが存続すれば保持事実を追加できるが、修復の因果は未確定である。
上限や保護仕様を変更せず、通常VSSのデータ保持条件を満たす範囲で修復を続ける。

2026-10-27の最新PR方針により、マージ前には別セッションのセルフレビューとラバーダックを
同じ最終コミットへ行い、Must以外の修正指摘も含めて解消確認を得る。

22:02:43～46に修正したrun-onceのMode Inspectを実行し、実HEAD/clean source照合と
3sourceのhash/pin後のfresh build、子の起動/回収が同じ窓口で機能することを確認した。
別processの拒否と取得後の別objectの拒否は両方80042316で、親所有1/Abort完了1/Release1。
timeoutなし、preflight全取得成功の3snapshot/2pointのため、期待どおり
live_preflight_not_met_no_write/exit1で終わった。書込process起動0、positive-cd票はまだない。
この固定Inspect票も上書き・削除・再試行しない。現在のsnapshotを切替のために削除せず保持する。

## 現在の実測と検査

gofmt/Go vet/通常build成功、保持条件の13種類の拒否例と出力上限の関連テスト成功。
emptyなscript引数はexit2で拒否した。通常AdminBrokerのread-only preflightも全取得成功した。

11:19:14～17の`--inspect-cd`は、別processのStartが0x80042316で拒否されることを
実際に確認し、timeout/打切りなし、終了待ち確認済みで戻った。保持中のpreflightは全取得成功、
C→C/50GiB/使用割当0、全snapshot0/point0、D実NVMe/空き2,074,361,589,760Bを返した。
親のAbortは0x00040202、所有空集合1/Abort完了1/Release1で終わった。
この実行のpositive_resize欄はなく、resize processは起動していない。
inspectの原request/resultを追記保存し、元probeの未確認票を成功票へ書き換えていない。

後続のsourceでは、別processの匿名call/HRESULT/所有数も結果へ残すようにした。
書込モードは未実行であり、その別モデルレビューと実適用・通常保持の受入が残る。

一次資料：

- [StartSnapshotSet](https://learn.microsoft.com/en-us/windows/win32/api/vsbackup/nf-vsbackup-ivssbackupcomponents-startsnapshotset)
- [AbortBackup](https://learn.microsoft.com/en-us/windows/win32/api/vsbackup/nf-vsbackup-ivssbackupcomponents-abortbackup)
- [ChangeDiffAreaMaximumSize](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nf-vsmgmt-ivssdifferentialsoftwaresnapshotmgmt-changediffareamaximumsize)
- [vssadmin resize shadowstorage](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/vssadmin-resize-shadowstorage)
- [MigrateDiffAreas等はfuture用に予約されている](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nn-vsmgmt-ivssdifferentialsoftwaresnapshotmgmt2)
