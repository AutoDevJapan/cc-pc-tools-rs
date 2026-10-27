# CのVolsnap 25と復元用データ消失（元4011、依頼元3796、親3628）

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/565

**現在（10/27 06:25）**：最初の即時受入未達後に原設定へ戻し、遅れて登録された新point263と
既存point262の2世代を保持した。4039の具体指摘を修正後、新pointを追加しない設定工程で
正規provider minimumの320→3200MiBを実確認した。C→C/50GiBと2世代は不変。
現在はDWORD3200、夜間保持と初期割当の効果はまだ未検証であり、設定だけで修復完了にはしない。

## 対象と受入条件

既存Windows VSSの保持だけを修復する。元3796の約71GiB全増加原因、保護台帳2467、
清掃3757/3942、孤児回収3755を再実装しない。OS、未完了成果、Eの既存データを保持する。
新しい常駐、定期タスク、監視、復元ポイント作成ループ、サービス停止は追加しない。

完了には、原因の種類、実変更、正常な新しい復元ポイントとpersistent C snapshotの成立、
通常I/O中の同じsnapshotの保持、上限不変、C/D/E空きの実測を必要とする。
過去の失われたポイントの復旧と、今後用の保持成立を区別する。
レジストリへの保存、成功戻り値、snapshot 0の観測だけでは修復完了にしない。
再起動と実際のOSロールバックは今回の機能確認に含めず、未検証のまま明示する。

## 修正前の実証（2026-10-27）

元3796指定の匿名JSON6件は各原pathの通常guardがclear/access_allowedを返した後に読み、
原本は変更していない。Cの元volume/保存volumeをWindows登録で照合した。
02:23頃には使用45,781,585,920B、割当46,226,849,792B、上限53,687,091,200B。
10/26 04:00:22と13:10:57 JSTに作られたpersistent/client-accessible C snapshot 2件があった。
03:41の原観測では使用/割当0、同じ上限、C snapshot 0。これは清掃成功ではない。

本担当04:06頃もC→C/50GiB、使用/割当0、snapshot/復元ポイント0を実確認した。
Microsoft software provider 1件。SR既存タスクは有効、最終実行10/26 06:36:53/結果0。
SystemRestoreのRPSessionInterval=1、SystemRestorePointCreationFrequency=0を保持し、
DisableSRの明示停止はなかった。WindowsBackupのAutomaticBackupは未登録。
Defrag既存タスクの最終実行は10/21であり、今回の時刻とは一致しない。
writer 11件はNo error、MSSearch/WMIがstate 5、他はstate 1。
state 5はバックアップ完了待ちであり、failedと誤記せず、サービス再起動を行わない。

**原記録の時刻換算を訂正する。** Windows原イベントXML/固定metadataのUTCは
`2026-10-26T17:40:04.7968196Z`、JSTは**10/27 02:40:04.7968196**。
依頼文/原差分票の03:40表記はUTC+9との不一致であり、原資料を保持して訂正する。
直前の2件登録02:24→02:40の消失とも整合する。もう1件は10/26 01:45:13.6862611 JST。
PID4はイベント処理元であり、削除を要求したプロセスの証明ではない。

volsnapはclassic providerなのでEvents列挙が空だった。インストール済みvolsnap.sysの
定義はEventLogRecord.FormatDescription(IEnumerable<object>)に16個の固定値を渡して取得した。
原イベントのMessageや挿入される私的値は出力していない。
定義25は差分領域の拡張が間に合わず全世代を失うもの、24は空き不足の警告、
35は拡張失敗、33は上限以下に保つための最古世代の削除であり、区別する。

最新2回の25の各前後5分には、指定したdisk/Ntfs/storage-driver/volmgr/partmgr/
FilterManagerとVSS/WindowsBackup/SystemRestoreの追加warning/errorは取得されなかった。
取得上限200未到達。直近I/OはC約1.8～3.9MB/s、平均write約0.10～0.21ms、queue 0。
これで過去の一時的I/O負荷や具体プロセスを否定できない。過去のI/O時系列は未取得。
実証した直接の失敗は**C自身に置かれた差分領域の拡張遅延**であり、ハード故障や
外部からの明示削除、50GiB上限到達を確定したものではない。

本担当開始時の空きはC354,815,750,144B/D2,145,847,541,760B/E858,052,489,216B。
Cの回収0。C空き差とVSS割当差の残り約7.185GBは元3796の未解明範囲として保持する。

## 修正した具体手順（未適用・追加レビュー前）

初回の51200MiB案は適用しない。初期確保がsnapshotごとなら2世代が既存上限へ
収まらない可能性を指摘されたため、**3200MiB（3.125GiB）**へ候補を修正した。
snapshotごとに初期確保される場合の2世代分は6.25GiBで、50GiB上限に対し43.75GiBを残す。
初期割当を大きくして拡張頻度を減らせるかは、増分単位が未確認の仮説として実測する。
以前の失敗は使用約45.8GBで発生しており、3.125GiBの設定だけで解消する保証ではない。
通常の世代保持と実際の拡張を確認してから効果を判定する。旧50GiBのminimumは未適用である。

