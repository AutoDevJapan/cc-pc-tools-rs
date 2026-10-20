# ToDo3103 再開記録

元Issue289。旧PR297はレビュー指摘の修正のため閉じた。作業は3103で継続する。
UI/CSS/配置と顧客側の資源・原名簿は未変更。

## 受領した証拠

- RedのToDo3108: 修正前ソースのコンパイル後に7件失敗、2つのnextestはexit100。3件の後段・通常経路は未到達。
- 最初の候補の正式CI: Build speed 39432734317で2297件成功・6件除外、doctestとClippy成功。Channel reportsもWindows/Linuxとも成功。CodeQLの一部は順番待ちだった。
- Opus5.5/maxのToDo3119（実job 20261020-225254-todo-3119）の原応答を受領。SHA256 8CA23F5228AD86B436BCD5326F99849FCB44117B1EA3995F3AAE740E6B5E41E8。13ファイルの読取範囲と未読/未実行の限界を原応答に保持。
- Must3件は公開ソースの実所有対応、通常の主担当がMCPの副関連で100へ落ちること、主帰属の絞込みで従来の非表示判定を狭めたこと。
- 実repoは通常OAuthでpublicを確認。実数値の対応は公開しない方式へ変更する。旧PRを閉じ、過去候補を含まない新しい作業ブランチを作る。

## 今回の修正

- Productの定義は固定ID/正式名/部署/順位/所有規則だけ。実repository/ownerの組は非公開の配布入力（4つのCC_PRODUCT_MCP_*変数）で渡す。公開テストのIDはすべて合成値。
- APIから実repository/ownerを外す。カタログはcatalogue、作業はname_only/owner_declared/verified/unknownを区別。
- 主担当がある通常jobの順位を保持。unknownでも正式な必須依存の順位は保持し、事業対象とは分ける。
- 主担当とjob名の矛盾はunknown。主帰属が1件でも非表示判定は全関連で行い、互換な失敗履歴を各ToDoに残す。詳細APIでも受託とunknownを閉じる。
- 本人指定のα3件と反対の明示識別を同時に認めない。MCPの全角空白も同じ正式名へ解決する。
- rustfmt/git diff --checkまで確認済み。Cargo割当はなく、重いコンパイルは親で未実行。

## 次の工程

新規のレビュー回帰を以前の固定実装へ当てる有限Red検査を--needs cargoで登録する。新候補のCIを成功済みの旧結果と混同しない。
固定した修正候補のOpus5.5/max再レビュー、必要CI、Must修正、merge、非公開入力付きの既知実行物のビルド/配布、稼働API読戻しが残る。
ToDo3084とowner-distinct-godd-mcp-products-20261020.mdへ、反映済みと未反映を区別して配送する。通常完了通知/メンションは不要。
旧PRの参照やキャッシュが消えたとは主張しない。原資料はこのPCの既存Gitと元担当の記録に保持する。

## 後片付けとCLI

前回までのsafe-delete所有フォルダはfinish済み。今回の所有IDはd4b59d9e3ec6e1ce74aa914dfcd87d5f。
新branchを使ったらscheduler checkpoint-workspaceへid3103/current CC_JOB_ID/expected_branch/新branch/実HEADを保存する。
Windows GUIサブシステムのCLIは `& ... | Out-String | Write-Output` で完了を待つ。request-fileを先に片付けない。
