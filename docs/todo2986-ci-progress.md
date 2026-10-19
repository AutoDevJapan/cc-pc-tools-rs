# PassShare の CI 削減（ToDo2986）

## 確認した範囲

2026-10-19、正規GitHub APIで AutoDevJapan/PassShare（ID 1131498455）が public、
main が `c67bfb864acd51af0aafe972b2ea09e53d187ed7` と確認した。
mainのbranch protectionは404「Branch not protected」、rulesetsは空。
未確認のPC上の製品treeは使わず、この固定SHAのworkflowと必要な設定をAPIで取得した。
元の凍結監査資料は書き換えていない。Prd・予算・公開・ライセンス・署名設定は変更していない。

## 分担

- 親2986（このブランチ）：PassShareのquality-gate.yml / mobile-build.yml。
- 子2994（Cargo割当を要求）：desktop-build.yml / 専用Windows正式runner。
  起動仕様は [Windows工程](todo2986-windows-ci-spec.md)。
- 子へdesktop workflowの書込を渡した。親は同じworkflowを編集しない。
- 元2957と公開2982にはwaitを戻さない。親は独立した通常CIの変更を先に進める。

## 元の重複と異なる条件

| 項目 | 元の実行箇所 | 候補の実行箇所 |
| --- | --- | --- |
| Desktop smoke / JS同期試験 | Quality GateとDesktop Buildのubuntu-latest、Rust stable、Node22 | Quality Gateに一本化する準備。Desktopの削除は2994が担当 |
| Desktop native_lock::tests / tsc | Desktop Buildだけ | Quality GateのDesktop smokeへ追加 |
| Go配布候補検証器 | Desktop Buildだけ、TEST_SCRATCH=runner.temp | Quality Gateの先行検査へ移動する準備 |
| Mobile JS試験・画面ビルド・auth API | Quality GateとMobile Build、同じlockfile/Node/作業先 | Mobile Buildをworkflow_call専用にしQuality Gateから1回だけ呼ぶ |
| Mobile Rust | Quality Gateは全crateテストとClippy、Mobile Buildはsmoke_のみ | 広い全crateテストとClippyを保持 |
| Mobile React受入 | Quality Gateだけ、core exampleとapps/mobile実ファイル | 同じコマンド・条件を再利用workflow内に保持 |
| Linux本番・DB統合・SDK・拡張・Web等 | Quality Gateの独立ジョブ | 実コマンド・env・依存・設定の構造一致を検査。先行検査の依存だけ追加 |
| Cargo/pnpm監査 | security-audit.yml、PR/main/毎日 | 変更なし。重大audit失敗を免除しない |

Quality Gateの入口・PR/mainイベント・concurrency・permissionsを保持する。
新しい変更判定はPRのbaseと実merge checkout、push全体のbeforeとafterを比較する。
削除・移動元/先・複数commitを扱い、未知の比較元は成功skipにせず失敗する。
frontend/extensionのみの変更以外は共有/資料/lock/toolchain/workflow/未知の依存を保守的に含める。
集約は失敗を伝播し、影響なしと実判定したnative clientだけskipを許可する。
元の集約から漏れていたGo SDKの両バージョンも必須の依存に含める。

## 検査と保存

`tools/passshare-ci` はGo製の候補生成とYAML構造比較。beforeは上記固定SHAのAPI bytes。
対象外の全ジョブはneeds追加以外のrun/env/steps/設定が同じことを構造で照合した。
変更判定の実Bash stepを実Git履歴15ケースで実行した。各ケースで通常範囲、初回push、
存在しない比較元も検査し、全成功。移動元が検査対象から落ちる初回失敗を
`--no-renames`で修復した。Go vetとactionlint（同じ候補の再利用workflow解決込み）成功。
変更判定後の集約厳格化について関連構造検査を再確認した。

元の実行量は `.ci-work/baseline-runs.private.json` に保存。
2026-10-12のmainには通常CI/独立Mobileが各1回成功していた。
10月13日以降の定期監査には数秒で終わる失敗があり、本文と実課金原因の照合は未完。
新候補の正式PR CI、別モデルレビュー、マージ、main確認、runner分数/請求増分は未完。
設定とfixtureを実CI成功や費用削減実績として数えない。

## 再開手順

1. この記録・自分のToDo note・子2994の配送を読み、APIで現在のmain/PRを再照合する。
2. `.ci-work/candidate/`の二workflowが生成結果と一致することを確認する。
3. 通常の固定候補PRを一度だけ作り、正式CIと別モデルレビューを追う。
4. 実害ある指摘/実CI失敗を修復し同HEADの結果を確認して通常マージする。
5. 2994の正式Windows job/hash/回収を受領し、全体の重複排除を統合して実測する。
6. 元2957/自分のnoteへ結果と未達を保存する。通常完了通知は送らない。

私的API payload・ログ・実行ID等は.gitignore対象の`.ci-work`に保持する。
作業用所有rootはsafe-delete ID `7df7fa6570a2f553852784166091480a`。
終了前に証拠を保存してplan→別呼出削除→finish。拒否はquarantineし迂回しない。
GPU・Civ6・重Cargo・新費用・新APIキー・ブラウザ操作はこの親工程では実行していない。
