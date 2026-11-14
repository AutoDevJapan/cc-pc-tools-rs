# Unslopが使うRust toolchainの確認

Unslopの正式ビルドには、固定したRust 1.95.0のWindows向けtoolchainを使います。
公式の配布情報と、今保持している実行ファイルの出所を確認してから、ビルドへ渡します。

## 現在の状態

公式の配布manifestと公式チェックサムは取得し、一致を確認しました。
配布物の署名は、まだ取得・検証していません。
一方、指定パスの祖先がジャンクションであることをメタデータで確認し、一般guardは `physical_identity_mismatch` を返しています。
今回のguardが実際に解決したパスは未取得なので、その判定の内訳までは断定していません。
公開された配布ハッシュだけでは、手元の実行ファイルが同じものだと確認できません。

今回のconsumerは、この確認を行う正常なproviderとの接続点です。
固定した前候補のconsumerは、通常のhosted CIでコンパイルと合成テストを確認しました。
今回追加した保持範囲とbindingの接続テストは、ローカル工具の資格が未成立のため、まだ実行していません。
providerが未接続なら、固定された取得不能の理由を返し、toolchainを開いたり、コンパイラを起動したりしません。

## 入力と使える範囲

対象はUnslopの固定ソースとビルドコマンドに結び付いたRust 1.95.0・x86_64-pc-windows-msvcです。
CLI引数、環境変数、呼出し側が書いたJSONから、任意のパスやハッシュを許可する入口は作りません。

正常なproviderは、公式manifestの版・対象・componentごとの配布URLとハッシュを確認します。
インストールaliasの祖先と解決先、現在のファイルの完全なidentity、ACL、保持中の内容も、その出所と同じ用途に結びます。
公開manifestの取得と署名の検証、現在のファイルの照合、実行の成功は、それぞれ別の事実として残します。

Unslop側は固定用途の一致と保持中の再確認が成立した場合だけ、同じバッファを後続処理へ渡します。
`Consumed` は消費callbackを呼んだ事実を示し、その後に再確認や後片付けが失敗しても、その事実は消しません。
成功を示す `Verified` は、providerがすべての処理を正常に返した後にだけ設定します。
内容を別パスから開き直さず、一般のデータ読取資格やインストール許可も返しません。

## 受入条件

- providerがなければ取得不能を返し、本文の取得もビルドも始めない。
- 別製品、別ソース、別コマンド、別の版や対象のtoolchainを受け入れない。
- 保持中のidentityや内容が変わった場合、途中の取消、providerの失敗、後片付けの失敗を成功にしない。
- 正例では検証した同じ保持バッファが渡され、戻り値や診断には本文や生のエラーを含めない。
- 合成テストの成功と、正常なproviderによる実toolchainの確認・実ビルド・配布の成功を分ける。

## 後続の実受入

実toolchainの資格が成立した後にだけ、vendor受入を1回実行します。
テスト外側は3分、アーカイブ処理の本体は2分、テスト全体の実行上限は210秒とし、次の失敗が出たら固定診断と実測を残します。
同じ成果物でのCLI・MCP受入、元インストール先への反映、元2例の独立評価は、その後に別々に確認します。

元の配布・評価の契約は[Unslopの専用処理](unslop-software-purpose.md)、生成と履歴の工程は[native生成と履歴取得](unslop-native-capture.md)を参照してください。

公開配布の形式は、[Rustupのtoolchainの説明](https://rust-lang.github.io/rustup/concepts/toolchains.html)と[公式の配布manifest](https://static.rust-lang.org/dist/channel-rust-1.95.0.toml)を参照します。

## 保持範囲をcaptureへ結ぶ

`captureUnslopReleaseWithToolchain` は、正常なproviderの `WithHeld` の中で `captureUnslopRelease` 全体を呼びます。
正常終了またはworkerの回収を確認できた分岐では、Before・Build・Afterから、成果物の保持、ACK、入力の後片付け、receiptのSeal・Commitまでをcallback内で行います。
準備関数だけを `WithHeld` で囲み、保持を閉じてからsessionを返す使い方はしません。

一方、workerの回収を確認できず `live_worker_collection_unknown_inputs_preserved` が返る分岐では、captureの失敗返却後に同期callbackも終わるため、現wrapperはtoolchainの保持継続を保証しません。
通常入口へ接続する前に、workerの実回収を確認するか、回収まで保持を継続する正規のownerへ引き継ぐ必要があります。
この条件が未成立の間はnormalfactoryとnative・workerの配線を有効にしません。

Commit後にproviderの再確認やCloseが失敗した場合は、公開済みの `ReceiptPublished` を残して `Qualified=false` とし、返却するreceiptを外します。
公開済みのファイルを削除したり、未公開に書き戻したり、同じreceiptを自動で再Commitしたりしません。
final receiptはproviderの後処理より先に公開され得るため、その存在や内容だけで処理全体の成功を推定できません。
将来のinstall consumerには、providerの後処理と実際の返却結果を永続的に結び、失敗・未確認の実行を保留にする接続が必要です。
現在の合成テストは返却値と公開済み状態の区別を確認するもので、再起動後のreceipt判定や実インストールを実証するものではありません。

worker用の `runUnslopWorkerWithToolchain` は、worker自身の正常なproviderが保持した内容からbindingを作り、親からの期待値と一致したときだけworker処理を呼びます。
期待digest、PID、親から来たJSONだけでは、providerを構築したり入力の読取資格を与えたりしません。
このcallback内に入力保持、Cargo、Ready、ACK受領、Final送信を置き、providerのClose後に終了コードを決めます。

## bindingが示す範囲

`unslopRustToolchainBinding` は、固定用途、公式Rust origin、installaliasと解決先のfull identity・ACL、保持中cargo・rustcのSHAとサイズを、固定順の構造から計算する比較値です。
既存の4実行物ハッシュをまとめる `ToolchainSHA` の意味は変えません。
この新digestはRustの2実行物の範囲であり、gate・sccache、標準ライブラリ、DLL、リンカー全体の実行入力を証明するものではありません。

normalfactoryとnative・workerの実routingは、この変更だけでは有効にしません。
次の統合では、元manifestのBefore/After、workerの独立保持、最終receiptに同じbindingを結び、入力資格とACL方針の受入を正常なprovider側で済ませる必要があります。
古いreceiptに新しいbindingを後から補いません。
