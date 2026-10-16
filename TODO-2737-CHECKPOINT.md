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
