# PR148 Linux helperの再開記録

初回レビュー2778の5件のMustへ対応した。全差分を読んだ再レビュー2781の残Must（環境変数の重複）も修正し、追加差分を再レビュー2785へ通常登録した。2785の応答確認と親2736への最終配送は未完了。正式な製品実行は親に残す。

## 2026-10-17・11時40分以降の再開（こちらを優先）

同じjob `20261017-110029-todo-2751`。停止前のitem_76は完了済みで、原記録を先に照合した。Go検査と成功したfixtureを繰り返していない。原itemは`.ci-work/item76-completed.private.json`に保存した。

- レビュー2781は実Sonnet5.5、succeeded/exit0。原応答SHA256 `07FFB3AC89278054D7C85DEED6570D0AB13F3B9056695FC6014D120089615D39`。固定base/headの全18差分と25ファイルを読了し、元5Mustのうち4件を解消、M2に環境変数重複を1件残した。レビュー側の実検査は0。
- `f2199d3`でenvpをキーごとのmapへ畳み、固定YAMLの厳しい並列制限を残してキー順にした。生スライスの重複を直接検出するunitへ改めた。Git設定とActions contextの上書きを拒否し、CI/GITHUB_ACTIONSを継承した。
- 実Rust1.95の原doctestコマンドで最初にnoexecな/tmpのPermission deniedを検出した。`dd17a4bdcb7bb007673e831a24a560408b298b59`で新guestの/work/tool-tmpをTMPDIRに固定し、原Cargo引数と/tmp noexecを保った。失敗ログも保存した。
- 最新module treeは`269264365cc6b22c2bb6314722f8021f39b038af`。このcode headでGo unit/vet成功、Windows/Linux amd64 build成功。Windows SHA256 `0A3465AD5CB10EB04CBBF3E410B650B230CB26F6508970874B08EF1EF04885F8`、Linux SHA256 `B4D4778828751E43762A3394AD3D67FDE8401F80578F492536B8A4B57266E874`。
- 新namespace/新homeに原rustupコマンドで1.95.0/rustfmtを導入し、合成lib/bin/doctestだけで原4種のCargoコマンドを実host共有gateへ1本ずつ通した。fmt/all-targets/doc/release binsがすべてexit0/drained=true、25.903秒。Rustのテストから直列値1とCI/GITHUB_ACTIONS=trueも確認した。製品検査や原全scenarioの成功ではない。
- 差分レビュー2785はc1635c9→dd17a4bの4 Goファイルと既存3文脈ファイルを固定本文で供給した。Sonnet指定の通常subwork、spec 101308 bytes、SHA256 `E302F7A762A48964367406480F72351BD67456812F5A202C38F0D0AC75C6E64D`。保存されたspecとの全SHA一致を確認した。登録を繰り返さない。
- この追加検査の2 namespaceはTreeGone/Removed=true、当該labelのcontainer inventoryは0。新一時owner `2c9d447e6463c2d4421ca0880fb410dc`はplan（45 entries/24696908 bytes）→別通常削除→finish source_absentを確認した。
- 原証拠41ファイル/24696908 bytesは`.ci-work/todo2751-env-rust195-evidence`へ全SHA一致で保存。manifest SHA256 `040610C24379FFAF9442D25E1431348B4D7A6CCBF1967FC44BFA339EAD513C8B`。削除後も全41ファイルのbytes/SHAを再照合した。前段の113証拠も保持する。

次は2785の実モデル・固定head・4差分読了・Mustを確認する。Mustが残れば直す。Mustなしなら親2736へ実hash、原応答SHA、fixtureと正式CIの区別、残る制限を最終配送する。製品workflow/source/runner登録/dispatchをこの子から実行しない。親の完了は待たない。レビューだけが残って終了する場合は正規`wait 2751 --on 2785`と最新HEADのscheduler checkpointを保存する。

Discordの後片付け投稿は既知のQuestionStateRecoveryRequiredのため未送信。再送や迂回をせず、元noteに残す。本人の判断待ちではない。

## 2026-10-17・11時台の再開（こちらを優先）

実jobは`20261017-110029-todo-2751`。前回のcleanなc28ae93から再開した。元2736のspec/checkpointと運用指示を読み直し、同じ検査やレビュー登録を繰り返す前に元2778の完了を確認した。

