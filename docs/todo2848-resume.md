# ToDo2848 再開メモ

親ToDo2757から切り出した、ビルダー互換性修復の独立工程。親のsource・branch・原資料は
変更しない。製品preflightの再実行は親2757へ返す。

## 完了したこと

- 原因を再現(Debian slim digest固定、旧設定: chown /etc/gshadow operation not permitted)
- 方式: 外側の権限を変えず、QEMU TCGのゲスト内でKanikoを実行(release_build_vm.go)
- 単体検査(Fake daemonとstub provider)と実Docker fixture(全9ケース)が成功
- 詳細・実値: [todo2848-vm-builder.md](todo2848-vm-builder.md)

## 残り(順に)

1. PR作成(作業branch scheduler/todo-2848-work → main)
2. 別モデルレビュー(共有ToDo --spec)でMust0
3. 必要なCI success → 通常merge
4. mergeしたsourceからWindows/Linuxをビルドし、D:/CC/Tools/product-ci へ正式配布
   (原子的置換、旧exeを product-ci-pr<N>.atomic-backup.exe に保持、install receipt)
5. 親2757へ、head/tree/PR/merge/レビュー原応答hash/CI/fixture結果/Windows・Linux hash/
   receipt一致/回収をnoteで返す

## 資源

- 一時: safe-delete所有ディレクトリ(spike用とfixture用)。作業の最後にplan→別呼出し削除→finish。
- Dockerに残すものは無い(今回pullしたimageは削除済み)。
