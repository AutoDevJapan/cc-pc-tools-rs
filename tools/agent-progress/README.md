# 共有ToDoと並列担当の読取専用データ

共有ToDo2547の有限sidecarです。元2543のRust TUIが別セッションの担当を表示するために、1回だけJSONを取得します。画面描画・現行TUIの置換・ジョブ再開・通知・常駐処理は含みません。主担当が別モデルレビュー、統合、UI方向決定後の表示とresizeの受入を行います。

## 起動と統合

```powershell
go test ./...
go vet ./...
go build -o bin/agent-progress.exe .
./bin/agent-progress.exe --once
./bin/agent-progress.exe --once --output artifacts/snapshot.json
./bin/agent-progress.exe --once --task 2547 --task 2543
```

ビルドと起動はこのディレクトリで行います。保存先の親ディレクトリは先に作成してください。`--output` 指定時はstdoutへJSONを重ねて出しません。保存版はTUIが毎回開いて読み、ファイルを開いたまま保持しません。バックグラウンドからの起動は呼び出し元で `CREATE_NO_WINDOW` を使ってください。この道具自身は子プロセスを起動しません。

schema v1は [snapshot.schema.json](snapshot.schema.json)、Goの型は [types.go](types.go) です。Rust側は対応したversionか確認してから読みます。`captured_at` は取得開始、`heartbeat_at` はこの有限取得の完了日時です。TUIは保存版を読んだ時刻との差も確認してください。保存ファイルが残っていても、heartbeatが更新されていなければ現在の状況と決めつけません。

| 欄 | 意味 |
| --- | --- |
| `tasks` | ToDo番号、許可した名称から作る表示名、部署、優先度、保存された状態、確認済み進捗、関連担当、待ち理由 |
| `jobs` | 元の担当IDをSHA-256にしたキー、番号による対応、保存された状態、プロセスの観測結果、実行段階、待ち理由、時刻と元ファイルの状態 |
| `percent` | 正規 `progress` の整数0〜100かnull。doneは100。時間・tokens・steps・plan数・別名の進捗欄から算出しない |
| `progress_state` | measured、not_measured、invalid、doneを区別 |
| `recorded_status` | ToDoのdoingと担当のrunningを別に扱う。recovery_unverifiedはそのまま保持し、未知の入力値だけをunknownにする |
| `execution` | PIDと作成時刻が一致するrunning、見張りだけのsupervising、alive_identity_unknown、queued、starting、not_observed、finished、unknown |
| `phase` | 保存された活動の型から固定語へ射影。入力のコマンド・検索文・ファイル名・発言を出さない |
| `task_type` | 実装・レビュー・調査・検査等の固定enum。入力の種別が許可した値に一致しない場合はunknown。本文から推測しない |
| `task_type_source` | ToDoの正規欄ならtodo、表示対象の主な担当から得た場合はjob、どちらも未記録ならunknown |
| `detail` | kind=todoと元のtask_idだけを保持。TUIの詳細動線に使い、原本文やパスを返さない |
| `wait_reasons` | 依存番号、再確認時刻、本人用projectのblock、根拠不明のblock、混雑、利用枠、実行枠、別担当、見張り再開等を区別。recovery_unverifiedには固定のlabel「復旧状況が未確認」を付ける |
| `record_updated` | ToDoの記録更新。実活動と見なさない |
| `last_activity` | runningの最終イベント、終了した担当の終了時刻。queuedの生存記録から作らない |
| `last_heartbeat` | 担当の最終イベント記録。待機中の見張りも更新するため、実活動とは別に扱う |
| `sources` | 正規原本の読取結果。jobs集合のmodifiedは単一のmtimeを持たないためunknown。各担当のsourceに実ファイルのmtimeを保持 |
| 時刻オブジェクト | UTCのat、取得開始時点のage_seconds、fresh/stale/future/unknown/invalid。futureの経過秒はnull。not_beforeのfutureは予定時刻として正常 |

