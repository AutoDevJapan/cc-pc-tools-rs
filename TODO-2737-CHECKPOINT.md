# ToDo2737: GoDD MCP αの通常CI

## 2026-10-17 01:21再開後の保存点（こちらを優先）

### 02時台の追加実測

9f4e256へ保存後、最初のAzure VHD guestは作成/起動したがreadinessが4分で未確認となりexit1。
そのVMはOff/メモリ0/除去が成功した。最初のserial本文は未保存であり原因を推定で確定しない。
原source/CI/GitHub登録はこの検査に一切入っていない。
正式な汎用qcow2（625612288 bytes、SHA256 6a81c37564db9b1ee84e141922625e1d7c5b389b99bb3c572e0243607d5bb4d2）を別に取得。
自分の工具containerでpublic qemu-utilsを入れ、同じ検証済みRAM bytesをVHDXへ変換した。
attempt-79d80772587251249e3fa615122232a2で新規guestを作り、smoke-genericはexit0。
mTLS、実parent/child PID、normal/EOF cancel、実group消滅、VM Off/メモリ0/除去/不在を確認した。
guest-smoke.private.jsonもofficial_ci_success=false/product_source=falseのまま。
新attemptの生boot logは私的保存し、本文や位置をmodel/stdoutに出さずmarker件数だけ確認した。
New-VMの動的memoryがtrue/最大1TiBだったため、次のconstructorから固定2GiBを明示して修正した。
CPU2は元どおり。fixtureはpassしたが、この値を本番のCargo受付済み割当とは呼ばない。

作成したVM2件は回収済み。工具containerだけが次のSDK準備用に稼働中。
初回Go/Linux転送/VM失敗の全記録を保持し、同じデータの上書き・再実行はしない。
新しいhost helperはci2737-host-v5.exe、Linux側はci2737-guest-v2。公開imageの取得/展開/変換は済んでいる。
Windows2746はa218d5eのhost Job Object fixtureを保存したがguest未作成。現在は通常枠で自動続行予定。
同じ2746へ公式Windows imageと無人provisioning/Go serviceの技術手順を追加配送した。新しい同種ToDoはない。
2748は新Go補助の別モデルreviewを共有ToDoで受付済み、実応答はまだない。
旧2740 Must0を新guest差分のMust0へ読み替えない。
親2641と自分2737へ上記結果をnote配送済み。状態や元18/1153の完了は変更していない。

9f4e256のscheduler checkpoint初回はTimeoutErrorだった。保存入力の拒否ではない。
実装の共有lock取得で出るエラーであり、scheduler.lock/todo.lockがその後利用可能なことを
非変更のbyte lock probeで確認してから、同じ正規コマンドを1回再試行しexit0/saved=true。
共有lock削除・scheduler設定変更・無限再試行はしていない。
次の現HEADでも終了/待機前にcheckpointを保存する。

job20261017-012131-todo-2737。前回のec55875から再開し、2740の実応答を確認した。
review-2740-received-original.mdは別モデルClaude Sonnet 5.5のMust0、Suggestion4件。
このreviewは旧69627cbのread-onlyな経路案だけで、後述のguest補助は含まない。
原PR3613は実APIで旧固定b194のまま、同owner/private/admin/nonfork/同repoを再照合した。
原製品source・原workflow・原PR/branch・公式CI/登録・予算・共用runnerは変更していない。

Windowsの専用guest準備は重複しない既存ToDo2746へ有限specを保存した。
job20261017-013926-todo-2746の起動まで確認した。製品検査/公式登録はその依頼に含めない。
こちらはtools/ci-guest-2737に、独立Linux guest用のGo補助を書いている。
現時点ではproduct executor、Cargo、公式runner/CI、source転送は未実装。

新しい所有rootはC:/Users/makhmeto/AppData/Local/Temp/codex-owned-9389b3ad45abf8cb9bafdfe93159097c。
safe-delete ID9389b3ad45abf8cb9bafdfe93159097c。このrootは継続用で消さない。
公開Ubuntu release-20260926のAzure VHD tarball847957663 bytesを正式配布URLから取得し、
SHA256 3543723afd820d7a8a64ea7399376856a95ce15200439c38447170652a60a5f3を照合。
これは公開SDKであって製品sourceではない。host上のVHDも公開SDKだけ。
初回展開はtar type S（GNU sparse）を拒否してexit1、VHDは未作成だった。
Go Readerのsparse対応を認めて修正後に展開がexit0。32213303808 bytesとfooter conectixを確認。
ダウンロード・展開を繰り返さず、linux-image-extracted.private.jsonを使う。

SDK工具containerはtools-created/ready.private.jsonにID/name/所有を保持。
cc.todo=2737で新設した自分の1個だけで、genisoimage導入済み。既存imageは未変更。
GoのWindows unit/vet/build、Linux buildが成功した。
最初のLinux test転送はDocker cpが稼働中tmpfsに届かずchmodで失敗。
exec stdinへ直した次の実行も/workがnoexecで失敗。mountの実optionsで確認した。
データ用tmpfsのnoexecを保ち、工具containerの/usr/local/binへ実行ファイルを配置して修正。
修正後の実Linux単体検査は全て成功し、正常/取消しの実子PID・group消滅を確認した。
元log linux-unit-v3.private.logのSHA256はE4329D6602AB11F7D2B164C74F830076B37BA409DBC9235075D8ADC5E2505CFB。
これは工具containerのfixtureで、Hyper-V guestや原CI成功ではない。

次はci2737-host-v4.exe smoke <owned-root> <owned-root>/ci2737-guest-v2を1回実行する。
VM作成/起動/guest TLS/正常取消し/停止/除去を個別に確かめる。VMはまだ作っていない。
工具containerは種ISO作成に必要なため継続中。終了後cleanup-toolsで停止/PID0/除去を確認する。
guest補助の別モデルreviewと、Cargo受付・原source/head/tree・checkout認証除去・公式jobは残る。
2746だけの完了待ちにする段階では正式wait --on 2746を使い、親2641の完了は待たない。
終了/待機前にこのjobと実HEADのscheduler checkpointを保存する。

## 現在の結果

通常CIは未実行であり、成功ではない。元PR3613はopenで、保存された固定headのまま。
原run38893656960のattempt1は、verifyとCI GateがActions予算で検査開始前に失敗した。
WindowsとPR tree receiptも未実行。4jobとも工程0件・runner未割当を再確認した。
既存のCodeQL成功、旧headのnative受入、37件の静的確認を再実行・読み替えしていない。

今回の独立checkは主担当のソース編集・独立PR・mergeを担当しない。
自分の作業branchだけで、親へ返すためのread-onlyな実行経路案を実装した。
Go処理は製品workflowをRAMで読み、YAMLの構造とdigestを確認する。
4jobのruns-on scalarだけを置換した案のSHA256を計算し、原sourceは保存しない。
Windowsの13個のCargo commandを確認した。その他の全bytesを保持する置換方式で、
Docker parity、profile、直列指定、条件、権限、timeout、必須gateを削らない。

## 固定値

- repo_id: 1154066087、PR3613、owner/private/admin/nonfork/同head/base repoを実API照合。
- source: b194d66524ef86330bb73ca3f22f9e5dd6ae0a07
- tree: f7eea82c96449b7749f98dd6c4b0344b64f1d651
- original workflow SHA256: 01d3351d4a6bce1faeb7e3b1eaf29555affcc304ea7b91caa979825cc94bc4b0
- proposed workflow SHA256: f42c1ab5aedfc6d3e14cdee105a3a4f6a1041ea600b677dd77d5c419d5a94cf0
- run38893656960 / attempt1 / failure。
- checks116948883594,116948895629のfailure annotationを実照合。
- 各annotation JSON-value SHA256: 0bc72aa9ae2bcba0ce8b0cf73991da0d71f374528856a2556dc947d91655fde9
- official原logは未開始jobのため要求していない。annotation hashを原log hashと呼ばない。

## 実装と検査

tools/product-ciのnormal-ci-planだけを追加。通常の実行処理・常駐runnerは変更していない。
実行前後のsource固定、API tree、原run/job/annotationと提案をreceiptへ分けて保存。
proposal_only=true、all_required_checks_success=false。

Goの初回検査で合成fixtureのテスト2件が失敗した。
Linux共用labelが3回あるのに1回としたassertと、branch中のslashのURL encoding漏れだった。
テストを直し、Go test/vetとWindows buildが成功した。修正後の実API検査もexit0。
これは実runner・Docker parity・Windows13工程の実行成功ではない。

## 残る実行基盤

1. 既存CodeQL guestはPATHからCargoを除外し、Docker daemonも渡していない。
   normal CIの代わりに起動しない。原repoのrunner inventoryも今回0件だった。
2. 新規の専用Linux実行資源が必要。独立したDocker parityと共有Cargo gate、
   専用target/sccache、source HEAD/tree、checkout認証除去、normal/cancel回収を満たす。
   既存のCC共用bridge/runnerとhost Docker socketを借りない。
3. 新規の専用Windows実行資源が必要。原13工程/profile/直列/20分を保持する。
   原sourceはRAMまたは専用guestへ限定。旧2710のhost-sourceを再利用しない。
   今回、host OSはWindows11、Hyper-Vは利用可能。RAM disk用コマンドは見つからなかった。
   これはWindows guestやRAM diskの不可能性を証明しない。
