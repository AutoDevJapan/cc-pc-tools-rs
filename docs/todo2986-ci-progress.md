# PassShare の CI 削減（ToDo2986）

## 最新HEADと次の受領（最優先）

PR720は`d96fbabe6559e6b55eb8d5be2e10889f03a9a472`、元mainから20ファイル。
Windows2workflowを3010から引き継いで統合した`8398204`と、その前の`8d4a21b`を履歴として保持。
3012の実レビューは完了。Must1は既知の生成定義の更新漏れ、他の確認範囲は問題なし。
ただしpnpm-lock2本文とmain_test.goは未読を明記しており、その範囲は補足確認に残す。
3015は8d4a21b→8398204の4ファイルを読取中。そこへ今回のsourceを勝手に混ぜず、
追加のraw改行とmobile/windowsの5ファイルだけを別の有限補足へ配送する。

8398204のNative WebView job118096806375は同じdrift検査で失敗したが、内容差ではなく
desktop-schema.jsonのCRLFだった。Windowsのgit apply/hash-objectのフィルターにより、
正規化後blobと実ファイルbytesを取り違えた。原失敗は保持し、今回から--no-filtersのraw blobと
GitHubのPOST blob応答を照合してから、base64から作った正しいblobをtreeへ指定する。
Desktop schemaの正しいLF blobはc2202ed、SHA256はf0e08336353666f6174241964bfe0260cca6379fa7659825433b1263f82a91bd。
初回のSHA c5724c5...はCRLFであり、正規CIのraw一致とは扱わない。

同じTauri更新が両clientの生成物に及ぶため、desktop/windowsとmobileのACL/desktop/windowsもそろえた。
元の各clientのdesktop/windowsファイルが同一blobであることをAPIで確認した。
Mobile desktop schemaには、実Desktop generator差分の4つの共通定義hunkだけをcontext検査付きで適用。
Mobile ACLは、実Desktopの旧→新差が4つの定義追加だけであり、対象の旧Mobile pluginと同一である
ことを厳格に確かめて、その定義だけを移すGo helperを使った。既存grantを変える入力は検査で拒否する。
Rustのstruct field順と元JSONの生バイトを保ち、全体のGo map再直列化は使わない。
Mobileはこの時点で新たに実生成したとは主張せず、正式CIのdrift/実renderer受入で最終照合する。
原候補・raw hash5件・base blobs・patchは`.ci-work/all-schema-receipt.private.json`等に保持。

3012/3015の原応答を保存し、各実job/thread/cwdとturn_contextで実Sonnet5.5を照合した。
3012原SHA39b8d682b66a8c14cd86356af8cf24257c0b2ccc73b697c449d9fb2b20d2c646、
3015原SHA1e3c6b3705caac663d6f82e15446100dde6835e2418aed89b12212d0d014a595。
両者のMustは今回修復した生成定義/CRLFで、3015はWindows2workflowの意味変更にMust無し。
3015はAPIに接続できず提供済みの私的request/response資料を読んだ点を区別し、
実remote blob/sourceの照合は親が正規APIで補う。拒否された接続設定を変更していない。
3017は追加5件と前回未読のlock2/main_testだけを確認中。読むための限定raw9blobを
共有Git object storeに保存し、`.ci-work/review-source-blobs.private.json`へ列挙した。
raw製品sourceを補助repoの公開commitへ入れていない。

残りは3017の補足レビュー、同HEADの全CI成功、通常merge、main確認、実行量と課金APIの再読。
PR721は原候補の親をPR720に含めたので、通常merge時の元PRの状態も確認し必要なら整理する。
共有ToDo2986/元2957/引継ぎ3010へ、実施済みと未達を最新noteとして配送した。
scratch ef08はクロスドライブ退避が未実施で元にある。レビュー中の入力を保持し、
終了回収時に同Dの削除希望へ退避する。旧PROTECTED_PATH rootを再操作しない。
CIの無目的rerunは一度も実行していない。ソース修復ごとの通常PR同期だけ。

## 生成定義の修復と3010からの統合引継ぎ

`8d4a21b`の品質ゲート、Desktop全体/doctest、Mobile、監査、DB統合は正式成功。
Native WebView job118088948209はRust回帰試験の成功後、`git diff --exit-code`で
Tauri依存更新に伴う生成定義2ファイルの差を検知し失敗した。元のCSP/実画面受入は消さない。
正規jobのログpatchを保存し、許可した2pathsだけを元commitのAPI bytesへ適用した。
`git apply --check`成功、生成後blobはログのindexと一致した。

