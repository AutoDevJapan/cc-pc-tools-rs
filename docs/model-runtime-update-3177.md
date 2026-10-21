# ToDo3081の実行物更新を限定して検証する（ToDo3177）

対応Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/277 。
この独立実装は配布資格の不足だけを扱う。モデル処理の実装、正式CI、別モデルレビュー、
統合と配布は親3081が担当する。通常の保護分類、本人停止、ToDoの状態は変更しない。

## 入力と結果

`scrub391 --verify-model-runtime-update <実行物の絶対パス>` を専用入口にする。
callerからmanifest、hash、source、ToDo番号や許可ディレクトリを受け取らない。
compiled profileの完全一致leafだけを検証し、引数の混在・重複・不足は
`{"qualified":false}` とexit 1で拒否する。既存CLIのhelp/errorは維持する。

対象はtodo.exe、discord_ask.exe、必要ならscrub391.exeの3leafだけ。
jobs/schedulerは既存の通常分類を使う。review.exeは独立した旧配布証拠がないので
未実装・未配布として拒否する。親ディレクトリ、兄弟、backup、ADS、未知原本、
private dataや他製品に資格を広げない。

旧版はcompiled SHA256と独立した固定receiptで検証する。証拠rootの物理pathと
Git root/origin、元保護、leaf/parentの物理pathとリンクを先に確認する。
receiptの一致後だけ、同じ限定handleで実行物を照合する。書込・置換を共有しない
handleを最後の再照合まで保持し、リンク数、handleの実path/identity/size/write time、
元保護とGit root/originを再確認する。欠落・改変receipt、誤origin、元保護、
symlink/junction/hardlink、照合中の変更は拒否する。原実行物を変更しない。
receipt自身を実行物とした証拠、旧receiptを新候補のbuild証拠に流用する形も拒否する。

新候補はまだ生成されていない。新SHAを空のまま保持し、旧版が一致しても
`qualified:false, baseline_verified:true, reason:"candidate_unbound"` とexit 1を返す。
これは旧softwareだけの観測で、更新やデータアクセスの許可ではない。
旧版の現在一致も確認できない場合は汎用の`qualified:false`だけを返す。

新候補を結ぶ際は、親が固定source/head、候補の正確なpath/SHA256、独立build receiptの
正確なpath/SHA256をソースへ追加し、レビューと検査を受ける。同じroot/origin・保護・
物理同一性検証を候補にも使い、未知または未結合の候補では資格を発行しない。
成功結果には実際に一致したcurrent SHA、新SHAと`data_access_granted:false`を含める。
既に新候補が入っている場合も別に識別する。

## 受入と検査順序

1. 既存CLIがこの目的の汎用拒否とhelpを持たないことを、合成pathの実Redで確認する。
2. 合成実行物/receipt/Git rootで旧版の観測、新候補の結合、既配布一致をGreenにする。
3. scope外・任意manifest・欠落/改変receipt・誤origin・元保護・リンク・並行変更を拒否する。
4. repair-guardパッケージの関連Go test/vetで既存CLIと共用readerの回帰を確認する。
   この変更はGoだけなのでCargo/GPU/Civ6を使わない。

## 根拠と未完了

todoのPR232/source `89459071aabcff8cc99b4e3db50e54813c8bc70f`の保存記録と配布script、
DiscordのPR279/source `8dd9bb80a6b1ce4833e2798e0a46ab9ed1e436da`の指定runtime記録を使う。
後者にはguardの旧SHAも保存されている。現在の実行物との一致は別途readerで確認する。
更新中のlive-statusはpinにしない。保存記録の本文や別のprivate pathは公開しない。

ToDo3085の既存実装`d7655f042787925e71928d996675cce7d08d47fb`の
capacity_update_windows.go/tests/main接続を参照した。目的の分離、compiled pins、
独立receipt、候補照合を再利用し、3085の関数名・資格・main接続は上書きしない。
todoの並行配布が旧SHAを変えた場合は一致したと推測せず拒否し、親が新しい独立証拠を結ぶ。

指定2903の直下の配布記録を有限に確認したが、review.exeの旧配布証拠は発見していない。
通常入口がcc-liveのPaths.here/Cargo.toml/src/bin/review.rsにあることだけでは
実行物の来歴を証明できない。旧SHAは埋めない。

## 正規readerでの現在一致（最終確認2026-10-21 04:26 JST）

自sourceからGoで検査用readerだけを生成し、窓を出さない通常の子処理で各leafを照合した。
旧実行物を別の方法でhash/コピー/逆解析していない。本番のguard/targetは変更していない。

| leaf | 旧証拠との現在一致 | 更新資格 |
| --- | --- | --- |
| todo.exe | 指定PR232の`ba1d…6811`と一致 | 新候補未結合のためfalse、exit 1 |
| discord_ask.exe | 指定PR279の`eb14…9295`と一致 | 新候補未結合のためfalse、exit 1 |
| scrub391.exe | 正規readerが拒否。保存`78d9…5069`との現在一致は未確認 | false、exit 1 |
| cc-live review.exe | 独立した旧配布証拠なし。profileへ未登録 | false、exit 1 |

