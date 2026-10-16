# ToDo2737: GoDD MCP αの通常CI

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
- 終了前にcommit/pushとscheduler checkpointを保存。note配送と別モデルreviewは次に記録。
- 回収はsafe-delete plan、別呼び出し通常削除、finish。拒否時は正規quarantineのみ。
