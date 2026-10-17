# ToDo2790 再開記録

## 15時台の追加確認（以下を優先）

コードhead `d5e04c5643abd075b1a2fab763b15e7aa50f695d` をpush済み。
Windows module tree=`8974edd80634097711e755c8f49efc98c3f2f685`、
共通package tree=`cbdb19827cd035d66a527818a8fb2df89705a059`。
その後のcheckpoint文書だけのcommitはこのGo moduleの内容を変えない。

工具入口は専用service SID・AU deny-only・Job所属を確認するよう追加修正した。
job/actionが追加したPATHや明示compilerを上書きせず転送する。
最新正常fixture aeec65ede895bbebc31f838fe17233feはこのGo内容で成功した。
worker13260/child37860、原Cargoの両versionがexit0、source改変の入口拒否exit1、
元Cargo失敗101、保持3PID排出・worker/service0を確認。正常controllerの終了コードは直接保持して
おらず、空stderrと停止後のservice/worker/drainを確認した範囲だけである。
owner tokenから同じRustup入口を呼ぶ負例は専用service identity必須としてexit1で拒否した。
SDKの実行を管理者に取り次ぐ入口にはしていない。
最新Windows Go race64pass/2opt-in skip、vet/Windows build/Linux cross buildがexit0。
共通package側を含むproduct-ciのWindows race145pass/1skip、vet/両OS buildもexit0。
Linux上のGo unit実行や親の未push product-ci全sourceを検査したとは主張しない。

取消しの実Rust/11PID排出は592ef03相当のGo内容での証拠として保持する。
追加のidentity/PATH差分を含む最新内容で取消しを再実行したとは書かない。
binaryは各commit前の同じGo内容からbuildしているため、埋込VCSは前head/modified=trueの場合がある。
原binary SHAとその時のGo module/source内容を対応付け、埋込VCSを新headだったと改変しない。

### 別モデルレビュー2797

複数file38全文とca2d6af→d5e04c5の全追加差分を通常ToDo --specで供給した。
Sonnet5.5を指定。モデル名にslashを渡した最初のaddはCLI入力検査でValueErrorとなり未受付。
実装のmodel slug規則を確認してclaude-sonnet-5-5へ修正し、2797だけを受け付けた。
元2736がsubworkの親で、2790は独立checkの子であるため、孫subworkを偽装せず通常登録した。
親2736/2640や2791の完了への依存は作らない。

2797がjobs=[]の間は固定入力を最新sourceと新fixture事実へ更新し、正式保存specとの
改行正規化後一致を確認した。最後のspec SHA256は
79E63C19241BE6DD2C5DF255BCE97BDEC5CED91970201F4445A8EF76550FA1D5。
その更新時には実job20261017-145950-todo-2797がdoingへ移っていたため、最終応答が
実際に読んだhead/spec/全file/実モデルを必ず照合する。最新specを保存したことだけで
実jobがその最新版を読んだとは主張しない。原応答/Mustはまだ未受領。
reviewの入力原本と追補は恒久root内review-2797-*.private.mdへ保持。
再開時に実jobはSonnet5.5/queued/0step/0tokenを確認し、起動・読了・Must0とは扱っていない。

### 自分の後片付け

下の4件に加え、新aeec65ede895bbebc31f838fe17233feもservice不在/PID0/追加ACE0、
query可能な当該SID process0。新SIDは
S-1-5-80-3810339510-873866660-22809462-1019646786-3118911523。
5件ともprofile/hive/classesが残り、DeleteProfileWは各1回だけWin32=32。
その後のunload/再試行/別APIは0。

5fixture rootsのsafe-delete planと正規quarantineは各1回だけ
LINK_OR_REPARSE_POINTを返した。これはsafe-delete自身の拒否であり、自動承認レビューの拒否ではない。
SDKのrustup proxy linksと、新homeのINetCache linkをメタデータだけで確認し、link先は辿っていない。
再plan/別削除API/linksを外して再削除などはしない。元整理担当2381へこの5件の結果だけを配送済み。
2381はrecovery_unverifiedなので、メモ配送を着手/回収/復旧成功へ転記しない。
旧noteがnote_historyへ原文のSHA一致で保持され、状態/spec不変であることを読み返した。
各rootのbytes_excluding_link_targetsは恒久rootのresidue-metadata-*.jsonに保存。
原logs/receipts/SDK fingerprints/binaryと検査jsonlは全bytes/SHAを保存した。

