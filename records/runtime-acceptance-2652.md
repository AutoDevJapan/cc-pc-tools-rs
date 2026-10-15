# 配布済みLinux検査の限定受入（親2488）

**最終状態（2026-10-15 11:18 JST）: この限定受入は実成功。** 既存adapterを通した通常package検査11件、占有中の自分のcaller中止、Linux group消失、Windows gate終了、target解放と後続コマンド成功を確認した。親2488・製品全体・原登録一覧の完了は宣言しない。以下の未完了記述は各回の履歴であり、正常package検査を繰り返さない。

2026-10-15 02:39〜02:44 JSTの確認。受入は未完了。既存CIの利用とcheckoutの不一致を実測したため、通常Cargoと中止検査を開始していない。ToDo2652を正規の技術再試行待ちで保持する。親2488、他のToDo、製品全体の完了は宣言しない。

## 確認済み

- source、runtime、target、作業場所の保護判定は `client_resource=false` / `classification_unverified=false`。原登録一覧の復旧を意味しない。
- Windows host: SHA256 `1ccf146572738759b6b27a656beaf74f1ce62ae51908bad8bb8f866cb5b44dbc`、11,223,040 byte。
- Linux bridge: SHA256 `a376c1195e72333cd023692fbdbcb5cfe036db287c5057db0dbc17246b6c245f`。
- コンテナID `a81efc5c219e6fd99ed319d26369cf9b2f42d67378b894fbb1d4326b69082d3a`。image ID `sha256:89988fa8449f89b0d8f12fd2369aa5b167bfe3389189df877808a15055869011`。
- Init=true、PID1=docker-init、実UID1001、Privileged=false、CapDrop=ALL、no-new-privileges=true。devices空、device requestsなし、bindなし、persistent mountsなし。tmpfs設定は原ログに保持。
- 127.0.0.1:17852のListenerは1件、host PID52516。Linux Runner.Listenerは1件。正規GitHub APIでrunner27、online、busy=false、Linux/X64/self-hosted/cc-pc-tools-linuxのlabelを確認（02:41時点）。
- 正規APIと `git ls-remote` のmainは `5010498531449fa81640e3f2570be5e2c5c623cc`、tree `99e4335e5b665a2dd84cbb7e13b8a0dd4a300004`。
- checkoutは `191b17f6ab01a865dd3e7e10786a8fead8d04753`。remoteは `https://github.com/AutoDevJapan/cc-pc-tools-rs`。checkoutとmainのGo module treeはともに `2074a16ba0d45e80a8733f813f911f613c0ffca9`。
- targetのWindows byte lockは02:41と02:42の両測定で空き。取得後に即解放。測定の間も空いていたとの主張はしない。

## 開始を止めた実条件

02:42:59のLinux process原応答でRunner.Workerが1件、PID52272、開始02:42:26 JST。CodeQL/go-extractorがその子孫で稼働。checkoutは最新mainと異なる。共有Cargo slot0はWindows OSエラー33で占有し、別のCargo gate PID41504も実測した。測定時点は別々であり、原応答を一貫した単一スナップショットとは扱わない。

GitHub APIの有限一覧では同checkoutのPR CIが進行中。Build speed run38608792869のWindows job116006478392がin_progress。Linux側のWorkerを停止せず、fixture、checkout変更、bridge呼出し、Cargo test、中止実験はすべて未実行。

## 未確認と再開手順

通常metadata/--version、小さい既存packageのcargo test --locked、占有中のcaller中止、Linux group消失、initの孤児回収、Windows gate終了、target解除、後続コマンド成功は未確認。

同ToDo2652・同作業場所で再開する。最新の保存済み指示と原specを再読し、現在main/checkout/module tree、Worker0、他gate0、target空きを再測定する。条件を満たしてから既存adapterだけで有限受入する。現在mainと一致する非秘密fixtureを使う場合は模擬contextと明記し、keyは既存adapter内部だけで扱う。拒否時に原設定や認証を変更しない。ソース編集、新CI、rerun、merge、再配布、常駐再起動、Discord、APIキー、新費用、GPUを使わない。

## 原ログと保持資源

