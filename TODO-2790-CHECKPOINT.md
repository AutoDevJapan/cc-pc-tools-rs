# ToDo2790 再開記録

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
