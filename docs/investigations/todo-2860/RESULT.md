# ToDo2860: 公開 pnpm action の初期化失敗を再現

公開合成fixtureで、指定された分類（Node 18.19.1 / self-installer /
ERR_INVALID_ARG_TYPE / paths[0] undefined）を再現した。
直接原因は、bootstrapの `@pnpm/exe@11.25.0` が Node 18 に存在しない
`import.meta.dirname` を `path.resolve` の第1引数へ渡すこと。
action本体をNode 24で起動するだけでは防げず、PATH上のNodeも合わせる必要がある。

これは公開合成試験の原因確定であり、原製品の原因・CI成功を確認したものではない。
親のソース・設定・ログ・公式run・VM・既存containerへアクセスしていない。
今回の合成packageManagerが原製品と同じであるとは主張しない。

## 固定対象と環境

- action: [pnpm/action-setup@ea17c68df8912ef543352723c149a84f56e3d413](https://github.com/pnpm/action-setup/tree/ea17c68df8912ef543352723c149a84f56e3d413)。
  [action.yml](https://github.com/pnpm/action-setup/blob/ea17c68df8912ef543352723c149a84f56e3d413/action.yml)
  は node24 / dist/index.js。取得したdistを変更せず実行。
- 許可された既存image:
  `sha256:89988fa8449f89b0d8f12fd2369aa5b167bfe3389189df877808a15055869011`。
- 自分だけのcontainer: `todo-2860-pnpm-20261019`。
  ID `de6b0e0f6490fda7c1a905d88af7c49b0d72f9f874aa7d4625d5846b8639e50d`。
  ラベル `cc.todo=2860` / `cc.job=20261019-034852-todo-2860`。
- 1CPU / 1GiB（swap込み上限も1GiB）/ pids96 / no-new-privileges / cap-drop ALL。
  privileged=false、mount/device/GPUなし。hostの資料をbindせず、公開ファイルのみdocker cp。
- SDK実測: `/opt/actions-runner/externals/node20/bin/node` = 20.20.2、
  `/opt/actions-runner/externals/node24/bin/node` = 24.21.0。
  imageのsystem Nodeは18.20.4であり、親から聞いた18.19.1とは異なる。
  そこで公式18.19.1 tarも取得し、公式SHASUMS256と照合した上でbin/nodeだけ展開した。
- 全試験のaction本体はSDK Node24を絶対パスで起動。各試験をtimeout150秒で制限。
  Windowsの起動は `Invoke-Hidden.ps1` のCreateNoWindowを使用。
- `env -i` から HOME=/home/ci2737、LANG=C.UTF-8、指定PATHを設定。
  追加は公開のaction入力と、合成GITHUB_WORKSPACE=/case/workだけ。
  runner登録・認証値・追加のrunner環境は不要だった。
- fixture: `/case/work/package.json` の内容は
  `{"private":true,"packageManager":"pnpm@10.11.0"}`。
  versionは未指定、dest=`~/setup-pnpm`、package_json_file=`package.json`、
  standalone=false、cache=false、run_install=null、cache_dependency_path=`pnpm-lock.yaml`。

## 有限試験の結果

1仮説（PATH上の古いNodeでbootstrap installerを実行すること）のみを、5つの対照で検査。
環境欠落やpackageManagerの別仮説は追加せず、原因が確かめられた時点で終了した。
全試験でfixture・固定action・action本体Node24・HOMEを維持。
actionは各実行時にdestを作り直す。HOME内のnpm/pnpmキャッシュは共有した。

| PATHで選ばれるNode | PATHで選ばれるnpm | action exit | 結果・証跡 |
| --- | --- | --- | --- |
| system 18.20.4 | system 9.2.0 | 1 | 同分類を再現。h1-node24-system18.json |
| 公式18.19.1 | system 9.2.0 | 1 | 指定Node版でも同分類。h1-node24-exact18.json |
| SDK 20.20.2 | SDK 10.8.2 | 0 | 初期化成功。h1-node24-path20.json |
| SDK 24.21.0 | SDK 11.19.0 | 0 | 推奨修正の対照。h1-node24-path24.json |
| SDK 20.20.2 | system 9.2.0を専用symlinkで優先 | 0 | npm版を固定しても解消。h1-node24-path20-npm9.json |

Node20/npm9の最終対照は、同じ公開npm-cli.jsを`/case/npm9bin/npm`へリンクし、
PATHを `/case/npm9bin:/opt/actions-runner/externals/node20/bin:<指定PATH>` とした。
Node/npmの解決先とversionはruntime-controls.json / npm9-control-versions.jsonに保存。
Node20の最初の成功後とNode24の成功後、およびnpm9固定の成功後に、出力された
bin_destのpnpmを実行し10.11.0を確認した（保存された実コマンドは後2つのreceiptを参照）。
最初のNode20成功後のversion確認はツール結果のみで、独立receiptは保存していない。

## 公開sourceと因果関係

[run.ts](https://github.com/pnpm/action-setup/blob/ea17c68df8912ef543352723c149a84f56e3d413/src/install-pnpm/run.ts)
の33–40行では、PATHから `node --version` をspawnし、22.13未満なら入力standalone=falseでも
standaloneを自動選択する。63行でPATH上のnpmを使い `npm ci` を起動する。
action本体のnodeディレクトリはPATHの末尾へ追加されるため、先頭のNode18が優先される。

この分岐の[公開固定lock](https://github.com/pnpm/action-setup/blob/ea17c68df8912ef543352723c149a84f56e3d413/src/install-pnpm/bootstrap/exe-lock.json)
は `@pnpm/exe@11.25.0` を指定する。
[公開tar](https://registry.npmjs.org/@pnpm/exe/-/exe-11.25.0.tgz)を取得し、
lockのsha512と一致することを確認した。
setup.jsの60行でownDirにimport.meta.dirnameを設定し、64行でpath.resolveへ渡す。
setup.js SHA256:
`22e88d59e3143c230ca946a9f9c81a78df54ab9dbfdb2cd6894224d889bc99bf`。

[Node公式資料](https://nodejs.org/download/release/v20.20.2/docs/api/esm.html#importmetadirname)
ではこのプロパティは20.11.0で追加されている。
18.19.1/18.20.4による公開installerの実失敗は、setup.js:64のpath.resolveに一致する。
HOMEやGITHUB_WORKSPACEのundefinedではなく、モジュールのdirnameがundefinedになる。
Node18.19.1/npm9からNode20/npm9への対照も成功したため、npm版更新は解消の必須条件ではない。

失敗したnpm ciの後にpackageが残らず、最初のsource取得は失敗した。
同じ場所を再読取せず、上記の公開tarから確認した。
この失敗receiptはpublic-installer-source.json、成功したsource/hash照合は
verified-public-installer.jsonに区別して保存している。

## 親担当が採用できる最小修正案

合成試験で有効だった推奨案は、actionを実行する子環境のPATHで、検査済みSDKの
Node24のbinを先頭にすること。action本体の実行ファイルもruns.usingに従いNode24とする。
このimageでは対応npm11.19.0も同じbinで解決され、追加インストールなしで成功した。
親でのSDK配置パスは未確認なので、`/opt/actions-runner`を親へ無条件に転記しない。

```text
PATH=<検査済みSDK>/externals/node24/bin:<元のallowlist PATH>
実行: <同SDK>/externals/node24/bin/node <固定action>/dist/index.js
事前確認: command -v node; node --version; command -v npm; npm --version
事後確認: 出力されたbin_dest/pnpm --version
```

Node24をargvに指定するだけ、SDK ELFの存在確認だけ、standalone=falseの指定だけでは、
この失敗を解消した証拠にならない。Node20/npm9でも本fixtureは成功するが、
node24宣言と実行環境を合わせる推奨案はNode24とする。
原製品のpackageManager変更やaction固定版の変更は、今回の根拠からは不要。

親の失敗も同じ因果経路かは、親担当が自己の許可範囲で確認する必要がある。
追加で必要なのは公開可能なnode/npm解決先・実version・PATH順と、
選ばれたbootstrap package名/versionの分類だけ。原ログ本文・原ログ位置の共有は不要。

## 保存と終了

全command/exit/stdout/stderrはこのフォルダのJSONに保存。
出力中のnpmログ位置は自分の合成containerで新しく生成されたもので、原ログではない。
公開source pin/ファイルhashはpublic-inputs.json、Node実行ファイルhashはnode-versions.jsonと
runtime-controls.json、資源制限・所有照合はpre-cleanup-ownership.jsonを参照。
独立PR作成・マージ、親code修正、公式CI実行はしない。
停止/PID0/除去と原SDK image保持の実結果はcontainer-cleanup.jsonへ保存済み。
所有tempは29,836,181 byte / 8項目。safe-delete planの通常削除を別の呼出しで実行し、
finishでsource_absentを確認した。temp-cleanup-plan.json / temp-cleanup-finish.jsonを参照。