所有ID `eec49a469ac294440b59520ef14d06ba` のDドライブ専用フォルダにstdout/stderrの原byte、各コマンドの引数、実exit、開始終了、caller PID/作成時刻、bytes SHA256、ロック測定、guard応答を保持。`capture.private.ps1` は使い捨ての記録補助で、すべての子プロセスをCreateNoWindowで起動した。Gitへ入れるのはこのrecordsのみ。

親向けの全文結果は作業rootの `runtime-acceptance-2488.private.json` と `runtime-acceptance-2488.private.md`。秘密・profile・credentialは読んでいない。常駐runnerと旧保持コンテナ2件は変更していない。今回の受入caller/group/container/fixtureは存在しない。誤ってCドライブへ作成した空の所有付きフォルダはplan、別呼出しの通常削除、finishで処理済み。

## 03:04からの同ToDo再確認（第2回）

前回コミット `ac8b0579bebb61997baba728a867edb485ca485a` と原ログを保持して再開した。最新の保存済み指示とAGENTSを読み直した。現在mainは `183ac4fc7f0348021f9544cd8ac232669a3b8620` / tree `ba968c27ec66382bd1db87fbf0d46c53e78384e9`。正規GitHub APIとgit ls-remoteが一致。コンテナcheckoutもこのmainと一致する。Go module treeは現在mainとcheckout双方で指定の `2074a16ba0d45e80a8733f813f911f613c0ffca9` と一致。

source/runtime/targetのguardを再測定しfalse/unverified=false。hostのhash/11,223,040 byte、Linux bridge hash、container/image ID、Init=true/PID1docker-init、UID1001、capsALL、no-new-privileges、devices/binds無し、loopback17852/Listener1は前回と一致する。runner27のonlineと正規labelsも一致。03:04:54のAPIはbusy=true、Linux Worker PID82723（開始03:03:40）が存在した。03:05:43と03:06:39のprocess原応答ではWorker0となった。

開始を止めている条件はWindows側の既存Cargo検査。gate PID44372、作成03:04:58.02345、Cargo PID48440、nextest PID19280（作成03:04:58.363825）を正規OS照会で確認した。mainのBuild speed run38610554267 / job116012473478 / Windows runner28は03:05、03:07、03:08、03:10時点でin_progress。03:10:16にも同gateが存在する。対象target byte lockは03:04:55と03:10:17の両測定で空き。これらは別時点の有限観測であり、CIやgateの停止・失敗とは主張しない。

同じDの所有付きフォルダに `r2-*` の原byte応答、実exit/時刻/PID/作成時刻/bytesSHAとreadinessを追加して保持した。既存adapterを使う受入の使い捨て手順 `accept-r2.private.ps1` を準備したが、開始条件が未達のため実行していない。新CI/rerun/全件検査の反復、fixture作成、通常Cargo・中止検査は未実行。準備済み手順も次回の最新main・checkout・module treeと空き条件を再検証してから使う。正常実行が一度成功したら、保存結果を読んで繰り返さず未完の中止確認へ進む。

所有付き原ログ、前回と第2回のprivate結果、常駐runner、旧停止コンテナを保持する。本人の操作・判断は不要。同2652の正規wait-retryで続行する。

## 09:11からの同ToDo再確認（第3回）

第2回の記録コミット `8020d6c71fb86ffcb6379fd26a8cfe0f297c1542` と原ログを保持して再開した。起動時に最新の保存済み指示とAGENTSを再読した。09:11のsource/runtime/targetのguardはいずれもfalse/unverified=false。Windows hostのSHA256と11,223,040 byteは指定値のまま。Gitのremoteは引き続き対象のPrivate repo URL。

現在の開始条件は未達。09:12:02のOS照会では127.0.0.1:17852のListener0、指定パスのhostプロセス0。既存タスクCC-LinuxRunner-todo2510の直近実行は03:48:36、LastTaskResult=1。対象target byte lockは09:12:05の測定で空き。taskの登録があることをhostの稼働成功と扱わない。