- 原レビュー2778: 実Sonnet5.5、succeeded/exit0、原応答SHA256 `CDF80D55E329CFE9B1BC33365E6B39C3EDAB648AA94DA346CD20833CC62D0B01`。15ファイルのworktree読解で、固定diffの読了は未達。実装側が旧worktreeと0377dd0のmodule一致を照合した。原応答と受領情報は`.ci-work/review2778`へ保存した。
- 5Mustの修正は`bdd9a61c30e09601e2dfe77ecc3e8b407d6a9b50`へcommitした。全出力の排出/完全log保存、固定YAMLのenv・作業場所・timeout継承、Landlock ABI3/REFER/TRUNCATE、記録欠落時のnamespace停止とbroken化を実装した。詳細は`docs/todo-2751-review-response.md`。
- 公式Listener --versionの初回起動では祖先`/`のディレクトリ一覧が拒否された。fileのread/exec/writeを広げず、SDKの一覧だけを許してexit0を確認した。原Rustup commandはreadonlyな設置先のself-updateで一度失敗した。新CARGO_HOME/binへSDKのRustupをコピーし、原commandを変えずに新homeへ1.95.0/rustfmtを導入してexit0を確認した。両失敗も保持する。
- bdd9a61で、新実HTTPの正常/取消し/33MiB超/記録欠落の4fixtureと原Rustup導入がすべてpass、合計31.325秒。記録欠落はkernel群不在の確認後にmetadataだけを欠落させた安全なfixtureで、任意host強制終了を実証したものではない。制限下のmv/ln、helper/plan書換え拒否も実測した。
- 最終code headは`c1635c9ce37cb07524335130e5e2b040f549fa92`、module treeは`252685436e69fe57bab8e29a682c82e1719b381c`。追加差分は、数値repo IDをworkflow読取より先に照合する処理とそのunitだけ。fixture経路を変えておらず、既済の実fixtureを重複実行していない。最終Goはsubtest込み126 pass/4 skip/failure0、vet0、Windows/Linux build0。
- 最新Windows artifact SHA256 `C2119F8737FD728423A7AAAFBC9F183DC1498DF1422EAE64A28EE399F7BCF688`、Linux artifact SHA256 `2BA32E68899F05902DA117D022F3FD43A520E51DFB25B478C498A82E310AB94D`。
- 固定全diff、18 headファイル、既存7文脈ファイルと有限bridge抜粋を1つの入力へ直接供給した。新レビュー2781は通常subwork/Claude Sonnet指定、登録specは360222 bytes、SHA256 `C48B1898B73CDA39567BB615A2A8E3FD728AA8957F953B822E73E42EE6B3A508`。原specと保存specの一致を確認した。gitが起動できない読取環境でも、固定blobを読める形にした。再登録しない。
- 自作の追加15 namespaceを停止/PID0/削除し、最終inventoryは当該container0。新一時owner `dfb24dac2f92ec49391fc7251827ae1f`は141 entries / 229279614 bytesのplan→別通常削除→finish source_absentまで確認した。
- 新しい原証拠は`.ci-work/todo2751-review-fixes-evidence`の111ファイルへ保存した。manifest SHA256 `1266E0BED74B7C7038F0E0C941757E6A3658C70E694BAB71124AE388905A9AE5`。最終追加2ファイルは`.ci-work/todo2751-scope-final-evidence`へ保存し、全113ファイルの参照・bytes・SHAをまとめたmanifestは`F9D39580CE135881EF5DD21D0EF395E9C8C322D1572CA0F5C0D3B56567311388`。既存111ファイルを重複コピーせず参照し、削除後も全113 SHAを再照合した。原証拠を消さない。
- helper repoの実APIはAutoDevJapan/private/nonfork、ID1406818713を再確認した。最初のTLS timeoutだけを技術再試行し、成功を確認した。製品PR148は元75a/base1100のまま。原Rust runのjob APIで両OSの名前が`test (ubuntu-latest)`/`test (windows-latest)`、runner0、steps0であることも再照合した。これはjob metadataであって原実行logや正式検査成功ではない。

子工程で製品source取得/展開/実テスト、workflow書込、dispatch、runner登録、GPU/Civ6、service、旧bridge/guard/manifest/旧root変更は0。元2736/2640/880の停止・依存・未完を維持する。既知のQuestionStateRecoveryRequiredを迂回してDiscordへ送信せず、今回も後片付けの投稿を再送していない。

次は元2781の実応答のbase/head・全差分/18対象読了・実モデル・原SHAを照合し、Mustが残れば実害を直す。追加レビューを受けるまで正式利用可能とは報告しない。親の完了を待つ逆依存は作らない。レビュー待ちで終える場合は正規`wait 2751 --on 2781`を保存し、最新の実HEADをscheduler checkpointへ残す。

以下は前回までの履歴である。

## 固定した成果

