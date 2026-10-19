# PassShare 監査修復（ToDo2996、親2986）

## 対象と固定候補

GitHub APIで AutoDevJapan/PassShare、ID 1131498455、publicを確認した。
製品baseは `c67bfb864acd51af0aafe972b2ea09e53d187ed7`、親PR720候補は
`3dec5ab5d02cf45e77c4bbbb14a734f2508be78b`。この子はworkflowを変更せず、
製品PR・通常CI・親branchへのpushも行わない。正式CIと統合・mergeは親の担当。

取得した製品ファイルは固定baseのCargo manifestとlockfile。依存解決用に各crateへ
空のRust targetを置いている。これは製品ソースや製品テストではなく、製品の成功とは
数えない。製品側updaterの読む側はAPIで確認し、desktop lib.rs:951の
`Builder::new().build()`の登録だけで、zipの型を直接扱う箇所はない。

変更は次の2ファイルだけ。Git blobはこのworktreeと親が共有するGit object storeへ
`git hash-object -w --no-filters`で保存した。製品raw sourceをPC道具の公開commitへ
含めていない。blobだけではGitHubへpushされないため、親は同じ共有Gitから取り出す。

| 製品内path | base blob | 候補blob |
| --- | --- | --- |
| Cargo.lock | 2b07f61aa749594a49dfdffc0209a455d5323857 | 3a573c3dd2bbf2bb3be2956874d101480aec36f4 |
| apps/desktop/src-tauri/Cargo.toml | 88cc777ceb5643f42a5058038d3a463fdebbd713 | 9a882fc6c33f92c5f1a4d776227491b96f5e86e7 |

## 修復方法と一次資料

実CIのzipは6.0.0ではなく4.6.1。quinn-proto 0.11.18、rustls 0.23.46へ
Cargoの通常updateで更新した。zipはtauri-plugin-updater 2.10.1のWindows依存^4から入る。
公開済み最新stable 2.14.0も^4のままであり、zipだけのメジャー更新は解決できない。

上流PR3698は2026-10-16にzip ^9へ変更してmergeされた。desktopの依存をその固定merge
`ba98bfadc4c868c9a7e1a226c83af528e1f11b89`へ変更し、Cargoがzip 9.0.5を解決した。
この上流はtauri ^2.12を要求するため、tauri 2.12.4と関連ライブラリも更新される。
MSRVはupdater/tauriが1.90、zipが1.88。検査PCはRust/Cargo 1.95.0で、
親のWindows工程の固定SDKも1.95.0。共有coreのrust-version 1.86は変更していない。

監査DBの同じ固定版で、元lockにquick-xml 0.39.4の重大2件も実検出した。
その読む側plist 1.9.0を1.10.1へ更新し、quick-xml 0.42.0へ解決した。
これは監査成功に必要な追加修復で、元の12件へ混同せず14→0と記録する。
unmaintained/unsoundなどの情報warningは重大脆弱性エラーと区別する。

- https://github.com/tauri-apps/plugins-workspace/pull/3698
- https://github.com/tauri-apps/plugins-workspace/blob/ba98bfadc4c868c9a7e1a226c83af528e1f11b89/plugins/updater/Cargo.toml
- https://crates.io/api/v1/crates/tauri-plugin-updater/2.14.0/dependencies
- https://crates.io/api/v1/crates/plist/1.10.1/dependencies
- https://rustsec.org/advisories/RUSTSEC-2026-0369.html
- https://rustsec.org/advisories/RUSTSEC-2026-0194.html
- https://rustsec.org/advisories/RUSTSEC-2026-0195.html

上流mergeのchecksはSecurity auditがsuccess、一部integrationとmatrixはfailureであり、
上流CI全成功とは主張しない。このPCでupdaterのWindows依存コンパイルは成功した。

## 実測済みと残条件

