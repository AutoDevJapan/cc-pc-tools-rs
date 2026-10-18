# ToDo2848 再開メモ

親ToDo2757から切り出した、ビルダー互換性修復の独立工程。完了済み。親のsource・branch・原資料は
変更していない。製品preflightの再実行は親2757が配布済みの新exeで行う。

## 完了したこと

- 原因を再現した(Debian slim digest固定、旧設定: chown /etc/gshadow operation not permitted)
- 方式: 外側の権限を変えず、QEMU TCGのゲスト内でKanikoを実行する(release_build_vm.go)
- 単体検査(Fake daemonとstub provider)と実Docker fixture(全9ケース)が成功した
- 別モデルレビュー(gpt-6.1-sol)3回。最終は head cdfb9b9 で Must0
- PR #226 を通常mergeした(merge 7e68b65f32504512930af02ae0f19fbfe16fb42e)
- mergeしたsourceからWindows/Linuxをビルドし、D:/CC/Tools/product-ci へ原子的置換で正式配布した。
  旧exeは product-ci-pr226.atomic-backup.exe に保持し、install receiptを読み戻した
- 親2757へ、head/tree/PR/merge/レビュー原応答hash/CI/fixture結果/Windows・Linux hash/
  receipt一致/回収をnoteで返した
- 詳細・実値: [todo2848-vm-builder.md](todo2848-vm-builder.md)

## 残り(別の工程)

- 製品sourceのbuild成功は未確認。製品preflightの再実行は親2757の担当
- 実製品buildの所要時間はTCGのため未測定(上限は従来どおり45分)
- 別モデルレビューのSuggestion2件(後続の小さな改善。固定バイト照合の突破にはならない)
  1. scanGuestLayerのLimitReaderが上限に達したとき、通常のEOFと区別して明示エラーにする
  2. 同じ層で固定ファイルの後ろに .wh..wh..opq があるとき、同層のファイルまで削除扱いになる。
     下層の隠蔽と同層の追加を分ける

## 資源

- 一時: safe-delete所有ディレクトリ4件(spike用、fixture用、build用、stage用)は回収済み。
- Dockerに残すものは無い(検査で使ったimageは削除済み)。