4. 両方の実行基盤と別モデルMust解消を確認してから、親が原branchへの通常更新を行う。
   sourceが変わったら新head/treeを親が明示固定し、新しい必要CI/CodeQLを整理する。
   旧headを黙って採用し直さず、公式workflow_dispatchのfull経路を使う。
5. 原job/同SHA/物理tree/全工程/原log hash/cleanupを親2641へ配送する。
   原PRmergeとPrdは親の担当。このcheckで原18/1153や製品全体を完了にしない。

## 所有データと再開

- job: 20261017-004230-todo-2737
- branch: scheduler/todo-2737-godd-mcp-ci
- safe-delete所有ID: 334ed87952cf137434cd6214b64e62b3
- 所有root: C:/Users/makhmeto/AppData/Local/Temp/codex-owned-334ed87952cf137434cd6214b64e62b3
- rootには自分のGo実行ファイルと数値/hash主体の私的receiptだけを保存。製品checkoutなし。
- 原2710/2648/2730の旧資源には一切変更しない。
- Go実装の保存commit: 69627cba7fba563f7e409dd6c9de07d67947d60b。push済み。
- 別モデルreviewは既存ToDo2740、gpt-6-luna/high、保存specあり。実応答はまだない。
  最初の登録はmodel名のslashがCLIで不受理、次は親2641の終了後だったためsubwork受付不可。
  親の資源例外を使わず、通常の共有ToDo枠へ2740を登録した。子Codexの直接起動はない。
- 上記Temp rootは終了済みのGoバイナリとmetadataだけを含んでいた。
  receipt等3fileを永続証拠へ全SHA一致で保存後、plan→別呼出し通常削除→finish source_absent。
- 永続証拠: D:/CC/Tools/product-ci/evidence/todo-2737-20261017/334ed87952cf137434cd6214b64e62b3
  manifest.private.json SHA256: 50E90D4FC44DC5D157359C1F4874304CDD9C9B4E92A6881177340F0927E87E83
- 旧Temp rootから再開しない。reviewのspecは共有ToDoの保存済みコピーを使う。
- 終了前に親2641と自分のnoteへ結果を配送し、実HEADのscheduler checkpointを保存する。
  2740だけを待つ区切りはwait --on 2740で記録し、レビュー実応答後にMustを修正する。
  続く実行基盤と通常CIは未完了のまま保持する。レビュー完了をCI成功や製品完了と呼ばない。
- 回収はsafe-delete plan、別呼び出し通常削除、finish。拒否時は正規quarantineのみ。

## 2026-10-17 ToDo2748 Linux guest 補助の別モデルレビュー

対象は固定commit `8439d62850b7e9cc5b1632ca90183c40514a8923` の `tools/ci-guest-2737/` のみ。レビュー応答はcc-pc-tools-rsの担当ブランチへcommit `4b602b5e9e42000b646381a2a7a84f03320af23c` として保存・push済み。

### Must（1）

- `tools/ci-guest-2737/host_windows.go:334-345, 456-465` — `New-VM` 後、作成receiptの書込み前に設定処理が失敗すると、作成済みVMのIDを読み戻さず、`smoke` のID条件により後始末が登録されない。`Start-VM` 失敗でもreceiptを読み戻さないため、VMが残り得る。Hyper-V資源を占有し、停止確認前のVMが残る可能性がある。`New-VM` 直後にID・name・notes・rootを記録し、失敗時もreceiptを読み、厳密な所有照合後に停止・メモリ0・除去を行う。照合不能時は要照合状態を保持する。

### Suggestion（0）
なし。

### Nit（0）
なし。

README/checkpointは、この補助の実測を原製品source・公式runner登録・workflow・Cargo・通常CIの成功から明確に分離している。TLS pin/readiness、fixture要求境界、実Linux子プロセスとgroup回収、通常成功時のVM回収、窓非表示とAdminBroker利用にMustはない。元2737のToDo状態は変更していない。

## 2026-10-17 再開時点の最新状態（上の旧job/root記述を更新）

- jobは20261017-012131-todo-2737、branchはscheduler/todo-2737-godd-mcp-ci。
- 所有rootは C:/Users/makhmeto/AppData/Local/Temp/codex-owned-9389b3ad45abf8cb9bafdfe93159097c。
  safe-delete所有ID9389b3ad45abf8cb9bafdfe93159097c。未完了SDK資産と原失敗証拠を保持中。
- レビュー2748のMust1を実装で修正した。VMの返却IDをNew-VM直後に記録し、エラー時も読む。
  設定前失敗とCreateNewによるreceipt保存失敗を別々の新規VMで実測し、Off/メモリ0/除去/CIM不在まで成功。
  前者VM 0f3eeffd-6b1a-4bd3-8ef3-33f427d138c7、後者f06fd48a-522c-4e20-bf31-5d4d76f73144。
- 公開SDK転送の2つの旧attemptは切断失敗、双方VM回収済み。最初の失敗を保持して再実行成功と混同しない。
  公開runnerアーカイブ227624222 bytesの展開検査で、通常directory末尾slashと内部symlink拒否が判明し修正。
  その後の検査containerの/tmp容量不足も原logを保持し、自分のcontainer内の専用領域で公開全SDK展開成功。
  成功log public-sdk-test-3f724acbffff3b253a0a2349d2545cd2.private.log SHA256
  307AECD7CAF14A080FDA54115BAED71F6E1D043F7886CB4AB513F8B74DE1C544。
- 最新Linux unit log linux-unit-452a387604659f8a.private.log SHA256
  FE06A7773B63F31C2FEFC2FB238E73D8AD670DE52D161691C1D4A2E3781A6712。Windows Go test/vet/build成功。
- 実行中の新SDK smokeはhost-v14.exe/ci2737-guest-v7、sdk-v14.stdout/stderr.logに結果。
  eventsの完了を確認してから結果を回収し、同じコマンドを重複実行しない。typed sdk-resultとcleanupを別々に確認する。
- 工具containerはtools-ready.private.jsonの自分のIDだけが稼働中。原SDK imageは不変更。
- 原PR3613/b194、公式CI失敗は未変更。製品source/runner登録/Cargo/公式CIは未接続で未実行。
  旧2740 Must0はnormal-ci-planだけ、2748は旧guestだけ。SDKと今回の修正は追加有限レビューが必要。
- Windows実guest準備は既存2746で続行中。別の同じ作業を登録しない。
- 残り: SDK guest実bootstrap→source物理HEAD/tree→単一公式job受付と共有Cargoゲート→公式4jobのroute/dispatchと13工程/Docker parity→原log hash/cleanupを親2641へ返す。
  親の原PRmerge/Prd、CodeQL37件の別確認、旧資源の回収はこのcheckで重複しない。

## 2026-10-17 公開SDK guest実行の結果と次回の入口

- SDK smokeはexit0。attemptはsdk-attempt-6bb6e61d104863319f64aad3c81a9c52。
  VM e106ca52-e833-47cf-9488-a89ad6fd1fa3はOff/メモリ0/除去済み。
  同guestの新規userでrunner2.338.0、Cargo1.97.1、Docker29.1.3、buildx0.30.1、sccache0.18.0の版確認が成功。
  Cargo build/test、原source、runner登録、原CIは実行していない。
  SDK原log SHA256 648ac2ac87b55aee2c1ec6b0bc3c9e823b7bc3205f9bf739d3428d6afb9c7208。
- 新SDK準備とMust修正のsource保存commit7e6f8b9551cead6bb5b388dc26db904e9599bb69はpush先一致。
  同commitのscheduler checkpointもexit0/saved=true。
- 原失敗/修正後のreceiptとlog等94fileを全SHA一致で永続化した。
  D:/CC/Tools/product-ci/evidence/todo-2737-20261017/phase-sdk-7e6f8b9/evidence-hashes.private.json
  manifest SHA256 3F8F5E544D5986615039AFCC61B346D26DA03AFD76C57A20A1CE44742862942E。
- SDK用VHDXは上記SDK attempt/guest.vhdx（製品sourceなし、runner未登録）。再生成せず既存SDK資産として使える。
  SDK template receiptは所有root/sdk-template-ready.private.json。使用前に記録SHAと実file、元VM不在を再照合する。
  private TLS keyはRAMのみでguest停止時に失われ、seedには公開pinだけ。host所有rootを製品checkoutへ流用しない。
- 自分の工具containerは停止してPID0を確認済み。削除はしていない。tools-paused.private.jsonを参照。
  続行時はtools-ready.private.jsonのexact ID/name/cc.todo/cc.instance/image/mount/devicesを確認し、そのIDだけstartする。
  /workと/tmpのtmpfsは停止で消失したため古いseed/test executableを利用しない。public生成資産はhost所有rootにある。
- 追加レビューは2753へ保存spec付き通常受付済み。base8439d62→head7e6f8b9、read-only、原応答はjob last_message.mdへ返す。
  登録時点でtodoで、実応答はまだない。2748のMustが追加レビュー済みと誤認しない。