build Temp 0d8caec7e805f545122a8b1d7d3d0cfbは44file/99,577,264byteを全SHA一致で
恒久rootへ保存した後、plan→別通常削除→finish=source_absent。保存後再照合も不一致0。
追加build Temp4561a50e1b517d5683fc7d1c650f9dccは10file/16,358,623byteを
identity-build-artifactsへSHA一致で保存した後、plan→別通常削除→finish=source_absent。
恒久root自体は原証拠なので保持する。最終metadata APIのrunner0も別fileで保存した。
恒久rootの原保存352fileの最終index SHA256は
29A69A4B459F7D5AF4F1DB9E652068B3DBA862A33DA9718DA5CD16852427C4FB。
index作成後の配送・待機receiptは追加資料として別に保存する。

親2736への実装・検査・未達と証拠の配送は完了し、原wait2790/2791は保持した。
配送receiptは恒久rootのdelivery-receipt.json。製品source/正式CI成功は主張していない。
後片付けの無通知Discord logを1回だけ試みたが、CLIがQuestionStateRecoveryRequiredで拒否した。
これは質問台帳/本人回答の原記録未復旧による補助CLIの拒否であり、自動承認レビューの拒否ではない。
投稿は未完で、再試行/別投稿API/空台帳の作成は行わない。親/整理担当へのToDo配送は成功済み。

次は2797の実応答を受領し、読了headを照合してMustを修正する。今回の実装・検査と
残留は先に親2736へ配送する。正式製品source/runner/dispatch/CIを子のfixture成功へ置換しない。
レビュー待ちは正規todo wait2790--on2797で保存し、scheduler checkpointに実HEADを記録する。

2026-10-17。job `20261017-143130-todo-2790`、branch
`scheduler/todo-2790-pr148-windows-source`。親2736/2640/880の未完・停止・依存は保持する。
親のworktreeは読取のみ。自分の独立PR作成・マージ、製品source取得・展開、workflow変更、
登録、dispatch、正式製品Cargo、GPU/Civ6/再起動/新常駐/共有ACL変更は実施していない。

## 供給したソースと実装

- 出発点は指定されたpc-tools object `4f47c1258d24a275125cb53490d28cfde14517e8`。
  Windows module、共通pr148scope、go.mod/go.sumだけをca2d6afへ取り込んだ。
  remote PR189の古いheadや親branchは書き換えていない。自分のbranchのpushは成功した。
- materialize.goは認証を持つcontrollerでGitHub JSONを有限RAMへ読み、commit/tree/blobの
  全object IDを再現してから、新しい専用workspaceへobjects/HEAD/shallow/index/全bytesを作る。
  実モードは原75aのみの親・三workflowの共通生成bytes・他の全tree/path/mode不変を先に照合する。
  原folderのclone/checkoutや任意host commandを公開しない。未対応mode/pathは省略せず拒否する。
- 新homeに公開SHA256を検証したrustup-initとRust1.95.0/+stable、fmt/clippyを供給する。
  filtered service tokenで既存MSVC/Git/PowerShell/sccache/gateの利用を実測した。
  controllerが既存cargo_gateを実際に保持し、serviceはRAM pipeで観測済み割当を受ける。
  service自身の共有lock書込権限は実証しておらず、必要ともしていない。
- sccacheは既存D:/CC/Tools/sccache/sccache.exeを、新しい専用cache/portの有限foreground
  processとして同じJob Objectに保持する。共有owner-serverを使うとcompilerがJob外へ出るため、
  そのserver/共有cache/ACLは変更・使用していない。kernelのlistener PIDとJob所属を確認する。
