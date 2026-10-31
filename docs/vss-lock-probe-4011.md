# 通常VSSの空集合による有限診断

snapshotを作成・削除せず、通常のVSS作成APIの排他と後始末を確認する診断。
同じOS threadでCOMを初期化し、context0x1d・backup type5で空集合を開始する。
AddToSnapshotSet、DoSnapshotSet、保存先変更、レジストリ変更は呼ばない。

最初の2objectの診断ではStart=S_OK、2つ目=0x80042316、Abort=0x00040202だった。
最後の値はEVENT_S_NOSUBSCRIBERSというCOM成功値である。
元の未確認結果は保持し、6種類のHRESULTを使うRed→Greenで分類を修正した。
別の単一Start/Abort/Releaseで通常の受付可用性を確認した。

slot順序はMicrosoft SDKのinterfaceと照合した。
Release2・InitializeForBackup5・SetBackupState6・AbortBackup15・SetContext35・StartSnapshotSet36。
出力pointerはSyscallNの引数で直接変換し、cleanupも同じlocked threadで行う。

後続の別processによるStartも0x80042316で拒否され、保持中の匿名metadataを取得できた。
IN_PROGRESSは集合の存在を示すだけで持主を証明しないため、二度読みの一致と同様に過大評価しない。
子の終了・Abort完了・Release・COM終了を区別し、未確認を成功に変えない。
同じ取得済みの実行票を消去したり、診断を繰り返したりしない。

C→Dへの切替案は成功経路がなく撤回した。
[撤回と残した読取診断](vss-cd-positive-attempt-4011.md)を参照。

一次資料:

- [StartSnapshotSet](https://learn.microsoft.com/en-us/windows/win32/api/vsbackup/nf-vsbackup-ivssbackupcomponents-startsnapshotset)
- [AbortBackup](https://learn.microsoft.com/en-us/windows/win32/api/vsbackup/nf-vsbackup-ivssbackupcomponents-abortbackup)
- [Microsoft SDK](https://github.com/microsoft/win32metadata/blob/main/generation/WinSDK/RecompiledIdlHeaders/um/vsbackup.h)