- 独立したsourcepacket/materializerの実装は2754へ保存spec付きで通常受付済み。
  tools/ci-source-2737独立Go moduleのみ、原製品を取得せず合成fixtureで実装/検査する。既存source_materializationの再利用を指示。
  統合側2737は実応答とcommitを照合し、自分のbranchへ必要差分だけ取り込み、実Linux合成Git検査と別モデルレビューを行う。
  2754は原PR/CI/runner/VMを変更しない。子Codex直接起動はない。
- Windows専用guestは既存2746の実結果を待つ。2753/2754/2746へ重複依頼・同一コマンドを送らない。
- 残る自分の処理は、各成果を統合して同PR/head/refを固定した公式runner単一job受付とCargo共有ゲートを接続し、原CIを完走すること。
  Docker parity、Windows13工程/profile/直列/20分、workspace/関連crate/doctest、checkout認証除去を維持する。
  route更新は原PR同branch expected旧SHA/force=falseだけ。新head/tree・CodeQL再解析要否を親2641へ配送する。
  原PRmerge/Prdは親2641のみ、37件の静的確認を再登録しない。追加費用・本人PC操作は不要。

## 2026-10-17 job 20261017-112125-todo-2737 の再開結果

- 2746/2753/2754はdone。2753原応答SHA2564155DFD2F261AF93258D049F5C343EA33828179307421D1150343CD915CBBBF5を照合。
  Must1は通常receipt保存とrollbackの両失敗でIDが失われる経路。New-VM前に別の排他的64KiB復旧journalを確保し、IDを通常receipt/回収前にflushする設計へ修正。
  host-v15.exeの新しいdouble-failure実fixtureはexit0。journalから正確なIDを回復し、そのVMだけを回収した。
- 2753は固定head7e6のTODO-2737-CHECKPOINT.mdを不存在と申告したが、git cat-file -e同head:pathはexit0。
  元のbase SHA申告も末尾欠落がある。原応答は保持し、未読を合格にせず、次の新差分レビューで正しいhead/pathを渡す。
  root CLAUDE.mdはこのcc-pc-tools-rsには無い。原製品のCLAUDE.mdとは区別する。
- 2754のmodule（原code7221708、docs ee6ecfec）をtools/ci-source-2737へ取り込み、unsigned API commit payload、GitHub global PAX、出力上限、gzip drain上限、実保存file hash/Unix mode照合を修正した。
  Windows Go test/vetと実Linux合成Git全件が成功。実GitHub Fetchの初回packet拒否はglobal PAX誤認が原因で修正。
- 原GitHubのAGENTS.md/CLAUDE.mdを同b194のRAM sourceから読み直した。hashは235dcf3b2c3ad55fafa835c8f3d3309fd26ef70451270a381ff65be95e895493 / 70c7de2f3d35091324a75ea72177c061e014f38aeac49bf521d9f3110f8890de。
  guidance検査の最初の2回は失敗し、hostのstdout fileは空。成功時はpipe/RAMだけで読んだ。原sourceをhostへcheckout/保存していない。
- source smokeはhost-v19.exe/ci2737-guest-v9、exit0。所有root/source-attempt-6b1737b0aa5a383d24f838e58fe61813に数値/hash proofとVM cleanup。
  b194d66524ef86330bb73ca3f22f9e5dd6ae0a07 / treef7eea82c96449b7749f98dd6c4b0344b64f1d651、実保存1904blob全件一致。
  archiveSHA2562d995c2594fe65813907fb8768c8bf4a92f94777a823bfcbf86ebbf9116aee21。取得前後/guest操作後の実owner/id/private/admin/nonfork/PR/ref/headを再照合。
  sourceは専用guest内のみ。VM Off/メモリ0/remove成功、Cargo/runner/公式CI未実行を維持する。
- 2746のWindows fixtureは実guestで成功したがSDK/原source/公式runner/13工程は未実行。
  未実装の公開SDK準備だけを2783へ--spec付きで通常受付した。元fixtureの複製ではない。
  新しいowned root/nonce VMと公開SDKだけ、重いCargo/原source/runner登録/原PR更新は行わず、成功SDK templateを回収確認後に親へ返す仕様。
  旧2746の隔離物は変更/解除/削除しない。旧2710/2648/2730にも触れない。
- 親2641の012150候補は受領したが、原PR/headはこの作業のb194のまま。候補の検査成功へ流用しない。親や2761/CodeQLレビューを自分の待ち先にしない。
- 残りは新差分の別モデルレビュー、単一job/live source受付、共有Cargo gate/専用target/sccache、公式runnerの取消し/登録/lock回収、原通常CI全工程。原PRmerge/Prdは親2641のみ。

### 同jobの公式job照合部（未接続）

- product-ci/normal_ci_admission.goを追加。controllerが明示固定するhead/tree/workflowhash/run/attempt/roleから、実APIのqueued jobまたは自分のrunnerが受けたin_progress jobへ束縛する。
  repo1154066087/PR3613/ref、workflow_dispatch、ci.yml、準備された4label、原30/20/5/5分、Windows13Cargoを保持する。
  別source/event/run/attempt/job/label/runnerを拒否する合成API test、Go test/vetとWin/Linux build成功。
  最初のunit失敗は既存合成fixtureの架空repo名が固定実repo条件と違うため。fixtureを実条件へ合わせ、実repo検査を緩めず成功した。
- このresolverはread-onlyでCLI起動経路なし。単発所有state、live hook、Cargo gate、runner登録、公式CIはまだ接続していない。
- 6549a7566afed98dbdb3333af722b3c42fb05778はpush/checkpoint保存済み。新差分の有限レビュー2784はspec保存済みで、登録確認時はtodo。
  Windows公開SDK2783はjob20261017-114030-todo-2783でdoingを確認。未実行の原13工程を再登録したものではない。
- Taskの明示例外により、準備/レビュー後のci.yml routeだけは原PR同branchのexpected旧SHA/force=falseで更新可。
  原feature sourceは編集せず、別の原製品PRも作らない。新head/tree/CodeQL再解析要否を親へ返し、親の完了を待たない。

## 2026-10-17 job20261017-135829-todo-2737 の再開結果

- 2784のレビューを受領。basee544990→headedc9c6e、tree6fc812a1b18a719479d178c362659741b73bd58f、19差分file/未読0、Must0/Suggestion0/Nit0。
  原応答SHA2563C545242C6BCE06C0E22563D494B8A8C060CFF6865F6BDBB8A1B3812579B8A59。検査を再実行したレビューではない。
- 新owned root C:/Users/makhmeto/AppData/Local/Temp/codex-owned-ab88838863fc707ba9361ba2fb93d1a3。
  safe-delete create IDab88838863fc707ba9361ba2fb93d1a3。旧9389...のSDK資産を読み、新しい検査資源は新rootへ作成した。
- canonical shared cargo_gate.exeの実normal/EOF取消しfixtureが成功。生成先byte lockがadmitted中に保持され、child drain後に解放された。
  Rust compile/原workflowは実行していない。親はstart ack前の失敗だけgateを止め、開始後はstdin EOFを取消しとして送りdrainを待つ。
- 最初のbootstrapはGHA scope readが失敗。APPDATAをhost child whitelistから落としていたため、既存OAuth設定の参照先が失われた。
  同じgh_gate APIを最小環境で比較し、APPDATAなしexit4、有りexit0/repo1154066087を確認。秘密値は転送しない。
- 次は管理者窓口clientのPowerShell native実行が失敗。PATHEXT/ComSpecが無いためexeをdocument扱いしpipelineに置けなかった。
  無害なgo versionでも同じ失敗を再現し、public実行環境値を渡すと成功。broker承認拒否ではない。標準run-as-admin経路を維持した。
- 修正後ci2737-host-v4/ci2737-guestのbootstrap-offlineはexit0。原b194の1904blobを専用guestで準備し、公式runnerをoffline/not-busyで登録確認。
  専用offline-bootstrap labelだけ、ephemeral/no-default-labels/disableupdate、run.sh/listener未起動。原CIやCargoは未実行。
  新root/bootstrap-9c267524764ef7212917f5c233b1fc44 のproofとcleanupでVM消滅/登録不在を確認。shared target lockも解放。
  tokenはRAM/TLS/公式runner環境だけ、config stdout/stderrは破棄し、credential fileを読んでいない。
- normal-ci-outlineで原checkoutのpersist-credentials:falseとrelease_source_sha入力のref選択を確認。
  live hookではdispatch入力が別sourceを選ばないことと、実checkout後のHEAD/tree/auth不在の追加照合が必要。
- 直接ephemeral labelをmainへ残すM1を新たに作らないため、4runs-on提案を条件付きexpressionへ改めた。
  vars.CC_PRODUCT_2737_CI=enabled / workflow_dispatch / 元task refだけが専用label。変数不在と他ref/eventは原OS labelへ戻る。
  原checks/if/permissions/timeout/Windows13/profile/直列/Docker parityを削らない。未適用。変数の所有/作成/回復、追加delta reviewが必要。
- 新Go helper gate/runner codeとroute修正は2784のレビュー対象外。新しい有限差分レビューを実行接続後に行う。
- 残り: actual job hooks/single-use/live API binding、共有gate下での実job、取消し時のprocess/登録/lock回収、Windows SDK2783、原公式CI完走。
  親2641や2761を待たず自分の範囲を返し、原merge/Prdは親に残す。旧受入/旧回収や37件を複製しない。

