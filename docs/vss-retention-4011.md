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

## 別モデルに提出する具体手順（未適用）

`tools/vss-4011/configure.ps1`は既存C→C/50GiB、MS provider 1、他volume snapshotなし、
明示DisableSRなし、事前割当後も200GiB以上空くこと、MinDiffAreaFileSize未設定を再確認する。
既存VolSnapキーのDWORD **MinDiffAreaFileSize=51200MiB（50GiB）**だけを原子的に設定する。
全PCに効く設定であることを明示し、現在の唯一のC関連付けだけであることを実適用前にも確認する。
現在の50GiB上限を増やさず、その範囲を先に割り当てて以後の領域拡張を不要にする狙い。
この値で実際の割当が50GiBになることは未確認であり、実測を受入条件にする。
既存ポイントが並行して新たに現れても削除する処理はなく、保存先/上限は変更しない。
大きなPC共通minimumが将来の別volumeバックアップへ及ぼす影響をレビューで扱う。

`create-point.ps1`を同じAdminBrokerから一度実行し、SystemRestore.CreateRestorePointの
BEGIN/END通常操作で「ToDo4011 VSS retention verification」を作る。Windows自身のVSS排他を使う。
同名があれば重複作成せず、成功戻り値と実登録を別に記録する。サービス/他workerを止めない。
既存creation frequencyを変更しない。既存snapshotが現れても保持する。

前後に本担当所有Tempの小さな既知データを用意し、可能なら新しいsnapshotのその完全一致path
だけを個別guard後に読み、上書き前のデータを保持していることを確認する。
未知のsnapshot内容を一覧走査せず、取得資格が成立しなければ別path/コピーで回避しない。
通常I/Oの続く有限な観測区間で同じsnapshot、差分使用増、割当50GiB/上限50GiB、
新しい24/25/35なしを確認し、観測時間と未検証の長期間/再起動を明示する。

登録だけでは完了にせず、割当が意図どおりにならない場合は有効化成功と呼ばず追加調査する。
この手順には削除/縮小/再関連付け/無効化/OS再起動がなく、Dへの私的データ移動もない。
レジストリを戻す必要が生じたら本担当が追加したDWORDだけを元の未設定へ戻す案を
別途結果とともに扱い、既存snapshot/関連付けはそのままにする。

## 一次資料

- [Microsoft: diff areaのminimum、値の単位とPC共通範囲](https://learn.microsoft.com/en-us/windows/win32/backup/registry-keys-for-backup-and-restore#mindiffareafilesize)
- [Microsoft: resizeはsnapshot消失のおそれがある](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/vssadmin-resize-shadowstorage)
- [Microsoft: CreateRestorePointの戻り値と作成頻度](https://learn.microsoft.com/en-us/windows/win32/sr/createrestorepoint-systemrestore)
- [Microsoft: 実値を置換してイベント定義を取得](https://learn.microsoft.com/en-us/dotnet/api/system.diagnostics.eventing.reader.eventlogrecord.formatdescription)
- [Microsoft: writer state 5はbackup complete待ち](https://learn.microsoft.com/en-us/windows/win32/api/vss/ne-vss-vss_writer_state)
- [Microsoft: SystemRestoreが追跡するファイルの範囲](https://learn.microsoft.com/en-us/windows/win32/sr/calling-srsetrestorepoint)

私的な匿名実測とBroker request/戻り値は自分の`.ci-work/4011/`へ保存し、公開ソースには
volume GUID、ホスト名、元ファイルpath、タスクの引数、実データ、認証情報を含めない。

## 適用前レビューの受付

固定base461e0ab5→a229c070の6ファイルを、通常の独立レビュー4014へ登録した。
実job20261027-041610-todo-4014、正常statusでOpus5.5/max/runningを確認。
途中の6ファイル読了報告ではmin=maxの設定が世代数を減らすおそれを調査中であり、
最終判断は未受領。OS変更はまだ0、候補を採用・適用・修復済みとは扱わない。

SystemRestoreのScopeSnapshotsは未設定。Microsoftの仕様上、ユーザーTemp canaryの
保持を一般VSS全volume保護やSystemRestoreの必須機能と同一視できない。
OSファイルを直接hashする案はそのC pathの通常guardが分類未確認で実施していない。
既存backup関連taskの限定metadataでは10件中9件Microsoft namespace、他1件CC namespace。
後者のVSS使用/保持仕様は未確認であり、他のbackupが存在しないとは断定しない。
指摘と実保持への影響を解消してから設定を適用する。