`stale` はその記録が古いという意味で、停止や完了の断定ではありません。Windowsのprocess presenceはraw FILETIMEの作成時刻を照合します。ハンドルの取得が拒否された場合は、その有限取得で一度だけ検証したOSのプロセス一覧を使います。取得に失敗した一覧を空の一覧とせず、unknownを保持します。生存を確認できても作成時刻を照合できなければalive_identity_unknownです。PIDの再利用はidentity_mismatchです。終了状態の担当は、作成時刻が一致する実稼働のworkerを確認できた場合だけrunningへ変更します。確認できない終了担当はfinishedを保持し、未知のpresenceだけを理由に稼働一覧へ加えません。Windows以外ではこのPCの作成時刻を照合できないためunknownを保持します。元の状態を修復するCLIを呼びません。

recovery_unverifiedは復旧状況の確認が必要な記録です。起動枠の順番待ちとは別のkindと表示文を返します。この表示は原保護設定の復旧完了や、空の保護原本が正しいことを意味しません。queuedで待ち理由が記録されていなければunknownを返し、実行枠待ちと推測しません。

### Windowsの取得根拠と検証範囲

主経路は同じOpenProcessハンドルに対するWaitForSingleObjectとGetProcessTimesです。拒否や識別情報の取得失敗時には、動的リンクしたNtQuerySystemInformationのSystemProcessInformationを使います。一次資料はMicrosoftの[NtQuerySystemInformation](https://learn.microsoft.com/en-us/windows/win32/api/winternl/nf-winternl-ntquerysysteminformation)、[MS-TSTSのプロセス情報](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-tsts/96702c7d-6f5f-4965-8f29-5fdbea73278e)、[GetProcessTimes](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-getprocesstimes)です。

Microsoftのnative構造体では時刻部分が予約領域になっています。MS-TSTSはその意味と100ns/1601年UTCの単位を記載していますが、プロトコルの型をnative ABIの保証とは扱いません。64-bitの型付き構造体を使い、ヘッダ256 bytes、thread 80 bytes、CreateTime/PID/sessionの配置をunsafe.Sizeof/Offsetofで検査します。各取得で自分自身のGetProcessTimesとCreateTimeの完全一致を確認した場合だけ一覧を採用します。この実証は現在の実行環境についてのもので、全Windows版への保証ではありません。未対応配置・API欠落・一致しない配置ではunknownを保持します。生の固定offsetだけで時刻を確定しません。

ReturnLengthの実範囲、NextEntryOffsetの境界・整列、thread配列のサイズ、重複PID、PIDの幅と時刻の符号を検査します。一覧は全体の検査に通った場合だけ採用します。初回bufferは64KiB、最大32MiB、必要長の変化に対する呼び出しは最大6回で、取得期限にも従います。プロセス名・ポインタ先・thread本文は読み出しません。SystemBasicProcessInformationのSequenceNumberは正規metadataのpid_createdと単位が違うため、この照合には使いません。

## 原本と保存形式の根拠

読取先は `D:/CC/Tools/todo/todo.json` と `D:/CC/Tools/codex-jobs/jobs/*/job.json` です。任意の本文・spec・prompt・last_message・last_error・実行ログ・クライアント除外原本・メール・認証情報は出力しません。最小の構造体に必要な欄だけを解析します。

優先順位の任意設定は `D:/CC/Tools/scheduler/priorities.json` です。欠落はnot_configuredとして標準順を使います。読めない既存設定はdegradedです。`--todo`、`--jobs`、`--priority-file` で合成入力や同じ保存形式の別配置を指定できます。空のpriority-fileで任意設定を無効にできます。

根拠は同じrepositoryの `cc-todo/src/lib.rs`（保存先・progress・done・更新時刻）、`cc-todo/src/job_refs.rs`（保存済み担当ID）、`cc-job-policy/src/todo_store.rs` と `docs/todo-capacity.md`（64MiB/BOMの契約）、`cc-job-policy/src/todo_wait.rs`（依存と再確認）、`cc-job-policy/src/priority.rs` と `cc-jobs/src/queue.rs`（部署順・優先度継承・待ち理由）、`cc-jobs/src/events.rs` と `cc-jobs/src/jobs.rs`（活動と待機中の生存記録）、`cc-jobs/src/platform.rs`（プロセス識別）です。queue-configの実設定は読取調査だけ行い、この道具は配分・再試行・設定を変更しません。

2026-10-13の一次調査で、ToDoの文字列更新時刻とUnix数値時刻の混在、担当履歴の一部が文字列以外であること、jobs直下に担当ディレクトリ以外の多数のファイルがあることを確認しました。文字列時刻はRFC3339または正規CLIのJST `YYYY-MM-DD HH:MM`、数値時刻はUnix秒として扱います。本文や履歴から最新の担当を推測しません。番号・related_todo_ids・jobsの有効な文字列・scheduler.jobだけで対応付けます。

## 名称と並び順

出力できる名称は本人が列挙した公開製品とこのrepositoryの道具をコードで列挙したものだけです。任意のproject/department/title/pathはそのまま使いません。未分類は「名称未確認」、受託部は「受託案件」です。guardが未分類の案件から名称やパスを取り出しません。`client-projects.txt` は読みません。許可した名称の前方一致も認めず、入力の制御文字を除去して名前を復活させることもありません。担当IDは元の文字列を出さずSHA-256にします。

受託部10 → 万世紀20 → 情報システム部30 → 発信部40 → Skills50 → GoDD60〜67 → 自社100の順です。Skillsの部署表示は情報システム部です。GoDD内はMCP-Alpha、Press、AO、13a13e1、Rosetta、Genesis、VirtualStreamerの順です。順位は表示名の許可一覧から独立して、正規cc-job-policyのproject_rank/item_rank/todo_ranks/job_ranksに合わせます。非公開の別名も順位計算に使いますが、コードには対応するSHA-256だけを保持し、出力は数値と部署の固定値だけです。ハッシュは推測可能な名称を秘密にする保証ではありません。

参照する正規ソースはcommit `33b96765777a69663abba275b1df5b49e30458b2`、`cc-job-policy/src/priority.rs` のLF正規化後のSHA-256は `c31787bfd508c40b62fd5bd57872a0c8d1a9ff31e907f6003bff52180287c86d` です。Windowsでのcheckout bytesのSHA-256は `d93c7d15c5ccf61010927e22472139d51348e45b04c397926d047f05672a2712` でした。検査では正規ソースの全46個の完全一致規則と公開名の部分一致規則を照合します。非公開の文字列を診断へ出しません。

未完の明示依存は親の高い優先度を引き継ぎます。番号で紐づくToDoがある担当は、そのToDoの順位だけを使い、担当自身のprojectとの最小値にはしません。subworkがobjectの独立担当は親から継承せず、取消要求中の親からも継承しません。部署は適用した順位に対応する固定値にし、公開表示名の許可範囲は変えません。同順位では実行確認、最後にToDo番号または不透明な担当キーで安定して並べます。

既定は未完ToDo、稼働中または不明の担当、表示対象ToDoの最新終了担当です。`--include-done` は終了ToDoも含め、`--task` は完了状態でも指定番号を出します。関連不明の稼働担当は既定一覧から落としません。番号フィルタを明示した場合はその番号に対応する担当だけを出します。

## 有限取得・失敗・保存

原本は64MiB、各jobは1MiB、jobsの合計読取は128MiB、優先度設定は1MiBを上限とします。ToDoは最大50,000件。担当の名前を128件ずつ期限まで全走査し、最大10,000件の新しい名前だけをheapに保持します。正規IDの日時接頭辞に従う降順でmetadataを開きます。古い順の先頭10,000件で打ち切らず、directory以外のentryにもmetadata枠を使いません。途中で期限を超えた走査はdeadlineとして扱います。

未完ToDoのjobs/scheduler.jobが参照する有効なIDは、名前の走査より先に取得します。これらは新しい履歴10,000件の上限から除外し、古い担当も保持します。参照分も1件/合計bytesと期限の上限に従い、不正なID・directoryのsymlinkを辿りません。新しい履歴に入らず、参照もない担当を省略した場合はjobsのsourceをdirectory_limit/complete=false、issuesをjobs_directory_limitにしてmetadataの部分取得を明示します。MaxRowsによる表示の省略は別にtotal/truncated/row_limitへ記録します。

表示は各一覧1,000件、走査期限は20秒です。行数・担当数・古さ・期限はCLIで有限範囲内に調整できます。シンボリックリンクは読まず、通常ファイルだけを開きます。上限+1 byteまでの読取と同じバイト列の解析を行い、途中のサイズ/mtime変化を検出します。OSの単一read自体を強制中断する機能はありません。

すべての原本を一括ロックするtransactionではありません。`consistency` はper_file_observationで、取得中の正規writerの変更を否定しません。読取失敗・上限・期限・破損・欠損はsourcesとissuesへ固定コードで出し、成功した空配列へ置き換えません。読めない担当は不透明なキーとunknownの行を残します。ToDoを読めない場合でも確認できた担当を保持します。

終了コードは0が取得成功、1がdegraded/unavailable、2が引数または保存失敗です。1でも失敗状態を示すJSONを返すか保存します。TUIは一部取得を「全担当停止」と解釈せず、前回の一覧を保持する場合はその一覧の古さと今回の失敗を表示してください。フィルタ後の省略もtotalとtruncatedへ出します。

保存先は原本・原本の別名・ToDoディレクトリ・jobsディレクトリ・このPCの正規運用ディレクトリと重ならない場所を使います。同じディレクトリの一時ファイルに書き、sync/closeして原子的に置換します。WindowsはMoveFileExWのREPLACE_EXISTING/WRITE_THROUGHを使用します。読取中の短い共有違反に対して置換だけ最大8回、合計140msの待機で再試行します。以前のsnapshotを先に消したり切り詰めたりしません。保存失敗時は以前のファイルを保ち、自分の一時ファイルを片付けます。

Windowsの読み手はopen時のERROR_ACCESS_DENIED(5)とERROR_SHARING_VIOLATION(32)だけを5ms間隔で最大150ms再試行します。`readPublishedSnapshot`はこの契約を検査するGo実装です。それ以外の失敗は直ちに返します。150msは再試行の予算で、OS呼び出し自体の強制中断を保証しません。失敗した読取から得たbytesを採用せず、前回検証済みの世代を保持し、失敗と古さを表示します。Rust TUIにも同じ契約を適用してください。この担当はTUIの読み手を編集しません。Windows以外の読み手はrename中の共有違反がないため再試行を追加しません。

## 検査

合成入力で計測値/null、不正な進捗、未来/古い/不明の時刻、正規依存、本人用projectのblockと根拠不明のblock、モデル混雑と利用枠、生きた識別不明の担当、PID再利用、原本破損・欠損・容量、BOM、16MiBを超える入力、優先度継承、名称/秘密/制御文字の排除、原本hashの保持、CLI終了コード、保存先衝突、並行readerからの原子的保存を確認します。Windowsでは自分の実プロセスの作成時刻も照合します。`go test` と `go vet` を実行し、schemaとの整合を確認します。実運用の原本をテスト用に複製しません。

主担当への受入はこのデータ契約と固定差分です。原2543・画像修復2546・144のソースや共有状態には書きません。別モデルのMustレビューとTUIの実表示・resizeはこのsidecarの完成条件に含めず、主の統合工程へ引き継ぎます。

ToDo2562の関連検査には、10,003件超で新しい担当と古い正規参照を保持する検査、終了担当のPID再利用、復旧未確認のschema、正規順位・紐付け・継承、型付きnative一覧の境界/ReturnLength/失敗、150msの読取予算を含みます。並行保存の検査はWindows上で単独の関連repeatを行います。

実運用の有限実測は次の1回で実行できます。出力は件数と状態の固定値だけです。原本のhashはメモリで前後を比較し、変化した件数だけを記録します。同時に正規writerが動くため、変化があってもこのreaderの書込とは判断しません。合成入力のhash保持検査では、この道具が原本を変更しないことを確認します。

```powershell
$env:AGENT_PROGRESS_NATIVE_SAMPLE = '1'
go test -run '^TestNativeWindowsOperationalCounts$' -count=1 -v .
Remove-Item Env:/AGENT_PROGRESS_NATIVE_SAMPLE
```
