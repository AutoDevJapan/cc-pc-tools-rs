# Console更新を既知の実行ファイル2点へ反映する

元ToDo3050 / Issue261の実装・UI・固定候補を作り直さず、ToDo3070で来歴確認と正式分類だけを修復する。元3050のaccess質問への本人回答を作らない。本人の2026-10-20直接依頼に必要な通常技術工程として扱う。

## 確定した設置先と証拠

1. `D:/CC/Tools/localai-web-rs/target/release/localai-web-rs.exe`。元3020の独立配備スクリプトと配備receiptはtodo-2903-qualified-release/.ci-workに保存。旧SHA256は`C63A5E8028E90EE720D3D5CC0350C48361CEFA8336233A466A909CB833C30B61`。
2. `D:/CC/Tools/agent-progress/agent-progress.exe`。元3050の`workforce_observation.rs`、既存Codexのlauncherとパッチが同じ設置先を使用。Git管理済み`tools/codex-progress-tui/activation/verification-3021.json`が従来のcollector SHA256 `581c813d3321c47580d21f07dd7ef7a3b654bd5d4d761cdbf7bb275cd8e7f8d1`を記録。3044の配備でもcollector変更なし。

新候補は元3050のrevision `fe715e4eb9212698e5b1b4f0d15cda1f2c5e2ed2` / tree `ab1156e44079487c9a1512fa3202cca697f57470`から作成済み。専用sourceの実HEAD `703c9f3d90b597c096a0d75f044bc82392a3adf4`を保持。候補Console SHA256は`0E40EF0FFA4BFC87C42AD682942F450F79E37299426C896F5C708720B38E8437`、collectorは`EC3D2C15E0A3E9D3F0A615B1DC40CBCF4E74C8373AEE36D5DCC8926E2C187D46`。正式guardで候補sourceを非保護と確認し、サイズとSHAを実照合した。本番の現物は正式分類前に読まない。

## 受入条件

- AC1: 正確な2leafだけ、旧版または今回候補の固定SHAと固定証拠、Git origin、物理パスが一致するときに正式分類が成功する。
- AC2: 改変、未知版、別leaf、親directory、ADS、欠落、symlink、junction、hardlink、原保護一致、証拠の改変/欠落/転送、source origin不一致を拒否する。原名簿の空生成や広いdirectory許可を追加しない。
- AC3: 関連Go回帰、vet/build、別モデルレビュー、必要CI、通常merge後に、3047の先行guard修復を含む版をCAS・backup付きで有限配備する。scheduler/jobs/CodeQLは再配備しない。
- AC4: 正式guardの2leaf成功と旧SHAを確認してから候補を通常配布する。原子置換・実hash・既存taskの再起動を区別し、元3050へreceiptを返す。
- AC5: 通常技術確認として不要となった同じ1質問だけを正規withdrawで取り下げ、元担当/sourceから再開する。本人回答を捏造せず、原成果を古いcheckpointへ戻さない。

## 元3050へ返す追加受入

2026-10-20の本人追記: 発信部のproducts/workerが0でも、全体集計には優先度40の仕事待ちと動画ジョブがある。部門未登録と便宜的projectの仕事を取得範囲・未分類として残す。未取得/部分取得/Native未観測を0と表示せず、ToDo約200件を200人としない。既存画面構成を維持する。Nativeの全PC永続APIは未接続であり、実装済みと報告しない。このUI/source変更は元3050が担当し、3070には加えない。

## 検証と到達点

受入のRed/Green、レビュー、CI、merge、正式分類、配布と稼働確認を実測後に追記する。原3020のreceiptは旧版の証拠であり現在の実物一致とはまだ主張しない。PR266のマージ前固定候補のCI成功と、マージ後の受付失敗を区別する。

新規4回帰は拒否stubで4件Red、実装後4件Green。Go全件86.859秒とvetが成功。第2固定pinと証拠なしpinの境界も追加して成功。PR273の正式repair-guard CIも成功した。PR266のマージ前固定候補は全CI成功、マージ後のWindows/collectorは検査前の`obsolete branch revision rejected`、CodeQLはbridge403で失敗しており、検査を実行した成功と混同しない。現在headのみ受付のmain競合修復は既存3047/PR271が担当する。

初回独立レビューは既存OAuthの実ジョブ`20261020-160454-todo3070-console-runtime-review`がSonnet5.5で完了し、貼付3ファイルとmain接続説明にMust0。main全体と既存helperは未読だったため、補足のsource-root一致検査と合わせて同担当へ追加確認する。依存証拠は原3020/3050の作業場所にあり、今回の有限配布とその後の現版分類で使う間は削除しない。固定受領記録の存在・バイト列と、手作業で確定した対応SHAをcompiled pinで別々に確認している。証拠JSONを権限設定として解析しない。分類は呼出時点の検証であり、配布直前にもguardとSHAのCASを再実行する。

現用guardのSHA `8A625C3171B0D68044C480AA3887B1F099C82A03CBF442FCB9E6E7B1BC99FA93` は元3058の正式配布記録で確認し、配布sourceはmain `0edc26c`。PR264の限定資格も含む。3070はこのmainを基準としており、3047/3058の修復を古いguardで上書きしない。

## 3076 UI修復版の追加pin (2026-10-20)
Console候補 E943ACF9…FA5C / 13972992B を同じ正確な1leafのpinに追加。ソースは9d2e612、localai-web-rs tree ade257e…はmain c8d71fbと同一。証拠は docs/console-ui-3076-artifacts.json（SHA 7cc2b1df…3bf1）。0E40EF0F…は回帰版のため配布不可として別に保持し、新規配布の対象にしない。一般解除なし。
