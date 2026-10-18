# 現在の継承先を通常schedulerへ接続する契約（元2834）

PR221の分類だけではAO2839は受付へ進まなかった。`cc-scheduler/src/engine.rs` のdispatchは原名簿が存在しない場合、既存のPC修復3projectと元2407以外を拒否する。通常stepのreconcileは原名簿不存在でFileNotFoundErrorとなる。原名簿を空で作らず、既存の有限なrepair-stepを保ち、新しい現在の継承先だけを通常のdispatch/prepare/資源/所有権審査へ渡す。

## 分担

Go guardと残る製品の現在の有限な宣言・継承を2849が所有する。Cargo付き2851がschedulerのRust adapter/dispatch/関連検査・配布を所有する。同じファイルを並行実装しない。原2792と2836の関数、product-ci/gh_gateは変更しない。

両担当は相手のToDo全体のdoneを待たない。2849は先にAO2839のこのAPIとGo正式更新を完成し、source/PR/CI/実配布SHA/実CLI結果を自分のworktreeの `current-succession-delivery.private.json` と通常ToDoメモへ保存する。その後、製品担当の実進捗の工程を続ける。2851はその部分配布を照合してRust側を更新し、AOの実受付を確認する。2851の受付修復がなければ2849の残る製品が動かないため、2851→2849全体完了のwaitは循環になる。片側の部分配布だけが未達のときは同じToDoの有限な技術wait-retry（例300秒）を使い、原本待ちや本人判断待ちへ戻さない。独立してできる実装・レビュー・CI・配布は先に進める。

## 正式guardの読み取り専用API

`scrub391.exe --verify-current-succession <新ToDo番号>`

成功時だけexit0、JSON1件。失敗はexit1の不成立を返し、未知の名前やpath・原名簿・本文を出さない。これは原番号の復旧でなく、固定spec・現在の本人指定・実所有者・物理root・現在の新担当を束ねた有限な資格。2839は既配布の固定AO宣言を使い、ほかは2849が照合した現在の宣言だけ。呼出側の任意manifestをそのまま信頼しない。存在する原deny/読取不能は優先する。

成功例の形式:

```json
{"domain":"current-product-succession-v1","verified":true,"origin_todo_id":2834,"todo_id":2839,"project":"GoDD-AO","registered_path_sha256":"<lowercase hex64>","spec_path_sha256":"<lowercase hex64>","spec_sha256":"<lowercase hex64>","original_registry_recovered":false}
```

pathのfingerprintは、通常の絶対path文字列のbackslashをslashへ置換し、末尾slashを除き、小文字化したUTF-8のSHA256。リンク・`..`・別common等の実体検査はguardが別に維持する。spec本文は保存済みの実バイト列のSHA256。これらの値はRust側の現在のitem/project登録root/specと一致しなければ拒否する。pathや認証付きremoteそのものを応答・ログへ出さない。

## Rust側の条件

`Backend`へ既定falseの `current_product_allowed(item, registered_root)` を追加し、RealBackendは正式APIのdomain/verified/元2834/新id/project/3つのfingerprint/原未復旧を確認する。specはguard成功後だけ上限1MiBで読み、実バイトを照合する。さらに正式guardのroot判定、作成済みworktreeの判定を保持する。不成立・旧binaryの未対応・欠落・不正JSONはfalse。raw応答やprivate pathをログへ出さない。

dispatchの原名簿NotFoundだけの分岐へ、この資格による限定受付を加える。既存名簿が存在する場合のdenyと読取不能、本人停止、未完依存、資源、担当競合、通常のspec/起動世代と作業場所の照合を変えない。未知192件・旧元番号・資格のない製品は保留のまま。正常repair-stepで新しい継承先の実起動・成果更新と次の自然巡回まで確認し、単なるCLI分類成功を受付成功としない。

## 検査・配布

Goの成功/未知id/変わったspec/denyとRustの現item不一致/不正receipt/読取不能/旧番号/別project/停止/依存/資源・所有権の回帰を確認する。変更クレートと利用側の関連検査・doctest、別モデルMust0・公式CI・通常merge・最新mainの並行変更保持・正式更新を行う。GoとRustの両側が現mainへ入り実配布されるまで、受付・製品開発の成功としない。guard APIがまだ未配布でも、Rustを誤って全許可へ落とさない。