`configure.ps1`は既存C→C/50GiB、MS provider 1、他volume snapshotなし、明示DisableSRなし、
事前割当後も200GiB以上空くこと、MinDiffAreaFileSize未設定、出力receipt未作成を再確認する。
既存VolSnapキーのDWORD **MinDiffAreaFileSize=3200**だけを書き、before不存在/target/実readbackを保存する。
空の原設定を新造せず、関連付け/上限/作成頻度/既存snapshotは変更しない。
このminimumはPC共通なので、今後別volumeやbackupの小さい上限を追加する担当には本記録と
元3796/3628への返却を参照させ、元4011がその新しい保持仕様まで検証したと扱わない。

追加レビューMust解消後、以下を同じ担当が通常AdminBrokerから有限に行う。

1. 変更直前の匿名観測を保存し、configureの実readbackを確認する。変更時刻を以降の
   `observe.ps1 -SinceUtc`の開始点にし、0件と取得失敗/上限到達を区別する。
2. `create-point.ps1 -Stage A`で通常SystemRestore BEGIN/ENDを一度実行する。
   既存creation frequencyは0のまま。事前token/実戻り値/登録されたown descriptionと
   sequence/作成後tokenを保存する。新snapshotを1件に絞れなければ対応不明として全て保持する。
3. AのBEGIN直前/END直後のused/allocated/maxを同じreceiptへ保存し、割当増3200MiB以上かつ
   割当増−使用増2880MiB以上、上限50GiB不変をコードで確認する。自然な通常書込の増加を
   minimum反映と誤認しない。BもAの実測から同条件を再計算し、不成立ならBを作らずrollbackする。
   Bの前後差も記録し、初期確保がsnapshotごとかを断定せず実測する。
4. `-Stage B -FirstReceiptPath <Aの実receipt>`で2件目を一度実行する。
   Aのtokenが残っていることと、quota内に新しい初期割当+1GiBの余地があることを直前確認する。
   A/B両方のpoint・token・persistent/state12・既存世代不減を確認する。
5. 新しい24/25/33/35/36、取得失敗、部分取得、作成失敗がないことを確認する。
   33も「正常なcleaning成功」とせず、今回の新規作成で保持世代を押し出していないか判定する。
6. 通常I/Oで少なくとも初期割当を超える拡張1回と、A後からの使用増1GiB以上を観測する。
   さらに01:45/02:40の両時間を含む次の1夜を経て、2token保持/上限50GiB/新25なしを確認する。
   これは以前の約45.8GB全負荷の再現ではないので、観測差分・増加速度と限界を併記する。
   観測は適用から最大48時間・最大2夜。最大値まで上の実拡張/使用増に届かなければ
   「新しい保持は確認、25対策の効果は未検証」とし、担当がminimumだけrollbackして再調査する。
   次の夜間確認には既存ToDoの`wait-retry --after ... --reason ...`を使い、
   新しい監視/task/point作成loopを追加しない。未完を全体完了にしない。

rollbackの実行担当も元4011とし、条件は割当が想定と違う、24/25/33/35/36の新規発生、
point作成失敗（既存SRの実行結果を含む）、保持対象token/世代数の減少である。
`rollback.ps1`は4011のbefore不存在/target3200/永続request/現在DWORDを
照合し、**本担当が加えたMinDiffAreaFileSize値だけ**を元の未設定へ戻す。
値が変わっていたら他担当の変更を保持して止め、snapshot・関連付け・上限を削除/縮小しない。
registry readbackの不存在とdriverの実効値復帰は区別し、即時の効果を未確認のまま断定しない。
反映に再起動が必要と判明した場合も先に自分の値を戻し、必要な復帰条件を既存窓口で準備する。
本人にPC操作を割り当てたり、未検証のログオン復帰を前提に再起動したりしない。

作成呼出し前は「要求結果不明」のphaseを先に保存し、タイムアウトを未作成と断定しない。
BEGIN/ENDの非0でも実登録を読める場合は先に記録し、同名/既存receiptがあれば自動再作成しない。
実操作のrequest/begin/end-request/resultは`.ci-work/4011/os-v2`の固定名へCreateNew/Flush(true)で
追記専用に保存する。部分ファイルでも次の実行を拒否し、任意OutputPathでstage再作成を回避できない。
configurationのrequestは値を書いた直後のクラッシュにも残り、成功result保存前でも自分の値を戻せる。
既に未設定ならrollbackで変更せず区別し、driver実効値は未確認と明記して現在token/割当も保存する。
成功したBEGINはbegin結果票の保存失敗時もfinallyで通常ENDを呼び、OS変更中の単純な中断を減らす。
writerは`observe.ps1 -IncludeWriters`で作成の前後だけ取得し、通常観測は実行しない。
SinceUtcにはZまたは明示offsetを要求し、volsnap_query欠落/complete false/errorsありは取得失敗とする。
SystemRestoreの選択的追跡を一般VSSの全ユーザーファイル保護と同一視せず、未分類Cファイルや
snapshot内容を別path/コピーで回避して読まない。過去point復旧/実OS rollbackは未実施のまま区別する。

