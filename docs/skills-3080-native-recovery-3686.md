# 元3686：発行済みprepareの限定復旧

元3080の作成者が実行した、2026-10-26のprepareを再実行せずに再開する。
対象は公開manifest `3d27a7072716df74d1edab8eb3187afba9c8a7f6a315260e4ed8fc54e04d9a91`
の53ファイルと、元の52ファイルの作成証明だけである。
元16資料の全体完了、未知の既存Skill、一般guard、原保護台帳には広げない。

元の呼び出しはパス区切りを二重化していた。prepareは成功していたが、後続の
厳密なnative照合には一致しなかった。元の入力と応答は編集せずに保持する。
この1件だけ、固定した作成者・request・call ID・原入力・原応答のSHA256と
呼び出し/応答時刻を、正規jobの時間窓内にある原nativeから直接照合する。
コピー、callerが書いたreceipt/ID/本文、別job、一般的なpath aliasは採用しない。
正規入力のSHA256と元の不一致は、`native_recovery` に両方記録する。

既存の `qualify-skills3080-revision`、`apply-skills3080-revision`、
`consume-skills3080-revision` を使う。入力の `revision.prepared` は、元の
作成者、元prepareのrequest path/SHA256/call IDを示す従来の参照のままとする。
新しいprepare、CreateNew、手書きの復旧receiptは不要であり、受け入れない。

原作成と全文レビュー3931、固定公開Git、受入済みproducerの通常の検証を維持する。
復旧時は元の51ファイルをIDと同じ読み取りbufferのhashで検証し、処理中保持する。
旧メタデータ・pending・history directoryも原native発行のID/bytesへ照合する。
prepare直後、旧版移動後、新版公開後の状態だけを認める。applyは対象を再度開いて
照合し、既存のno-replace renameで旧版をhistoryへ、新版を元の位置へ移す。
中断後の再試行は既に済んだrenameを繰り返さない。異なるID/bytes、hardlink、
reparse、先行作成、保護や本人停止は従来どおり拒否する。

今後のprepareは、実際に進行中のnative呼び出しが正規の6項目に一致することを
書き込み前に確認する。変数・追加構文・別path・短縮名・ADS・実行済み呼び出し・
`null` や欠けた項目は拒否する。通常呼び出しに二重化パスの一般許可を追加しない。
native応答発行前のクラッシュで発行証明がない場合は、自動採用せず原版を保持する。

既に配布した `3a0f5065…/a067ff63…` のproducerは独立した配布記録と
元creatorの実native応答で確認し、旧issuerとして保持する。新規3件20ファイルを
再作成しない。別callerの通常consumeも同じ元作成証明を利用する。

テストは、固定した実例の投影、改変/別actor/別時刻/別応答の拒否、書き込み前の
native照合、51ファイルと旧新版のID/bytes、NTFS上の中断/競合/再試行、および
既存Skillsの回帰を対象とする。testdata内の投影はテスト入力であり、実運用の
receiptの出所として読まない。正式別モデルレビュー・正確headのCI・通常merge・
Windows更新・親自身の53ファイル受入は、それぞれ実結果で確認する。

過去からの物理的連続性や現在のOS writerは証明しない。647件の取得保留、428件の
未取得、匿名の旧対象の保留、元の本人回答、PR538と全73220出現の残作業を保持する。
