# PassShare の CI 削減（ToDo2986）

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