- cargo-audit 0.22.2、DB 999ec89d5d3f10f20be14750ec8c2d8b44e0ac9b（1338 advisories）。
- 元rootはexit1/14件。候補rootはexit0/0件。ignoreは空。
- backend/nativeの独立lockは変更せずexit0/0件を確認。
- 候補はcargo metadata --locked成功。逆依存をcargo tree --locked --target allで確認。
- `cargo check --locked -p tauri-plugin-updater`はWindowsで48.90秒、exit0。
- 独立した依存API受入はnextest 5/5成功、0 skip。ZIPの通常展開、親ディレクトリへの
  脱出拒否、plist XML/binary往復、製品と同じupdater登録API、reqwest両版の通常の
  証明書検証を使ったHTTPSを確認した。対象の主要依存versionは製品候補と一致する。
- quinn-protoとreqwest両版のWindows checkは独立受入manifestで7.02秒、exit0。
- 別モデルレビュー3001を受領。固定2候補blobを照合しMustなし、Suggestion3件とNit。
  原応答4973 bytesのSHA256はd7456ce0fce1859b2f6165c64e551701af51c68ed8697adc5fd7d23bac1f4e1a。
  job20261019-212942-todo-3001はexit0/18steps。threadとcwdが一致する実turn_contextで
  anthropic/claude-sonnet-5-5を照合した。原応答はreview3001-original.private.md、
  限定モデル証拠はreview3001-turn-context.private.jsonに保持。
- レビュー担当が未読と明記したaudit JSONなどへの補足確認と、親への最終配送が残る。
- 製品本体の全テスト・正式CI・mergeはこの工程では未実施。

依存API受入のコードは`tools/passshare-audit-2996/`へ保存した。受入用lockは製品lockと
別であり、製品の全依存構成の動作証明ではない。製品lockでのupdater checkと区別する。
doctestコマンドは成功したが、この補助ライブラリのdoctestは0件なので検証件数へ加えない。
進捗75%は、固定差分・重大監査成功・関連検査の3区切りが済み、レビュー受領の補足と
配送の1区切りが残ることを示す。製品CIの達成率ではない。

## レビューの指摘への対応と統合条件

3001は固定差分を確認したが、audit JSONの実内容、mobile等の個別Cargo manifest、
受入metadata/lockは未読だった。既確認の差分・コード・検査を繰り返さず、この未読だけを
補足確認へ渡す。base2blobは当初保存していなかったため、証拠の原バイトから
git hash-object -w --no-filtersで保存し、元のGitHub blob IDとの一致を確認した。

Suggestionの版整合は実害を防ぐ必要がある。固定baseのdesktop/mobileのpnpm lockは
@tauri-apps/api 2.11.1、cli 2.11.4。desktopのplugin-updaterは2.10.1である。
候補Rustはtauri2.12.4/updater2.14.0なので、公式の同minor/同version条件を満たさない。
親は正式製品CI前に、両clientのAPIを2.12系列、desktop updaterを2.14.0へ通常解決して
整合させ、CLIの対応も確認する必要がある。npm公式registryでAPI2.12.0とupdater2.14.0の
公開を確認し、後者がAPI^2.12.0を要求することも確認した。この子の書込所有はCargo2ファイル
だけなので、JS manifest/lockは変更せず、具体的な必須統合条件として親へ配送する。

一次資料：https://v2.tauri.app/develop/updating-dependencies/#sync-npm-packages-and-cargo-crates-versions
固定baseのpackage.json/pnpm lockとsecurity-audit.ymlは、GitHub APIで取得した原バイトを
.ci-work/compatibility/へ保持している。既存の未知PC製品treeは使っていない。

監査範囲について、固定baseのsecurity-audit.ymlはrootとbackend/nativeの2lockだけを
要求することを実確認した。ほかの3lockはこの正式監査jobの範囲外で、監査成功を主張しない。
また、既存.cargo/audit.tomlはquick-xmlの2件を既にignoreしていたことを確認した。
これが元CI12件と、設定を読まない明示lock監査14件との差である。候補はこの2件も解消し、
ignore空の厳しい条件で0件になっている。子は免除を追加していない。既存ignoreの古い説明を
除去する必要は親へ配送し、監査設定を書き換えるためにこの子の範囲を広げない。