### 同jobのlive job接続差分（公式job未実行）

- canonical gate内でLinux単一jobを扱うhost controller、guestのstart/complete hook、post-checkoutの実HEAD/tree/index/blob/mode/auth不在検査を実装。
  sourceはseedで明示固定、公式run/attempt/job/runner ID/nonce/ref/contextを照合し、登録前intent、listener前numeric runner IDを保存する。
  原workflowの手動入力は実API/RAM outlineで0項目。workflow_call向けrelease入力を手動実行へ流用せず、Inputsは空のみ許可する。
- hook完了を成功にせず、runner exitと公式job APIの同SHA/同runner成功を確認する構成。公式logはprivate bounded fileとhashへ保存し、partial取得も区別する。
- 取消しはTLS EOF、guest process group停止、exact VM停止/回収、exact登録削除の順。専用durable intentで不確定な実行の再受付を止める。
  VM回収失敗時は共有Cargo leaseを保持し、read-only broker観測で不在またはOff/memory0を確認するまで解放しない。
  例外fenceは実機未検査。発生時は独立した技術回収を登録し、同じ拒否のstop/deleteを無限再試行しない。
- Windows Go test/vet（guest/source/product-ci）成功。新Linux test binaryを生成し、既存owned工具containerだけで実行した。
  source8件/guest9件のtop-level testはすべて成功。log hashes46D7E0C5AFF6B32CA863525918E78B08C2E0E474403E154AE623088DF455C127 / AE2F51A950F6CEA32DAB504E128075DB68390D536007CB367B6B3CEC0EF4EDAD。
  最初のWindows compileはtime import欠落で失敗し修正。Linux最初の呼出しは生成完了前でbinary拒否、生成終了後の実検査は成功。原失敗を保持する。
- 新しいlive controllerは実公式job/登録/VMへまだ接続していない。original b194/ref/workflow/Hosted予算は変更していない。
  原13工程とDocker parity、最終Rust nextest/workspace/doctestの実行は残る。review2784 Must0はedc9までで、この新差分の合格にしない。
- 次はこの有限deltaの別モデルレビュー、Windows SDK2783の受領、route変数の所有/回復と4scalar同branch expected SHA更新、実公式CI完走。
  newroot ab888.../ci2737-host-live.exe と ci2737-live-guest-final は検査用buildであり、配布済み工具や公式成功ではない。

### job135829の保存・追加レビューの入口

- live接続差分c304508fdcbcbf961a7cc4ad5218f5fb78c45604、その後のbinding schema修正33294cfeca39ca57c5a22c479a12559e2d4d9ed0をpush済み。
  product-ciのOS schemaは`Linux`で、GoのGOOS`linux`との取り違えを修正。timeout30/5分もresponse側で照合し、Windows unit/vetを再検査して成功した。
  これにより古いci2737-host-live.exeは最終sourceと一致しない。再開時は最終commitからhost/product-ci/Linux helperを再buildし、古いexeで実jobを受けない。
- 追加レビュー2795はgpt-6-lunaを明示して--spec付き通常登録。実装jobはgpt-6.1-solで、別モデルを選択した。
  登録確認時todo/jobs0。specのbaseはedc9c6e、headは最終保存commitへ更新し、対象は差分25file。実応答を受領してMustと未読を処理するまで実official jobを起動しない。
  2784のMust0はedc9まで。2795の登録をレビュー成功へ読み替えない。Windows SDK2783は同じ担当のdoingを維持し複製しない。
- gate/offline bootstrapと新unit/vet/Linux結果の84fileを
  D:/CC/Tools/product-ci/evidence/todo-2737-20261017/resume-135829-live へ全SHA一致で保存。
  hashes.private.json SHA2561F9C0CC15A4A9C022E6AB7A10CF98F5D0CA5B056BDB87743775C160AFB686418。
  binding追加検査の別manifest binding-delta.private.json SHA2560FC24BF17A047597AFE85C5DAFC4EF9B1FA193AD30A978058DA74E4E0067CCEB。
  元assets/原失敗は保持。製品source packet/credential/VHDXをevidenceへ複製していない。
- owned工具containerはexactID/name/label/image/no host mount/no device/no GPU/PID modeを照合後に停止、PID0確認済み。
  最初のPowerShell確認はnull配列を1件と数えて拒否しただけでstopは未実行。null/emptyを正しく分け同じidentity条件を確認して停止した。
  tools-paused-live.private.jsonを参照。未完SDK layer/所有root9389とab888は保持し、次回のgeniso利用時にexact所有を再照合する。
- 自分の新live VM/official runner/公式jobはまだ0件。offline bootstrap VM/登録は既存proofどおり全回収済み。
  原PR3613はb194のまま。残りは2795レビュー対応、2783 Windows SDK、route変数の所有/回復、原4scalarだけのexpected旧SHA通常更新、元CI全工程と必要Rust最終受入。
  手動eventで原pr-tree-receiptが条件によりskipされることは保持し、全4job successと誤称しない。原Windows条件/13工程/timeout/profile/直列とDocker parityを削らない。
  待機前に親2641へnote配送、2795と2783をwait --onへ登録し、同job/branch/実HEADでscheduler checkpointを保存する。

## 2026-10-17 job20261017-152246-todo-2737: 2795 Must2の有限修正

- 司令塔の訂正に従い、SDK2783を待つ前に元2795の原結果を受領した。
  job20261017-145446-todo-2795/last_message.mdのSHA256は11C89ACB143800CB91F0335C5F4E43013433C5BA2EDD3EF894C8A3003D1E6D1Fで一致。
  gpt-6-luna、baseedc9→head3826503/treef87ec45、25file静的レビュー、Must2/検査0。旧2784のMust0は流用しない。
- Must1: ProductCI入力を削除し、旧field/case違い/Executor/Commandをunknownとして拒否。
  既存normal CI admission/identity/YAML policyをtools/product-ci/normalciへ移し、CLI/controllerで同じcodeを静的linkする。照合のため別host exeを実行しない。
  clean Git build/fixed module/linked coreを確認し、実行中exeのhash、code revision、actual PIDをtraceする。これは署名証明やrequestの自己申告を権限にする仕組みではない。
  inspect-executorのdirty build拒否（exit1）は確認済み。commit後clean buildの同probeと、固定新headの差分レビューが次に残る。
- Must2: Linuxはfilesystem rootからopenat/O_NOFOLLOW、WindowsはOPEN_REPARSE_POINTと親handleのwrite/delete共有禁止で全要素を開く。
  同handleから読んだbytesをGit blob hashへ使い、hardlinkを拒否。読後に各dir/fileの同一性を再照合する。
  Gitは実metadataのbounded private snapshotだけを読む。検査済みconfig bytesを再読せず使い、auth/filter/hooks/include/extensions、gitdir/common-dir/alternates/replace refs/config.worktreeを拒否する。
  Materializeの旧Lstat/パス再open checkerも除き、HEAD/index/commit設定後に同VerifyCheckoutを呼ぶ。原sourceは新規private guest rootへ配置し、成功proofの前にjob userへ渡さない。
- 旧VerifyだけをGit固定3826503からGo overlayで戻したbaselineは、Windowsのexternal-identical .github/.git junctionを受け入れたため期待どおりexit1。
  Linux baseline最終版はtop-level10pass/3fail、修正版は13pass/0fail。linked parent/root/ancestor、metadata、途中の親差替え、hardlink、secondary worktree configを含む。
  Linux原log SHA256 baseline C70AA319F34ED91D4C11683CE3D72D08D3AEC8C44746B5719E2FAB0096B5404C / fixed 7E396926384BA01687B9B24D827B3FA494D3389D647BA64C72E430646EBE11DC。
  Windows source/guest/product-ci testsとvetが成功。新Go共有型のunkeyed vet、unused io、移動時のverify文字列大文字化、snapshot refs空dir不足の初回失敗を修正し、元ログも保持。
- 実Linux helper buildと合成guest9testsも成功。Go検査だけで、原Rust/Cargo/公式job/VM/runner登録は起動していない。
  元b194のAGENTS/CLAUDEはRAMで再取得し、前回235dcf.../70c7de...と一致。取得前後のrepo/PR/head/ref guardが成功。host product checkoutは無い。
- 新owned rootはC:/Users/makhmeto/AppData/Local/Temp/codex-owned-97f56865fe265249ac69160fd68b35be（safe-delete ID97f56865fe265249ac69160fd68b35be）。
  最新baseline-overlayとGo binaries/失敗/成功logを保持。実Product source/credentialsをrootへ保存していない。
  SDK工具containerはexact ID/name/labels/image/mount/deviceを再確認してこの2回の合成検査だけに利用し、その都度停止/PID0を確認した。未完SDKlayerは保持。
- 原product PR3613/headb194/tree f7eea82c/refは維持し、原workflow/route変数/guard/manifest/旧資源を変更していない。
  2783は元jobがguest資源再割当待ちで未完。source修正の前提ではないため、この段階で2783だけのwaitへ戻さない。
  次は固定新headの有限deltaレビューを通常ToDoへspec付き登録しMust/未読を処理する。新レビューだけが残る時はそのIDにwaitし、Must0確認後に2783の実SDK受領を確認する。
  SDKが未完なら正規wait2737 --on2783へ戻す。通常CI/製品merge/Prd/cleanupの全完了ではない。