- 新CARGO_HOME/binのRust tool入口はcheckout後の実HEAD/tree/index/生bytes/auth不在を検査し、
  既存verify-source receiptへ接続する。canonical workflowに新stepを追加したとは主張しない。
  argv/stdout/stderr/exitは同じfiltered accountの実SDKへ渡す。Cargo exit101を維持した。
  targetは実source/tools/target。Rustupの自動self-updateはこの新home内だけ無効にする。

## 実検査（すべて合成source/API、正式CIではない）

- Go unit/race/vet、Windows build/Linux cross buildは両moduleで成功。
  最新Windows moduleの原jsonl/vet/binaryは下記build rootのfinal-*。
  product-ciは変更した共通packageとの接続確認であり、親の最新product-ci全sourceの検査ではない。
- 合成Git APIのcommit/tree/blob、実Git HEAD/tree/indexと生bytesを照合した。
  incomplete tree、blob改変、path/mode/case collision、assume-unchanged下の改変を拒否した。
- 正常fixture `31746d0dbe8bd573099950f363d922e9` はSDKから実Rust1.95.0/+stableの小さい
  unitをコンパイル・実行してexit0。worker53348、child41752、保持3PIDを排出、active0。
  そのbinaryはee8e2e6のsourceに対応する。
- 最新cancel fixture `7673df8bafb82b59c5a7b98c8afcc447` は両Rustの合成unitがexit0、
  source改変で入口exit1、実Cargo失敗exit101を確認。その後Rust test53592が実行中で
  readinessを書き、SCM stopでworker53344/Cargo47048/cache50344を含む保持11PIDを排出した。
  worker exit125、controller exit0、cancelled=true。原fixture.jsonとdrain/launcherを保持する。
- 最初のa7b23ac4はVsDevCmdのcmd.exe quotingが原因でexit1。a3464431はWindows APIの
  成功時GetLastErrorをGoの非nil errorとして扱ったためexit1。両原因を修正し、原失敗も保持した。
- 旧opt-in controllerテストのCreateProcessWithTokenWはAccessDeniedで失敗した。
  通常の担当tokenも管理者groupが有効だったため、非管理者controllerの再実証はしていない。
  これを成功へ転記しない。今回の実service SDK検査は別にfiltered token/AU deny-onlyを実証した。
- repo metadataだけを実APIで読取り、AutoDevJapan/private/nonfork/repo1392152644、PR148
  open/draft、元75a/base1100/branch、同head/base repo、runner0を確認。製品source本文は取得していない。

## 所有資源・証拠・残留

恒久証拠root（Gitへ追加しない）:
`codex-owned-a92de917e7fd2ae1dd53d6b0b50b9ff0`。
原control/runtime JSON/logをbytes/SHA一致で保存し、各fixtureのmanifest.jsonを保持した。

build root: `C:/Users/makhmeto/AppData/Local/Temp/codex-owned-0d8caec7e805f545122a8b1d7d3d0cfb`。
生成binary/原検査/各管理者script/controller失敗とSDK原logsを保持する。恒久保存後にsafe-deleteの
plan→別通常削除→finish。safe-deleteが拒否した対象は再試行や別削除APIに進まない。

新fixture rootsはTemp直下の次の4件だけ。すべてservice不在/PID0/追加ACE0、query可能な
当該SIDのprocess0。censusの読めない他process1件は完全列挙成功へ含めない。

| root所有ID | 新SID | 結果 |
|---|---|---|
| a7b23ac492c7861382aca58a581e7342 | S-1-5-80-3418226636-2567032269-4052967740-1994420855-2256801509 | 原SDK quoting失敗・排出済み |
| a3464431d7b7021a9adc58b109cd8fa5 | S-1-5-80-4061181211-1397399610-408150598-3645813937-424415708 | 原cache判定失敗・排出済み |
| 31746d0dbe8bd573099950f363d922e9 | S-1-5-80-3150914304-2394924066-2279176494-3371984151-1616343997 | 正常成功・排出済み |
| 7673df8bafb82b59c5a7b98c8afcc447 | S-1-5-80-3492813724-3565127043-1951024796-1595492979-2285114255 | 実Rust取消し成功・排出済み |

