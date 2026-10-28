# 4011：データを変更せずVSSの作成排他を照合する

Volsnap25が07:25に再発したため、本担当の3200DWORDは10:10に元の未設定へ戻した。
C→C/50GiBは保持し、復元用データの消失を清掃成功へ分類しない。
次の保存先分離案には、新しいsnapshotとの競合を防ぐ通常VSSの排他が必要である。
現在0件を何度読んでも、その後の作成との競合は防げない。

この段階で追加するGo helperは**空のsnapshot setの排他を1回調べるだけ**である。
保存先/上限/レジストリ/保護設定の変更、volumeの追加、snapshot作成/削除、writer metadata取得、
private fileの読取を行う関数やモードは含めない。別モデルでABI/排他の解釈/後始末を確認してから
通常AdminBrokerで実行する。新製品・監視・常駐・定期taskではなく4011の一度きりの診断である。

## 実際に確かめる条件

1. 同一process内の異なるIVssBackupComponentsを2つ作成する。COMは同じOS threadで扱う。
2. InitializeForBackup、no-writersのclient-accessible context、copyのbackup stateを設定する。
3. 最初のStartSnapshotSetがS_OKのときだけ、その空集合を自分の所有として記録する。
4. 次のobjectがVSS_E_SNAPSHOT_SET_IN_PROGRESSを返すかを記録する。
   両方S_OK、別のerror、timeoutなら排他が確認できたことにしない。
5. 自分のStartがS_OKだった空集合だけにAbortBackupを呼び、両objectをReleaseしてから
   CoUninitializeする。既存の他担当のbackupの中止・service停止はしない。
6. 前後の通常observerでsnapshot/point/storage設定/イベントを照合する。
   既存snapshotが現れた場合は保持し、Dへ切り替えるために削除しない。

helperのJSONは実HRESULT、所有空集合の件数、Abort結果、Release件数を返す。
成功したgetterやCOM呼出しだけで排他を成立扱いにしない。
終了コード0はprobeが最後まで動いたことだけを示し、排他の観測はJSONの
exclusion_observedと全Start/AbortのHRESULTで別に判定する。
cleanupの失敗でphaseが変わった場合も、phase_before_cleanupとcallsに元の結果を保持する。
`exclusion_observed`は同一processの2object間の観測に限り、保存先移行の原子性を証明しない。
従ってこのprobeだけを根拠にD変更を実行しない。GUIDやwriter名/ファイル一覧は出力しない。

呼出側はCREATE_NO_WINDOW、stdout/stderr非同期回収、90秒の有限期限で起動する。
期限切れはこの呼出しで作ったhelperだけを停止して原結果を保存し、同じprobeの無限再試行をしない。
helperには子processがなく、AddToSnapshotSet/DoSnapshotSet/PrepareForBackupを呼ばない。
中断時も「Abort成功」とは記録せず、通常observerと次の正規VSS状態確認で照合する。
空集合を持つ短い間は、他のSystemRestore等の作成がIN_PROGRESSで拒否される可能性がある。
前後の照合では実started_utc～finished_utc間の既存VSS/SystemRestoreの作成失敗も調べる。
timeout/Abort未確認時は排他が解除されたと仮定せず、後続の通常作成が拒否されていないことを
別に確認するまで保存先変更や修復成功の判断へ進まない。

候補sourceはgofmt/Go vet/通常Go buildを成功確認し、引数なしは期待どおりexit2で拒否した。
取得したMicrosoftの公式vsbackup.hからinterface順を独立に数え、使用する5メソッドの
slot（5/6/15/35/36）がすべてsourceと一致することも確認した。実probeは未実行である。

追加review4082は固定e5cacb9dの2対象を全文読了し、Must0/Suggestion3を返した。
実Opus5.5/max/native_turn_context、succeeded/exit0/41stepsを照合し、原回答は別に保持した。
実行前に、GUID出力先の変換を自作のuintptr中継からSyscallNの直接の引数式へ直し、
cleanup前のphase保持、終了コードの意味、他の作成への一時的な影響の照合を補った。
この具体修正後もGo vetと通常buildは成功し、APIの種類や診断の目的は変更していない。

## 10:52の実probeと後始末の照合

通常AdminBrokerで1回実行し、最初のStartはS_OK、2つ目は0x80042316を返した。
Abortは0x00040202、2objectのReleaseは実行され、Goは約40msで終了した。
元の厳密なS_OK判定ではAbortを未確認としてexit1になったため、この原結果を保持した。
snapshot/point/使用/割当は前後とも0、C→C/50GiBと原MinDiff未設定を維持し、新volsnapは0件だった。

0x00040202は[EVENT_S_NOSUBSCRIBERS](https://learn.microsoft.com/en-us/windows/win32/com/com-error-codes-1)で、
イベントの購読者がいなかったことを示すCOMの成功値である。no-writersの空集合で返った実値を
0以外すべて失敗とした診断側を修正し、0とこの値だけをAbort完了として扱う。
購読者の部分失敗や未知の正値を成功へ広げず、6種類のHRESULTの実Red→Greenを確認した。
古い原票のphaseやexitは書き換えない。

`--available`はreviewの事後照合に対応する単一の空集合の通常Start/Abortだけを1回行う。
元の2object probeの再試行ではなく、解除後に通常のVSS要求が受け付けられるかを照合する。
`availability_observed`は単一Start・Abort完了・Releaseを確認したときだけtrueになる。
保存先変更や実snapshot作成は含まない。これが確認できるまではDへの変更へ進まない。

## 保存先分離の候補は撤回

C/Dの異なるNVMe・NTFS・空き・他関連付けなしという実測は保持する。
ただし既存C→Cを残した正のresizeには文書化された置換の成功経路がなく、
snapshot0でも成立しない。10/28に書込候補を撤回し、実装も読取専用にした。
詳細は[候補の撤回記録](vss-cd-positive-attempt-4011.md)を参照する。
元のC→C/50GiBと現在のsnapshotを保持し、0縮小・Delete・保護停止で切替を強制しない。
IN_PROGRESSの再取得だけでは空集合の所有同一性を証明しない点も明記する。
元4011の修復全体は未完了であり、保持中のCへの書込みを有限に切り分ける。
一次資料：

- [StartSnapshotSet](https://learn.microsoft.com/en-us/windows/win32/api/vsbackup/nf-vsbackup-ivssbackupcomponents-startsnapshotset)
- [AbortBackup](https://learn.microsoft.com/en-us/windows/win32/api/vsbackup/nf-vsbackup-ivssbackupcomponents-abortbackup)
- [Microsoft WinSDKのinterface順とCreateのinline](https://github.com/microsoft/win32metadata/blob/main/generation/WinSDK/RecompiledIdlHeaders/um/vsbackup.h)
- [ChangeDiffAreaMaximumSize](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nf-vsmgmt-ivssdifferentialsoftwaresnapshotmgmt-changediffareamaximumsize)
- [AddDiffArea](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nf-vsmgmt-ivssdifferentialsoftwaresnapshotmgmt-adddiffarea)