- acl-manifests.json: `b7e734d38c3f2bf0d4f257a7896332271c6b54ac`
- desktop-schema.json: `c2202ed85785caaf57f12c34137d930945617694`

JSONの実差はcore:appのallow/deny-exitとcore:windowのallow/deny-set-fullscreen-on-monitorの
定義追加。既存default_permissionは変化せず、capability/CSPの許可設定を変更していない。
原patch・小さい意味差は`.ci-work/generated-schema.private.patch`と
`generated-schema-semantic-diff.private.json`に保持。

3010の共有noteが、PR721/e19dbf27ff1a27df344dba52bcde436552464b79の2workflowの
最終統合を2986へ引き渡し、3010は並行編集しないことを明記した。本人最新方針も同じ分担を指定。
親はAPIで固定候補2bytesを取得し、CLI側はそのまま、Desktop側は関連変更のjob内判定を加える。
元のWindows固有commands/env/途中失敗伝播/artifact設定は構造比較で保持。
関連なしPRは軽い変更判定で終了し、Windows native試験と配布buildは起動しない。
関連変更/新しいmainではWindowsの元の試験と配布を維持する。TS型検査をnative compileより先にする。
共通の変更判定は既存の実Git17ケースをそのまま用い、Desktopの意味保存検査も成功、4workflowのactionlint成功。

残る差分レビューは、3012の固定8d4a21bの結果に加え、生成定義2とWindows2の4ファイルだけを確認する。
PR721の元commit/レビューを保ち、統合・必要CIの実成功後に重複PRを整理する。
本人PCへの正式移管・Windows製品全体の完成はこの統合の成功へ含めず、元2957/2994に残す。

GitHubの対象repo限定の2026-10-19使用量APIは、Linux/Windows/macOS/storage全SKUで
netAmount=0 USDを返した。暫定の取得時点の観測であり、確定請求や毎月の削減額とは主張しない。
原データと時刻は`.ci-work/passshare-actions-20261019-billing.private.json`と
`billing-observation-before.private.json`。

新しい所有rootのplanは37,577,232 bytes/1029 entries、隠し属性でquarantine指定。
quarantineは移動前にQUARANTINE_CROSS_DRIVEを返し、元rootを保持した。
最終source/証拠保存後に、規則どおり同じDドライブの削除希望へ退避する。
旧PROTECTED_PATH rootには触れていない。

## 現在の固定候補（最優先）

PR720のHEADは`8d4a21b73f3ad6301ba37ecb056b221c9d5c626e`。
前段`2decea3`でcargo-audit（19秒）、DB E2E（167秒）、Rust Docker（98秒）、
Desktop smoke（178秒）が正式成功し、初回の実失敗2系統を解消した。
一方、共有WASMの出自manifestが古いroot Cargo.lockを要求し、画面/拡張等のbuildを拒否した。
これは監査の免除や無目的rerunで隠していない。

同じ`2decea3`で自動起動していた公式Cloud WASM acceptance run39260197802はsuccess。
verify job118085531351で元のRust/WASM/Node/web transport受入とbrowser bindings生成が成功した。
同repo/同head repo、固定source SHA、PR branch、workflow path/ID、成功/attempt、artifact IDを
APIで照合後に公式artifact12191110523を一度だけ取得。ZIP SHA256
`959d7059852d96a89ec700c6b3082ef45287ca39a8b46f149d2de52796744efc`と公式digestが一致した。
3assetだけを取り出し、source inputs40件を固定commitのAPI bytesで全照合した。
旧manifestのrootlock SHAとsourceCommit/runを正しい正式実行へ更新し、JS/WASMを同じ実バイトへそろえた。
これらの記録は`.ci-work/wasm-promotion-receipt.private.json`と`wasm-source-inputs.private.json`。
WASMをPCで再コンパイルしていない。出自/ソースの検査を緩めていない。

共有依存の更新がdesktop全体に及ぶため、最終候補のDesktop検査は2つのfilter付き呼出しを
`cargo test --locked -p passshare-desktop`の1回へまとめ、smoke/native_lockと全体/doctestを含める。
core/connect/op-cliの元Docker検査とmobile全体/doctestも保持し、5workspace membersを覆う。
独立の手動最終workspace workflowは削除していない。
合計12ファイルの必要修復となり、Desktop/op-cli配布workflowやPrd/公開/予算/署名には触れていない。
3012へこの最新候補の追加レビューを配送し、正式CIと同じ固定HEADで照合してからmergeする。

## 2026-10-19 22時台の再開