各新profile/hive/classesは残り、DeleteProfileWを各1回だけ実行してWin32=32を保存した。
unload/再試行/別APIは行わない。元整理担当2381へこの4件のみを配送し、旧SIDは触らない。
この残留を親の製品CIの前提へ戻さない。

## 残り

1. 固定した新headの複数fileをOAuth Sonnetへ共有ToDo --specで有限レビューし、原応答と読了範囲を
   受領してMustを修正する。直接子Codex、親側2791レビューの重複実行はしない。
2. 原検査・binary・SDK fingerprint・失敗・残留の恒久SHAを確定し、自分の一時資源のplan結果と
   拒否を記録する。2381へ限定配送し、親2736へsource/検査hash/レビュー原応答/残条件を渡す。
3. 自分の実job/branch/HEADを正規scheduler checkpointへ保存してからレビュー完了待ちを記録する。
   親2736/2640の完了を待つ逆依存は作らない。正式製品source/runner/dispatch/原workflow4jobは親工程。

## 2026-10-17 19時の追加レビュー受領とSID修正

実job `20261017-185644-todo-2790`。原2797/new184908のSonnet5.5応答はMust0。
actual投入に保存spec全文が一致することを自分でも照合した。raw SHA256は
06C216F531130F6A0C1B6A12B614A234812D0A3B09EBC7C44FE93C9A31823B1B。
固定base d5→head9b4a529の追加差分/指定15本文/原review/summaryだけの静的確認で、実検査0。
返答中の完全head展開に誤記があるため、投入した完全headを基準にした。原応答は恒久rootの
review-2797-original-184908.private.md、照合はreview-184908-evidence-qualification.json。

原正常74file/取消し75fileの全manifest bytes/SHAを照合した。preparedのhelper SHAと
保存bootstrap4 SHAは両方一致する。bootstrap4のVCSは23274f6+modifiedであり、9bから
clean buildしたbinaryとは呼ばない。原成功build event item10と後のcd7 commit、cd7→9bの
Go差分0からsource内容を関係付ける。埋込みVCSでclean source資格を証明した結果ではない。
NUL観測は両方exit0で、新HOMEより先に同じsourceで走った。従って後のmain/post成功は
繰返しcheckoutであり、pristine-source checkoutの独立実証ではない。READMEを訂正した。
取消しの原fixture.rust_test_pid=50228が、原drainの保持11PIDに含まれることを確認した。
checkout完全commit11d5960a326750d5838078e36cf38b85af677262、blob/JS/Node ZIPのdigestは
原SDK receiptに保存済み。製品のaction指定版一致とofficial runner同梱Nodeは未実証で親工程。

S1のicacls30秒/単発prepareは、製品規模未測定の有限な準備失敗条件として明記した。
タイムアウトした資源へprepareを再試行せずcollectionする。S6の同accountが書ける工具へ
同accountで行うhash照合は悪意job codeへの防御ではない。Job APIの測定は何らかのJobへの
所属で、元Jobへの資格は継承/保持handleによることもREADMEで明確化した。
gzipを扱わないupload-packは固定された小さい合成source限定のまま。製品serverへ転用しない。

NitのSID出力に対する256要素の仮想sliceは、標準syscall.SID.Stringへ3箇所を置換した。
初回のuintptrからの変換案はraceのcheckptr panicとvet警告で失敗。Win32出力を型付き
*syscall.SID、Tokenuserも標準pointer型に直し、両原失敗を保存した。
修正head `25579e6dd0b02ee488718398902a7b59180af24e`、module tree
227fe892bd2e24d805f0a898af774ff9ba78934e。自分のbranchへcommit/push済み。
最新Go race69pass/2既存opt-in skip、vet成功、Windows build成功。Linux buildも成功し、
その後に変えたGoはWindows専用のみ。新service/SDK/旧fixture/Rust全件は再実行していない。
この新SID差分で実service/Rust成功を新たに実証した結果とは扱わない。

