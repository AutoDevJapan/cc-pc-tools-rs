# GoDD Press2766：使い捨て環境の準備と統合診断の有限レビュー

あなたは2766の読取レビュー担当。新しい子ジョブ/ToDo/質問/Discordは作らず、source/API mutation/VM/Cargoを実行しない。結果保存・commit・状態更新は不要。最終返信で指摘を返す。親2766/2642の完了を待たない。

## 固定対象

修復repo1406818713 AutoDevJapan/cc-pc-tools-rs。base f79cd1499c063525609074fae908bf8560e610b0 → head2349f4ee404e076894bae759fc72b18ac4254917 / tree dc1d4be45bf096c53f6127059270eaa03c70171f。固定blobをgit showで読み、作業HEAD/後続記録と混同しない。

元3299は固定7d6/tree7b0の期限修正を読了してMust0/Suggestion3/Nit3。実job20261021-163547-todo-3299/exit0/succeeded、対応turn_context anthropic/claude-sonnet-5-5/maxと原応答一致を2766が照合。REVIEW-2766-DEADLINE-3299.mdを全文読み、旧32fileレビューを重複しない。

対象は新しいPress専用SDK準備と、私的workflowを隔離容器へRAM経由で送るGit merge-file診断だけ。元製品repo1386702183/PR175はprivate/open/Draft/headb2/base4dcのまま。製品branch編集・merge/配布・CodeQL9件・元29/2191は対象外。今回の成功は公式CIではない。

## 全文を読むファイル

- tools/product-ci/pressci/sdk.go、sdk_test.go
- tools/product-ci/pressci/merge_probe_test.go、merge_probe_windows_test.go、merge_probe_linux_test.go
- tools/ci-guest-2737/press_sdk_linux.go
- tools/ci-guest-2737/public_sdk_probe_windows.go
- tools/ci-guest-2737/timeout_windows.go、timeout_windows_test.go
- tools/ci-guest-2737/job_policy.go
- tools/windows-ci-runner-2737/press_sdk_windows.go、press_sdk_windows_test.go

次は全差分と変更関数/直接呼出し元：ci-guestのgate_windows.go（新閉mode）、host_windows.go（CLI）、guest_linux.go（probe-public-sdkのPress選択）、protocol.go（SDK inventory）、job_contract.go（event）、job_linux.go（prepare before materialize）、job_windows.go（pre-token budget/SDK failure記録）、windows_job_windows.go（pre-session budget）。Windows runner guest_windows.goのprepare/fixedEnvironmentValues、contract.goのsessionTimeout、変更されたdeadline/channel/pressテストも差分を確認。

必要なら既存bootSDKForSource/runGatedJob/cleanup/fence/takeNormalCILock、Windows runTree/fixedGuestService/runnerAccessCommandsForの局所を追加。VM/Windows helper/sourcepacket kernelは複製していない。secure_files*/source_materialization/normal_ci_admission本体や原製品folder/名簿/保護一覧/guard/manifest/認証値へ探索を広げない。原GitHubAPIの新取得は不要。

## 実装と実測

