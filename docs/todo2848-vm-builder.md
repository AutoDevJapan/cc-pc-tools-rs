# 配備CI用ビルダーの互換性修復（ToDo2848）

親ToDo2757の正式配布済みpreflightは、固定製品sourceのベースイメージを展開する段階で
所有者変更がEPERMとなり、build exit1になった。この文書はその原因、修復方式、
CC自作fixtureの実結果、残る制限を記録する。製品sourceでの再実行は親2757が行う。
この修復だけでは製品buildの成功を意味しない。

## 原因（実測）

外側のコンテナは `--cap-drop ALL`、`no-new-privileges`、root(uid0)で動く。Kaniko
v1.28.6は、ベースイメージのレイヤーをこのコンテナ自身のルートへ展開するとき、
元のtarヘッダーの所有者(uid/gid)へchownする。CAP_CHOWNが無いと、root所有以外の
ファイルを含むベースで必ず失敗する。公式資料も、ベース展開とRUNの権限によって
rootlessの可否が変わると明記している
(https://github.com/osscontainertools/kaniko/blob/v1.28.6/README.md#security)。

CC自作の最小再現（Debian slimをdigest固定、`RUN true`だけ、旧設定と同じ外側の
権限・資源）は次のとおり失敗した。親の実失敗(125 bytes、base filesystem展開の
chown/operation not permitted)と同じ種類である。

```
error building image: error building stage: failed to get filesystem from image: chown /etc/gshadow: operation not permitted   (exit 1)
```

package toolも同様に権限(setuid/setgid/chown)を必要とするため、後段のRUNも
同じ制約の下では動かない。製品Dockerfile、元image、レイヤーの所有権・modeは変更していない。
失敗の無視、chownの無視、権限エラーの成功化も行っていない。

## 方式

外側のコンテナには何も足さない(capability・device・KVM・privileged・host bind・
host socket・host network・seccomp/AppArmor無効化・no-new-privileges解除は無し)。
代わりに外側のコンテナの中でQEMUを**CPU命令のソフトウェア実行(TCG)**で動かし、
小さなLinuxゲストの中でKanikoを動かす。ゲストのrootが持つcapabilityは、ゲストの
カーネルの中だけに存在し、外側の権限・cgroup・PID/メモリ/CPU上限は変わらない。

- 外側のコンテナ: `tianon/qemu@sha256:8b8a194c14322053a9bafba8fdae0d2fffb722dd5d991b9e65970184fe1ead4e`
  (QEMU 11.1.1、Debian trixie)。entrypointとcommandは固定スクリプト(`sh -c`)だけで、
  daemonのinspect値(Config.Entrypoint/Cmd)も照合する。
- ゲストのkernel: `linuxkit/kernel@sha256:3eae204fb152652742f4679da795a3f2848ad70c83bf1db31af0a0039e40ae67`
  の `/kernel` (SHA-256 `89f7d4f3...c0fd`、11,535,360 bytes)。
- ゲストのuserland: `ghcr.io/osscontainertools/kaniko@sha256:7ab44d0131c3051b6dea128067e258c7afb5c4815082a93b9cf6c624f2140bf0`
  (v1.28.6-debug)から、BusyBox(`899f11dd...`)、executor(`7e14e335...`)、CA bundle(`714d457d...`)。
  executorは以前に監査した `...kaniko@sha256:2fa20b0a...` のexecutorとバイト単位で同一
  (SHA-256 `7e14e3351435495fb6c9eb18c16a418332f45a87e568fc64a362a1f2898a957b`)。
- 4ファイルはRAM内で固定SHA-256とサイズを照合し、payload作成時にもう一度照合する。
  取得は `docker image save` のtar streamを読む方式で、containerを一切作らない
  (レイヤーは上書き・whiteout・同一レイヤー内のhard linkをコンテナと同じ規則で解決し、
  symlink・zstd・欠落は拒否する)。今回pullしたimageは永続記録のうえ後始末で削除し、
  元からあるimageには触れない。
- ゲストはQEMUの `-sandbox on,obsolete=deny,elevateprivileges=deny,spawn=deny,resourcecontrol=deny`
  を付け、デバイスはvirtio-blk 2本・virtio-net・virtio-rngだけ。user-mode NATで外側の
  コンテナのbridgeを通る。ホストの共有・転送ポート・追加serial・monitorは無い。
- ゲストのディスクは外側のwritable layer上のsparseファイル(製品8 GiB、fixture 3 GiB)で、
  ゲストのRAM 4096 MiB・2 vCPUは外側の6 GiB/1.5 CPUの内側に収まる。
  `container rm -v` で一緒に回収される。
- ゲスト内ではKanikoをchroot(ゲストのディスク上)で動かす。Kanikoは自分のルートを
  消して置き換えるため、RAM上のinitとchrootを分ける。chrootには、コンテナランタイムと
  同じ形のproc/sys(ro)/最小限のdevtmpfs/pts/shmとresolv.conf等を用意する。結果は
  ゲストの2本目のserialだけに出し、chrootの`/dev`にはそのノードもディスクのノードも作らない。
- 入力(固定context、Dockerfile名、nonce、出力上限)は検証後にinitrdへ入れ、stdinの
  tar(kernelとinitrd)だけで渡す。コマンドラインには載せない。
- 出力は2本目のvirtio-blkへddし、外側がゲスト申告のsize/SHA-256で切り出して照合してから
  `/kaniko/output.tar` に置く。ホストは従来どおりRAMで受け取り、artifactの検証を行う。
- 終了コードは従来の意味を保つ: 成功0、Kaniko失敗はその終了コード、結果欠落125、
  出力不整合126、QEMU自身が非0で終了(クラッシュ等)127、OOMは外側がOOMKilled。結果行は
  QEMUが0で終了した場合だけ採用する。キャンセル/timeoutは外側のstop→PID0確認→rm→不在確認。
- ゲストのファイルを取り出すためのcontainerは作らないので、応答が失われたcreateが後から
  現れて回収を逃れることはない。Dockerに残りうる資源は外側のimageと2つのguest source imageだけで、
  どれも起動前に永続記録され、今回導入したものだけを削除する。

## CC自作fixtureの実結果（Docker Desktop 29.7.2、WSL2、2026-10-19）

fixtureは製品sourceを使わない。Debian slim(digest固定)にCC製のGo probeをCOPY/RUNし、
product profileと同じ1.5 CPU / 6 GiB / 448 PIDsで動かした。

| 確認 | 実値 |
|---|---|
| ゲストkernel | 6.6.71-linuxkit、RUNのCapEff `000001ffffffffff`(CAP_CHOWN/SETUID/MKNODあり) |
| ベースの非root所有の復元 | /var/mail 0:8 mode2775、/etc/shadow 0:42 mode0640、/usr/bin/chage 0:42 mode2755 |
| RUNの所有者変更 | /srv/app 1234:1234 mode0750、setgidファイル 1234:5678 mode2640 |
| 権限の降格 | uid1234に降格したプロセスは自分の領域に書け、/etc/shadowは拒否された |
| 出力image内の実ownership | レイヤーヘッダー var/mail 0:8:2775、srv/app 1234:1234:750 |
| 外側のcpu.max / memory.max / pids.max | 150000 100000 / 6442450944 / 448(daemonのinspectと一致) |
| 外側のCapEff / NoNewPrivs | 0000000000000000 / 1 |
| 実測ピーク(normal) | memory.peak 1,019,629,568、pids.peak 15(上限内) |
| 入力と出力 | normal成功の出力image 29,645,824 bytes、レイヤーのdigest・diff_idを全件照合 |

さらにproduct profile(8 GiB disk)で、Python slim(digest固定)にapt、pipでuv導入、
useradd/chownを行う製品Dockerfileに近い構成が成功した(約130〜150秒、出力image
約67 MB、ログ約10 KB)。layerヘッダーで home/app、srv/data、
srv/data/marker が 1001:1001 であることも確認した。

ライフサイクルの実Dockerでの確認: normal、cancel、timeout(どちらも
子プロセスの存在をprobeのマーカーで確認してから)、不確かなcreate(応答消失/未到達)、
inertな残骸のsweep、削除拒否後のrecovery。全ケースでPID0、container removed/absent、
volume absent、今回追加したimage(外側・kernel・userland)の削除、PhaseMayRelease=true を
確認した。

## 再現コマンド

```
cd tools/product-ci
go vet .
go test . -run 'TestBuilder|TestRelease|TestSweep|TestKernel|TestBuilt|TestProduct|TestNameHold|TestPrepare|TestGuest|TestOuter|TestDockerGuest' -count=1
# 実Docker(新しいsafe-delete所有ディレクトリを指定する)
CC_RELEASE_BUILD_FIXTURE_ROOT=<dir> go test . -run TestReleaseBuilderRealFixture -count=1 -v -timeout 90m
```

## 制限と未確認

- 実製品sourceのbuild成功は未確認。製品preflightの再実行は親2757が、配布後の
  実行ファイルで行う。fixtureの成功は製品の成功ではない。
- TCGはCPUを命令単位で実行するため遅い。fixtureでは問題にならなかったが、製品のbuild時間は
  実測前で、上限は従来どおり45分。超過はtimeout(回収は従来どおり)になる。
- ゲストのrootはゲストのカーネルで全capabilityを持つ。ゲストはbuildのためだけに作られ、
  使い捨てで、外側の資源・ネットワーク・成果物の境界は従来と同じ。chrootは通常のコンテナ
  ランタイムと同じ形のファイルシステムを見せるためのもので、ゲスト内のroot権限を持つ
  悪意あるRUNに対する境界ではない。実行するのは監査済みの固定Dockerfileだけであり、
  結果の偽造は脅威モデルの外とする。ホストが受け取るのは、寸法・SHA-256・全レイヤーの
  digest/diff_idを照合したimageだけである。
- 起動のたびにkernel imageなど約250 MBをpullする(元からあるimageは再利用し、今回追加した
  ものだけを削除する)。
- 外側のQEMU imageはDocker Hubの `tianon/qemu` (Tianon Gravi)、kernelはLinuxKit、
  userlandはKaniko公式forkのimageを、いずれもdigestで固定した。供給元の更新で意味が
  変わることはない。digestを変える場合は、上の表の値と一緒に監査し直す。