## 一次資料

- [Microsoft: diff areaのminimum、値の単位とPC共通範囲](https://learn.microsoft.com/en-us/windows/win32/backup/registry-keys-for-backup-and-restore#mindiffareafilesize)
- [Microsoft: resizeはsnapshot消失のおそれがある](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/vssadmin-resize-shadowstorage)
- [Microsoft: CreateRestorePointの戻り値と作成頻度](https://learn.microsoft.com/en-us/windows/win32/sr/createrestorepoint-systemrestore)
- [Microsoft: 実値を置換してイベント定義を取得](https://learn.microsoft.com/en-us/dotnet/api/system.diagnostics.eventing.reader.eventlogrecord.formatdescription)
- [Microsoft: writer state 5はbackup complete待ち](https://learn.microsoft.com/en-us/windows/win32/api/vss/ne-vss-vss_writer_state)
- [Microsoft: SystemRestoreが追跡するファイルの範囲](https://learn.microsoft.com/en-us/windows/win32/sr/calling-srsetrestorepoint)
- [Microsoft: 同時snapshot作成のVSS排他](https://learn.microsoft.com/en-us/windows/win32/api/vsbackup/nf-vsbackup-ivssbackupcomponents-startsnapshotset)

MinDiffAreaFileSizeの現行記述は32MBの倍数・3200の例を含む一方、Vista/Server2008等の
古い版には300～3000MB/300MB単位の注記がある。このPCのbuild26300への実効は実割当で判断する。
minimumが上限を超えるとshadow copyが正しく機能しない場合があるため、将来の小さい
別volume/backup設定まで検証済みとせず、現在の1関連付けだけで適用を判断する。

私的な匿名実測とBroker request/戻り値は自分の`.ci-work/4011/`へ保存し、公開ソースには
volume GUID、ホスト名、元ファイルpath、タスクの引数、実データ、認証情報を含めない。

## 適用前レビューの結果

固定base461e0ab5→a229c070の6ファイルを、通常の独立レビュー4014へ登録した。
実job20261027-041610-todo-4014、最終succeeded/exit0/16steps。
正常statusのexecution_observedはnative_turn_context/anthropic/claude-opus-5-5/maxで、
author/excluded gpt-6.1-solと異なる実行を確認。固定6ファイル全文読了、Must2/Suggestion7。
原応答6548字は自分の.ci-workへ保持し、SHA256は
E1E5F63F1297860FD36030C450EEA2C25D4BE39A231A59FEB5C40F34C6449BE1。
Mustはmin=maxの根拠不足/世代減少リスクと、2世代確認・33/36・rollback条件の不足。
上の3200MiB候補とA/B作成、事前/事後token、rollback、0件判別へソースを修正した。
修正後の追加固定レビュー・実OS適用・実保持はまだ未完了であり、Must0とは扱わない。
他のSuggestionのうち将来backup影響/有効範囲/障害時間帯/phase/登録先行/空イベントも反映する。
OS変更は0、Dへの無損失移行を現在0件だけで実行できるとは断定しない。

SystemRestoreのScopeSnapshotsは未設定。Microsoftの仕様上、ユーザーTemp canaryの
保持を一般VSS全volume保護やSystemRestoreの必須機能と同一視できない。
OSファイルを直接hashする案はそのC pathの通常guardが分類未確認で実施していない。
既存backup関連taskの限定metadataでは10件中9件Microsoft namespace、他1件CC namespace。
後者のVSS使用/保持仕様は未確認であり、他のbackupが存在しないとは断定しない。
指摘と実保持への影響を解消してから設定を適用する。

## 4020の結果と実適用前の追加修正（2026-10-27 05時台）

元4020は固定a229c070→e1c3ffefの6対象を全文読了、Must1/Suggestion7。
実job20261027-044913-todo-4020、succeeded/exit0/16steps、execution_observedは
native_turn_context/anthropic/claude-opus-5-5/max。原応答3810字/SHA256
CD695B03DFF92A130A3BCCA28965C9B284EFBCB02992EEB1F27E032F7E0ACF10を保持した。
Mustの実割当前後差の保存とB前提の再計算、Suggestionの固定request/クラッシュ復帰/空時刻/
writer任意/有限2夜/上限注意をソースと上の手順へ反映した。

同じ保持確認で2回指摘されたため、receiptを上書きする方式をやめ、固定名の永続requestを
通常CreateNewで確保する設計へ整理した。common.ps1で実割当判定を共有し、通常書込増・
既に大きい割当・不足未使用量・上限変化・減少を拒否する8ケースと、固定path/二重claimの
2ケースを実PS5.1/PS7で成功。PS5.1でGet-FileHashが使用不能だった問題は.NET SHA256で修正。
observerの未来の空窓/offsetなし拒否/writer省略は読み取り専用AdminBrokerで確認したが、
未来の空窓を保持成功の根拠にはしていない。現7sourceは構文確認成功。

05:18の改めての匿名観測でもC→C/50GiB・used/allocated0・snapshot/point0・writer11No error。
C352,599,576,576/D2,115,095,773,184/E858,052,489,216B free。OS変更はまだ0。
過去の04:00:22snapshotには0.14秒後のSystem Restore8194成功、11秒後のscoping8300があり、
少なくともこの1件はSystemRestore作成との時刻相関がある。13:10:57のもう1件は前後3分に
対応する同providerイベントなし。両方をSystemRestore/全volume保護と断定せず、原本を保持する。
System-Restoreのprovider正式名をcorrelateへ追加し、2回の25の前後5分の既存結論不変を実確認した。
この修正後の固定レビュー/Must0と実適用は、まだこれからである。

## 4028の受領と実OS適用（05:53～05:59）

4028は固定e1c3ffef→d2a3aaf3の9対象全文を読了、旧Must解消/新Must0。
実job20261027-053028-todo-4028、succeeded/exit0/32steps、execution_observedは
native_turn_context/anthropic/claude-opus-5-5/max。原応答3506字/SHA256
C6E06B8DD21CAC5282A7B2FF9F480991D770E39EF0851C6FC7B5CFE04A519A3Aを保持。
後続の1344aa13はRESUMEだけの変更で、実装byteは同じ。CodeQLの全5チェック成功も確認した。
通常AdminBroker内でcommon読込と10guard成功を確認し、設定/作成/rollbackを各1依頼へ分けた。
観測・rollback判断が終わるまでこのworktreeと無視対象のos-v2を保持し、別checkoutから実行しない。
observerの出力先にはos-v2を指定しない。原request/resultを上書き・削除しない。

05:53直前には本担当以外で05:43作成されたpoint262とpersistent C snapshotが1件あったため、
以前の0件観測を流用せず、新たな既存世代として保持した。05:54:19のconfigureは終了0で
3200DWORDの保存・型・readbackを確認。C→C/50GiBは不変。
05:54:32のA要求BEGIN/ENDは共に0だが、05:54:46直後のCIM登録はまだ旧1件のみだった。

| 同じA要求の実測 | BEGIN直前 | END直後 | 差 |
| --- | ---: | ---: | ---: |
| 使用B | 2,671,165,440 | 2,871,148,544 | +199,983,104 |
| 割当B | 3,154,116,608 | 6,158,565,376 | +3,004,448,768 |
| 上限B | 53,687,091,200 | 53,687,091,200 | 0 |

未使用の増分2,804,465,664Bは受入の2880MiBに未達、割当増も3200MiBに未達だった。
不成立receiptを当時のまま保存し、05:55:24に自己DWORDだけを正常rollback/終了0で未設定へ戻した。
snapshot削除/上限変更/関連付け変更/サービス停止/OS再起動は0、Bを作成・Aを再試行していない。
この保守的な未達は「設定は絶対に効かない」という証明ではない。

rollback時には新tokenが現れ、05:56の正常再照合でown description完全一致point263/type12と
新persistent/state12 snapshot（作成05:54:41.991208）を確認。SystemRestore8194成功05:54:42.052986、
8300 scoping05:54:53.010932とも整合する。元の262と新263は両方存続した。
要求の成功戻り値とCIMで読めるまでの時間差が実証されたので、非0/unresolved票だけで未作成と断定しない。
新しいreconcile票へ結果を保存し、Aの原resultを成功へ書き換えていない。

05:55:53の使用3,513,860,096B/割当6,158,565,376B/max50GiB。
空きC351,853,670,400/D2,121,461,518,336/E858,052,489,216B。並行I/Oを含み回収量ではない。
設定開始後のvolsnap新イベント0、complete true/errors0だが、短期の保持に限る。
Goの有限・読取専用`minimum_windows.go`で正規IVssSnapshotMgmt2.GetMinDiffAreaSizeを照会し、
05:58:56 HRESULT0/335,544,320B（320MiB）を実取得した。登録やdriver全体の復旧の証明ではない。
Goのchanges0は「設定変更の関数を呼ばない」というコード上の記述で、COM起動に伴う
手動サービスの開始などを実測して0と保証する値ではない。
Go通常build/vet成功。API GUID・vtableはMicrosoft WinSDK vsmgmt.hと照合し、
snapshotの作成/削除/移動/resize、volume内容の読み取り機能は持たない。

別の有限匿名診断では、最新2件の25のIO_ERROR_LOG_PACKET固定ヘッダはFinalStatus0/Unique0xA8、
24のみFinalStatus0xC000007F、35は0/Unique0xA5。可変dump/生binary/挿入文字列/Messageは非出力。
optional FinalStatus0は成功や具体IO障害不存在の証明ではなく、原因の直接経路は従来の拡張遅延のまま。

## 次の限定是正候補：既存2世代を保ったままminimumを正規APIで確認する

旧Aの即時gateを緩めたり、失敗claimを消したり、新しいpathでAをやり直したりしない。
現在は必要2世代が実際に存在するため、次の候補は**新しいpointを一切作らず**minimumの
正規provider値と、既存2世代の通常I/O中の割当拡張を確認する一度きりの設定工程である。
3200がCIMの厳密な初期差へ現れるかと、現在のprovider報告値は別の証拠として扱う。

`reactivate-minimum.ps1 -Mode Apply -QueryExe ... -QueryExeSha256 ...`は、既存os-v2へ
新しいreactivation-requestを追記する。元configuration/request・rollback/result・A不成立票があること、
現在minimum未設定/provider320MiB、MS provider1、C→C/50GiB、他volume snapshot0、
明示DisableSRなし、空き>=3200MiB+200GiB、既存point263と原2token/persistent/state12を確認する。
新しい同段階のrequestがあれば実行しない。読み取りhelperは直前に作った自分のGo byteのSHAを照合。
DWORD3200を一度設定し、正規provider値が3200MiB/readback型値一致/全旧token存続を確認して記録する。
未達/例外時は自分が書いた同型同値だけを元の未設定へ戻す。初期割当の成功には読み替えない。

以後の通常I/Oで、既存2tokenとpoint262/263、上限不変、新24/25/33/35/36なし、
provider minimumと実割当の拡張量/増加速度を観測する。前後のsample時刻とtoken集合を保存し、
**集合が同じ区間の割当増だけ**を既存世代の拡張として扱う。集合に新tokenが現れた区間は
本担当以外の新世代作成を含む区間として分け、合計割当増を既存世代の拡張やminimum効果へ流用しない。
Microsoftが保証するのは初期minimumなので、既存世代で拡張の違いが出ないだけでは無効と断定しない。
通常の新世代について作成前後を明確に照合できた場合だけ初期割当の別証拠にする。
default時と大きなminimum時を区別し、少なくとも同じtoken区間の実割当増/使用増1GiB、
次の01:45・02:40両時刻を含む1夜の保持を確認する。刻みや初期予約の因果が不明なら効果未検証と記録する。
担当から新しいsnapshot/pointは追加しない。最大48h/2夜、初期効果の証拠未達または異常なら
`-Mode Undo`でこの追記requestに対応する自分の3200DWORDだけを未設定へ戻す。
別の値や型なら保持し、registryが不存在でも区別して記録する。必要pointを消して見かけ上改善しない。
記録は同じworktree/os-v2のまま保持し、原結果を消す再試行・審査拒否の迂回は行わない。
この限定候補は4039の確認と指摘対応を経て適用する。原Aの厳密gate未達を成功へ変えるものではない。

PS5.1/7で8source構文確認とA判定receiptのJSON往復テスト成功。新しいsettingの実適用/
途中停止後の動作はまだ未検証。既存claimの維持と新phaseを明示し、新たなpoint作成loopを作らない。

追加一次資料：

- [Microsoft: GetMinDiffAreaSize](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nf-vsmgmt-ivsssnapshotmgmt2-getmindiffareasize)
- [Microsoft WinSDK: COM interface GUIDとvtable](https://github.com/microsoft/win32metadata/blob/main/generation/WinSDK/RecompiledIdlHeaders/um/vsmgmt.h)
- [Microsoft: IO_ERROR_LOG_PACKETの固定フィールドとoptional status](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/wdm/ns-wdm-_io_error_log_packet)

親への最初の実適用結果note保存は共有ToDoの64MiB上限で失敗したため、自己4011の過去メモ7件だけを
正規archive-historyで原文21,443B/ハッシュ/通常history読返しを保って別保管し、3796/3628への
同結果保存を成功させた。他担当の履歴や状態を清掃/改変せず、この修復を容量清掃成功に加算しない。

06時台の追加照合：既存point262の作成要求者は未特定であり、Windows自身の自動作成とは断定しない。
05:57:23のSystemRestore8303/0x80070057は、SnapshotPathと現在のDeviceObjectを匿名照合して
point262側のtoken305F...へ一致した。今回追加したA側のtokenEFA3...は06:09:53の
8302/ErrorCode0へ一致し、対象整理を正常完了した。2世代ともpersistent/state12を保持した。
同providerには過去の8303/80070057と8302/0が混在しており、8303が25の原因だと因果を確定しない。
約1000万files等のcounterはC全域の独立棚卸しではなく、このSystemRestore処理が返した数値である。
通常権限の最初のtoken照合はCIM初期化失敗で0件と表示されたため採用せず、失敗票と
AdminBroker正常再照合を分けた。06:11使用5,523,341,312B/割当6,158,565,376B/max50GiB、新volsnap0。
TaskScheduler Operationalは元からdisabledで履歴未取得。新たな有効化/監視を追加していない。

4039は固定1344aa13→1109ff05の4対象全文と必要参照を読了、実Opus5.5/max/succeeded/exit0/20steps。
新pointを作らない方針とGo ABI/解放は妥当、Must3（Apply失敗票/Undoのhelper依存/初期と拡張の混同）。
実装側で失敗票に返ったprovider値・自己値/型・復帰/readbackを保存し、hash検査をgetter直前へ移動した。
Undoはregistry結果を先に保存し、helper不在/不一致でもregistry復帰を妨げない。metadata取得失敗も記録する。
成功出力をApplyのcatch外へ移し、quota余地も追加した。観測条件は上の同token/新token区別へ修正。
既にレビューした目的と操作範囲を広げず、3件の具体修正を構文/JSON往復と実操作の結果で確認する。
未実行の異常経路やdriverの再起動後の状態を検証済みとは言わない。

## 06:24の限定設定工程の実結果と次の確認

4039の原応答3151字/SHA256
9AD4FD5C999B3B1488B5F023ECDD30A658536C86269C99B03D88E1E4D3A12E92を保存。
指摘3件を164fbeadで修正し、PS5.1/7の8source構文とJSON往復を成功確認してから、
同じ元担当が通常AdminBrokerの1依頼で新Applyを一度だけ実行した。
06:24:57～58に正規GetMinDiffAreaSizeが335,544,320→3,355,443,200B、両HRESULT0を返し、
registry3200DWORDと全原token存続を確認した。新snapshot/point作成0、snapshot削除0、上限/関連付け変更0。
これはproviderの現在値の確認であり、初期予約差や25防止効果を成功へ読み替えていない。

| 同じtoken集合の最初の区間 | 設定直前 | 06:25:38 | 差 |
| --- | ---: | ---: | ---: |
| 使用B | 7,977,254,912 | 8,578,416,640 | +601,161,728 |
| 割当B | 8,507,375,616 | 8,977,137,664 | +469,762,048 |
| 上限B | 53,687,091,200 | 53,687,091,200 | 0 |

両token305F.../EFA3...・point262/263・persistent/state12を保持、新volsnap0/complete true/errors0。
これは時刻を区切った既存集合の割当変化であり、拡張操作の正確な回数や新世代の初期割当とはしない。
06:25空きC349,132,865,536/D2,088,747,687,936/E858,052,489,216B。並行I/Oを含み回収量ではない。
原Aのgateは不成立のまま保持し、StageBは未実行。失われた過去のpointを復旧したとはしない。

現在の増加速度では50GiBの通常世代交代が夜間より前に起きる可能性もあるため、まず既存ToDoの
wait-retryで**10/27 07:00頃の1回**を再開し、実保持/設定/イベント/新tokenを確認する。
次に必要な夜間観測を行い、最大48h/2夜（10/29 06:25まで）を超えて確認を繰り返さない。
新しい監視/task/point作成ループは追加しない。33による通常のquota世代交代と25による全消失を
区別し、未確認の旧世代の必要性や新世代の初期量を推測で埋めず、実際の保持結果として返す。
異常/受入未達ではreactivateのUndoで自己DWORDだけを元へ戻す。Undoはhelper不在でも
registry復帰と結果保存を先に行うので、Tempは必要source/manifest保全後に正規回収できる。
必要なら次回、自分の新Tempへ同じGo sourceを通常buildしてqueryを再取得する。

06:28の追加端点は同じ2token/point262263、used9,768,173,568/allocated10,151,542,784/max50GiB。
設定直前との差はused+1,790,918,656B/allocated+1,644,167,168Bで、同じ集合での増分を実測した。
新volsnap0/complete/errors0、registry3200を保持。初期予約の効果の証明には流用していない。
実空きC347,923,210,240/D2,081,290,506,240/E858,052,489,216B。原票を上書きしない別観測として保存。

## 10月27日07:16の有限な保持確認

前回の再確認時刻を経て、同じ4011がjob20261027-071223-todo-4011として通常再開した。
bf2462cd/同branch/未コミット0を確認し、新しい設定変更・point作成・削除を行わず、
既存AdminBrokerで匿名metadataと正規GetMinDiffAreaSizeを取得した。

| 項目 | 設定直前06:24 | 今回07:16 | 差 |
| --- | ---: | ---: | ---: |
| CのVSS使用B | 7,977,254,912 | 18,427,707,392 | +10,450,452,480 |
| CのVSS割当B | 8,507,375,616 | 18,842,140,672 | +10,334,765,056 |
| C→Cの上限B | 53,687,091,200 | 53,687,091,200 | 0 |

両端のtoken集合は同じで、point262/263と対応するpersistent/state12の2世代を保持した。
registry3200を保持し、07:16:22の正規APIは3,355,443,200B/HRESULT0を返した。
06:24:57.8094956以後のVolsnapイベントは0件、上限100未達、complete true/errors0。
最初の照会失敗票を正常0件へ置き換えておらず、正常結果は別の原観測として保存した。

最初の呼出側PS7でConvertFrom-JsonがUTCをDateTimeへ変換し、launcherの文字列展開が
offsetを落としたことを実sourceで確認した。observerは意図どおり照会を拒否した。
原UTCを-DateKind Stringで保持して別launcher/出力へ取り直し、設定や検査条件は変更しなかった。
比較の最初のコンソール表示はOrderedDictionaryの射影でnullになったが、保存JSONには
実値があり、通常のJSON再読込後に全数値を照合した。原票は保持する。
read-only Go helper内のjob欄はBrokerの環境でnullだが、元呼出しのjob・実行物SHA・
normal Brokerの依頼/応答との対応を同担当のmanifestと比較票へ残した。

実空きはC339,110,768,640/D2,096,728,371,200/E858,052,489,216B。
並行I/Oがあり、これらの差を本担当の清掃回収量にしない。失われた過去のpointの復旧でもない。
実際の2世代保持と同じ集合の割当増1GiB以上は確認できたが、新世代の初期予約効果、
夜間の保持、25の再発防止効果は未検証のまま残す。

次の固定確認を**10/27 10:00 JST**とする。今回の割当増に対して50GiB上限の余地を
確認する受入の区切りであり、毎時の監視や新しい定期taskは作らない。
異常がなければ**10/28 03:00 JST**に01:45/02:40を通過した後の保持を確認し、
最大48h/2夜、**10/29 06:25 JST**の期限は延長しない。新世代が現れた区間を
既存世代の拡張へ流用せず、33による正常世代交代と25による全消失を区別する。
受入未達/異常時の自己設定復帰はreactivateのUndoで行い、原snapshotや原票は保持する。
実タイマーの受付と状態読返し、親への結果返却は別に確認する。

sourceを変えていないbf2462cdのCodeQL全5チェックがSUCCESSであることを確認した。
今回の変更は実測と再開情報の文書だけであり、製品全体のビルドは追加していない。
PR568は現用設定の受入未完を示すdraftのままであり、merge/全体完了とは報告しない。

## 07:25の再発と10:10の自己設定復帰

10月27日10:10の通常AdminBroker観測は、C→C/使用0B/割当0B/上限50GiB、
C snapshot0/restore point0をerrors0で返した。設定適用以後の新イベントは、
**07:25:22.7070176 JST**のC一致Volsnap25/error（UTC22:25:22.7070176Z、record186112）1件だった。
照会上限100未達/complete true。07:16に確認した2世代は失われ、33の正常世代交代ではない。
PID4はevent処理元であり、削除要求者や書込元を証明しない。
過去のpointと今回の2世代の消失は、どちらも正常な清掃成功へ分類しない。

10:10:49の正規GetMinDiffAreaSizeは3,355,443,200B/HRESULT0だったため、既定の
受入不成立条件に従い、reactivate-minimum.ps1 -Mode Undoを同じos-v2から一度だけ実行した。
10:10:57に本担当のDWORD3200だけを元の未設定へ戻し、320MiB/HRESULT0を確認した。
元request/apply-resultと旧configuration/A票を保持し、Undo結果を新しい追記票へ保存した。
snapshot削除・上限縮小・保存先変更・VSS/System Protection停止・OS再起動は0。
現在のC→C/50GiBも保持する。設定読返しと短期の保持は、修復成功の証明にならなかった。
既存世代への今回の再適用で再発を防げなかったことは実証されたが、未実証の新世代の
初期予約効果まで不存在と断定しない。同じ設定実験の再試行やpointの反復作成はしない。

再発前後5分の既存System/Applicationログを有限に照合し、対象のstorage-driver/VSS関連の
追加warning/errorは見つからなかった（件数上限未達）。10:12の3つの現在IOサンプルは
低遅延だったが、07:25のIOピーク・実プロセス・直接の下位ドライバ障害は未特定のまま。
元からdisabledのTaskScheduler Operationalを新しく有効にしていない。
10:10実空きC439,135,739,904/D2,105,684,430,848/E858,052,489,216B。
並行I/Oや他の清掃を含み、C空きの変化をVSS解放の総量や本担当の回収量にはしない。

保存先分離の検討に必要な通常metadataとして、C=disk0、D=disk1、両方NTFS/NVMe/
Healthy/Onlineを確認した。Dはboot/systemでなく、Dに関する既存shadow storage/snapshotはない。
Dの既存ファイルや私的データは閲覧・移動しない。この確認だけでDへの変更を実行しない。
通常VSSの排他とAPIの保持条件を、一次資料と別モデルの確認を含めて具体化する。
現在0件を二度読んだだけの手順、0への縮小・Delete・保護停止で切替を強制する手順は採用しない。

参考にした一次資料は、[StartSnapshotSetの単一作成操作](https://learn.microsoft.com/en-us/windows/win32/api/vsbackup/nf-vsbackup-ivssbackupcomponents-startsnapshotset)、
[ChangeDiffAreaMaximumSizeの条件と戻り値](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nf-vsmgmt-ivssdifferentialsoftwaresnapshotmgmt-changediffareamaximumsize)、
[AddDiffAreaの既存関連付け・既存shadow制約](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nf-vsmgmt-ivssdifferentialsoftwaresnapshotmgmt-adddiffarea)。
これらを移行の実成功や原子的な切替の実証として引用せず、実施前の条件確認に使う。

## 追加診断の実結果と次の候補（11時台）

4082の固定2対象の別モデルレビューはMust0で完了し、実Opus5.5/max/native_turn_context/
succeeded/exit0/41stepsと原回答を照合した。pointerの渡し方等の具体指摘を修正してから実診断した。
元probeで2つ目のStartが80042316、Abortが00040202を返した原票は未確認phaseのまま保持し、
後者がCOMのEVENT_S_NOSUBSCRIBERS成功値であることを一次資料と6種類のRed→Greenで確認した。
10:58の別の単一Start/Abort/Releaseは通常に戻り、11:19には別processの拒否と、排他中の
匿名preflightが正常に読み出せることを実測した。保存先変更やsnapshot作成/削除は行っていない。

通常可用性の確認後もC→C/50GiB/使用割当0、snapshot0/point0。新volsnap0/照会complete true/errors0。
11:00実空きC438,636,896,256/D2,098,001,723,392/E858,052,489,216B。
既存VSS/SystemRestoreログの同窓にはwarning/errorがなく、情報8224が1件だった。
これを復元用データの保持修復や原pointの復旧と扱わない。

次の具体的な限定候補は[正の50GiBだけを使うC→Dの試行](vss-cd-positive-attempt-4011.md)。
別モデルで書込手順とsourceを確認してから適用する。現在この書込は未実行であり、
空の旧設定を新造したり、0への縮小/Delete/保護停止で関連付けを強制しない。

## 21:52以降の再開と、現存snapshotを保持する分岐

4092の再開実job20261027-212901-todo-4092は、固定ecc24406の5対象全文を確認してMust0と
修正提案を返した。実Opus5.5/max/native_turn_context、succeeded/exit0/18stepsと原回答を受領し、
初回195832のfailedを成功へ数えなかった。修正は[切替候補の文書](vss-cd-positive-attempt-4011.md)へ
各指摘に対応して記録した。現用OSの保持修復成功とは別の結果である。

21:52に本担当が作っていないC snapshot3件、SystemRestore point264/265が存在した。
snapshot作成は12:35:24/12:35:55/13:28:13JST。3件ともpersistent/client-accessible/state12であり、
最後の1件だけno-writersだった。元のpoint262/263や原3796の旧pointが復旧したとはしない。
明示ToDo/ownA markerなし、CCの既知38taskでVSS command一致なしだが、実要求者は未特定のまま。
これだけでWindows自動作成や他担当の修復成果とは断定しない。

22:02:43～46に修正したrun-once -Mode Inspectを通常AdminBrokerで実実行し、
実HEADとsourceの未変更・固定hash・pin後のfresh build・起動/回収を確認できた。
別processのStart拒否とmetadata取得後の別objectのStart拒否は両方80042316、
親は所有1/Abort完了1/Release1、timeoutなしで終了した。
3snapshot/2point/既存割当を検出してlive_preflight_not_met_no_writeとなり、resizeの起動は0。
原request/resultを同os-v2の別工程へ追記し、positive-cdの試行票は消費していない。
snapshotを削除したり0件条件を緩めて書込へ進んだりしない。

C→C/上限53,687,091,200B、MinDiff未設定を維持。22:02は使用13,475,987,456B/
割当13,943,291,904B、保持中空きC428,950,200,320/D2,077,171,159,040B。
Eの直近空き858,052,489,216B。並行I/Oを含み、本担当の清掃回収量ではない。
21:52までの新volsnapは0件/complete true/errors0だが、現存世代の夜間保持や是正の因果は未証明。

新しい[Point-in-time restore](https://learn.microsoft.com/en-us/windows/configuration/point-in-time-restore)は
SystemRestoreと保持・保護範囲が異なるため、一般VSSと混同しない。今回の限定読取では、
既知CSPのnumeric設定値とMDM_Recovery class metadataは得られず、このPCの有効状態の証明にはならない。
元VerifyWinRE disabled/SR Readyは変更していない。原所有・保持仕様や不明な私的範囲を新造しない。

現在の3件を保持したまま01:45/02:40を通過した事実を有限に確認し、
データを失わない具体的な修復工程を継続する。設定変更や関連付け保存だけで全体完了にはしない。
本日の最新owner-pr-review方針に従い、マージ前には別セッションのセルフレビューとラバーダックを
同じ最終コミットへ実施し、修正指摘がすべて解消したことと必要CIの成功を確認する。
