# 配布済みLinux検査の限定受入（親2488）

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