## 2026-10-17 job20261017-185738-todo-2737: 修正差分レビュー受領とSDK待機

- 2800の追加原応答job20261017-170946を受領。SHA256 C5658001F3C927ADFED98C6BB1A8E3BAEE4255CC584EBA5BDA888B52821206D7。
  base382650300c39cff5891f6227811722c6cd6de9cd→headc0f59e12e7a1cb08fe1fec08cfbebc2c71113c5a/treec5929a97989af8c425d5401991230e8ddea5a796の2Mustを再照合し、Must0/Suggestion0/Nit0。
  25差分一覧と各Mustに関係するcode/README/局所caller/fixtureを対象とした静的レビューで、実行検査は0。製品source/VM/SDK/正式CIの合格ではない。
- 最初のjob20261017-165750も保存した。原SHA256 EF8D70644910A386B4198106D22A1962D1A13911FDD23EFF107D53B3E546E19A。
  静的Must0だがモデル確認と原2795全文照合を未確定としていたため、それ単独では確定しない。追加原応答が原2795全文と同threadの明示model gpt-6-lunaを再照合して補足した。
  両jobの保存metadataは明示gpt-6-luna/succeeded/exit0。原2795のSHA11C89ACB...も前回独立照合済みで、応答を消したり都合のよい部分だけ置換していない。
- 2応答とreceiptはD:/CC/Tools/product-ci/evidence/todo-2737-20261017/resume-185738-review。
  review-receipt.private.json SHA256 B0AA321088EA6E04E29C740EE53A45EF035CE7EC2956F2DB2926AA5C6F65DA66。
- 原2783はdoing、同job20261017-131423の保存statusはrunning/exit未確定。最新SDK成功receiptは未受領。
  古いlast_messageは64MiB超過の拒否を述べるが、司令塔15:07のnoteではその制限修復済み。過去の拒否を現在の確定原因へ昇格しない。
  jobに残る503再接続記録もSDK成功ではない。元job/資源経路を保持し、別担当/別VMの複製や原2737側での資源再割当をしていない。
  source Must0を先に確認できたため、指示どおり2737を2783だけの正規waitへ戻す。SDK受領後は実template/hash/VM回収/公開SDK版を照合してから公式WindowsCIへ進む。
- 今回はこの文書と受領記録だけを更新。2026-10-17本人指示TestBuildScopeを適用し、c0f59e1の有効な関連Go/Win/Linux検査を再利用する。
  文書だけの保存を理由にRust全workspace/Go全件/buildを追加しない。今後も変更処理と利用側、該当library doctestを選び、必須の原workflow/13工程/profile/直列/Docker parityは維持する。
  古い「毎マージ前に全workspace必須」という記述は当時の履歴で、現行方針はこの本人更新を優先する。
- 原b194のAGENTS/CLAUDEをRAMで再取得し、235dcf.../70c7de...と一致。前後のrepo/PR/head/ref固定を再確認。
  製品headb194/tree f7eea82cと、補助code headc0f59e1/tree c5929a9を区別する。原PR/ref/workflow/route変数/予算は変更していない。
  今回はVM/runner/container/原検査を起動していない。未完SDK資産と旧回収記録を保持し、通常CI/原merge/Prd/cleanupは未完。

## 2026-10-18 job20261018-234740-todo-2737（作業は10/19へ継続）

- 原2783がdoneとなり実SDKを受領。固定source9622a766e99a378427eccf9f21967f0fb9ca890f、runtime2381383d097779fb153689b5c83eb4c4f31f2456/finalizerf7697c927cd0723e5d1c5cd595a471e9760ce5f1。
  保持diskはD:/CC/Work/unattended-operations-20261013-wt/todo-2783-godd-mcp-ci-windows-guest-sdk/codex-owned-56e63ad56bbdd616f61707bf87799948/guest.vhdx。
  実21,277,704,192bytes全体をFileShare.Readだけで開きSHA256 e19d76f807c012ebeff2d5d161df3540d851a447aceb9ccbf7471eb877d2d816へ一致確認。原assetは変更/移動/削除せず保持。
  read-only broker照合でも元VM f0561845-8dfe-4fcd-bd9d-2d909549a670のCIM不在、VHD Attached=falseを確認した。
- private evidence hash9a6044c2dc6c7e25824ddf870bb4eff49a865541cd9aefeff3c606bb8cc2cc4eとdiagnostics/operations.log hash2fb1e2f00209099061f0d99c6856dd97fd30c62e52936fd769e7008b7a6cb3d2が一致。
  初回はraw log hashを集計sdk-operations.private.jsonへ誤って照合して拒否した。集計JSON自体は455ec19c34f5580ec271fd5ef518ca222a3720c6dd7e887936a75d4a341315e4で、原logとは別の証拠として保持する。SDK破損と報告しない。
  SDK5準備/6版確認は2783の実結果。原source/runner/CI成功とは別で、再インストール・旧SDK fixtureの再実行はしていない。
- cc repo1406818713のAutoDevJapan/private/admin/nonforkをAPI再照合し、元製品b194のAGENTS/CLAUDEをRAM再取得。235dcf.../70c7de...一致、repo1154066087/PR3613/head/refの前後固定を確認。
  親2641/旧CodeQL候補を黙って採用せず、原製品folder/guard/owner-work/登録manifest/旧未回収を触っていない。
- 新safe-delete root109d39499fb82a312701c7879faa728d（C:/Users/makhmeto/AppData/Local/Temp/codex-owned-109d39499fb82a312701c7879faa728d）に受領/回収/検査/再開用の記録を作成。
  Windows SDKは準備済みだが公式job用guest backendは未実装だったため、次の有限工程だけを2828へ--spec付き通常登録した。SDK準備やMust修正の複製ではない。
  2828はtools/windows-ci-runner-2737のtyped Go backend/agentと必要な既存SDK libraryの最小接続を担当。原source/実VM/登録/重Cargo/原PR操作は実行させず、親が実受入を担当する。
  同taskに実装前のlabel契約を保存specへ反映した。新担当はjob20261019-000032でdoingを確認済み。直接子Codex起動なし。
- 親側ではnormal-ci-route-apply/releaseとnonce所有のrepo変数、元4runs-on scalarの最小変更、sole旧parent/force=false更新を実装した。まだGitHubへ実適用/dispatchしていない。
  source/PR/ref/tree/workflowhashを再照合。別の既存変数値を上書き/削除せず、失敗時は自分のnonceだけ回収しsource refは戻さない。lost replyもstart/end receiptで残して照合する。
  変数一覧は公式page最大30/全体1000件の有限範囲とし、欠落/null schema・部分一覧・case違い重複を不在扱いにしない。variable DELETE自体にvalue CASは無く、task専用namespaceの排他と前後照合が境界。
- 実運用labelは固定prefixだけでなくrole/runIDへ束縛する。Linux verify/gateは<base>-<role>-<run-id>、Windowsはcc-product-2737-1154066087-windows-windows-native-check-<run-id>。
  nonce一致+workflow_dispatch+元refだけformat/github.run_id式で選び、他条件と変数不在では原OSへ戻る。legacy read-only proposalは保持するがlive requestには32hex RouteLeaseを必須とした。
  guestとhost/公式job APIで同じrole/run labelを検証する。原M1の任意exe入力は復活させず、M2 verifierも変更していない。新差分は2800の旧Must0の対象外で追加レビューが必要。
- 関連product-ci/normalciと利用側ci-guestのWindows Go test/vetが成功。lost POST/PATCH、4scalar以外不変、他run/role/nonce、既存flag、read失敗、missing schema、同時head進行、回収replayを合成APIで確認。
  Linux consumerのtest binaryもcross-compile成功。今回は実Linux/Windowsの公式job acceptance、Cargo/原workspace/13工程/原log取得はまだ0。sourcepacketの有効な旧検査は再実行していない。
  文書追記のpatch context失敗はソース編集の照合エラーで、実行承認拒否ではない。対象位置を読み直して修正した。
- 次は新route差分の有限別モデルレビュー、2828受領/統合/関連検査、fresh nonce SDK cloneでの実受入。その後だけ原PR同branchの期待旧b194/force=false更新を行い、新head/tree/wfhashとCodeQL再解析要否を親2641へ配送する。
  元公式CIを手動successへ置換せず、原13工程/条件/profile/直列/timeout20/Docker parityを維持して完走する。親のmerge/Prdは担当しない。
  SDK2783へのwaitはもう不要。ほかの担当だけを待つ時は新2828/新差分reviewへwaitを記録し、実HEAD/jobでcheckpointを保存する。

### 同jobの保存・担当結合補修と次回の入口

