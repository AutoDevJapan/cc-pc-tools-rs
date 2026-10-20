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