Docker経由の読み取り専用process照会は09:11:09から45秒で応答せず、stdout/stderrとも0 byte。これはDocker CLIの応答期限超過であり、LinuxのWorker0、group不在、container/image/PID1/UID/capsの現在値を証明しない。自分が起動した照会CLI PID42940、作成00:11:09.7091431Zを保存した。09:12:38の後始末照会ではそのPIDは既に不在で、終了コードは取得不能。killは実行しておらず、このCLI不在をCargoの中止成功とは扱わない。

既存OAuth経由の正規gh_gateでmain APIはHTTP404/exit1、runner APIはHTTP401/exit1。現在main SHA/checkout/Go module treeとrunner27 online/labelsは未再確認。前回のSHAやonline結果を現在の確認へ流用しない。トークン・key・credential・profileは読まず、認証の再設定もしない。

この受入では禁止されている常駐再起動・再配布・認証/guard変更・source/checkout変更による復旧を行っていない。正常adapter、fixture、通常Cargo、中止検査はいずれも未実行。通常実行と占有中caller中止、group排出、init回収、Windows gate exit/target解放、後続コマンドの実成功はすべて未確認のまま保持する。

原応答全文、開始終了、実exit（取得不能はその旨）、PID/作成時刻、raw bytesSHAを同じ所有付きフォルダの `r3-*` に保存する。前回private結果は上書き前に保存し、現在の結果を親の指定場所へ返す。同2652・同cwd・同specで正規wait-retryを記録し、次回はhost待受、Docker照会、正常API取得から再確認する。親2488や他ToDoをdone/blockにせず、本人のPC操作・判断を求めない。

第3回の記録は作業ブランチへコミットした。既存Git認証によるpushはexit128で失敗し、Usernameを取得できないとの応答だった。認証画面が出ないよう対話を無効にして通常のpushを1回だけ行った。前回のpush成功を今回へ流用せず、今回の記録はPC内のコミットと親へのprivate結果に保持する。認証値取得や別経路のpushは行わず、次回同じToDoの通常経路で確認する。

## 10:26からの同ToDo再確認（第4回）

第3回コミット `e623773269f0adf96e44ca1aa910c56881137042` と全原ログを保持して再開。最新の保存済み指示とAGENTSを再読した。source/runtime/targetのguardはfalse/unverified=false。既存runtimeの待受、Docker、正常GitHub APIが復帰したことを実測した。今回の担当は再起動・再配布・認証変更を行っていない。

Windows hostの指定SHA256/11,223,040 byte、Linux bridgeの指定SHA256、同container/image ID、Init=true/PID1docker-init、UID1001、capsALL、no-new-privileges、GPU/devices/binds無しは再確認。17852のListener1（host PID11768）、既存task Running、runner27 online/busy=falseと正規labelsを確認した。Worker0、他gate0、target byte lock空きを呼出し前と後で確認した。

現在mainはAPIとgit ls-remoteで `183ac4fc7f0348021f9544cd8ac232669a3b8620` / tree `ba968c27ec66382bd1db87fbf0d46c53e78384e9`。checkoutは `f3a5feeb6fef2c8230ce8831d660cdf289f6b0fa`、remoteは指定repoであり、現在mainと一致しない。Go module treeはmainとcheckout双方で指定の `2074a16ba0d45e80a8733f813f911f613c0ffca9` と一致。有限API一覧ではこのcheckoutのPR CI3件はsuccess/completedだった。新CIやrerunは実行していない。

実repo/remote/main SHAと一致する非秘密push eventを自分専用/tmpへ置き、**手動の模擬context**として既存 `/opt/bridge/cargo --version` を10:27:45に1回呼んだ。実exit1、stdout0 byte、stderr `bridge rejected (403)`。元のadapterとkey処理をそのまま使い、秘密値を担当が取得していない。target lockは呼出し前後とも空きで、占有中のCargo実行とは扱わない。

拒否条件の切り分けとして同じmainの模擬contextを使った既存事前認証hookを10:29:23に1回だけ呼び、exit0を確認した。元ソースの処理順は認証/引数検査、current-head認証、checkout SHA照合、Cargo gate起動。正常hookとSHAの実照合から、少なくともmain/checkout不一致が既存のcheckout照合条件を満たさないことを確認した。CLIは一般403だけを出すため、HTTP error bodyの原文を取得したと主張しない。guard/認証/ソース/root/workflow/checkoutを変更して成功を作っていない。

