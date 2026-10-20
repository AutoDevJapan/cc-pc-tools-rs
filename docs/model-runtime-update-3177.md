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
