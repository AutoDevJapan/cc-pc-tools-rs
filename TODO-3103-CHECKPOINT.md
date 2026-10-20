# ToDo3103 再開記録

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/289
Branch: scheduler/todo-3103-godd-mcp-godd-mcp（作業中にブランチ名は変更していない）
Base: c21b2a5
Red対象HEAD: 21e4ae9

## 保存済み

- AGENTS.md、関連Skill、本人方針owner-distinct-godd-mcp-products-20261020.mdを読了。
- origin/APIでAutoDevJapan/cc-pc-tools-rs（repository ID1406818713）を照合。作業元と既知の配布先2leafは正式guardがclient_resource=false。
- 対象2repoの数値owner IDを通常GitHub OAuth読み取りで再照合。仕様には数値だけ保存。
- docs/distinct-godd-mcp-products-3103.mdに入力・照合順・受入条件・制限を記載。
- 保護中/通常のAPI双方へ計6回帰、共通順位へ1回帰を追加し、21e4ae9へコミット。
- Cargo資源を持たないため親はコンパイルしていない。RedはToDo3108（--needs cargo、subwork parent=3103）に登録し、保存されたspecと受付を照合。
- 顧客側のソース・設定・資料・旧フォルダ・一覧原本・認証情報は開いていない。UI/CSS/配置は未変更。

## 調査結果と次の実装

カタログにはGoDD MCP αだけがあり、受託GoDD MCPが欠落。GoDD接頭辞の順位推定、名前/作業場所主体のAPI帰属、同じjobの複数ToDo関連がある。
policy.items.2641.projectはGoDD-MCP-Alpha。2737/3093はその設定が無い。本人が指定した3件はα側という根拠を保ち、コード管理のcc-pc-tools-rsへ登録されていることと事業上の対象を分ける。
保護中APIは現在policyのproject帰属を使っていない。通常APIだけを直して完了にしない。

ToDo3108の実測結果（固定HEAD、7件のコンパイル成功後の意図したRed、ログ）を受領するまで実装しない。
受領後はcc-job-policyの共通識別をcc-scheduler経由で利用し、operations_catalogと両APIを修正する。
product_identityの検証済み事業上の対象をコードrepoより優先し、矛盾・未確認ownerを名前で救済しない。既存の正確な名前はname_onlyとして所有確認と区別する。
同じjobのprimary帰属を1箇所にし、ToDo自体と記録済み進捗は削除しない。APIの固定製品IDは追加フィールドとし、既存のUI構造は変えない。

## 未完了

Red実測、実装、Green/利用側関連検査/doctest、Sonnet5.5/maxまたはOpus5.5/maxの別モデルレビュー（共有ToDoへ--spec、直接子Codex起動なし）、CI、PR/merge、既存経路での配布、実API読戻し、3084と本人方針への結果配送。
通常完了通知/メンションは不要。実装/merge/配布/API確認を別々に残す。
現時点でowner判断やPC操作は不要。3108の正規依存待ちとして保存する。

一時所有ID: 68a8512c8d0912bb85c6c114a0066537。
一時場所にはIssue本文と登録済みRed-specのコピーだけ。必要なものは本仕様/ToDo正規specへ保存済み。safe-delete plan→別呼出し通常削除→finishで片付ける。
