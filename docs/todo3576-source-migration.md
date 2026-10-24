# PR149の固定main合流と未実行枠の移行（ToDo3576）

元2962が製品PR149、実台帳、配布、登録、実行の唯一の担当。この工程は
cc-pc-tools-rs（repo ID 1406818713）のcontract / route / queueと関連資料だけを変更する。
製品のpublish / rerun / 登録 / サービス / 配布 / 台帳実操作は行わない。

## 固定仕様

- OriginalHead / Tree / Base、Proposeと旧replace-expiredは保持する。
- 追加世代は旧tip `2bd3a366f8974ba54df83927102aa5bdad3983e5` を第一親、
  main `392e7f9bbcea6ee5867b78da37de1efd9f9fec48` を第二親とする。
  main treeは `b5680a158cfcad929e49e97702eec210cc82135d`。
  変更はmainとの差を3workflowだけに限定し、親集合・順序もAPI照合する。
- mainのAPI原sourceをtestdata/main392へ保持する。イベント、権限、job名、
  Rust/Lua/シナリオ/all-targets/doc/release/fmt/Clippy、checkout版を保持。
  旧concurrencyを再適用し、Windows限定、固有label、同repo head checkout、hash記録だけを加える。
  mainのdoctestを重複追加しない。forkはhostedへ送る。
- route prepareはGETと候補保存だけ。追加世代のpublishは元の準備とは分離し、
  固定old tip・main・tree・親・3file、exclusive intent、非force PATCH、再読を要求する。
  mainが進んだ場合は追従しない。
- queueの明示移行は旧initial3keyだけから同workflow/jobの新世代attempt1へ一対一。
  元予算配列、期限、延期、結果、cleanup、supervisor履歴を保持する。
  旧expired_replacementsとは別のproof配列を使う。通常tickで移行・rerun・成功生成をしない。
- 旧取消・未割当・stepsなし、元source、元資源closed/root不在/同SCM/登録不在/
  privileged marker/queryable account process 0を移行前後に照合する。
  未作成は厳密なneverCreated。foreign fenceを消さず、再差替えを拒否する。
- proofは同期後に排他的公開。中断時は原At/Censusを保持し再検証する。
  最終台帳更新は全体CASとatomic保存。Windows1、Cargo gate、24h/6延期を保持する。

## 実測と受入記録

2026-10-24、正規GitHub APIで両repoの実owner/ID/publicを確認。
PR149はopen/head2bd3/base1100/dirty、実main392e7を確認。
main原workflowのblob / SHA256:

| file | blob | SHA256 |
|---|---|---|
| rust.yml | ac17a1126f7a8b19f16e52b6d92a17e630cc2801 | 72858e501824f1949b3caa4a298e6610212b27f1382e68b98bd2b7b1a11d077f |
| native-driver.yml | 4ef3cf54b291f313647c77126374d5b41fe62c40 | c2eb4b8e7710e52810e190183c30df40e114a88670357ddb83fc96c772c0414c |
| live-chain.yml | c72c23c71e67d131b841289fb2e90dd20fb63b5b | d1f68680e1be885240d205a4981011a46209dbc27c4fc34a7c54f51ec891f989 |

Red: `go -C tools/civ6-ci-contract test -run TestFixedMainMergeAdmission -count=1` は
旧コードで `initial routing commit must retain sole original parent` により失敗。
`go -C tools/civ6-ci-queue test -run TestExpiredReplacementRejectsUnsafeOrUnboundedRecovery -count=1`
は成功し、別head/attempt等を拒否する既存境界を確認した。
追加世代実装後のcontract / routeの通常Goテストは成功。
contract / route / queue / Windows helper / Linux helperの5moduleについて
`go test -race -timeout 180s ./...`、`go vet ./...`、`go build`が成功。
Windows上の既存Goインターフェースの検査であり、Linux利用者向けの新受入ではない。
Rust/Cargo/GPU/Civ6の実行は0。

2026-10-24、生成したroute候補の `prepare-main392 <新owned root>` を実行し、
正規APIの固定old/main/全tree照合と候補保存が成功した。
返値は `operation_complete:true, official_ci_success:false`。
この工程でpublish・rerun・登録・サービス・配布・既存実台帳操作は0。
ローカル検査の実行物を配布済みとはしない。

関連テストは正規main合流、旧世代の維持、実mainの全コマンド/checkout保持、
同名別repo/fork/owner/base/親順/追加親/全tree追加file/command/checkout拒否、
旧manifest流用拒否、非force publishと未知応答観測を確認する。
queueは3枠一対一、旧履歴とraw result保持、二重/別phase/実行済み/不明cleanup拒否、
前後のcollection/census、foreign fence保持、通常tickとの排他、全台帳CAS拒否、
同期proof保存後の中断/原At/Census再利用を確認する。
レビュー・CI・CC側merge・親への返却は実施後に追記する。