- worktree: `D:/CC/Work/unattended-operations-20261013-wt/todo-2751-pr148-linux-runner-cargo`
- branch: `scheduler/todo-2751-pr148-linux-runner-cargo`。切替はしていない。
- 最初の保存: `52d2ff2`。修正後のコード: `0377dd0f66bfb69b58331cb497625b4f5e8b6ae5`。
- helperのmodule tree: `066dab0b76d6194121e092f0f4c14e9c73b541b9`。
- 元2736の計画は`822b4cb`から同じファイルを取り込み、既存のprobeも取り込んだ。共有resident bridgeと原製品には変更していない。
- `pr148-linux <pin-json> <new-owned-receipt-dir> <Linux-helper> <official-pwsh-tar>`を親向けに実装した。この子工程では呼び出していない。

## 実装した制約

受け付ける製品は元指定のrepo1392152644 / PR148のみ。元75aの固定tree/base/branchを保持する。実行用pinは元75aとは異なる新HEAD/treeを明示する。新commitの親が元75aだけであること、全indexのpath/type/modeと変更範囲、三workflowの生成案と全bytes一致をAPIで検証する。原HEADへ戻す操作や任意の最新sourceを受け付ける経路はない。

同branchの全OSを含む`workflow_dispatch`とrun/attempt/Linux jobを固定する。専用labelだけの公式ephemeral runnerを、新しい非root・init付き・readonly containerへ配置する。hostのbind/socket/GPUは渡さない。PowerShell assetは親が確認したSHAで照合し、stdinで新tmpfsへ渡す。Cargo/Rustupには新しいtmpfs homeを使い、guestのtargetは原scenarioが参照する`tools/target`のまま。

有限callbackは元workflowのCargo引数集合だけを受け、repo/run/attempt/source/nonceと実jobの割当を毎回照合する。hostのcommand/path/envをHTTPから受け付けない。ホストの共有Cargo gateが追加したtargetと並列数は検証後、guestの固定targetへ対応付ける。fmtは元gate自身の非ビルド扱いを維持する。

Landlockでworkerから本物のCargo/toolchainの実行・読取とhelper/planの改変を拒否する。実Cargoの全子孫にはseccompを継承させ、プロセス群からの離脱と監督プロセスへの操作を拒否する。入れ子の直接Cargoも、既に取得した同じpermitとプロセス群の内側に残る。callbackの再入・再送は拒否する。

guest supervisorが実Cargoを始める前のhandshakeでPGIDをhostへ渡す。正常/取消しとも群を排出し、hostが別のexecでkernelの群不在を確認する。単なるCLI exitやguestのファイルだけでgateを解放しない。群の確認が不能ならowned namespaceを止める。それも確認できない異常時は、証拠を残してgateを保持する復旧待ちとなる。この異常経路を新しい常駐サービスの成功とは扱わない。

source取得/全blob/index tree/実HEADの検証には既存のmaterializerを使う。start/complete hookとhostの前後で、job/source/workflow/OS/checkout認証不在を確認する。runnerのexitから正式CIの成功を登録するAPIはない。

## 検査済みのこと

同じ最終コードの検査は繰り返さない。

- Go unit: subtestを含む119 pass、3 skip、failure 0。
- Go vet: exit 0。
- Windows buildとLinux amd64 cross build: exit 0。
- Windows binary SHA256: `7CE9A8A99D8A4CED2915C76FB477085CBD22710E5DB1C3197799E0C26CA9F8F4`。
- Linux binary SHA256: `DE3DC6578DEA5094B7140CBAC754C6FF82CA1AF0F2C6D020EA07D9B5857C4EC3`。
- 新namespace、実guest HTTP callback、実host共有gate、合成Rust crateを通した正常/取消しfixture: 両方pass。正常exit 0、取消しexit 125。実子プロセスを観測後、gate解放時に不在を確認した。
- workerがSDKのCargoを直接実行すること、コピーすること、toolchainを読むこと、proc経由で読むこと、helper/planを改変することは実kernelで拒否された。
- これはSDK既存toolchainを使ったfixture。Rust 1.95.0による原製品検査、全scenario、公式runner登録、製品sourceの取得/展開、workflow書込/dispatchは実行していない。正式jobの成功は未確認。

最終fixtureは7.203秒でpass。unit/vet/buildと同じコードをその後0377dd0へcommitした。build時点のGit metadataは未commit差分ありだが、実行したGo source bytesは保存後と同じ。新しいcommit名だけを理由に再検査しない。

## 失敗の経緯と自分の回収