- 全SDK assetは閉selectorで公式HTTPS URL/bytes/SHA256固定。Rustup1.28.2 Windowsは公式 .sha256 と実bytes一致。Google Chrome MSIは取得時にAuthenticode Valid / CN=Google LLCを確認後hash固定。Linux Google signing keyも同bytes/hash固定。requestからURL/command/secretを受け取らず、HTTP proxy/別host redirectを拒否する。upstream変更はfail closedで、新版を黙って採用しない。
- Windowsは固定SYSTEM guest serviceだけがfresh press-sdkを作り、公開rustup-init --no-modify-path --default-toolchain1.95.0/componentsを起動。V2 HTTPSだけChrome MSIを/qn/norestart。Press PATHはportable1.97.1を外してcargo-home/binとstock node24へ。α環境不変。host test binaryはinstall入口で拒否。元ACLのcargo/rustup writable箇所を利用し、親SDKをホストで変更しない。Windows実guestへの新処理実行は0で、Server2025 assetと元matrix Windows2022との差は未実証として保持。
- Linuxは新しいclone/rootでUbuntu24.04を確認し、single-use準備guard、O_EXCLのGoogle key/source list、APT署名検証を通るChrome導入、user1001でRust1.95.0/rustfmt/clippyを導入。元workflow側の導入/検査手順は削らない。固定command groupをcancel時killし、WaitDelayを付ける。各stdout/stderrは32KiB上限、過大出力はcommand失敗。public SDKの出力だけをmetadataと一緒に記録し、tokenや製品sourceを渡さない。
- public probeの新modeは固定Press pinをseedへ選ぶだけで製品Fetch/登録/runner/Cargoをしない。元canonical cargo_gate/normalCI lock/intent/bootSDKForSource/VM cleanup/fenceを再利用。live-jobではsourcepacket.Decode後、token前に準備する。失敗metadataもhostへ保存する。
- SDK準備時間を元timeoutから差し引かないため、PressだけLinuxguest role+30、host socket+35/listener+35、Windows session+40、wholehost+60へ拡張。元workflow90/20/360は変更0、αの期限は維持。gate待ち/boot後に残りがrole+40未満なら登録前に拒否。3299 Suggestion1/3への処置。timer/factory配線追加テスト、shard隣接交換、role一覧共通化、1秒許容のNitは今回未対応として残す。
- 元Windows production callerだけをoverlayして新PATH検査は「rustup proxyでなく1.97.1」で実FAIL/exit1。修正版GREEN/exit0。全product-ci race(main1.683s/pressci4.774s)/runner16.317s/guest19.433sとvet成功。最終guest focused/Windows PATH focused、Linux test compile成功。最後のログ追加後はguest focusedと実Linux SDK probeで検証。原完全logは2766の私的保存に保持する。
- clean312b9612ef5bcc516f4804e71f614ac004f3c26fのhost/Linux helperをbuildし同rev/clean/hashを確認。hostSHA bba4f0a262c7317ed555b8fb54157967e9c7ea53d573d3a630bf58aa40c2bd5c。使い捨てHyper-V VM af6757c7-0dc6-4a32-89d7-1cec779bb575でpublic probeを実行。Ubuntu24.04、Chrome156.0.8078.47、rustc1.95.0(59807616e 2026-04-14)、5command/3Rust probe全exit0、compiler単体SHA bff349e72704ff70bc08a234a3847338e797065bbedde5e556808bc87b7bf7c6。SDK準備だけで、ブラウザ実動作/製品Rust/公式CI/永続goldenの再利用検証ではない。VM停止/メモリ0/除去、normalCI intent不在、gate release/host実終了exit0を確認。工具containerは既存の同ID/所有/非hostbind/GPU等を照合し停止/PID0、再利用assetとして保持。
- workflow診断はopt-inだけ。host read-only APIで原owner/private/admin/ref/head/tree/3workflowを前後検証。baseとheadとmainのworkflow原bytesはhostRAMだけ、containerstdinへ転送。容器はCPU1/RAM256MiB/通信なし/read-only/全cap drop/no new privileges、GPU/hostbind/socketなし。guestはfixed名のtmpfsへnoexecで保存し、物理git hash-objectで全対象workflow blobを照合、git merge-fileを実行し、bytesを返さずhash/exit/conflict数だけ返す。全製品のblob/tree/commit証明ではない。
- 初回診断は/workがroot0755でimage default runnerに書けず失敗。guest stdoutを初版が保存しなかった欠落を保持し、実mode/非writable/空workを確認して原因を分離。失敗容器c370...を停止/PID0/除去/不在確認後、mode1777へ修正し失敗時もguestの閉error/metadata outputを保持。別nonceの修正後は実成功exit0。local-checksは物理merge exit7/conflict7、V2 HTTPS/hiddenは0/0。診断で競合を観測した成功であり、製品CI/mergeの成功ではない。最後の容器87a4f...もPID0/除去/不在、製品API mutation0。

## 観点と返却

許可済みの通常検証をPolicy名や未採用案だけで拒否しない。実被害/実制約を根拠に、hostへのinstaller実行や製品source保存、秘密/owner profile/hostsocket/GPUへの到達が無いか、SDK auth前prepare/取消し/排出/期限、source-free probeとlive authorityの分離、公開URLのredirect/pin、私的workflowをstdoutやhostfileへ漏らさない点、元検査/αの回帰を確認する。

今回コードは実検証用の専用物だけで、常用版の配布や製品PR変更は0。通常CIは未成功。7競合は親へ技術的な範囲調整として返し、親のbranchを勝手にmerge/rebase/書換えしない。本人の新判断は不要。親/子の完了待ち循環を作らない。

D:/CC/AGENTS.md、作業AGENTS、D:/CC/Policies/owner-agent-conduct-20261021.md、TODO-2766-CHECKPOINT.mdの元scopeを適用。最新方針は対応不要ならメンション/--notifyなし。原scopeのDiscord直接送信禁止も維持。一次資料は必要ならGo exec/context/http、Microsoft msiexec、Rustup公式installation、Git merge-file、Docker runの公式だけ。

固定範囲を読み終えたら先頭は「状態: 完了」、次段落に読取レビューだけの完了と書く。Must/Suggestion/Nitとfile:line/具体修正、固定版、全文/局所読了と未読、実検査と未実施を3000字以内で返す。Mustがあっても読取終了を保持し、修正は2766が行う。モデルは親が実job/turn_contextで照合するので自己推測しない。