# CのVolsnap 25と復元用データ消失（元4011、依頼元3796、親3628）

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/565

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
2世代の初期確保合計は6.25GiBで、50GiB上限に対し43.75GiBの余地を残す。
これは小さな初期割当による拡張の頻度を減らす候補であり、その後の拡張をなくす保証ではない。
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
3. 実割当が少なくとも3200MiBへ増えたこと、persistent/state12、事前token保持を確認する。
   割当が320MiB等のままなら即時反映を証明できないので次のpointを作らずrollbackする。
4. `-Stage B -FirstReceiptPath <Aの実receipt>`で2件目を一度実行する。
   Aのtokenが残っていることと、quota内に新しい初期割当+1GiBの余地があることを直前確認する。
   A/B両方のpoint・token・persistent/state12・既存世代不減を確認する。
5. 新しい24/25/33/35/36、取得失敗、部分取得、作成失敗がないことを確認する。
   33も「正常なcleaning成功」とせず、今回の新規作成で保持世代を押し出していないか判定する。
6. 通常I/Oで差分が増えて初期割当を超える拡張を観測し、2token保持と上限50GiB不変を確認する。
   01:45/02:40の障害時間帯か、障害前に相当する実使用増を含む有限な観測まで継続する。
   短い低負荷区間だけで「25を解消」と完了しない。時間が必要なら既存ToDoの正規
   `wait-retry --after ... --reason ...`を使い、新しい監視/task/point作成loopを追加しない。

rollbackの実行担当も元4011とし、条件は割当が想定と違う、24/25/33/35/36の新規発生、
point作成失敗（既存SRの実行結果を含む）、保持対象token/世代数の減少である。
`rollback.ps1 -AppliedReceiptPath ...`は4011のbefore不存在/target3200/成功readback/現在DWORDを
照合し、**本担当が加えたMinDiffAreaFileSize値だけ**を元の未設定へ戻す。
値が変わっていたら他担当の変更を保持して止め、snapshot・関連付け・上限を削除/縮小しない。
registry readbackの不存在とdriverの実効値復帰は区別し、即時の効果を未確認のまま断定しない。
反映に再起動が必要と判明した場合も先に自分の値を戻し、必要な復帰条件を既存窓口で準備する。
本人にPC操作を割り当てたり、未検証のログオン復帰を前提に再起動したりしない。

作成呼出し前は「要求結果不明」のphaseを先に保存し、タイムアウトを未作成と断定しない。
BEGIN/ENDの非0でも実登録を読める場合は先に記録し、同名/既存receiptがあれば自動再作成しない。
各point作成の前後にwriterを観測し、自分の作成呼出し中にlist writersを重ねない。
SystemRestoreの選択的追跡を一般VSSの全ユーザーファイル保護と同一視せず、未分類Cファイルや
snapshot内容を別path/コピーで回避して読まない。過去point復旧/実OS rollbackは未実施のまま区別する。

## 一次資料

- [Microsoft: diff areaのminimum、値の単位とPC共通範囲](https://learn.microsoft.com/en-us/windows/win32/backup/registry-keys-for-backup-and-restore#mindiffareafilesize)
- [Microsoft: resizeはsnapshot消失のおそれがある](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/vssadmin-resize-shadowstorage)
- [Microsoft: CreateRestorePointの戻り値と作成頻度](https://learn.microsoft.com/en-us/windows/win32/sr/createrestorepoint-systemrestore)
- [Microsoft: 実値を置換してイベント定義を取得](https://learn.microsoft.com/en-us/dotnet/api/system.diagnostics.eventing.reader.eventlogrecord.formatdescription)
- [Microsoft: writer state 5はbackup complete待ち](https://learn.microsoft.com/en-us/windows/win32/api/vss/ne-vss-vss_writer_state)
- [Microsoft: SystemRestoreが追跡するファイルの範囲](https://learn.microsoft.com/en-us/windows/win32/sr/calling-srsetrestorepoint)

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
