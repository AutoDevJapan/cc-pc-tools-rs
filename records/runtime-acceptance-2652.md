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