- route実装codeは899ed68ff281588b71a07e35f26978f7d869c27e/treebf3526f94c2ccd8fd218faf9011d144e1e1c498bへcommit/pushしremote一致。今回の新差分16fileは別モデルgpt-6-lunaの有限review2831へ--specで登録し、fixed basec373a18→head899ed68を保存した。まだ実応答/Must0は未受領。
- checkpoint初回はInputRejected。cc-scheduler/workspace_checkpoint.rsを読み、実jobがToDo.jobsに含まれずscheduler.jobもnullだったことを照合した。
  ToDoはtodo/prepared、保存key d6e1b5c964954cb695c94afd77309161と実running job20261018-234740-todo-2737のlaunch_key/番号/cwdが一致。他の同worktree/同ToDoの6jobは全てsucceededだった。
  最初の補修前checkではPowerShellの@($null).Countを1と数えて停止し、変更はしていない。nullを除いたwait件数0であることを再確認。
  既存CLI todo start2737 --job同CC_JOB_IDで起動済み同一担当の結合だけを補修した。spec/needs/worktree/branchは読戻しで不変。新モデルjob/権限/guard/登録manifestを作っていない。
  同じcheckpoint APIを再度照合した結果exit0/saved=true/launched0/source_files_changed=false。ソース側のcheckpoint条件を改変したり、別jobのIDで通したりしていない。初回拒否も保持。
- SDK受領/回収と関連検査/登録/保存の47fileをD:/CC/Tools/product-ci/evidence/todo-2737-20261019/resume-234740-routeへ全hash一致で退避。
  hashes.private.json SHA256 58FE21520ACCB31E336029A44D1D70186D8556BBA676DBF8E73F0ACA44780A64。
  原21GB SDK/原private logs/元root56は保持したまま。root109も未完runtime/再開用の小さい検査資産として保持し、原assetを不要物planへ掛けていない。
- 2828のcandidate bec99388b45319bfed7e3db67f1cbd24422883fd/tree6c5c18608b0d7940178baaa97938e6dd0a8fb5cdを受領した。API/固定path/clone stagingはそのtools/windows-ci-runner-2737/README.mdとHANDOFF.mdへ記録されている。
  同candidateは新Go backend/SDK最小callback/実Windows合成pipe-TLS-processとrace/vetまで。原VM/runner/13工程は未実行で、2830レビュー待ちの2828は未完。まだ候補を最終codeへ統合していない。
- 次回は2831の原応答をまず受領し、newMust/未読があればWindows待ちの前に直す。source差分Must0後に2828の最終code/2830反映結果を受けて統合・関連検査へ進む。
  したがって現時点のwait先は2831だけ。2828や完了済み2783を同時waitにして、新source指摘の修正を遅らせない。SDK/Windows実acceptanceは正式CIの前提として保持する。
  新route codeはまだ実GitHub mutation0/dispatch0、通常CI未実行。新しいM0を旧2800から流用しない。

## 2026-10-19 job20261019-011740-todo-2737: routeレビュー受領とWindowsホスト接続

- 2831はdone。原3応答を改変せず受領し、最終job20261019-010536のSHA256
  9A70ADD33629120A4352AEBCCE668AB006BA7F83E140DCF7EBD76894C1FE7512を確認。
  fixed basec373a18→head899ed68/treebf3526fの16file/局所callerでMust0/Suggestion0/Nit0、検査実行0。
  原応答のmodel variant未確認という文面は保持したまま、受領側がexact thread
  01a14fae-92c7-78f3-8e11-e9739c67d875のrollout turn_context.modelとjob.jsonを照合しgpt-6-lunaを確認。
  実装元job234740はgpt-6.1-sol。再び子レビューを起動していない。実VM/公式CIの合格ではない。
- 2828はdoneとなった。最終delivery c0732ffbdd867bdc5ce0cfbd803deb4648614b53から
  tools/windows-ci-runner-2737とSDKを同じcanonical pathへ統合。runtimeはb60e85e7、追加review2833はMust0。
  2830の23file/2833の6file、raw responses/受領receiptは同module/reviewsに保持。
  新normalci/sourcepacketを旧baseへ戻さず、既存controllerへ直接linkした。
- windows-job entryを既存canonical Cargo gate、単一job lock/intentへ接続した。
  source/run/attempt/nonce/Windows/20分/role-run labelを既存normalci.Planで照合し、Linux経路へWindowsを渡さない。
  guest helperは正しいmodule/entry・Windows amd64・clean build・hostと同code revisionを、実読取handleのbuildinfoで検査する。
  requestからhost program/任意exeを選べず、helperをhostで実行しない。実bytesはwrite/delete共有を止めたhandleからclone用へcopyする。
- 固定SDK source diskはread-only copyし、実cloneの全bytes/固定hashを照合後、cloneだけをoffline mountする。
  公開seed/固定hashの公式runnerを配置し、元SDKの既存LocalSystem serviceをclone内だけで固定entryへ向ける。
  元asset、host service、switch/firewall、原guard/manifest/owner-workを変更しない。offline hive解放とdetachは失敗時にも実行する。
- source proof/queued identity→登録intent→短命token→exact offline numeric ID保存→listen ackの順序を接続。
  start/completeは同job/numeric runner/physical proofと順序を照合。runner exit/drainを公式successへ読み替えず、既存official completion/log hash確認へ渡す。
  VM journal/stop-memory0-removal、nonce登録不在、gate/fenceとintent回収を既存経路へ統合。新VMを実際に起動した結果ではない。
- 初回Go testはSDK BootstrapResultにInstance fieldがないためcompile failure。public COM receiptに自己instanceとBootを包む形へ修正。
  修正後host tests成功4.093秒、host race成功、host vet成功。統合backendのraceも成功。
  新回帰はsource ack前token拒否、queue変更/nonce重複、登録duplicate/busy/online/別run label、ID保存失敗、hook順序/再入/他runを確認。
  offline stagingの実scriptはPowerShell parserで構文だけ確認し、実mount/registry/VMは実行していない。
- 新owned root aaf5ba5d48e90a772ce5b14b7c15b46bはC:/Users/makhmeto/AppData/Local/Temp/codex-owned-aaf5ba5d48e90a772ce5b14b7c15b46b。
  raw review receipt/初回failure/修正後検査を保持。元SDK21GBを今回再hash/copyせず、SDK受領の旧有効証拠を再利用。
  元b194のguidanceをRAM再取得し235dcf.../70c7de...一致、repo/PR/ref/head固定も再確認。cc実repo1406818713の所有/権限もAPI一致。
- 次はこのWindowsホスト接続差分だけの有限別モデルレビューを受領・Must修正する。
  その後、同じ最終clean codeからhost/guestをbuildし、既存SDKのfresh nonce cloneを実受入する。
  新head/route適用/dispatch/公式job/原Cargo/13工程は未実行で、原b194を変更していない。
  2828や完了済み2831/2783へ再びwaitせず、新レビューだけを正規waitへ記録する。原merge/Prdは親2641。

### 同jobの配送・レビュー受付

- 新host codeはdcc4c2368fa2b866d4c58e849ff44522afdaaa7a/treee581972d5906622863724c49d3eedc7c65e9eda0へcommit/pushし、remote一致を確認した。
  SDK/backendはdeliveryc073から同pathへ無変更でimportしたことをgit diffで確認。実APIでもc073/tree86ace98ca691c40573f8c61895279a445c6689b9が一致。
- 新差分の有限レビューは2835、固定base703373a→headdcc4c23、指定gpt-6-luna。保存済みspecは全文読戻し一致。
  最初のaddはnested subwork親2737を指定してCLI入力検証で拒否されたため、未登録/重複なしを確認した。原2831と同じ通常add --spec経路で1件だけ登録した。
  これは実行承認の拒否ではなく、root workerだけを親にできるsubwork引数の誤指定。別jobのIDを借りたりroot2641を偽装していない。
- Linux consumerもGo1.27でtest -cが成功。実Linuxでの再実行ではない。host race5.442秒/backend race5.534秒とvet成功の範囲を保持。
  commit後のtree取得ではPowerShellが未引用HEAD^{tree}を変形してGit読取が失敗したが、正しく引用して再取得した。source/ref更新の失敗ではない。
- 次回の入口は2835原応答の受領・Must/未読処理。その後に同sourceのclean host/guest buildと実clone/COM/TLS/stock実受入を行う。
  完了済みSDKや子2828の受付待ちへ戻さない。原b194/通常CI未実行は不変。自身の実HEAD/jobでcheckpoint後、2835だけへwaitする。

## 2026-10-19 job20261019-013550-todo-2737: clone回収提案の反映

- 中断直前item_2の完了イベントをread-onlyで照合。exit0で2835原応答の取得と文書読取まで済んでおり、再実行していない。
- 2835 job20261019-012942の原応答はSHA256 BEB1B9611A61D6ED1676F5F372768189204765E8A70F55847A0F57B489D60C58。
  fixed base703373a→headdcc4c23/treee581972の10file/局所caller、静的Must0/Suggestion1/Nit0、検査実行0。
  exact thread01a14fd9-82e0-7310-aae7-0a32c15cd853の正規rollout turn_context.modelとjob.jsonで実gpt-6-lunaを確認した。
  原応答のvariant未確認という文面は変更せず、受領側の別記録で補足する。実装元はgpt-6.1-sol。