自己点検で、既存Aliveのfalseだけでは作成時刻欠落/照会不能を停止と区別できない点を
再現した（新テストが `missing process creation time was treated as stopped` でRed）。
新しい移行の入口だけに、記録されたsupervisor/helperのPIDと誕生時刻・同一handleの
終了signalを確認するVerifierを追加。照会不能/欠落/生存は拒否し、元の通常処理は変えない。
同一実processの生存、PID再利用、不存在、欠落、前後の照会不能をGoテストで確認した。
stdinのEOFで終了させる自分の有限な子processを使い、handleを保持した終了signalも確認。
この追補後のqueue全race（18.758秒）とvetは成功した。
Windows APIの扱いは[OpenProcess](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-openprocess)、
[WaitForSingleObject](https://learn.microsoft.com/en-us/windows/win32/api/synchapi/nf-synchapi-waitforsingleobject)、
[MicrosoftのProcessManager](https://github.com/dotnet/runtime/blob/main/src/libraries/System.Diagnostics.Process/src/System/Diagnostics/ProcessManager.Windows.cs)
を照合した。source_migration_requiredは通常tickの拒否理由/有限backoffにも残す。

製品全体のPR149完了とは区別する。旧PC実行0/過去CodeQL2success、
新source Windows3+CodeQL2、merge後Windows3+CodeQL2を分け、計12実行上限を保つ。

| 区分 | 作成/取消の実績 | 実行の実績・予定 |
|---|---|---|
| 旧Windows | 公式job3件作成、3件取消、全runner_id0/steps0 | PC開始0 |
| 旧CodeQL | run39423558012のAnalyze(actions/rust)2件 | 2件success（実API再確認） |
| 新source | この工程では未作成 | Windows3+CodeQL2の予定 |
| 製品merge後 | この工程では未作成 | Windows3+CodeQL2の予定 |

旧Windowsの取消3件を実行と数えず、元作成数も消さない。
旧Rust資源1件の作成/回収済みと未作成live/native2件は元2962の記録を保持し、
本工程のfixture検査を製品資源の実操作へ読み替えない。
旧job3件のAPI上のstarted_atはいずれも2026-10-20T12:36:23Z、
completed_atは2026-10-21T12:36:23Zで、差は86,400秒だった。
runner_id0/steps0のため、このstarted_atはPC実行開始の証拠ではない。

## 再開

branch `scheduler/todo-3576-pr149-2962`、初期HEAD `d234d3144c26973bd35d44c518507852ceaee55a`。
最新HEAD・レビュー原文・receipt・CI・残手順は担当の所有付き再開領域に記録する。
新しい作業領域はguard clearを確認。親の分類未確認の旧Tempは参照せず、
原sourceは正規APIから再取得した。未使用の空Tempは正規quarantineで退避済み。

## 新CLIと親2962の移行順

これは親が統合後に実行する手順。この工程では製品実操作を行っていない。

1. このCC側PRのmerge commitと、common guard/safe-deleteの最新source・資格・
   単一writerを照合する。旧候補で共通2本を置換しない。
   新schemaの資格を旧expired_replacementsの意味へ読み替えず、製品実操作の前に
   親が必要な共通側の統合を行う。Version2と新migration配列も統合対象。
   親はqueue/Windows helper/routeと外部pinをこの同一sourceから生成・正規更新し、
   通常tickが新queueのpinを使うことを確認してから手順5へ進む。
   移行後は旧queueへ戻さない。このソース工程は3455/3258等をwaitしない。
2. 親が新しくsafe-delete createした領域で、レビュー済みsourceからビルドした
   `civ6-ci-route prepare-main392 <owned-root>` を実行する。
   `proposal.private.json` はgeneration、old head/tree、main/base、baseline_tree、
   固定順のparents、3fileの原/候補digestとblobを記録する。
   既存の `prepare` のmanifest・candidateを流用しない。
3. 親が同じ候補に対して `civ6-ci-route publish-main392 <owned-root>` を1回だけ実行。
   intentはexclusive、全treeはmain比較、commit親は `[2bd3...,392e...]`。
   PATCHは非force。未知応答ならjournalと実refを観測し、同コマンドや別経路で再送しない。
   mainやold tipが進んでいれば記録を親へ返し、追従しない。
   固定sourceが必要な期間（publish〜新Windows3jobの完了）は、親が既存担当へ
   mainへの合流を入れないよう共有する。mainが進んだ場合は当該sourceを拒否し、
   自動追従や新しい差替えで上限を増やさない。
4. 親が公式APIと最新の予算記録を再照合し、新sourceのWindows3jobを特定する。
   旧24h取消/未割当、元result/閉じた資源/SCM/登録不在/privileged markerと、
   新jobのqueued/未割当/stepsなし/attempt1/同workflow/jobを維持する。
5. `civ6-ci-queue migrate-unexecuted-main392 --config <正規設定> --entry <旧key> --replacement <新run/1/job>`。
   未作成live/nativeを先に受け付け、Rustのforeign fenceを残す。その後Rustの
   前後のcollection確認を通し、自分のfenceだけを解除する。
   元の旧3keyは本書の元依頼どおり固定され、SHA/plan/manifestをCLIで与える入口はない。
   CLIの技術的な上限は10分。公式jobの実行期限や24h/6延期を延ばすものではない。
   supervisor/helperの識別欠落・照会不能・生存、非Windowsのprocess確認は拒否する。
   原台帳は変えず、原因解消後に同じ原proofのまま再検査できる。
6. 通常tickが新entryを1件ずつ供給する。既存Windows1/Cargo gateと24h/6延期を維持。
   新entryは原proofのverified_atを期限起点にし、延期回数を引き継ぐ。
   中断しても新entryの期限を後ろへずらさない。
7. 正式Windows受入・hash・回収→PR149通常merge→通常main eventの受入・回収は
   親2962が続ける。公式成功の確認は既存RequireSuccessで別に行う。

## 証拠schemaと互換境界

`Budget.initial/normal/continuation/expired_replacements`の原記録を保持する。
最初の移行のatomic保存で台帳Versionを2へ進める。新版は1/2を読み、旧版の
Version1専用readerは2を拒否するため、新migration項目を落として再保存しない。
追加は `budget.unexecuted_source_migrations[]` の
`original/replacement/generation/proof_sha256` と、新entryの
`replaces_unexecuted_source_entry`。旧entryはstateだけrefusedへ閉じ、
期限/延期/開始履歴/result/cleanup/reasonを保持する。
旧replace-expiredは同run attempt2だけで、新sourceを扱わない。
元固定3枠があるとき、continuation設定でも通常reserveで移行を迂回できない。

旧entry配下の `source-main392.json` はversion/generation、原entryと自分のfence、
replacement plan/job、旧取消API、retirement、前後のcensus（Unreadableを含む）、
元resultの原バイト（JSONではbase64）、never_created、verified_atを保持する。
一時fileをSyncしてno-overwrite公開後、そのハッシュと一対一関係を全台帳CASで
atomic保存する。公開後は原fileを読み戻して一致を確認する。
保存中断後は外部/collectionを再検査し、原At/Censusを変えない。
foreign fenceは台帳CASで保持し、他枠の過去fenceを自分の原proofへ固定しない。
原proofと異なるsource/取消/割当/結果/ledger変化は拒否し、証拠は消さない。
未作成はcleanup none/helperなし/過去資源なしを要求し、終了supervisorの履歴は残す。
created資源はverify-collectedの既存独立cleanup契約を使用し、旧branchの現在性を要求しない。
前後のhelper出力はsource-before/source-afterの別フォルダーへ残す。
過去のsynthetic merge commitを再取得する条件は置かず、元admissionで記録した
WorkflowDigestを固定old headの照合済みworkflowと比較する。

## 別モデルレビュー3579への対応

固定d234d314→ff360970の実Sonnet5.5/maxレビューは読取完了、Must0、
Suggestion7/Nit5。Go実行と外部APIはreview sandboxで不可であり、実装側の
成功結果とは分けた。原応答とmatching native contextは担当の正規記録に保持する。

- S1: 移行後のVersion2と旧writer拒否、同一sourceのpin更新/旧版へ戻さない手順を追加。
- S2: 先行するbbc085aeでRefusal/BudgetBlocked/5分backoffを追加済み。
- S3: 実取消時刻3件が24hであることをAPI再確認。孤立mergeへの依存を外し、原digestと固定old sourceを照合。
- S4: 有限なCLI上限を10分へ。原実行予算・期限・延期は維持。
- S5: 原proofに自分のfenceだけを記録。他枠が先に回収された中断再開もテスト。
- S6: 前後別のhelper出力を保持するよう変更し、2出力が残るテストを追加。
- S7: mainを固定している必要がある期間と、進んだ場合に追従しない手順を明記。
- Nit: 一時領域IDを公開手順から除去。新世代のcommit messageを明確化し、
  拒否理由を分け、公開proofを読み戻す。交差workflow/延期6回/履歴照合の直接テストを追加。
- 追加レビュー中に自己点検で、照会不能テストの「後」が前段で止まっていた点を修正。
  初回/collection前/後/最終の4箇所をそれぞれ通し、Census回数とproof未公開もassertする。

追加8fileの実Sonnet5.5/maxレビュー3581もMust0、Suggestion5/Nit4。
読取のみでGo/race/native実行やGitHubのCI再確認は行っていない。
テスト4境界/NewDeps配線、native子の30秒上限と生存理由、正確な5分backoffと
同時刻の再tick抑制、supervisor/helper/PIDを含む拒否理由、段落と手順を改善した。
アクセス拒否時のQUERY_ONLYによる再照会案は採用せず、照会不能を不明として拒否する。
既存resume-unassigned/replace-expiredのAliveだけによる確認は今回変更せず、
元の通常継続/独立cleanup/旧attempt2の仕様境界を保つ。親への継続記録に残す。
