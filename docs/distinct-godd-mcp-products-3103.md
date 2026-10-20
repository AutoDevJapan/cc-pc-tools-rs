# GoDD MCPとGoDD MCP αの識別を分離する

ToDo3103。本人の訂正を根拠とし、管理ツールのカタログ・部署・順位・読み取りAPIを修復する。
UI/CSS/配置と顧客側のソース・環境・原保護は変更しない。画面の方向は3084が保持する。

## 製品の識別

|固定製品ID|正式名|部署|順位|所有規則|
|---|---|---|---|---|
|godd-mcp-contract|GoDD MCP|受託部|10|顧客側の規則を保持|
|godd-mcp-alpha|GoDD MCP α|GoDD事業部|60|既存の自社製品の規則|

実際のrepository/owner IDの組は本人の非公開運用記録で確認する。
公開ソース・テスト・APIへその組を出さない。2つの組は通常のGitHub OAuth読み取りで照合する。
名前や数値IDを根拠に操作権限を追加するものではない。

作業の`product_identity`は`product_id`（省略可）、`repository_id`、`owner_id`を持つ。
事業上の対象を明示した場合は、コードの`repository_id`/`repository_owner_id`より優先する。
指定値同士の矛盾、所有未確認、不正な型はunknownとし、名前や作業場所で救済しない。
明示値がない旧記録だけ、正式名と既知の完全な別名を使う。GoDD/MCPの部分一致は使わない。
GoDD MCPをαの省略形にしない。他製品の名・部署・順位は維持する。

本人指定の2641/2737/3093は、登録プロジェクトがcc-pc-tools-rsの場合に限りαへ結ぶ。
反対の明示識別が付けばunknownを保持する。題名や自由文からこの対応を増やさない。

## 非公開の配布入力

配布用ビルドには次の4つの環境変数を、その担当のプロセス内だけで渡す。
`CC_PRODUCT_MCP_CONTRACT_REPOSITORY_ID` / `CC_PRODUCT_MCP_CONTRACT_OWNER_ID`、
`CC_PRODUCT_MCP_ALPHA_REPOSITORY_ID` / `CC_PRODUCT_MCP_ALPHA_OWNER_ID`。
値は非公開の本人指示と通常OAuthの再照合から得る。ログや公開CIの設定・成果物へ出さない。
値なしの実行物は明示したrepository/ownerを確認済みとしない。
検査では信頼するpolicy入力の`product_identities`に合成の2組を注入する。
これを原保護の解除やcaller allowフラグとして使わない。
保護対象の稼働設定へ新しい許可や対応表を書き込む必要はない。

## APIと集計の受入条件

1. 2つのrepository/ownerの組は別の固定製品ID・部署・順位になる。事業対象は編集repoと別に保持する。
2. 所有や固定IDの矛盾はunknown。名前だけはname_only、本人の有限な指定はowner_declaredとする。
3. 受託は10、自社αは60。通常製品の主担当があるジョブはMCPの副関連だけで100へ落とさない。
   recordedな必須依存の順位は、製品の所有を捏造せず引き継ぐ。
4. 他方のCIの実行状態・進捗・停止理由を混入させない。ジョブの名前と直接の主担当が衝突したらunknown。
5. 同じ実行中jobの主帰属は1回。主担当がなく旧関連が競合したら番号や順位で製品を選ばない。
   ToDo自体と記録済み進捗を削除しない。互換な失敗履歴は共有した各ToDoへ残す。
6. 未観測の担当をunobservedとし、CC記録0件からNativeの停止を断定しない。
   製品全体の進捗率はnull。各ToDoの進捗を他方から借りたり平均したりしない。
7. 通常と保護中のAPIで同じ識別を使う。主帰属の絞込みで従来の非表示判定を狭めない。
   顧客やunknownの本文・履歴・詳細APIは閉じる。repository/owner IDはレスポンスへ追加しない。