5fileだけの有限追補を同2797へ保存して通常の技術retryへ戻した。specは
2797-dd5b5c4760b64981b70a8d93624a2d40.md、SHA256は
BBC1743D81121E562311EF690C07CACA6E88EAA6DB215444E62E9BF20305A2B2。
旧root2736/review関係と原履歴は保持。原184908のMust0は25579e6へ無条件に流用しない。
返答受領後はこの小差分のMustだけを処理し、親2736へ最終資格を返す。

原検査/binaryは恒久rootのsid-final-checksへ全10file/29,521,548byteをSHA一致で保存した。
新build Temp eedd86ccf57d112b95af6ff0c2085aa9はplan→別通常削除→finishでsource_absent。
以前の残留13件は配送済みのまま触れていない。新たなSID/profile/サービス残留は0。
## 2026-10-17 17時台の追補（以前の状態より優先）

実job `20261017-171647-todo-2790`。最新機能sourceはcd7dbd3、文書を含む
固定レビューheadは `9b4a529e174bee5902555967158c07d5775027af`。
Windows module treeは `01e3062d8fda2813e471ed24244626c2af41ae2a`。
共通package treeは `cbdb19827cd035d66a527818a8fb2df89705a059` のまま。
自分のbranchへのpushは成功。親branch/worktree、共通workflow生成、製品sourceは変更していない。

### 追加実装と実測

- a525811で新HOMEのGit設定と専用SIDによるsource所有を接続した。元profileのACLは変更しない。
  SDK環境欠落をerrorへ、toolchain導入後に5工具入口のhashを再照合、sccache idleを0へ変更した。
- 公式actions/checkout v4の完全commit/blobと公式Node20 ZIPのdigestを照合し、
  実main/postをfiltered service tokenと元Job内で実行する合成fixtureを追加した。
  transportはloopbackの合成smart Gitだけ。製品sourceの取得・展開・runner登録・dispatchは0。
- 実checkoutが残す無効config.worktreeはcoreのsparseCheckout/sparseCheckoutConeとindexの
  sparseがfalseの場合だけ許可する。activation key、他設定、認証、alternateは引き続き拒否する。
  checkout後の実HEAD/tree/index/全生bytes/auth不在/plain Git/所有SIDを検証してからRustへ渡す。
- owner TempではNodeが祖先metadata取得をEPERMで拒否した。長いchild worktreeではMSVCが
  LNK1104で失敗した。祖先ACLやOSの設定は変えず、元登録範囲の短いhelper worktreeへ
  safe-delete createした専用rootを置いて解消した。この配置条件をREADMEに明記した。
- 合成manifestへ[workspace]を加え、host workspaceを誤認する失敗を修正した。
  原製品manifest/command/OS条件を変更した修正ではない。全失敗の原logsを保存した。
- 同じGo内容cd7dbd3相当で正常・取消しとも実checkout main/post、Rust1.95.0と+stableの
  小さいunitが成功した。source改変は理由actual tracked bytes mismatchで入口exit1、
  実Cargo失敗はexit101。正常worker3060/child31288、保持3PID排出、controller0/worker0。
  取消しは実Rust readyの後SCM stop、worker43300/Cargo49928を含む保持11PID排出、
  worker125/controller0/stop0。どちらもactive0と保持handleのsignalledを原記録に保存した。
  owner tokenから同じ工具入口を呼ぶ負例は専用service identity不一致でexit1。
- 原NUL設定のsafe.directory警告exit255は別の限定実測。新HOME設定のmain/post成功と区別する。
  Job所属APIの実測は「何らかのJob」、特定Jobの資格は元Jobからの継承/保持handleに基づく。
  loopbackは任意job codeのnetwork sandboxではない。直接cargo-clippy等は入口guard契約外。
- 全追加接続当初のGo raceは66pass/2opt-in skip、vetと両OSbuild成功。その後のmetadataと
  合成workspace変更はSource/Materialize等の関連race各12pass、新Windows実行物と最終Linux
  cross buildも成功。既存の有効結果を再利用し、製品workspace/旧fixtureは繰り返していない。
  最新9bは文書だけなので追加buildなし。製品規模の8分materializer予算/時刻帯は未実証で、
  子で製品sourceを取って計測しない。正式製品CI成功は未達であり親の統合後工程。

