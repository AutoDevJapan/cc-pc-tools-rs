# PassShareのWindows限定配布をmainで確認（ToDo3010）

2026-10-20 JST、Windows限定への変更は統合済みで、同じmainの正式CIと保存成果物を確認した。
この有限工程は完了。製品のWindows版全体の完成、署名配布、本人PCへの正式実行移管は未完了。

## 元の指示と保存済み候補

本人の2026-10-19指示「MacはWindowsが完成してからでOK。Linuxで使う想定のユーザーが今のところいないから必要ないんじゃね？必要になったらやればいいと思う。その前提で作業再開して。」に従う。
正本は `D:/CC/Policies/owner-windows-completion-before-other-os-20261019.md`。
変更対象は `.github/workflows/desktop-build.yml` と `.github/workflows/op-cli-build.yml`。
Linux本番のbackend検査を停止する指示ではない。

元の引き継ぎは、指定された2903作業場所の `.ci-work/windows-only-desktop-3010/checkpoint.private.md` を最初に読んだ。
出発blob `6248e465e34f677beb630dcd6b21480da9d069f8`、候補head `e19dbf27ff1a27df344dba52bcde436552464b79`、
YAML AST比較、17workflowの削除CLI成果物参照なし、実Sonnet5.5の複数ファイル文脈確認Must0を保持した。
レビュー原応答3件も同じ保存場所で確認した。候補・PR・レビューは再作成していない。

## 統合と差分の照合

- [PR721](https://github.com/AutoDevJapan/PassShare/pull/721) は上記候補headのままMERGED。
- [PR720](https://github.com/AutoDevJapan/PassShare/pull/720) の最終headは `d96fbabe6559e6b55eb8d5be2e10889f03a9a472`。
- mainは `27f840b3abfd23a618a0c0551ac177543ef97d63`。PR720 headとmainのtreeはともに `c64c7b04c2ebfb4090ad61323fc25b4a45694dc2`。
- compare APIで元3010 headがmainの祖先と確認した。PR721のmerge `8398204b1befb3d21dea461baee052b7188ea52f` は親の統合履歴に保持されている。
- CLI blobは候補と同じ `38cce00968305a22d3af0797146abde9abbd255a`。
- Desktopのmain blobは `e80d7949980c9d4ac0a775ebdcca8f05838bb56d`。元候補blob `adb6a43f3b7bd7a15d3fd8d41a462e97316fed47` と同一とは扱わない。
  親2986が関連変更の判定と条件付き実行を統合し、cloud syncの後へRust検査を移した。
  Windows限定matrix、smoke/native lock/cloud sync、各PowerShellコマンドの失敗伝播、成果物名・出力path・保存設定は維持されている。
- 親の公開記録 `docs/todo2986-ci-progress.md` で、追加レビュー3012/3015/3017の完了とMust解消を確認した。
  最終PR720のChecksはSUCCESS44、SKIPPED1、NEUTRAL3で、失敗・実行中はなかった。
  Cursorの利用枠到達通知は別モデルレビュー成功の根拠にしていない。

今回は両workflowを並行編集せず、GitHub上の統合結果を読み取った。新しい製品PRやCI再実行は追加していない。

## 同じmainの正式CIと成果物

mainのpush実行13workflowは全件success。過去のPR721のcargo-audit失敗を免除したのではなく、
親2986が2996の監査修復・JS版整合・DB E2E修復等を統合した後の結果を確認した。

| 正式実行 | 結果 |
| --- | --- |
| [Desktop Build 39268499045](https://github.com/AutoDevJapan/PassShare/actions/runs/39268499045) | Windows smokeとbuild成功。cloud sync、候補検証器、Rust smoke/native lockのstepもsuccess |
| [op-cli Build 39268499192](https://github.com/AutoDevJapan/PassShare/actions/runs/39268499192) | Windows build成功 |
| [Security Audit 39268498934](https://github.com/AutoDevJapan/PassShare/actions/runs/39268498934) | cargo-auditとpnpm監査6件すべてsuccess |
| [Quality Gate 39268499088](https://github.com/AutoDevJapan/PassShare/actions/runs/39268499088) | Docker E2E、Rust、Desktop、再利用Mobile等と集約がsuccess |

成果物APIで同じmain SHA・repo ID1131498455・実行ID・未失効を照合し、実ファイルをダウンロードした。
Desktop artifact ID12195541366（desktop-windows-latest）、CLI artifact ID12194161449（op-cli-windows）。
API報告のZIP digestはそれぞれ `42ca4de58d8e9abb2466de6c4a580e082e1ddcce766a81105384b26a2628a2a6`、
`96df9fde1fa45d0df660c82e77a22f5f6aa6c4272e7594addf7f8ede8473b03a`。
今回は展開後ファイルを検査し、ZIP自体のdigest再計算はしていない。

| 実ファイル | bytes | ダウンロード後のSHA256 |
| --- | ---: | --- |
| PassShare_0.1.0_x64_en-US.msi | 3010560 | CC818FB8521AE18BFA3B8CE90A67C366447EE1C056F09B52D931ACB19F4974CA |
| PassShare_0.1.0_x64-setup.exe | 2313599 | D08F118FC947F6D5A6C1D366A6217C52389A58619A4F950E0C14C277C0F24847 |
| op.exe | 2632192 | F0586B4BD00F600B008CD04BE6D279A7C63E5BDEF547DBF5BC02AEA66333884A |

3件ともAuthenticodeの結果はNotSigned。インストール・実行・このPCでの再ビルドはしていない。

## 別工程に残る条件

署名workflowのblob `e069fedf75ec515c500d387bb81d76e04ca733ca` は変更していない。
手動実行だけを許し、private repo条件と3OSのsigning ready条件がある。現在のrepoはpublicなので入口で拒否される。
公開artifact保存を署名候補の非公開配布先として扱えない。実行権限設定はActions enabled / allowed_actions=allだった。
秘密値を読まず、repo metadataだけで署名キー2項目の登録なし、signing ready変数3項目の登録なし、repo runner0件を確認した。
これは組織・環境側の署名能力すべてを証明する調査ではない。

署名の続行ではWindows向けの秘密の供給元・実行主体・候補配布先を確認し、公開repoに適合する工程を別途設計・検証する。
private条件を盲目的に外さず、Mac/Linuxの準備をWindows完成の依存にしない。実署名・公開は本工程で実施していない。
製品全体の署名・導入更新削除等は元1163の未完了条件として保持する。
本人PCへの正式実行移管、有限runnerの供給・隔離・終了回収・同一成果物hashの証明は既存2994/2997が継続する。
元2957の集約はこの結果を参照できるが、CIのOS選定だけをWindows製品全体の完成や実費用削減としない。

再開が必要な場合はこのmain SHAと上記実行を先に照合し、有効な同HEAD結果を重複実行しない。
この担当はスケジューラーの状態更新に任せ、通常の完了通知・メンションは送らない。