`/api/operations`と保護中の`/api/agent/display/{todo,jobs,handoff}`で、
作業の`productIdentity`と`codeProduct`を分ける。catalogueは静的な製品定義の印である。
recorded_activeはCC記録の観測であり、実処理の確認は既存workforceの取得範囲に従う。

## 検証の記録

最初の7回帰はToDo3108が固定した修正前ソースでコンパイル後に全件Red、両nextestはexit100。
3件は製品欠落で後段未到達、通常経路も未到達という限界を保持する。
全文ログと結果JSONは同担当のevidence/todo-3108-redへ残す。

最初の実装候補は正式CIで2297件成功・6件除外、doctestとClippyも成功した。
別モデルのToDo3119は読取だけ完了し、公開する対応表・通常主担当の順位・非表示判定にMustを返した。
候補を閉じ、実対応の配布入力化、合成fixture、主担当のない場合だけの競合判定、
全関連を使う非表示判定、詳細APIと失敗履歴の修正を行う。新候補のGreenはまだ未確認。
実装・Red/Green・独立レビュー・CI・merge・実配布・実API読戻しを区別して追記する。

## 配信まで同じ帰属を使う

2回目のレビューで、集計後のworkforce適用が旧関連から他製品の実行を復活させる問題、
名のない失敗ジョブ、単独表示の非表示漏れ、Go収集器の順位の不一致を指摘された。
各呼出し側で条件を足す方式を見直し、1回の更新で記録IDの索引を作る。
直接の主担当から解決したjobの製品を、主帰属・互換な履歴・非表示に共通で使う。
非表示は全ての元関連を確認し、履歴は互換な各記録へ残し、実行の主帰属だけを1件にする。
名のないjobも主担当の製品から判断し、矛盾を他方の履歴へ載せない。

新しい行は`processingJobKey`を持つ。Goのjob_keyと同じ、job IDのUTF-8に対する
SHA-256の匿名キーである。配信直前も、その行に割り当てたjobのキーが、最近の処理と
プロセス本人性を確認した収集器の証拠と一致する場合だけprocessingにする。
ToDo番号だけが一致した別jobや、主帰属のないunknown行では作業中にしない。
旧形式の一般行には従来の処理を保ち、MCPとunknownは旧形式でも確認を厳密にする。
refreshとsnapshotの両方が同じ世代の適用処理を通る。配信を読む回帰検査を追加する。

Go収集器も同じ2製品と有限な本人指定を解決し、MCPが関わる実行の主帰属を1件にする。
待機中の古い関連を現在の担当より優先しない。互換な終了履歴は両方へ残す。
製品の部署と、依存によって引き継いだ急ぎ度は別であり、αを受託製品へ変えない。
Rustのpriority/product_identityの両ソースの固定指紋と合成検査をGo側で確認する。
その2ファイルの変更でも収集器の正式workflowを起動する。

## 配布入力の確認

Rustの4入力と同じ組を、Goの非公開ビルドへ`-ldflags -X`で渡す。
対象はmain.mcpContractRepository/main.mcpContractOwner、main.mcpAlphaRepository/main.mcpAlphaOwner。
入力付き実行物を公開CIの成果物へアップロードしない。
ジョブ管理、scheduler、todoは`--product-identity-status`で設定済みかだけを返す。
この読取りは設置済みの記録へアクセスせず、入力値や推測できる指紋を返さない。
ConsoleのproductBindingsConfiguredと、workforce.collectorBindingsConfiguredも確認する。
配布担当は同じ非公開入力とソース版で全利用側をビルドし、実hashと設定済みの実応答を残す。
入力未設定の実行物や、旧収集器との部分更新を完成扱いにしない。

Goの新規5件は修正前に全件Red、追加の履歴・部署・待機関連3件も全件Redを実測した。
修正後のGo全件は14.104秒で成功し、go vetも成功した。全文ログは元担当の非公開作業記録へ保持する。
Rustの追加配信回帰、全利用側の正式CI、固定候補の独立再レビュー、実配布はまだ未確認。
