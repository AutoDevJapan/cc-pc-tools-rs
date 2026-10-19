PassShare監査修復ToDo2996の、レビュー3001が未読だった範囲だけを読取確認してください。
同じClaude Sonnet5.5の既存OAuthを使い、別の子ジョブを直接起動しないでください。
複数ファイルの照合です。Cargo/nextest/auditの再実行、製品PR/CI、書込、結果保存、
commit、ToDo更新は不要。最終返信だけを実装担当が受領します。

作業元は
D:/CC/Work/unattended-operations-20261013-wt/todo-2996-passshare-ci-rust-2986
です。guard確認の後、この範囲だけを読みます。保護対象は読みません。

元の依頼はPassShare ID1131498455/publicのPR720監査失敗を、免除を追加せずCargo manifest/
lockだけで修復し、親2986へ固定差分を返すことです。親が製品への最終統合と正式CI/mergeを行います。
製品base c67bfb864acd51af0aafe972b2ea09e53d187ed7、候補は変わっていません：
Cargo.lock blob3a573c3dd2bbf2bb3be2956874d101480aec36f4、desktop Cargo.toml blob
9a882fc6c33f92c5f1a4d776227491b96f5e86e7。
既確認の全lock差分や受入コード、原CIログ、外部上流の全面調査をやり直さないでください。

## 読む範囲

- docs/todo2996-audit-progress.mdの「レビューの指摘への対応と統合条件」節。
- .ci-work/review3001-original.private.md（既確認範囲と未読範囲を特定するためだけ）。
- .ci-work/audit-root-before.private.json、audit-root-final.private.json、
  audit-native-before.private.json、verification-receipt.private.json。
  DB/重大vulnerabilities/ignore/警告を区別し、原JSONのSHAがreceiptと一致するか確認。
- .ci-work/source-manifests/Cargo.toml、apps/mobile/src-tauri/Cargo.toml、
  crates/passshare-connect/Cargo.toml、crates/op-cli/Cargo.toml。
  依存の読む側、mobileのtauri^2、reqwest^0.12の伝播と共有coreのMSRVを確認。
- .ci-work/acceptance-metadata.private.json、acceptance-Cargo.lockの必要箇所。
  製品.ci-work/metadata-final.private.jsonの主要依存版と照合する。全graph同一とは主張しない。
- .ci-work/compatibility/にあるdesktop/mobileのpackage.jsonとpnpm-lock.yaml、
  .github/workflows/security-audit.yml、.cargo/audit.toml。

## 既に確認済みの事実と判断

3001は実Sonnet5.5/18steps/exit0で固定2差分Mustなし。原応答SHAは
d7456ce0fce1859b2f6165c64e551701af51c68ed8697adc5fd7d23bac1f4e1a。
updater Windows check48.90秒、独立依存nextest5/5、network check7.02秒もログ確認済み。
検査を繰り返さないでください。

JSの既存lockはAPI2.11.1、CLI2.11.4、desktop plugin-updater2.10.1。
候補Rustはtauri2.12.4/updater2.14.0で、公式条件と版が合いません。
親への必須統合条件として、APIを両clientで2.12系列へ、desktop updaterを2.14.0へ
通常解決し、CLIも対応確認することを配送します。子の所有はCargo2ファイルのみなので
JSを書き換えていません。JS整合未完のまま製品CI成功とは報告しません。
一次資料：https://v2.tauri.app/develop/updating-dependencies/#sync-npm-packages-and-cargo-crates-versions
必要ならこの公式一次資料とnpm公式registryだけ追加参照可能。

security-audit.ymlの監査対象はrootとbackend/nativeの2lockだけ。
既存audit.tomlにはquick-xmlの2免除があり元CI12件、明示lock/ignore空では14件です。
候補は2件もplist更新で直してignore空で0件。免除を追加していません。
古い免除説明の除去は親へ配送します。別の3lockは今回の正式監査対象外で成功を主張しません。
上流stableがzip^9採用版を公開し監査/関連検査が通った時だけ、固定git revから戻す追跡条件も
親へ配送します。単にlatestへ動かしません。

## 最終返答

対象の固定base/候補blob、読んだファイル、未読範囲と実行せず読取確認した検査を明記。
Must / Suggestion / Nitを場所と具体策つきで報告し、なければMustなしと書きます。
読取が終われば先頭「状態: 完了」でレビューだけ完了と説明。Mustがあっても読取は完了。
読み残しや実行失敗は具体的に記録し、成功と扱わない。2000字以内の日本語。