2つの模擬event用/tmpフォルダは、自分のevent.jsonだけを削除し、空のフォルダをrmdirし、不在まで実exit0で確認した。hook用eventは非秘密の送信byteもDの所有付き原ログへ保存した。metadata、通常package cargo test、占有中caller中止、Linux group排出/孤児回収、Windows gate exit/解放、後続Cargo成功は未確認。通常package検査はまだ一度も実行していない。

前3回と第4回の原byte/実exit/コマンド/時刻/PID/作成時刻/bytesSHAは同じDの所有付きフォルダへ保持する。既存runnerと旧停止コンテナを変更していない。通常Cargoと中止の受入を完了扱いせず、同2652の正規wait-retryで現在main/checkout一致を再確認する。前回未pushの記録も今回の通常pushに含め、成否はprivate結果とToDoへ記録する。

## 10:49からの同ToDo再確認（第5回）

コミット `91c0b5f1e1e2e8b6bb78e5e9ad0d9dac379e5aec` と前4回の原ログを保持して再開した。最新の保存済み指示、AGENTSと親の既存PROGRESSを再読した。元の「限定受入のみ、ソース編集/新CI/rerun/merge/再配布/常駐再起動禁止」の範囲を保つ。

10:49のsource/runtime/targetのguardはfalse/unverified=false。hostの指定hash/11,223,040 byte、Linux bridgeの指定hash、同container/image、Init/PID1docker-init、実UID1001、capsALL、no-new-privileges、GPU/device/bind無し、17852 Listener1（host PID11768）とLinux Listener1、runner27 online/busy=false/正規labelsを再確認した。Worker0、他gate0、target lock空きを実測した。

現在mainは正規APIとgit ls-remoteで引き続き `183ac4fc7f0348021f9544cd8ac232669a3b8620` / tree `ba968c27ec66382bd1db87fbf0d46c53e78384e9`。checkoutは新しい別PRの `255117adbdd7104c30c1d660c8300b570f321dfd` へ変わり、mainとの不一致が継続している。remoteは対象repo。Go module treeは両方で指定の `2074a16ba0d45e80a8733f813f911f613c0ffca9` と一致。有限API一覧でこのcheckoutのCodeQL/readiness CI2件がsuccess/completed、branchはscheduler/todo-2671-workと確認した。登録された原範囲をこの別PRへ広げない。

前回の既存adapter403/exit1と事前認証hook0の原応答を保持する。今もmain/checkout照合条件が未達なので、同じ403の再取得、fixture再作成、metadata/通常test/中止実験は繰り返していない。source/root/workflow、認証/key、guardやcheckoutを変更せず、未実行のpackage testと占有中caller中止を成功扱いしない。新CI/rerun/mergeを発生させてcheckoutを作る操作もしない。

第5回原byte/コマンド/実exit/時刻/PID/bytesSHAを同じDの所有付きフォルダのr5記録へ保存した。常駐runner・旧停止コンテナ・前4回結果を保持し、この担当のcaller/group/tempは新規作成していない。現在mainとcheckoutの一致および全開始条件を同2652・同cwd・同specの正規wait-retryで再確認する。親2488/他ToDo/原登録一覧/製品全体をdone/blockにしない。本人のPC操作や判断は不要。

## 11:10からの同ToDo有限受入（第6回・実成功）

第5回コミットと原資料を保持して再開し、最新指示/AGENTSを再読。初期guardはsource/runtime/targetともfalse/unverified=false。hostの指定hash/11,223,040 byte、Linux bridge hash/UID1001、17852 Listener1/host PID11768、Linux Listener1、runner27 online/busy=false/正規labels、Worker0/他gate0/target byte lock空きを実確認。同一container/image、Init=true/PID1docker-init、capsALL、no-new-privileges、GPU/device/bind無しを最終inspectでも再確認した。

