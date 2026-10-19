# PassShare監査修復の固定配送（2996→2986）

Cargoの2ファイルを修復し、免除なしの監査、関連検査、別モデル確認を完了した。
親は下記のJS版整合と古い免除の削除を合わせ、PR720へ一度だけ統合する。
製品本体の正式CI、マージ、配布の成功をこの子の結果へ含めない。

## 固定した製品と差分

対象は実APIで確認したpublic AutoDevJapan/PassShare、ID1131498455。
baseは`c67bfb864acd51af0aafe972b2ea09e53d187ed7`。
親候補はPR720/`3dec5ab5d02cf45e77c4bbbb14a734f2508be78b`。

| 製品内path | base blob | 修復blob |
| --- | --- | --- |
| Cargo.lock | 2b07f61aa749594a49dfdffc0209a455d5323857 | 3a573c3dd2bbf2bb3be2956874d101480aec36f4 |
| apps/desktop/src-tauri/Cargo.toml | 88cc777ceb5643f42a5058038d3a463fdebbd713 | 9a882fc6c33f92c5f1a4d776227491b96f5e86e7 |

これらのblobは親と共有するGit object storeへ保存済み。製品のraw sourceはこの補助repoの
公開commitへ含めていない。`git cat-file blob <修復blob>`を通常のバイト保持方法で取り出す。
baseの2blobも保存済みで、適用前のGitHub blobと一致を確認できる。

同じバイト列は子worktreeの`.ci-work/root-after.lock`と`desktop-after.toml`に保持。
前者137262 bytes/SHA256 `539d9ca113cb5c7a6a06186f073023cc06101a5b810ad07a8c3d51b95ade2ef8`、
後者894 bytes/SHA256 `18ba874ba40f3a6c78fb6be2f058f2a09efddef86d58f98ce792ef85280504c2`。
元2986へ製品ファイルを直接書込・pushしていない。workflow/署名/Prd/公開/予算は無変更。

## 更新内容と実測

quinn-proto0.11.15→0.11.18、rustls0.23.45→0.23.46、zipの実版4.6.1→9.0.5。
zip^9を採用した上流PR3698のmerge `ba98bfadc4c868c9a7e1a226c83af528e1f11b89`へ
updaterをrev固定した。公開済みregistry2.14.0はまだzip^4である。
上流のtauri^2.12要件に伴いtauri2.12.4等も通常Cargo解決で更新される。
さらにplist1.10.1→quick-xml0.42.0で旧2件の免除対象も修復した。

| 確認 | 結果 | 証拠（子の.ci-work） |
| --- | --- | --- |
| root cargo-audit0.22.2、ignore空 | 14→0件、exit0 | audit-root-before/final.private.json |
| backend/native独立audit | 変更なし0件、exit0 | audit-native-before.private.json |
| 製品lockでupdaterのWindows check | 48.90秒、exit0 | check-updater-windows.log |
| 独立manifestでquinn/両reqwestのcheck | 7.02秒、exit0 | check-network-final.log |
| 独立依存API nextest | 5/5成功、0skip | acceptance-nextest-with-quinn.log |

監査DBは`999ec89d5d3f10f20be14750ec8c2d8b44e0ac9b`、1338 advisories。
全原ログSHAはverification-receipt.private.jsonで照合済み。警告4件は重大エラーと区別。
受入5件はZIP通常展開/親パス拒否、plist XML/binary、updater登録、両reqwestの通常検証HTTPS。
主要依存版は製品候補と一致するが、受入と製品のtokio/hyper等は異なる。
この受入を製品graph全体や正式製品CIの証明に使わない。

## 別モデル確認

初回3001は実Sonnet5.5/18steps、補足3006は実Sonnet5.5/11steps、両方exit0/Mustなし。
それぞれ元job/thread/cwdと実turn_contextのmodelを照合済み。初回の未読範囲を補足で読了。
再ビルドや同じ差分の全面再レビューは行っていない。
原応答SHA256は順に
`d7456ce0fce1859b2f6165c64e551701af51c68ed8697adc5fd7d23bac1f4e1a`、
`b1d22558b24b355b9473d34976c26da31803c4282589e4b18eac2e6036f43676`。
原文と限定モデル証拠はreview3001/3006-*.private.*へ保持。

## 親が最終統合で行うこと

1. desktop/mobileのJS APIをRustの2.12系列と整合させ、desktopのJS updaterを2.14.0へ
   通常pnpm解決し、CLIの対応も確かめる。固定baseのlockはAPI2.11.1/CLI2.11.4、
   desktop updater2.10.1なので、Cargo2ファイルだけの単独適用では版が合わない。
   既存package.jsonの^範囲は対象版を許す。下限の引上げは再発防止になる。
2. `.cargo/audit.toml`の古いquick-xml免除2件と理由を除去する。元CIが12件だったのは
   この既存免除による差で、子の厳しい明示lock監査は14→0件。子は免除を追加していない。
3. 保留中の親の修正とまとめてPR720を更新し、最終候補の正式CIを確認して通常mergeする。
   必要なLinux/Windows工程と資源枠を保ち、成功済みの同条件検査を重ねない。
4. 上流stableの2.14.0より新しい版がzip^9以上を採用して正式公開され、監査と同等の
   関連検査が通った場合だけ、固定git依存をregistryへ戻す追跡条件を残す。

公式の版整合条件は
https://v2.tauri.app/develop/updating-dependencies/#sync-npm-packages-and-cargo-crates-versions 。
固定baseのJS manifest/lock、audit設定とworkflowは子の.ci-work/compatibility/に保持。
他3lockは正式security-audit.ymlの対象外で、監査成功を主張していない。
Rust1.90を要するclient側の検査には、親の固定SDK1.95.0を使う。

詳細な一次資料・失敗原因の修復・生成物退避・再開記録は[todo2996-audit-progress.md](todo2996-audit-progress.md)。
