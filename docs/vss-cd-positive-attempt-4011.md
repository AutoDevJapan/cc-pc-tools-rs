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
  `vssadmin.exe resize shadowstorage /for=C: /on=D: /maxsize=50GB`を1回実行する。

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
stdout/stderrを並行回収して各64KiBで打ち切り、wait未確認や打切りは先へ進む条件にしない。
preflightの子は組込みのWindows読取cmdletだけ、contenderの子にはさらに子を起動する機能がない。
原CLIの文章/Volume GUID/Hardware serialを出さず、終了コード・captureのhash・数値HRESULTだけを保存する。
Windowsのサービスへ送った要求の結果が不明な場合は、後状態と未確定を残し、同じ書込を再試行しない。

実行前には同worktree/os-v2へCreateNew/WriteThroughの固定requestを保存し、resultも追記専用とする。
inspectとattemptは違う工程の別票にし、元のempty-set-probe/availability/A/rollback票は上書きしない。
登録・起動・排他・コマンド実行・関連付けの実反映・復元point保持をそれぞれ区別して返す。

関連付けが成功してもsnapshot0のままでは修復完了にしない。必要な通常の一度きりの機能確認で
SystemRestoreの新しい2世代が保持されること、50GiB・C/D/E空き・既存機能・夜間の保持を確認する。
その機能確認は原A/Bの再試行ではなく、設定が異なる新しい是正工程の固定票で行い、point作成loopを作らない。
現在のcandidateにpoint作成は実装していないため、登録/設定だけで全体完了としない。
OS再起動や障害復元は未検証のまま区別し、本人のPC操作を依存にしない。

一次資料：

- [StartSnapshotSet](https://learn.microsoft.com/en-us/windows/win32/api/vsbackup/nf-vsbackup-ivssbackupcomponents-startsnapshotset)
- [AbortBackup](https://learn.microsoft.com/en-us/windows/win32/api/vsbackup/nf-vsbackup-ivssbackupcomponents-abortbackup)
- [ChangeDiffAreaMaximumSize](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nf-vsmgmt-ivssdifferentialsoftwaresnapshotmgmt-changediffareamaximumsize)
- [vssadmin resize shadowstorage](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/vssadmin-resize-shadowstorage)
- [MigrateDiffAreas等はfuture用に予約されている](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nn-vsmgmt-ivssdifferentialsoftwaresnapshotmgmt2)