guardの拒否を「現在SHAが保存値と違う」と断定しない。旧証拠は読了・pin済みだが、
物理path・元保護・共有handle・hash等の全条件を通った現在一致が得られていない。
unknown hashを表示せず、未確認の現在SHAを新しいbaselineへ昇格させない。
この観測はその時点だけの結果であり、3085等の後続配布の成功を保証しない。
4leafとも通常の`--guard-path`は`classification_unverified:true/client_resource:true`のままで、
目的の資格が通常分類に流れ込んでいない。検査用readerでもhelpはstderr/exit 0、
未知flagはstderr/exit 2、stdoutは空という既存の契約を確認した。

## 実Red/GreenとGo検査

Red source: `81f1da3`（起点mainに仕様と受入テストだけを追加したcommit）。
`go test -p 1 -count=1 -run '^TestModelUpdate(CLIHasPurposeSpecificFailure|HelpAddsPurposeWithoutChangingHelpContract)$' .`
はexit 1、2件失敗。前者はflag未定義でCLIがexit 2/usageを返し、後者はhelpの入口欠落で失敗した。
コンパイル失敗をRedとして数えていない。

実装source: `a1ca84d35b699c5153545c7cf2ff1fb5f7ace291`。
最終の関連検査は次のコマンドでexit 0、トップレベル29件、subtest込み76件合格、
失敗0・skip 0。合成の旧版/新候補/既配布一致、各拒否、実Git root/origin/head、
書込handleの競合、全ファイルの同時pin、最終再照合と既存CLIを含む。

```text
go test -p 1 -count=1 -json -run '^(TestModelUpdate|TestConsole|TestCurrentSuccessionCLI|TestProductCIRuntime|TestRuntimeExecutableOnly|TestSchedulerRuntimeRefuses|TestUnknownResource|TestPortable|TestContainment)' .
go vet -p 1 .
go build -p 1 -o <所有temp>/scrub391-3177.exe .
```

vet/buildもexit 0。`GOMAXPROCS=2`で有限に実行し、生成した最終sourceのreaderで上表を観測した。
途中のGreen版に対してはrepair-guardパッケージ全体も`go test -p 1 -count=1 -json .`で確認し、
トップレベル110件、subtest込み319件が合格、失敗0・skip 0。その後の新readerの独立証拠条件と
追加テストを最終の関連76件で再確認した。319件を最終source全件の結果とは表現しない。
Rust workspaceや他Go製品の全体検査、Cargo buildは実行していない。

## 親3081が残す正確な操作

1. このbranchの実装を親の統合sourceへ取り込む。mainの差分はhelpの2行と
   runGuardCLI先頭の3行だけ。3085/PR282のcapacityUpdate用struct/flag/routeは残す。
   2つのpurposeが混在する要求はどちらのverifierも呼ばず拒否する。新規PRはこの担当では作らない。
2. Cargo資源を持つ正規の工程で、最終Rust sourceから新しいtodo/Discord候補を生成する。
   sourceのGit root/origin/実HEAD、targetと候補の対応、候補SHA、独立したbuild receiptを確定する。
   `model3081UpdateLeaves`の各`candidate`へroot/path/head/sha256/evidenceをcompiled値で結ぶ。
   candidateのrootはそのheadに固定された正規Git worktreeとし、後からheadが動いたら拒否する。
   callerからmanifest/hashを受け付ける入口は追加しない。
3. 旧baselineはそのまま現在一致と推測しない。3085などがtodoを先に更新した場合は、
   その特定softwareの独立した正規receiptを別pinとしてreviewして結ぶ。
   guardの現在一致を証明する独立記録がなければ、guard自体の更新は行わない。
   review.exeも独立旧記録が見つかるまで未実装・未配布として親の未完了に残す。
4. Goの`TestModelUpdate`と既存CLIの関連検査/vetを行い、親の別モデルレビューと正式CIを通す。
   sourceにある「本担当ではproduction候補が空」のテスト期待は、実候補を結ぶ際に
   その固定build receiptとSHAの受入検査へ置き換える。空hashを推測値で埋めない。
5. 必要なguard候補Aを先に生成する場合、A自身のSHAをAへ埋める循環を作らない。
   Aは候補未結合のまま固定sourceから生成し、そのhashと独立build receiptを
   別の検査専用reader Bへcompiled pinとして結ぶ。BでAの更新を検証し、配布するのはA。
   BをAと同一実行物として報告しない。guardの旧版資格が不足すればこの工程は実行しない。
6. 成功receiptのdomain=`model-runtime-update-3081-v1`、todo_id=3081、target_kind、
   `qualified:true`、実current/new SHA、source_headと`data_access_granted:false`を照合する。
   baseline_verifiedだけでは配布しない。readerの返却時にhandleは閉じるので、receiptは
   将来の置換を予約するtokenではない。配布直前に通常の停止・元保護・物理path/リンク・
   source/targetの並行変更を同じ更新工程で再照合し、有限CASで期待currentを比較する。
   資格を他ToDoの完了・解除やデータアクセスへ使わない。

本担当は別モデルレビュー、正式CI、merge、本番guard配布、通常分類変更、原target更新を
実施していない。モデルの実effortとFastの上流適用もこのGo検査では確認していない。