旧記録は履歴として保持する。最新のOS方針
`owner-windows-completion-before-other-os-20261019.md`を読了。
desktop-build.yml / op-cli-build.ymlは3010/PR721の唯一の書込範囲であり、
この親はその完了を待たずPR720の検証とmergeを進める。Windows runner2994の残りは元2957が集約する。

2998の原応答を受領。実jobのthread/cwdとsession_meta/turn_contextでSonnet5.5を照合した。
原応答SHA256 `33f72f7ecec40af3860651a3e4b39cdc31cdcb066baac162c26188f46499afc5`。
Must1は両clientが読むfrontend/cloud-assets.jsonとfrontend/public/cloud-crypto/*の判定漏れ。
この2系統を先に両client対象とし実Git/Bash検査へ追加、17ケース全成功。
mainの連続pushで古い検査が取消されても漏れないよう、pushは両clientを常に対象とする。
PRはbaseからの全差分で選択する。全jobsのgate needs網羅も機械検査する。
YAMLコメント・元のキー順・actionの版注記を保持し、生成前後の構造一致を検査する。
DB commandは複数のnode --testをすべて直列化し、将来の$式は再評価による誤動作を避けて拒否する。
実Bash/jqで集約6ケース、DB command4ケースとYAML往復検査が成功、vet/actionlint成功。

2996のCargo修復を最終受領。Cargo2blobと原SHA256を照合して原バイトを保持した。
子の監査/関連検査/別モデル3001と3006の結果は原handoffへ保持し、正式製品CIとは区別する。
必須のJS版整合はpnpm10.11.0で通常lock解決し、両clientのAPI2.12.4/CLI2.12.3、
desktop updater2.14.0とした。Rust tauri2.12.4/updater2.14.0に整合。
API/CLIのminor範囲を2.12へ絞り、pluginは同版へ固定して将来の片側だけの更新を防ぐ。
古いquick-xml監査免除2件を除きignore空とした。security-audit.ymlは変更しない。
固定git updaterは、2.14.0より新しい上流stableがzip>=9を採用して正式公開され、
同等の監査・関連検査が通った場合だけregistryへ戻す条件をPRへ残す。

今回の最終候補は二workflowに加えて、CI失敗を修復するCargo2、JS manifest/lock4、
監査免除の設定1の合計9ファイル。製品機能・Prd・予算・公開・署名の変更はない。
正式CI/差分の追加レビュー/merge/成功後の実行量確認はこれから行う。
今回の新規所有scratchは`ef08eed1b9063dc5b9cb0a8b97c226eb`、workspace内でguard falseを確認。
旧保護rootには触れない。新しいGit fixtureは通常の.gitと混同しない別名metadataを使う。

## 確認した範囲

2026-10-19、正規GitHub APIで AutoDevJapan/PassShare（ID 1131498455）が public、
main が `c67bfb864acd51af0aafe972b2ea09e53d187ed7` と確認した。
mainのbranch protectionは404「Branch not protected」、rulesetsは空。
未確認のPC上の製品treeは使わず、この固定SHAのworkflowと必要な設定をAPIで取得した。
元の凍結監査資料は書き換えていない。Prd・予算・公開・ライセンス・署名設定は変更していない。

## 分担

- 親2986（このブランチ）：PassShareのquality-gate.yml / mobile-build.yml。
- 子2994（Cargo割当を要求）：desktop-build.yml / 専用Windows正式runner。
  起動仕様は [Windows工程](todo2986-windows-ci-spec.md)。
- 子へdesktop workflowの書込を渡した。親は同じworkflowを編集しない。
- 元2957と公開2982にはwaitを戻さない。親は独立した通常CIの変更を先に進める。

## 元の重複と異なる条件

| 項目 | 元の実行箇所 | 候補の実行箇所 |
| --- | --- | --- |
| Desktop smoke / JS同期試験 | Quality GateとDesktop Buildのubuntu-latest、Rust stable、Node22 | Quality Gateに一本化する準備。Desktopの削除は2994が担当 |
| Desktop native_lock::tests / tsc | Desktop Buildだけ | Quality GateのDesktop smokeへ追加 |
| Go配布候補検証器 | Desktop Buildだけ、TEST_SCRATCH=runner.temp | Quality Gateの先行検査へ移動する準備 |
| Mobile JS試験・画面ビルド・auth API | Quality GateとMobile Build、同じlockfile/Node/作業先 | Mobile Buildをworkflow_call専用にしQuality Gateから1回だけ呼ぶ |
| Mobile Rust | Quality Gateは全crateテストとClippy、Mobile Buildはsmoke_のみ | 広い全crateテストとClippyを保持 |
| Mobile React受入 | Quality Gateだけ、core exampleとapps/mobile実ファイル | 同じコマンド・条件を再利用workflow内に保持 |
| Linux本番・SDK・拡張・Web等 | Quality Gateの独立ジョブ | 実コマンド・env・依存・設定の構造一致を検査。先行検査の依存だけ追加 |
| DB統合 | Quality GateのDocker E2E | 初回候補では不変。正式実行で同時migrationのdeadlockを確認し、元Composeの全command/試験一覧を読み取ってNode試験だけ直列化する修正を保留候補へ追加 |
| Cargo/pnpm監査 | security-audit.yml、PR/main/毎日 | 変更なし。重大audit失敗を免除しない |

Quality Gateの入口・PR/mainイベント・concurrency・permissionsを保持する。
新しい変更判定はPRのbaseと実merge checkout、push全体のbeforeとafterを比較する。
削除・移動元/先・複数commitを扱い、未知の比較元は成功skipにせず失敗する。
frontend/extensionのみの変更以外は共有/資料/lock/toolchain/workflow/未知の依存を保守的に含める。
集約は失敗を伝播し、影響なしと実判定したnative clientだけskipを許可する。
元の集約から漏れていたGo SDKの両バージョンも必須の依存に含める。

## 検査と保存

`tools/passshare-ci` はGo製の候補生成とYAML構造比較。beforeは上記固定SHAのAPI bytes。
対象外の全ジョブはneeds追加以外のrun/env/steps/設定が同じことを構造で照合した。
変更判定の実Bash stepを実Git履歴15ケースで実行した。各ケースで通常範囲、初回push、
存在しない比較元も検査し、全成功。移動元が検査対象から落ちる初回失敗を
`--no-renames`で修復した。Go vetとactionlint（同じ候補の再利用workflow解決込み）成功。
変更判定後の集約厳格化について関連構造検査を再確認した。

元の実行量は `.ci-work/baseline-runs.private.json` と追加のbaseline-*資料に保存。
2026-10-12のmainには13 workflow実行があり、Quality Gateは14 jobs/1390 runner秒、
独立Mobileは1 job/202 runner秒だった。timing APIのbillable.total_msは0であり、
これを過去請求の確定値や返金と解釈しない。
10月13日以降の定期監査の実annotationは「Actions budget is preventing further use」だった。
公開・承認済み予算操作後の新PRでは実hosted jobsが起動している。予算を再操作していない。

## 公開した候補と新たな実失敗

PassShare PR [720](https://github.com/AutoDevJapan/PassShare/pull/720)、固定候補
`3dec5ab5d02cf45e77c4bbbb14a734f2508be78b`、二workflowだけをAPI compareで確認。
Quality Gate run39247094873 / preflight job118041539000は37秒で正式成功。
両clientの条件付きjobsが起動し、独立Mobile Build runは生成されず全12 workflowsとなった。
13→12という実行数の差は観測したが、main pushとPRイベントの差もあるため
製品全体の削減率や毎月の費用効果は確定していない。

Security Audit run39247094667 / cargo job118041538621は12 vulnerabilitiesで実失敗。
quinn-proto>=0.11.18、rustls>=0.23.46、zip>=9.0.0が必要との原結果を保存した。
pnpm auditは6ディレクトリ全成功。最小の依存修復をCargo割当付き子2996へ配送。
親は監査を削除/免除せず、子の固定変更を同じPRへ統合する。起動仕様は
[監査修復工程](todo2986-audit-repair-spec.md)。

Docker E2E job118041805399は、connect-token-revocation/cross-clientのcreateApp初期化中に
PostgreSQL 40P01 deadlockで失敗。元の並列Node test filesが同じDBのmigrationを競合実行する。
Compose configが返す元commandからtest一覧を保持して--test-concurrency=1だけを挿入する
修正を `.ci-work/candidate-repair/` に生成した。まだ製品branchへpushしていない。
新しい試験ファイルがComposeへ追加されても同じ一覧を使い、未知のcommand形式は失敗させる。
NODE_OPTIONSではこのflagを受け付けないことも実runtimeで確認し、その方式を採用していない。
実Bash+jqによる集約6ケース、元command保持/不明形式拒否2ケース、go vet/actionlint成功。
実Compose configの元7 test filesを確認した。DB再実行の成功は未確認。

別モデルレビューは子2998（Claude既存OAuth）。元2995のgpt-5.5指定はschedulerの
resource_waitとなって起動していないため、レビュー未実施を明記して同じ固定対象を移管した。
旧登録の終了はレビュー成功ではない。依存修復と2998の実レビューを受領してから保留修正をまとめて公開し、
無目的な初回候補のrerunはしない。Windows子2994は独立して供給実証を進める。
親はまず2998/2996だけを待ち、PR720の通常CI/mergeを先に終えてから2994の統合へ戻る。
2994は親2986をwaitしない。親PRのマージと2994の供給の相互待ちを作らない。

全CI成功、マージ、main確認、最終runner分数/請求増分は未完。
正式Quality GateのDesktop smokeは167秒、再利用Mobile smokeは213秒で成功した。
DB統合失敗を集約Quality gateが実際に失敗へ伝播したことも確認した。
同じrunの失敗を無目的に再実行していない。残る独立workflowの終端は再開時に確認する。
設定とfixtureを実CI成功や費用削減実績として数えない。

## 再開手順

1. この記録・自分のToDo note・子2994の配送を読み、APIで現在のmain/PRを再照合する。
2. 公開済み`.ci-work/candidate/`と保留中`.ci-work/candidate-repair/`を区別する。
3. 子2998のレビューと2996の固定依存修復を受領し、保留修正とまとめてPR720を更新する。
4. 実害ある指摘/実CI失敗を修復し同HEADの結果を確認して通常マージする。新しいrun/jobの失敗も取りこぼさない。
5. 2994の正式Windows job/hash/回収を受領し、全体の重複排除を統合して実測する。
6. 元2957/自分のnoteへ結果と未達を保存する。通常完了通知は送らない。

私的API payload・ログ・実行ID等は.gitignore対象の`.ci-work`に保持する。
作業用所有rootはsafe-delete ID `7df7fa6570a2f553852784166091480a`。
planと続くquarantine --reason policyは両方PROTECTED_PATHを返した。
safe-deleteの現実装が配下の`.git`を保護するため、実Git履歴fixtureが回収を阻む。
後続scrub391の当該root分類もunverified/client_resource=trueなので、別経路で移動/削除/読取をしていない。
所有IDと作成根拠を保持し、保護を迂回して片付けたとは扱わない。この拒否だけを本人判断待ちにしない。
GPU・Civ6・重Cargo・新費用・新APIキー・ブラウザ操作はこの親工程では実行していない。

## 2026-10-19 23時台 再開・PR720 main反映

PR720は最新固定HEAD `d96fbabe6559e6b55eb8d5be2e10889f03a9a472` で必須CI全件成功後、
merge commit `27f840b3abfd23a618a0c0551ac177543ef97d63` としてmainへ統合した。
Quality Gate、Docker E2E、Security Audit、Cloud WASM、Desktop/CLI Windows build、
Native WebView、Encrypted storage、CodeQL等を同HEADで確認した。Native WebViewは22m45s、
Encrypted storageは22m36sで成功。初回の監査・DB deadlock・WASM出自・schema drift失敗を
隠さず修復し、再実行は追加修正後の必要な最新候補に限った。

別モデルのレビューToDo2998/3012/3015/3017はすべて完了し、固定差分/補足でMustなし。
PR721もGitHub表示上merged、head `e19dbf27ff1a27df344dba52bcde436552464b79`。
PR720の統合差分にdesktop-build.ymlとop-cli-build.ymlが含まれることをAPIで確認。
main tree SHAはPR720 headと同一。2994の正式Windows runner実証・供給・hash/recoveryと
プロジェクト全体のWindows受入は別工程の未達として残す。

mainへのmergeで13個のpush workflowが起動。最初の照合では4 success、8 in_progress、1 queued。
GitHubの組織Actions billing usage API（2026-10-19日次、取得時点）でPassShareはLinux 685分、
macOS 23分、Windows 121分、storage 10.148 GB-hour。各SKU netAmountは0 USD。
請求APIは日次集計のため、merge後runのみの厳密な区分と確定請求ではない。
既存の事前観測もnet 0 USD。公開repo標準runnerの無料対象と整合し、費用減額・返金とは扱わない。
実際のrun/job分数の最終集計は13 push workflowsの終端後に再取得する。

ブランチは `scheduler/todo-2986-passshare-ci` のまま、worktreeはclean。
残作業はmain push workflowの終端確認、PassShare run/job実行数・分数と課金APIの再照合、
元2957への結果保存、2994/他製品の待機条件を維持したToDo状態反映。
