# 固定Rust toolchainの入力契約

## 目的と範囲

既存producerが使う `1.95.0-x86_64-pc-windows-msvc` の `cargo.exe` と
`rustc.exe` について、元のinstallalias・実際の解決先・正常な配布元・保持中の
同じbytesを結ぶための純validatorである。
一般guardのjunction判定、registry、ACL、インストール先や実行権限を変更しない。
元aliasの拒否を、canonical側を別経路で開くことで回避しない。

この変更の公開入力はCLI引数やJSONではない。
通常controllerに接続された `rustToolchainOriginProvider.WithHeld` のcallback内でのみ
入力を使い、`rustToolchainInputsMatch` は構造と同一性の整合だけを返す。
合成providerが渡した構造の成功は、実インストールの資格や出所の認証ではない。
本変更にはnative provider、manifest parser、共有dispatcher、compiler起動は含めない。

## 固定する組

| 項目 | 値 |
| --- | --- |
| version | `1.95.0` |
| host | `x86_64-pc-windows-msvc` |
| 元install root | `C:/Users/makhmeto/.rustup` |
| physical root | `D:/CC/Toolchains/rustup` |
| toolchain配下 | `toolchains/1.95.0-x86_64-pc-windows-msvc/bin` |
| 対象leaf | `cargo.exe`、`rustc.exe` |
| manifest | `https://static.rust-lang.org/dist/channel-rust-1.95.0.toml` |
| manifest SHA256 | `821ff14e4c4a1cbe1e8915f35aff0a3fbbdf8d293ad48ab8f31e3b0440c581f9` |
| 配布日 | `2026-04-16` |

componentは固定順 `cargo`、`rustc`、`rust-std` の3件とし、URLとSHA256は公式manifestの
同hostのgzip配布物に固定する。
別version、host、圧縮形式、proxy、追加componentへの暗黙の切替はしない。
`~/.cargo/bin` のproxyと固定toolchain本体は別の対象である。
公式の[toolchainの定義](https://rust-lang.github.io/rustup/concepts/toolchains.html)と
[proxyの説明](https://rust-lang.github.io/rustup/concepts/index.html)に基づく区別であり、
このPCの現在のインストールが公式配布物と同じであることの証明ではない。

公式HTTPSで取得したmanifestとpaired checksumの一致は配布元の材料として使う。
署名検証、元install transaction、現在のnative identity、ACL、保持中bytesの一致を
それだけで済ませない。
component archiveのhashと、展開した実行ファイルのhashも別である。
`ExecutableSHA` は正常providerが認証済みcomponent payload内の `bin/cargo.exe` または
`bin/rustc.exe` から求める値であり、archiveのhashや呼出者の自己申告値を入れない。
`rust-std` の `ExecutableSHA` は空である。
今回のmatcherはstd本体・DLL・linker・標準ライブラリ全体を読了したことにはしない。

## 型と信頼境界

`rust_toolchain_purpose_windows.go` のunexported型を使う。
`rustToolchainOrigin` はversion/host/manifest SHAと3componentを持つ。
`rustToolchainAlias` は元alias自身のidentity、aliasから解決したrootのidentity、
正規target rootのidentityと、alias/targetのACL SHAを持つ。
`ResolvedIdentity == TargetIdentity` が必要である。
`rustToolchainHeldFile` は元alias経由のleaf identity、同じ保持対象のidentity、
ACL SHAと `Raw` を持つ。
`ViaInstallIdentity == Identity` が必要である。
いずれのidentityもWindowsのvolume serial/file ID/creation timeを正規10進文字列で表す。
パスは上表の固定文字列へ正常providerが投影し、matcherはdot segment、ADS、別名、
UNC、余分なleafを正規化して受理しない。
二つの実行ファイルは別identityで、各保持bytesのSHA256が該当component payloadと一致する。

`rustToolchainHeld.Recheck(context.Context) error` は同じ保持tuple・alias・親chain・
file identity・ACL・bytesの再確認用である。
純matcherはこの関数を呼ばず、nilを拒否するだけである。
consumerはcallback内で前後の `Recheck` とmatcherを実行し、同じ `Raw` を検証と消費へ使う。
consumerが保持期間外にsliceを保存したり、パスから再openしたりしてはいけない。
実行を行うconsumerのsource/command/operationは、そのconsumer自身の固定契約へ別に結ぶ。

正常providerの接続には以下が必要である。

- 呼出者の任意path/hash/receiptではなく、正常software-origin controllerが固定対象を選ぶ。
- 公式manifest/componentの取得元、digest、必要な認証と現在のpayload同一性を照合する。
  不明な署名や過去のinstall transactionは不明のまま残し、架空のinstall receiptを要求・生成しない。
- bodyの読取前に元aliasとcurrent objectの目的資格を取得する。
  解除されていないguard拒否を別pathのopenで代替しない。
- alias自身とtarget rootを保持し、固定aliasだけを認識する。
  下層directory・leafの追加reparseや複数hardlink、変更可能な対象を受理しない。
- 同じheld handleからnative identity、現在のOwner/Group/DACLとbytesを取得して前後を照合する。
  ACL hashを知っているだけで安全なACLと判断せず、通常provider側で実policyを確認する。
- callbackは正常成功時ちょうど1回、同期で実行する。
  取得前後の取消、callback拒否、再確認やclose失敗はerrorとして返し、自由文は公開結果へ転送しない。

providerは認証済みmetadataの投影元であり、このstructをJSONから埋めるfactoryではない。
未接続の通常factoryを合成providerや過去hashで埋めない。
matcher自身にはfilesystem/network/process I/O、factory、JSON decoder、grant fieldがない。

## 結果と失敗

純matcherはboolのみを返す。
consumerは次の固定IDを使い、実際に消費した事実と最終成功を分ける。

| ID | 意味 |
| --- | --- |
| `toolchain_origin_provider_unavailable` | nilまたは通常provider未接続 |
| `toolchain_origin_unavailable` | 正常origin取得・再確認・終了回収が不成立 |
| `toolchain_inputs_mismatch` | 固定tupleまたは保持bytesが不一致 |
| `toolchain_cancelled` | context取消 |
| `toolchain_consumer_failed` | 同bytes consumerが失敗 |
| `toolchain_callback_invalid` | callbackが0回・2回等で契約外 |

provider未接続ではunknownで起動0とし、上記の成功を `access_allowed`、data grant、
`execution_allowed`、更新完了へ変換しない。
データの読取資格とcompiler起動の資格は同義ではない。

## 受入と未実行

純matcherの合成テストは固定tupleの正常例に加え、他version/host/component、
URL/hashの置換、alias/leaf identityの変更、空ACL、別パス、空/異なるbytes、
nil recheckと過大入力を拒否する。
matcherがcallbackや外部I/Oを実行しないこと、元sliceを書換えないことも確認する。
normal provider未接続・callback回数・recheck/close failure・固定source/command bindingは
別consumerのテスト範囲であり、純matcherの合格へ含めない。

本sliceの作成時、Go/gofmtのfresh資格が成立しなかったため、Go test、compile、gofmtと
RED/GREEN実行は未実施である。
テストsourceを用意したことをPASSと報告しない。
shared provider接続、固定candidateの独立レビュー、正式build、元aliasの実qualification、
同じ保持bytesでのcompiler起動と元製品の受入は未達の別工程として残る。
