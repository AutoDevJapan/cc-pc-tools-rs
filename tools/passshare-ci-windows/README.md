# PassShare の Windows 正式検査（ToDo2994）

このGo moduleとPassShareのdesktop-build.ymlだけが本工程の書込範囲。
親2986はQuality Gate/Mobile Build、2997はこのmoduleのcmd/runnerを担当する。
既存のrunner、共有ゲート、公開、署名、配備、ライセンスを変更しない。

## 到達点

- 2026-10-19、正規OAuthのGitHub APIでAutoDevJapan/PassShare、ID1131498455、
  public、main、self-hosted runner 0件を確認。監査基点と現mainは
  c67bfb864acd51af0aafe972b2ea09e53d187ed7、tree8ca6f83b93d11beb744d86d0b9f141ba3ef8bf74。
- 自分のsafe-delete所有root内へ明示repoを通常Gitで取得し固定checkout。
  未確認の既存製品treeを参照していない。
- 元Desktop YAMLのSHA256は94d18dfbaa807a303429b3a3b2d5e8fbffc011e65935f2cab2d00a74cc75a48f。
  original-desktop.ymlはその原バイト列。親PR720/3dec5abのQuality Gateは
  testdataの候補fixtureであり、main反映・正式成功の証明ではない。
- go test -race ./... / go vet ./... / helper build と actionlint v1.7.12が成功。
  実Gitとworkflow内の実Bashによる17ケース、移動元/初回push/未知比較元も確認。
- 登録・正式製品job・成果物hash・実回収・マージは未実施。
  未供給labelへ実際のmain workflowを変更していない。

## 入場条件

`internal/contract`はこのpublic repoのmain pushと唯一の手動候補branch
`scheduler/todo-2994-passshare-windows`だけを受ける。PRイベントは本人PCへ流さない。
source SHA/tree、現branch先端、workflowの全バイト、実run/attempt/job/ラベル、
先行するhosted admission成功と固有runner名をAPIで照合する。
hookは実SDKイベントと同じ固定Planを照合する。成功は正式jobのcompleted/successと
実stepがある場合だけ。listener終了やfixtureを正式成功としない。

## workflow候補

```
passshare-ci-windows propose <固定Desktop原本> <現mainのQuality Gate> <新規候補YAML>
```

原本の構造が監査時と違えば止める。重複smoke削除前に、現mainのQuality Gateの
Desktop smoke・native_lock::tests・tsc・Go配布候補検査器と集約への依存を構造で確認する。
適用時は別途、その現mainと同じSHAの正式成功をAPIで確認する。

通常PRにはGitHub隔離runnerの入口とresultを残す。main/手動候補は関連差分なら
同SHAのQuality Gate成功を待ち、失敗・未知比較・期限切れを成功skipにしない。
削除・移動は--no-renamesで両側を含める。例外はfrontend/extensionの独立部分だけで、
lockfile/toolchain/shared/crates/packages/documents/scripts/config/未知入力は対象となる。

Windowsは専用ephemeral runner、Linux配布は元のubuntu-22.04のコマンド・依存を保持。
Linuxの変更はcheckoutの固定/auth非保持、matrix値の解決、artifact pathの
Linux行への限定、同一job token表記への変更だけであることを構造比較する。
macOS常時配布matrixを除く。Windowsは1.95.0/Node22.16.0/pnpm10.11.0を固定し、
OS固有のdesktop全lib検査を行う。原Linux検査をWindows成功に読み替えない。

Windows候補の通常mergeで同じtreeを再ビルドしないよう、過去30件の正式成功から
同tree・専用runner実job成功・期限内artifactを照合し、取得した実バイトの長さと
SHA256がmanifestと一致した場合だけ再利用する。元のsource SHAとrunを保持する。
不明なAPIやhash不一致は失敗。期限切れ/対象無しの場合は新しい1回のビルドを選ぶ。

## 分担と残り

- 2997：固定参照28c9b9587345f082821c62af804365cf1a64bfe9のWindows有限runnerを参考に、
  cmd/runnerへ独立したservice/virtual account/state namespace、token stdin、
  Job Object drain、本人profile不読、共有SDK RX、Cargo gate/専用target/sccache、
  正常/取消/再利用fixtureと回収を実装する。既存runnerは変更しない。
- 2994：子の実装を統合し別モデルでMustを解消する。固定した公式runner配布zipを取得し、
  runner供給を実証。親改善後の現mainの実成功と構造を確認して候補を通常PRにする。
- 正式dispatchのrun/attempt/job/source/tree/artifact SHAと終了/登録不在を記録する。
  fixtureは供給実証の代わりにならない。実エラーを保存して修復する。
- 継続的なmain job供給経路も未実証。有限な正常供給を実測するまで正式切替完了としない。
- 親2986をwait先にしない。元2957/公開2982へ循環を作らない。

## 参照

[公式runner仕様](https://docs.github.com/en/actions/reference/runners/self-hosted-runners)、
[workflow構文](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax)。
runner配布は公式actions/runner v2.338.0、win-x64 zip、104063355 bytes、
SHA256 f48e0750a21812bca5f82de5f7f5aeae71abee647fab5a582f1742d07eba455fをAPIで再照合済み。
まだダウンロード・登録していない。