前回までmainとcheckoutの完全一致を待ったが、既存normal bridgeには同一repoの実PRを正規API/current-headで照合する経路もある。実PR175のheadは現在checkoutの `255117adbdd7104c30c1d660c8300b570f321dfd` / scheduler/todo-2671-work、baseは現在mainの `183ac4fc7f0348021f9544cd8ac232669a3b8620`。head/base repoとも指定AutoDevJapan/cc-pc-tools-rs、base ref main、state openを正規APIで確認した。APIのmerge_commit_shaは `aa6e062800cf23ed590abfad48b805b6160d4509`。

main/checkout双方のGo module treeは固定 `2074a16ba0d45e80a8733f813f911f613c0ffca9`。Cargo.toml、Cargo.lock、cc-task-title、cc-scheduler、bridgeについてmainとcheckout間のgit diffはexit0/0 byte。実repo/remote/current-mainのbase SHAと実PR head/branchに一致する非秘密eventを自分の/tmpへ置き、**手動の模擬PR context**と明記した。実際のGitHubイベントや本人指示とは扱わない。source/root/workflow/checkout、guard、認証、key、常駐・配布は一切変更せず、既存adapter/通常のrepo/current-head認証と共有gateを通した。

| 実確認 | 結果 |
|---|---|
| `cargo --version` / metadata --locked --no-deps | 両方exit0。cargo1.90.0、workspace既存root、Linux target /work/target |
| `cargo test --locked -p cc-task-title` | 1回だけ実行。11pass/0fail/0ignored、doc0case成功、caller exit0 |
| normal admission | 実build-jobs8/test-threads8。Windows gate PID42308、作成02:14:37.4900412Z、実exit0 |
| normal target/kernel | 11:14:37.934 lock占有、11:14:45.480解放。実Cargo group89663のkill(-pgid,0)はexit1/No such process |
| `cargo test --locked -p cc-scheduler --no-run` | 中止用の別packageを1回起動。実compile進行とtarget占有を先に確認 |
| callerの中止 | Linux caller PID91661/starttime501588/comm cargoを直前再照合して、このPIDだけへTERM。stop実exit0、caller exit143 |
| cancel gate/kernel | 実Cargo group91710。Windows gate PID10924、作成02:14:46.3444402Z、実exit125。groupへのkill(-pgid,0)はexit1/No such process |
| cancel target | 11:14:46.764および中止直前11:14:47.151は占有、11:14:47.440に解放 |
| 中止後の通常読み取り専用呼出し | 11:14:47.538開始の既存adapter --versionがexit0で応答。gate保持/要求待ちなし |
| 後片付け・維持 | 自分のeventとPID marker3ファイル/空の/tmpフォルダだけを削除し不在をexit0で確認。最終group/子孫・zombie残存0、target空き、runner online |

中止は仕事の自然終了を成功扱いしたものではない。caller creationを保存・照合した時点とstop直前に占有を測定し、compile stderrと実Cargo groupを確認した。init構成でkernel上のgroup全体が不在となり、最終process原応答にも試験子孫やzombieは残っていない。SIGKILLまでの強制経路やinit wait syscallの個別traceは今回実測していない。setsidでgroupを離れる任意子孫、GUI/OS再起動/ログオン前復帰を検証したとは主張しない。

実行用eventのD保存byteとLinux読戻しbyteのSHA256は `a25c62dc8239fd38dc4d90ab1bca073d8e4cc334dd74a32208b42bb3afd97b04` で一致した。秘密値は既存adapter内部だけで扱い、担当は読んだり保存していない。Cargo/child/gate/各照会の原stdout/stderr全文、引数、時刻、実exit、PID/作成時刻、lock before/during/afterとbytesSHAは所有IDeec49a469ac294440b59520ef14d06baのDフォルダr6記録に保持。最終source HEADとGo treeは開始時と同じ。

原ログ/metadata/前5回結果とこの実証を保持するため、Dの所有付き実験フォルダは保持する。既存Linux /work/targetの生成物、常駐runner、旧停止コンテナ2件、unknown/workを削除・export・copyしていない。自分のrequest caller/group/tmpは終了・不在確認済み。recordsだけをcommit/pushし、親へprivate結果と原応答全文を保存して返す。この2652の状態更新はスケジューラーに任せる。親2488/他ToDo/原登録一覧/製品全体をdoneにしない。