- Suggestionは実害があるため採用。VM作成前のclone detach失敗でも追跡が落ちないよう、broker mount前にclone-stage intentをflushする。
  staging失敗時/VM回収後に、同じbrokerの後続read-only flightでexact cloneのAttached=falseとtask offline hive不在を確認する。
  VHDのstate欠落/型違い/読取失敗/attached/hive残留は成功にしない。global intentを残し、既存unsafeVMLeaseのgate fenceにもclone観測を含める。
  失敗したdetach/unloadの再試行や別経路の回避はせず、観測だけを行う。元の失敗とcleanup結果は別に保存する。
- 回帰検査は本番と同じcleanup関数を使い、VM未作成でもclone不明ならintentが残り、fenceにclone義務が付くことを確認。
  未開始/実detached時は正常にintentを解放する正例も対で検査した。exact observation scriptはPowerShell上でHyper-V/registry readsだけをstubし、detached/attached/hive/read-error/missing-stateを確認。
  これは実mount/detach/VMの受入ではない。関連host raceとvetが成功。旧backend/sourcepacket/原Cargo/13工程の全件を重複実行していない。
- 新safe-delete root e8506df50926a134106b9983c4fd70fcはC:/Users/makhmeto/AppData/Local/Temp/codex-owned-e8506df50926a134106b9983c4fd70fc。
  原2835応答はD:/CC/Tools/product-ci/evidence/todo-2737-20261019/resume-013550-cloneに保存。旧SDK/必要layer/他担当/未回収は保持。
  元b194のguidanceをRAM再取得し235dcf.../70c7de...一致とrepo/PR/head/ref前後固定、cc実repo1406818713の所有/権限を確認した。
- 次はこのclone寿命差分だけを有限レビューへ渡し、同じclean codeの実行物でhost gateの正常/取消し排出と、Windows実acceptanceへ進む。
  Windows公式backendのseedは実queued job IDを必要とするため、cloneの本番接続はレビュー済みrouteを同原branchへ明示適用・dispatchした後、初回の公式jobとして受入する。
  古い「実公式claimを伴うclone受入をroute適用前に完了」と読める順序は実API依存に合わないため訂正。架空job IDや手書きmanifestで前提を埋めない。
  初回に失敗すればその公式失敗/未実行を保持し、原因修正・必要レビュー/受入を続ける。原13工程/条件/profile/直列/timeout/Docker parityは削らない。
  原b194/routeflag/Hosted予算/原guard/manifestはまだ変更0、通常CI未実行。原merge/Prdは親2641。

### 同jobの保存と実行物

- clone修正は4c60959bf3657ac2142699b8bf41ac6112d0d9d3/treea0aea436139784d3e469320798afa07536af4d43へcommit/push済み。
  有限レビュー2838はfixed base45fead8→head4c60959の6fileだけを対象に通常add --spec/model gpt-6-lunaで登録。保存specは全文読戻し一致。
  job20261019-014354がdoingで、原応答はまだ未受領。新差分のMust0は旧2835から流用しない。
- まとまったhost接続/寿命変更の実運用に必要なbuildを、上記clean commitから1回実施した。
  root e8506df50926a134106b9983c4fd70fcのci2737-host.exeはSHA256 f810a76e7e2658cd58fac0ad5ba74e9d7b574e0bad8772308d002a564ead4c90。
  windows-ci-runner-agent.exeはa9755bcaf0e2db2df2c454818188f73691f31a3ae3d2ee3fdd15aa78099f0688。
  ci2737-linux-guestはd4de885c90afd6384bcdc25881ac7fc8122537e0eddc883c2004e3257625789b。
  host inspect-executorが実module/linked core/PID/codeSHA4c60959/cleanを返し、guest buildinfoも同revision/clean/Windows amd64を確認した。
  codeを変えない文書保存のために再buildせず、実行時はこのreview対象code SHAと文書delivery HEADを分ける。
- 新hostのtest-gateがcanonical Cargo gateへ実入場した。通常/EOF取消しの2receiptは各jobs8/testthreads8、実target lock保持→解放、child_drained=true。
  原Rust Cargoはcompile0、公式CI success=false。親launcher PID43740/保存startを照合しプロセス不在、stderr0を確認。
  launcherのexit codeは直接保存していないため、captured exit0とは報告しない。2終端receiptの実結果と不在を別に残した。
- e850 rootは次回の実clone/公式jobへ使う必要な同版実行物を含むため保持。新VM/clone/runner/route更新/dispatchはまだ0。
  次は2838だけの原応答を受領し、Must/未読を直した後に実公式runへ進む。親2641に有限結果/証拠を配送し、実HEADで自身のcheckpointを保存する。

## 2026-10-19 job20261019-015035-todo-2737: 公式通常CI初回の実結果

- 2838の原応答はSHA256 F0F47E84ECC72831E5A8EC545EFE2436F604F96454B5A079EBCE2FCF0FE3AE85。
  fixed base45fead8→head4c60959/treea0aea43の6file/局所callerで静的Must0/Suggestion0/Nit0、検査0。
  正規job/同thread01a14fe7-ee35-7933-9c8d-df4d3618afd8のturn_context.modelを照合し実gpt-6-lunaを確認。原文のmodel不明申告は改変せず保持。
- レビュー済みnormal-ci-route-applyを元b194へ適用しexit0。原同branchへsole旧parent/force=falseで4runs-on scalarだけを通常更新した。
  **新製品HEAD085b5b007cab4ebd57d6690faf6df95bb40453ef、tree25ae0b09830d1fbeef079e8ff6bcc033928bc8d8、ci.yml SHA256ba2bcc656bfafc7d2c2da1d14770c00ba0d698b2bea7a6c086c6ae13ddf9abe8**。
  lease0b2414b10418d265c7823a4f87bb5881。変数/commit/tree/refの各mutation前後receiptを保存した。CodeQL/nativeの旧SHA結果を新SHAへ読み替えないことと再解析要否を親2641へ即時配送済み。
- 元workflow_dispatchを原refへ1回だけ送信しexit0。公式run39147155349/attempt1が同repo/newSHA/event/refでqueued、verify job117733211808を確認。
  ownedroot982d807ade8d920300c7e14c4f5dc08cのrequestはsourcepacket.Pinの実JSON schema head_sha/tree_sha/workflow_sha256を使い、新3hash/run/attempt/role/lease/空Inputsを明示した。
  schema修正は実行前に行い、架空job IDや手書きmanifestで条件を埋めていない。
- 同clean code4c60959のhost/Linux helperを使用し、canonical Cargo gate内で新VM fc9ed8c6aa01c36f767b4d31e444abcbを起動。
  VM instanceは上記nonce、実Hyper-V IDはb9a2d1a6-7623-4e29-bccf-338ee1c47a77。Linux SDKの元VM不在/全disk hashを確認し、新sourceをRAM転送。
  source-readyの実proofはrepo1154066087/newHEAD/newtree/1904blob/physical_verified=true。
  registered runner27（nonce名todo2737-fc9ed8c6aa01c36f767b4d31e444abcb/同run-role label）のoffline identityを保存し、公式APIの同job割当とstart hook proofまで確認した。
  seed生成用の自分のtools containerはexact ID/name/image/labels/mount/device/privilegedを照合し、seed.iso完成後すぐ停止/PID0を確認。layerは次回に必要なので保持。
- **初回公式verifyはfailure**。API上の失敗工程はnumber10「Set up pnpm」とnumber68「Complete runner」。start hookは成功したがcomplete proofは未受領。
  hostはprivate gate logにlive job hook proof rejectedを残し、outerのgated child drain unverified/exit1を保存した。合成検査成功を公式成功へ読み替えない。
  VM cleanup receiptはstopped/memory0/removed、runner27登録不在がtrue。処理終了後、同じ実targetのcanonical gate正常/取消fixtureを1回確認し、exit0/lock解放/child排出を保存した。
  残りのalways gateが通常cancel後もqueuedだったため、同じ自分のrunだけをforce-cancelし終端completed/cancelledを確認。
  job117733211808 failure、117734627599 cancelled、117734628283/117734629065 skippedを区別して保存。Windowsは未実行。
- run終端とVM/登録/target回収後、normal-ci-route-releaseはexit0で**自分のnonce変数不在**を確認。製品refは085のまま、resetしていない。
  route root25dde33266f4ad07e04d9639469022f5を保持。次の再実行では同じ085/tree/workflow/refを再照合し、既存条件式の同leaseに対応する変数をCreate-only契約で再開してから実run/attemptを明示する。
  hardcoded旧b194のroute-applyを再実行したり、別headを黙って採用したりしない。
- 公式log GETはupstream ghがresponse内のterminal escapeを拒否してexit1、stdout0。案内された--allow-escape-sequencesはapi --helpに実在したが、同canonical gh_gateはunknown flagでexit2を返した。
  これは自動承認審査/GitHub権限不足ではなくCLIの引数互換不足。実gh直呼び/環境変数/別HTTP経路で迂回せず止めた。原log bytes/hashは未取得で、0bytesの失敗fileを原logとして扱わない。
  有限修復2843を通常add --spec/model gpt-6.1-sol/needs cargoで1件登録。cc-gh-gateの正規flag互換・関連検査/別モデル/CI/通常PR/正規配布だけを依頼し、原guard/secret redaction/認証/endpoint/権限を弱めない。
  子は原product/ログ本文/公式run/routeを操作しない。修復後に親2737が同じゲートで原logを取得し、pnpmとcomplete proofの実原因を診断・修正する。