### 原証拠とレビュー

恒久証拠rootは従来の `codex-owned-a92de917e7fd2ae1dd53d6b0b50b9ff0`。
checkout-short-normal/cancel配下に原JSON/log/SDK指紋/全bytes・SHAのmanifestを保存した。
checkout-fixture-summary.jsonのSHA256は
`A6EF6E0CA7B96D6E363AAD1F9681DD8E5DBE503D3C400786FBEB4107BD949E2C`。
合成HEADはa20d630ca9cfb1bad230cd79c55febba514a8c68、treeは
73e51de5a32891aedb9e4d05100f11ac93849edc。製品headではない。
bootstrap4.exeのSHA256は6E517DBC56C14FDAB620038D2437B1A5B641ABE6B39D233D1588B54719666C67。
埋込みVCSは作成時の旧head+modifiedであり、新commitの埋込みと偽らない。

2797の原Sonnet5.5応答（job165727）は38全文とfixture6を供給してMust0、実検査0。
原応答SHA256は0A5EE3A5BACE230A9BDC4FFA693D17379100593C4816BC81274176F621146572。
S1実checkout等の追補は同2797の保存specを更新して有限再レビューする。
固定specは2797-4a6603d6b5394ea89dfc9dbcefbe8ec9.md、SHA256は
8760E66E4ED9D01C3AEB7F1AA1BB2C17EB6877C2DA6B8BD7884ED9E12BB7A232。
start時に旧終端jobが残り、schedulerが旧応答をdoneへ反映したため、新差分の結果とは扱わない。
その終端反映後に同2797をstartし、正規wait-retry60秒で通常割当へ戻した。
現在は新追補の原応答待ち。root2736/review関係と既存履歴を保持し、新規レビューToDoや
直接子Codexを作っていない。旧Must0を最新差分の確認成功へ流用しない。

### 自分の新しい残留8件

全8serviceは回収済みで不在、PID0、追加ACE0。照会可能な当該SID process0。
照会不能な他process1件は完全censusの成功へ含めない。各profile/hive/classesは
DeleteProfileWを各1回だけ呼びWin32=32。各rootのplanと正規quarantineも各1回だけ
LINK_OR_REPARSE_POINTで拒否された。再試行、unload、リンク先走査、別削除APIは行わない。
checkout-residue-inventory.jsonにroot/SID/容量/リンク数/拒否を個別保存した。

| 所有ID | 配置・結果 |
|---|---|
| 4e6b9d1d0ab180ad9ed1b0536c677c86 | owner Temp、Node祖先metadata拒否 |
| 63a25d868b2cf19c636e6b3fe0f19fe1 | 自分の長いworktree、無効worktree設定の拒否 |
| 7c76f16a1aa62f080b44aaeb25aff280 | 同上、無効index設定の拒否 |
| 27b6ee88b6a62c0c78255854c66482c1 | 同上、合成workspace誤認 |
| f58e52d9e04f0c9f99183b5bbd8fbca9 | 同上、MSVC長いpath失敗 |
| a8bc311f8a7e6808a1ca1ac891d05249 | 同上、MSVC長いpath失敗 |
| f90f4faa653d129d2748de35a0e9e1af | 元登録helper worktree、正常成功 |
| 0b180c837efe14958dce3f7cbb2226fe | 元登録helper worktree、取消し成功 |

build Temp d39753f14e03fbbee0bda6af8c9fa0d1は56file/58,680,720byteを恒久rootへ
全SHA一致で保存後、既存planの通常削除を別呼出しで実行し、finishでsource_absent確認済み。
新8件は2381へ具体結果を配送し、旧5件や終了した他担当のSIDには触れない。
Discord logの既存QuestionStateRecoveryRequiredは再試行せず、ToDoと原証拠へ保持する。
親2736へ実装と条件を先に配送し、2797完了後のMust解消・最新資格を返す。
終了前に実job/branch/HEADをscheduler checkpointへ保存し、wait2790--on2797を記録する。
親2736/2640の完了や2381の回収を技術実装の前提に戻さない。