最初のcreateではDocker JSON配列の切り出しを誤り、所有照合を拒否した。修正した。次の合成Rust文字列へ、回収用Go testを誤って混ぜたためfixture compileが失敗した。この回収testは除去した。回収を早く実行して自分の実行中fixtureを1個止めてしまったため、そのnamespaceの停止・PID0・削除を独立確認した後、ハンドルと開始時刻を固定した自分のcallbackだけを終了した。他担当の資源には触れていない。

標準入力をGoのコピーgoroutine経由で渡すと、正常終了後もEOFまで待つ問題も修正した。現在は元のpipe handleを渡す。後続の直接gate fixtureと、実HTTP fixtureの正常/取消しは成功した。

作成した全10個のnamespaceは、停止・PID0・削除を確認済み。最終Docker照会は当該cc.todo=2736のcontainer 0件。新runner登録、Chrome、GPU、Civ6、service、host製品checkoutは作っていない。

原証拠は一時rootの所有ID `505512cad16f3e4ab1aafd0dc86cc3e1`で作成した。67個・108300 bytesのJSON/logを、永続保管先`.ci-work/todo2751-evidence-20261017`へコピーし、全SHA256を照合した。`manifest.private.json`のSHA256は`DF7EED005B9A11450255395BD7CDB5DB3778044AF1C8B319875882FEFC1B8E62`。失敗時のJSON/logも保管した。イベント履歴の一部は元jobのevents.jsonlにも残る。

一時rootの削除はまだ行っていない。永続証拠を確認後、safe-deleteのplan→別通常削除→finishを実行する。拒否時は同操作を繰り返さない。永続保管先は原証拠として保持する。

## 残り

1. 固定0377dd0の全変更と既存有限adapterを、通常共有ToDoへ登録した独立Sonnet reviewerに確認してもらう。登録番号と原応答はここへ追記する。直接子Codexは起動しない。
2. Mustを価値で判定して直し、変更した範囲だけ検査・追加レビューする。製品source/正式実行は親2736に残す。
3. helper branchをpushし、実HEAD/module tree・Go/fixture実結果・別モデル原応答・残留/未達を元2736へnote追記配送する。親2736/2640の完了を待つ逆依存を作らない。
4. 所有rootを通常回収し、正規scheduler checkpointを保存する。レビューだけを待って終了するなら`todo.exe wait 2751 --on <review-id>`を必ず記録する。

2026-10-17 10:38台の再開では、元jobのitem_78がexit0で完了し、Go119 pass/3 skip/failure0とcontainer不在を確認した記録を先に読み直した。完了済み検査は再実行していない。

## レビュー受付と回収の追記

コード0377dd0と記録41b5d23は専用branchへpush済み。固定コードの独立レビューをToDo2778へ通常登録した。モデル指定は既存成功例と同じ`claude-sonnet-5-5`。元2751と実jobを結ぶreviewのsubworkで、受付時のstatusはtodo、jobsは空。まだ実レビュー応答は受け取っていない。

保存されたspecのSHA256は`9B7C1FF1FAECFE636EDE1A4B0C206415775999DB522F988EE9AF907CF806A469`で、登録前のファイルと一致した。最初の`anthropic/`付きmodel指定は、ToDo CLIの形式検査がslashを許さずValueErrorで未登録だった。登録済みの重複は0件と確認後、既存の成功例の短い指定で2778を登録した。実行審査の拒否や認証の問題ではない。

一時rootは全67証拠ファイルの永続コピーをSHA照合後、safe-delete planが返した通常削除を別呼出しで実行した。planは87 entries / 24600039 bytes。削除exit0、finishは`source_absent`。削除後も永続67ファイルのSHA一致とmanifestのSHA不変を確認した。新しい課金・製品操作・正式registrationは行っていない。

次の再開は元2778の実応答を受領し、固定base/head/全15対象の読了、実モデル、原応答SHAを照合する。Mustを解消するまで親の正式実行へ進めない。同じコードのGo/fixture検査や同じレビュー登録を繰り返さない。製品の未達と元2640/880の依存は維持する。

正規scheduler checkpointはd234b2dの実HEADでsaved=true、launched=0、source_files_changed=falseを確認した。要求と結果は`.ci-work/checkpoint-2751-*`へ保存した。この記録追記後の実HEADも最後に保存する。

通知しない後片付け記録を`discord_ask.exe log --topic 後片付けの記録`で1回試したが、`QuestionStateRecoveryRequired`によりexit1となった。日本語点検でも1行内の識別子数の注意があった。投稿成功とは数えず、原出力は元jobのeventsに保持する。これはDiscord道具の原台帳復旧待ちであり、自動承認レビューの拒否や本人の判断待ちではない。別経路で投稿せず、同じ送信を繰り返さない。実装と独立レビューはこの待ちへ付け替えない。