git依存は、上流stable crateがzip^9以上を採用して公開され、今回の監査と関連検査に成功した
場合だけregistry依存へ戻す。今の候補revを日付やlatest branchだけで動かさない。
親はこの条件を統合時の追跡項目へ残す。新しい同時Cargo・通常CI・製品PRは起動しない。

原応答とモデルの通常保存場所はguardで未確認扱いだったため、依頼済み1件の原応答と
限定モデル情報だけを正規の自動審査に提出し、承認後に直接読取した。保護設定の変更、
別経路への迂回、無関係なログや認証情報の読取は行っていない。

初回受入はWindows loaderが0xc0000139で起動前に失敗した。PE import全375件を照合し、
comctl32.dllのTaskDialogIndirectだけが未解決と確認した。独立受入には通常のTauri
アプリでtauri-buildが組み込むCommon Controls 6のmanifestが無かったことが原因。
補助受入だけに同manifestを埋め込んで5件成功。製品ファイルは変更していない。
また、製品で無効なoptional依存のquinn-protoを直接`cargo check -p`に指定すると
Cargo1.95のfeature resolverがpanicした。受入manifestへ同じ固定版を直接宣言して
有効化し、正常なcheckを実施した。元の失敗ログも保持する。

証拠は.gitignore対象の`.ci-work/`へ保存。root-before.lock/root-after.lock、
desktop-before.toml/desktop-after.toml、audit-*-before/final.private.json、
metadata-final.private.json、update-*.log、check-updater-windows.log、handoff.private.json。
受入の最終結果はacceptance-nextest-with-quinn.log（run933ed68e）、check-network-final.log。
再現用の受入lockはacceptance-Cargo.lock、元のCargo manifestはsource-manifests/へ保存済み。
コマンド・exit・結果と各原ログのSHA256はverification-receipt.private.jsonへ保存済み。
所有付き一時rootは本worktree直下に作成した、safe-delete ID `799bea062b6fe7ba91eed988f3f50215`。
scrub391で作成済みrootと本worktreeは両方falseを確認。未知Tempは空のまま通常plan削除し
finish済み。元資料は保持し、拒否された製品sourceを別経路で取得していない。

検査終了後、safe-delete planは8723項目/3,680,582,843 bytesをsize理由でquarantine指定。
quarantine自身はCへの移動をQUARANTINE_CROSS_DRIVEで事前停止したため、Skillの手順で
所有・絶対パス・祖先と全配下のreparse不在・Git領域不在・実行中の所有プロセス0を確認し、
同じDドライブの削除希望へ通常Move-Itemで退避した。元root不在と移動先の存在を確認し、
理由.txtへ完了を追記、finishはsource_absentを記録した。容量を回収したとの主張ではない。
詳細はcleanup-plan.private.json、cleanup-result.private.json、
manual-quarantine-verification/result.private.json。別経路で削除を再実行していない。

## 再開

1. この記録と親2986のnoteを読み、上記の固定候補blobを照合する。
2. Cargoは全て終端済みで一時生成物は退避済み。新しい変更や指摘が無い限り再ビルドしない。
3. 補足レビューの未読範囲だけを受領し、実モデルと固定差分を照合する。Mustがあれば修正する。
4. 親2986の既存noteを保持して、候補blob、audit、関連検査、残条件を追記する。
5. 必要な再検査で新規rootを作った場合だけ、所有付き一時rootの手順で片付ける。
6. 支援記録だけをscheduler/todo-2996-passshare-ci-rust-2986へcommit/pushし、
   終了前にscheduler checkpoint-workspaceへ実branch/HEADを保存する。