- 52fileの実API/VM/登録/回収/失敗証拠はD:/CC/Tools/product-ci/evidence/todo-2737-20261019/resume-015035-live。
  copied-hashes.private.json SHA256 A8681D562FD545F10E36584304E0679E940902D317FAF0A9F8CD166A9C1DA1D4。原logの未取得と旧SARIF/CodeQL37件は別に保持。
  新VHD/seed/runtimeのroot982/25/e850は再開/診断が未完のため保持。旧SDK/root/他担当/未回収/原productfolder/owner-work/guard/登録manifestを変更していない。
  新費用/Hosted予算/API-key AI/GPU/Civ6/本人PC操作なし。次は2843の実配布を受領し、原log取得→関連修正/有限review→同明示sourceの通常CI再開。
  完了済み2838/SDK/Windows実装へ再waitせず、2843だけの正規waitと自身の実HEAD/jobのcheckpointを保存する。原merge/Prdは親2641。

## 2026-10-19 job032350: 原ログ受領と通信期限の回帰修正

- 2843の実配布を受領し、canonical gh_gate.exe SHA256 8BAA838B874FD4995F577F724C77B280CA7DE4B975BE8CDF35BCEE7D76FDA871とPR222のmergeを照合した。
  正規api --allow-escape-sequencesで元job117733211808のログ取得がexit0。20983bytes/SHA256630293E9E0758EAF9A1C27EE9A7EDF8A7A31A672B8ED64454849EE64E0A4620A。
  原文/位置をstdout/modelへ出していない。原公式run39147155349 attempt1はcancelled、verifyはfailure、Windows未実行のまま。
- pnpm actionの固定公開commit ea17c68df8912ef543352723c149a84f56e3d413を照合した。エラー分類はNode ERR_INVALID_ARG_TYPE、paths[0] undefined、Node18.19.1/self-installer。
  固定公開manifestはnode24だが、これだけで実選択や原因を確定していない。元環境の公開SDK tar内node20/24はELFを確認しただけで、実version検査ではない。
  有限の公開合成再現2860を通常add --spec/model gpt-6.1-solで登録。原製品source/log/VM/routeや親codeの操作なし、独立owned資源だけを許可した。子Codexは直接起動していない。
- liveWire.permitの15秒write deadlineがstreamに残り、後続のfailed/runner-exited通知を失わせる別の実欠陥を修正した。
  各event送信で15秒を設定し、送信後に解除する共通関数へ集約。deadline設定/解除エラーも失敗として返す。source-ready/configured/terminal送信も同じ有限書込期限を使う。
  これは最初のcomplete proof拒否やpnpmエラーの根因確定ではない。原start/completeのsource/claim検査は緩めていない。
- 仮想clock付き実net.Pipeで、start admission後に1分進めたterminal送信を検査。修正前はfailed/runner-exitedの2ケースだけ失敗、修正後は両方成功。
  同じmoduleの実Linux合成全件成功、Windows関連race成功、Windows/Linux vet成功。原製品Cargo/全workspace/実VMの再受入は未実行。
  original log保存のcallerにも配布済みフラグを追加し、次回の自動ログ保存に反映。private bounded file/hash/partial failure保持は不変。
- 証拠はD:/CC/Tools/product-ci/evidence/todo-2737-20261019/resume-032350-diagnosis。transport-regression-evidence.jsonがred/green/関連検査/停止観測のhashを保持する。
  工具containerは同一ID/name/image/label/host bind/deviceなしを照合して今回の検査だけで再起動し、工程後stop成功、Running=false/PID0を確認した。必要なSDK layerと失敗VHDは保持。
  新診断owned root a0fb308270dede4d61ca5345f8ff883fには公開SDK検査helperと合成検査binaryだけを保持。原製品checkoutは作っていない。
- 次はこの小差分の別モデルレビューを受領してMustを修正し、2860の実再現を受けてpnpm根因を直す。
  製品085b5b007cab4ebd57d6690faf6df95bb40453ef/tree25ae0b09830d1fbeef079e8ff6bcc033928bc8d8/元refを維持。
  同route lease0b2414b10418d265c7823a4f87bb5881の変数は不在で、新公式run送信0。通常CIの全工程/Windows13/Docker parityは未完。原merge/Prdは親2641。
### 同jobの保存・回収・正規待機

- 実装と検査は56541a6d914cbe3c6ff19202e62e7f012224b197/tree57dbaf0ffc98d653403cf89a6fc5cd6362c4c739へcommit/pushし、実remote同HEADを確認した。
  追加4fileだけの有限レビュー2861を通常add --spec/model gpt-6-lunaで登録し、保存spec全文一致を確認。2860の公開合成調査も保存spec一致/doingを確認した。
  どちらも結果未受領であり、登録だけを修正受入完了としていない。
- 使い終えた診断owned root a0fb308270dede4d61ca5345f8ff883fは小さい公開helper/実検査ログ/実行物hashを同evidenceへ保全した後、safe-delete plan→別呼出し通常削除→finish source_absentを確認した（21570600bytes）。
  上記の一時保持記述を更新する。失敗製品disk/必要SDK/同clean runtime/他担当資源は保持。
- 終了前の実GitHub照合でも原repo1154066087/owner/private/nonfork/同repo/openPR3613/head085/ref一致。原sourceを黙って取り替えていない。
  次の実行物はレビュー済みの最終codeから生成する。現在の旧4c60959実行物に新期限修正が入っているとは扱わない。
  残る依存は2860と2861だけなので両方へ正規waitを記録し、実HEAD/currentjobのscheduler checkpointを保存する。
## 2026-10-19 job041741: 別モデル受領と実SDKのNode選択修正

- 2861の最終job20261019-041137/同thread01a1505c-f6c8-70d1-b73f-d12acfd9d6f4のturn_context.modelは実gpt-6-luna。
  原応答SHA25623502880FD8DC41C03C52F59A67EF39BB1038DF01B8A2216C0F53B605088EC99。固定base8f6e80a→head56541a6/tree57dbaf0の追加4fileと局所callerでMust0/Suggestion1/Nit0、検査0。
  Suggestionのdeadline設定/解除失敗注入を採用し、設定失敗なら送信0、解除失敗なら実送信後も失敗を返す対照を追加した。旧reviewを新差分へ流用しない。
- 2860のjob20261019-034852の原応答SHA25631E19D7364BE3638D29636E774DE9F1CAE36F3A15FA4C2A586E607A50BE60F12を受領。
  公開調査commit4b0d1148cfc74ccce65e829bbe05337bbb633b60のdocs/investigations/todo-2860/RESULT.mdを固定版から確認した。
  同public action ea17c68dで、Node18.19.1/18.20.4は@pnpm/exe bootstrapのimport.meta.dirname未対応によりpaths[0] undefinedを再現、Node20/24は成功。npm9固定対照も成功で、npm更新だけが必要という結論ではない。
  action本体をNode24で起動しても、installerがPATHからNode18を選ぶ経路がある。原ログは固定hashのまま公開package名@pnpm/exeと同エラー分類を確認したがversion11.25.0の文字はなく、原製品で同根因が確定したとは扱わない。
- 本番guestの既存SDK展開先/opt/ci2737-runnerを確認し、子環境PATHの先頭を同SDKのexternals/node24/binへ変更した。
  原workflow/action/packageManager/検査工程は変更しない。既存Rust path/target/gate/jobs/threads/sccache/hook/GPU無効化は維持。
- 新owned root bcc30b02a37fdcd1b5db109f2af03449をsafe-delete createで作成。実Linux module合成検査は11pass/0fail（SDK opt-in2skip）、Linux vet成功。
  同公開SDK tar hash c2063dcd969ac4cecb76a6fc81e476f78e331e7bed0a7eefc5c4304b56be5578を照合し、所有工具containerへhost bindなしで同展開先へ展開。
  新opt-in検査は実UID/GID1001と本番同PATHで子を起動し、node/npmが同SDKのbinから選ばれ、node v24.21.0/npm11.19.0を返すことを実確認した（exit0）。HOMEは合成/tmpで、公式action/原製品受入ではない。
  工具containerは工程後stop成功、Running=false/PID0を再照合。既存SDK image/layerは必要なので保持した。
- 証拠はD:/CC/Tools/product-ci/evidence/todo-2737-20261019/resume-041741-node。原log本文/位置・認証値は表示していない。
  原repo1154066087/private/admin/nonfork/同repo/openPR3613/head085/原refを取得前後で確認し、原AGENTS/CLAUDEをRAM再取得。235dcf.../70c7de...の既読同hashを確認しhostへ原sourceを保存していない。cc実repo1406818713/AutoDevJapan/private/admin/nonforkもAPI確認。
  既存初回runのfailure/cancelled、SDK/未回収/他担当/原guard/manifestを維持。新route変数作成/dispatch/実VM起動/原Cargo compileはまだ0。
- 次は本追加差分だけの別モデルレビューを受領し、Must解消後、同clean codeの実行物で同明示085/ref/tree/workflowの公式CIを再開する。
  Nodeの合成成功を公式CI成功に読み替えず、complete proof拒否の別原因が残れば実結果から修正する。Windows13/Docker parity/通常gateは未完、原merge/Prdは親2641。