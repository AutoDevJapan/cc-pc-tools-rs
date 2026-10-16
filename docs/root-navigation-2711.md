# 管理画面の最初の読み込みを復旧する（ToDo2711）

## 再現と範囲

2026-10-16、オーナーChromeの新規タブ798086348で本人指定の
`http://localhost:10012/` を開き、本文
`{"error":"Local AI Console requires a same-origin loopback request."}` を実確認した。
実際の各request headerは取得しておらず、特定headerを原因と断定しない。
再現タブは所有記録59512bd5-0ff6-4d0c-8415-26e2fb50671cとinventoryを照合して
個別closeし、closed 1 / failed 0を確認した。本人の他タブは変更していない。

変更元は実GitHub所有者AutoDevJapanのcc-pc-tools-rs、main
`008858e2201763696e131b59c3c26682e5bb1eeb`。
書込はmain.rsのHTTP保護・関連検査と本件文書のみ。
スケジューラーと巡回の他担当の実装は変更しない。

## HTTP契約

- Hostは当該portのlocalhost又は127.0.0.1だけ。重複Host/Origin/Fetch Siteは拒否。
- Originがあれば同じHostのHTTP originとの完全一致を要求する。nullは拒否。
- 通常APIと既存CLIの条件は維持する。CORS許可headerは追加しない。
- 外部から開始する読み込みの例外はGET `/`、Originなし、Upgradeなし、
  Fetch Modeがnavigate、Fetch Destがdocumentの各単一headerがある場合だけ。
  この場合だけ既知のsame-site/cross-site値を扱う。不正なsite値は拒否。
- iframe、XHR、WebSocket、別path・methodに例外を広げない。
- rootはコンパイル済みのHTMLのみ。console_page::indexからinclude_strの素材を
  組み立て、個別記録を読む引数や処理がない。作業情報は同一origin APIから取得する。
- rootにはno-store、frame-ancestors 'none'、X-Frame-Options DENY、nosniffを付与する。

追加した関連検査は両Host・4種のsiteによる文書取得とコンパイル済みHTMLの完全一致、
各APIのGET/POST/HEAD/OPTIONS拒否、悪意Host/Origin/null、frame/XHR/Upgrade、
欠落/重複header、同一origin/CLIの既存Operations応答を扱う。
この時点では検査実行・配布・表示復旧の成功を意味しない。

## 稼働物の照合

稼働listenerはPID13884、親2984、開始2026-10-15 03:46:07 JST。
実行ファイルは本人が今回指定した同runtime
`D:/CC/Tools/localai-web-rs/target/release/localai-web-rs.exe`。
SHA256は `0f23700b54086971304162635f4a5b35e9f1da2ecdc6f04e8ae37c0f0368e2c6`。
元2621のdocs/todo-2621-build-check.mdの生成・配布用成果と一致する。
その原ソースはb9201e3a8bfd52bf90e17e2ae7a788aafcdabf48。

source側guardはclient_resource=false/classification_unverified=false、
稼働exe側はclient_resource=true/classification_unverified=true、
両方original_registry_recovered=false。後者をfalseへ偽装しない。
今回の本人直接指定、既存生成記録、実hash一致を同じ限定修復対象の根拠とし、
一般製品や未知の配置先へ拡大しない。原名簿は作成・変更しない。

既存POST /api/resident/restartは既存タスクを起動してからサーバー自身をexitする。
taskkillや親・子孫の一括停止はしない。既存タスクのactionは
resident-guard.exe --launch-service operations。
ソースのlaunchKnownServiceはmutex・listener再検査後に同じexeを窓なし起動する。
実再起動前に親関係・タスク/監視の稼働を再確認する。

## 残る完了条件

1. 資源割当済みの独立担当が固定HEADの関連検査と同server/cc-console生成を実行する。
2. 別モデルの固定差分レビューMust0と、固定HEADのCI全件成功を確認して通常mergeする。
3. 同じ稼働物だけを通常配置し、既存の再起動経路で反映する。
4. 同じownerChromeの指定URLで実画面と件数の更新を確認して自作タブを閉じる。
5. sourceOk=falseと原記録の復旧未確認を保持し、元ToDoへ実結果を記録する。
6. 2628の有限一覧は既merge/未配布という別条件。包含したsourceとCLI実取得結果を
   元2628へ配送する。原2467の完了待ちや原記録復旧成功へ読み替えない。

この担当にはcargo/GPU/Civ6の割当なし。重いCargoは直接実行せず共有ToDoへ委譲する。
