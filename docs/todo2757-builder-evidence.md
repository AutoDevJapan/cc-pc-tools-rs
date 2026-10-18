# 配備CI用ビルド処理の実確認

ToDo2757。正式な製品job、配備、本番反映の成功ではない。
原資料と過去の失敗、未取得logを保持する。

## 正式source読取り

PR199はhead0f3309699f455f2ae9355e9a9e5b1705550f121bの全6差分/全文を
独立Sonnet2796が読みMust0。実入力全文と最終event/原応答の一致を確認した。
同head CI success8/neutral1/skipped1後に通常merge
bfbb6143ff8724a810bdfbaf4f917e8a2f93c74d。
検証head/mergeのmodule tree8f78eac130d9f9c884f57e8088ee1627baf68b7aは一致。

正式Windows SHA256 F2C6B3DB8B6D87D0E80E194AAEBB7D81FEF96E76A07347FBE2E33487EB2D1CF2、
Linux SHA256 64BD99F332D094E1E1E19491762C94350ED84ABC7871E10D9D01535DD42B7D3B。
設置済みrelease-source-inspectはexit0。repo1285649124/PR1002の固定merge41090と
tree007b84082008ea9effd5052f63856273aabbfb17を前後照合した。
914files/150directories/9123951file bytes、入力tar9913856bytes。
入力SHA256はfa0096ecd620e7453d3671bf2f4982fd2be9b8b43e09bf76f758f8303c847b6b。
sourceと名称はRAMのみ。実行許可はfalse。

## 固定した統合検査の構成

正式sourceと同じ不変treeから、同じ正式Go moduleの読取り/封印関数を使って
RAM内だけで追加の構造調査をした。原文はホストへ展開・保存していない。
調査結果の入力SHA256も上の正式実結果と一致した。

- Compose path SHA256: a37d4abc31af69a0ea3dcc8349af3360e7331c659efd3e1845baf2116cfc44f4
- Compose blob: 638b8b2e0a0ab60f7ceb42469e0c350192d21cb5
- Compose bytes SHA256: d5a064ad6dcc62d0654d5777adfec5dbbdbd4068b129a2c1cfd3591a0a66f797
- 4services。3つはdigest固定image、1つは既存Dockerfileをbuildする。
- Dockerfile blob: 514d16a9e9040ecaccbb52a683eb8f465966643e
- Dockerfile SHA256: 4b9ea56d35aa825eaded97671ec593a2c396f7ae75613f36da364a5a51cef964
- Dockerfile path SHA256: 2b953517a283d081ae392ee63728340d262f0e769bcd28edb8700be22e8b2571
- build context directory SHA256: 10e08a419e850eba1ebba18fdd28eb7ec1b7e8baa9bcc3b973e2b8891ec726be
- Dockerfileは639bytes、Python base digest固定。COPY3、RUN3、ENV1、WORKDIR1。
- backend workflowのstep2/3はsetup-buildx/build-push action。単なるrunner label変更では足りない。
- 元のCompose検査、条件、実結果、artifact保存、終了回収を省略しない。

値の意味全体、全script伝播、実drain、token、trust受領値をこの構造調査だけで
確認済みとしない。raw workflow SHA256とsemantic digestも同一視しない。

## tokenを使わない最小ビルドfixture

公式upstreamの公開imageを使うKaniko v1.28.6の互換性を調べた。
fixtureは新しく書いたGo probeとFROM scratch/COPY/RUNだけで、製品sourceは渡していない。
registryへpushせず、ホストのsource bind、Docker socket、device、privileged、cap_addなし。

固定tool image:
ghcr.io/osscontainertools/kaniko@sha256:2fa20b0a4b40c7db72b479d57e55c2cf79f2178533b2d3bcbd129d6bd3c087ed

最初は出力先/output/image.tarの親directoryが存在せず失敗した。
原stderrは103bytes/SHA256 18de115c3b9fb1d8e22074a5752b6729cf205872578ac57dc3ae8076e3af0bb3。
同じtool digest、入力、権限、資源のまま、既存の/kaniko/output.tarへ出力先を直した。
第2回はexit0、image tar1273344bytes。fixtureのRUNが記録したkernel実値は以下。

|項目|実値|
|---|---|
|cpu.max|50000 100000|
|memory.max|536870912|
|pids.max|128|
|CapEff|0000000000000000|
|NoNewPrivs|1|

outer inspectも0.5CPU/512MiB/128pids/networknoneと隔離条件を照合した。
両回ともnonce所有照合、停止後PID0、通常rm-v、不在再確認を実施した。
初回前に無かったtool image tagも両回とも通常image rmで回収した。
作ったimageをDockerへloadしておらず、製品runner登録/実CI/Prd変更は0。

これはscratch baseとGo probeの最小fixtureだけの結果。
実製品のPython base展開、apt/uvを含むRUN、Dockerfile全体、実資源phase合計、
切断/timeout、artifactの実利用は未確認。権限の緩和や強制flagで補っていない。

## 次の実装

独立したGo部品として、封印された既知sourceからだけbuild入力を作り、同じ
Docker namespaceでbuilder全体へCPU/memory/pidsを適用する。未知source/任意argv/URL/
hostpath/image/registry pushを受け付けない。builderはrunnerと分離し、合計予算を保持する。
実製品buildは未配布部品で行わない。ソース、別モデル、CI、通常merge、正式配布の順を守る。
既存runner/guard/manifest/queueを変えず、実装担当と親の成果・検査を別々に保存する。

## 2026-10-19 追記（ToDo2848）

実製品sourceに対する正式配布済みpreflightは、外側のcap-drop ALLの下でKanikoが
ベースイメージのchown(operation not permitted)に失敗し、build exit1だった。
上の最小fixtureはscratchベースだったため、この種の失敗を検出できなかった。
原因の再現と修復(ソフトウェアエミュレートしたゲスト内でKanikoを実行)、非root所有の
ベースを使う実fixtureの結果は [todo2848-vm-builder.md](todo2848-vm-builder.md) に記録した。

非公開の原receiptと再現用のGo fixtureはToDo2757 worktreeの.ci-workに保持する。
build temp7e66c30d07a55d6da5faa9ebb731951cとresearch temp6e94963e29b086a8f2a8b94b8c2e337dは
safe-deleteのplan→別通常削除→finish source_absentで回収した。

一次資料:
- https://docs.docker.com/reference/cli/docker/image/build/
- https://github.com/osscontainertools/kaniko/releases/tag/v1.28.6
- https://github.com/osscontainertools/kaniko#using-standard-input
- https://github.com/osscontainertools/kaniko#security
