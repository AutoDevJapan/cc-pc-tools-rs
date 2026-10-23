# safe-delete 1本の更新（ToDo3455）

元2962のPR385は通常マージ `6c57c3e78a799b1e253ab15944dd5e9899cae55e` 済み。
この工程は既存の `safe-delete.exe` 1本だけを更新する。guard、queue、helper、設定、
台帳、製品jobは更新しない。元3258のguard配布と、この工程の完了を区別する。

## 仕様と受入条件

- 最新mainの独立した公開Git取得からsafe-deleteをビルドする。実source、tree、
  VCS、依存、SHAと原ビルド記録を保存し、旧候補のSHAを付け替えない。
- 旧版は元3250のPR351原配布記録を別の独立anchorにする。原記録SHA
  `aa5f0f4abcf567be59751eb8b71a6a99fa0f12e34b8d1a7289bece0dd3376086`、
  記録上の旧版SHA `c77e78ee0755df35d36e641a3505c87cd25313aa8110046f18e98da4c463332f`。
  記録上の値と現在一致の実測を区別する。原byteは変更しない。
- 非配布readerは既存のmodel runtime engineを維持し、固定データだけをoverlayする。
  callerからmanifest、hash、root、grantを受け取らない。元client境界、固定source、
  proof、NTFS identityと最終再照合を保つ。一般データへの許可を発行しない。
- 関連テストでリンク、別名、改変、未知source、任意manifest、別target、旧版不一致を
  拒否する。別モデルmaxレビューと正式CI・通常マージのsourceを照合する。
- 既存AdminBrokerと固定installerでbyte排他、NTFS identity、CAS、元owner/group/DACL、
  旧版backupを保ち1本だけ更新する。失敗時は原結果を保存し、盲目的に戻さない。
- 配置後の用途資格、SHA、source、build、ACL、backup、非破壊helpを読み返す。
  小さな自分のfixture以外の清掃を実行しない。元2962/3258へ実コマンドと結果を返す。
- guardのbyte/sourceと既存retain5、JPEG、56資産の結果が変わらないことを確認する。

## 実測した区切り

2026-10-24の開始時mainは `0e16e81798ff4463c6ae6bcf6c4c57a695d9d13c`。
通常guardはsafe-deleteを `unverified` と返し、既存の
`--verify-model-runtime-update` はexit 1、`qualified:false` だった（更新前Red）。
元3369の指定3資料と元3250の原配布記録は、現guardによる個別clear確認後に参照した。
原記録SHAは上記と一致。現在実行物のbyteはまだ読んでいない。

元3344/3369は完了、3258のguard配布段階は完了。3258/2962はそれぞれ既存依存待ちで、
safe-deleteの別writerは一覧にない。GitHub OAuthの復旧3454は既存担当が進めており、
重複起動せず、復旧後の通常APIで実ownerと正式チェックを照合する。
独立公開checkout、固定reader、レビュー、実更新、読み返しはまだ未完了。

再開時は作業場所の `.ci-work/3455-resume.private.json` と原receiptを照合する。
常設のsource、原receipt、reader、controller、backupは次回更新にも必要な証拠として保持する。
