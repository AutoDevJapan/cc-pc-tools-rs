# CのVSS保持失敗の調査と検証

関連Issue: [VSS保持失敗](https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/565)

修復は未完了。Cを保存元・保存先とする既存構成と50GiB上限を維持している。
10月31日06:06の照会ではsnapshot2件とSystemRestore point267がある。
10月28日15:33以降のSystemログには、容量上限による世代交代を示す33が4件あり、25はない。
この間にOSの起動時刻も変わっている。再起動を実施した主体や復帰の全条件は確認しておらず、
無人再起動・障害復帰を検証済みとは扱わない。

## 確認した障害

10月26日01:45、10月27日02:40、同07:25に、Cに対応するVolsnap25が記録された。
25の直接の意味は差分領域の拡張が間に合わずsnapshotを失ったことであり、
容量上限による最古世代の削除33、空き不足24、拡張失敗35と区別する。
イベントの処理PIDだけで、削除要求者や書込み元を特定しない。

10月27日02:23頃は使用45,781,585,920B・割当46,226,849,792B・上限53,687,091,200Bで、
2件のpersistent/client-accessible snapshotが登録されていた。
03:41の照会では同じ上限のまま使用・割当0、snapshot0だった。
消失は正常な清掃実績ではなく、過去のpointも復旧していない。
元の03:40という障害時刻表記は、イベントUTCを換算した02:40に訂正した。

## 試した対処と撤回

MinDiffAreaFileSizeを3200MiBにする有限の試行では、設定中に作られた世代も後に25で失われた。
合計割当は3,008.00→約5,873.27MiBとなり、以後6,158,565,376Bのまま使用量だけ約2.5GiB増えた。
この数値は大きい初期領域を支持するが、既存世代の末尾解放を含む推論であり、世代別割当の直接実測ではない。
合計差の受入条件がfalseだったことを、初期領域が付かなかった証拠にはしない。
有効中の未使用余裕も約365～395MiBで、初期minimumと既存領域の拡張刻みは同一視できない。
追加した値は10月27日10:10に元の未設定へ戻し、正規APIの320MiB読返しを確認した。
この設定を対策候補へ戻さず、元の実行・復帰記録も改変しない。

C→Dの切替は、既存C→C関連付けを残したまま正のresizeで置換する文書化された成功経路がないため撤回した。
snapshot0になるのを待つ工程も撤回し、書込コードは削除した。
[切替候補の撤回と残る診断](vss-cd-positive-attempt-4011.md)を参照。
既に終わった設定・作成・復帰スクリプトも、別checkoutから再実行されないよう起動時に停止する。
原資料と必要な実行記録は別に保持している。

## PITとSystemRestore

[Point-in-time restore](https://learn.microsoft.com/en-us/windows/configuration/point-in-time-restore)と
SystemRestoreは対象や保持仕様が異なるが、VSS容量を共有する。
PITが有効なら、その最大使用量の設定がVSS上限にもなる。
適格な非管理Home/Proでは既定オン、保持は最大72時間で、上限や空き不足ではそれ以前に削除され得る。
このPCの上限との数値一致やCSP値の不在だけで、有効状態や設定の所有元を断定しない。
現在、有効状態は未確認であり、設定を新造・変更していない。
PITの期限、33の上限交代、25の障害消失を区別する。

## 10月31日の現在値

| 項目 | 実測 |
|---|---:|
| Cの実空き | 768,569,294,848B |
| Dの実空き | 1,917,998,546,944B |
| Eの実空き | 858,052,489,216B |
| C→C VSS使用 | 44,226,359,296B |
| C→C VSS割当 | 44,684,214,272B |
| C→C VSS上限 | 53,687,091,200B |

snapshotは10月30日08:56と10月31日03:36作成の2件で、persistent/client-accessible/state12。
SystemRestore point267は10月30日08:55作成。
10月28日15:33以後の4件はすべてC一致の33で、照会は件数上限未達・complete・取得エラー0。
元のpoint266やそれ以前の世代の不在を、実行者まで特定した削除とは扱わない。
並行I/Oや別の清掃を含むため、実空きの変化をこの対処の回収量へ計上しない。

## 予約領域と書込み負荷の切り分け

10月31日06:06の `fsutil storagereserve query C:` は成功し、ID1の保証は約6,127MiB、
使用は約42,614MiBだった。使用が保証を上回っている現値から、保証値を即座に容量上限と解釈しない。
予約領域使用とVSS割当が近いことは関連を示唆するが、過去25の原因の証明ではない。
3回の25の前後5分には、予約領域の除外・追加失敗を示す124/125/127/128/129は取得されず、
その後の窓には追加126だけが4件あった。ログ未収録や欠落の可能性も区別する。
予約領域、更新設定、ログの有効状態は変更しない。

92秒のプロセスI/O比較ではVSS使用が2,916,352B増えたが、WriteTransferCountには
全volume・network・pipeが含まれ、Cへの実disk書込元の証拠にはならなかった。
現在の低負荷だけで過去のピークを否定しない。
次の診断は、ファイル名や生traceを保存しない単発のdisk I/O集計を対象にする。
既知の書込み元が実測できるまで、無関係なプロセス停止や生成先の一括移動はしない。

## 検証と残る条件

変更範囲のGo vet・test・build、PowerShell 5.1の構文確認と撤回済みモードの拒否を確認した。
期限テストは実在する実行物の正常対照と、同じ実行物の期限切れ未起動を比較する。
[有限の排他診断](vss-lock-probe-4011.md)で、Start/Abort/Releaseと匿名metadata取得の結果を区別した。

通常保持が続いている事実と、対処による是正の因果は別である。
実書込み元、保持失敗の根本要因、必要な対処の効果が確認できるまで、修復完了とはしない。
新しい監視・常駐・定期タスク・復元point作成ループ、snapshot削除、保護停止、上限縮小は追加しない。

一次資料:

- [MinDiffAreaFileSize](https://learn.microsoft.com/en-us/windows/win32/backup/registry-keys-for-backup-and-restore#mindiffareafilesize)
- [GetMinDiffAreaSize](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nf-vsmgmt-ivsssnapshotmgmt2-getmindiffareasize)
- [PIT](https://learn.microsoft.com/en-us/windows/configuration/point-in-time-restore)
- [ストレージ予約のID](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/ntifs/ne-ntifs-_storage_reserve_id)
