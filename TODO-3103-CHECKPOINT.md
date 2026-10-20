# ToDo3103 再開記録

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/289
Branch: scheduler/todo-3103-godd-mcp-godd-mcp
Base: 8dd9bb8（元Red保存後のmain更新をrebaseで反映）
Red固定HEAD: 21e4ae9ea92f468b45dd975da93bdf08f519f4ea

## 保存済みと実測

- AGENTS.mdと本人方針を読み直し、origin/APIで自社管理ツールrepo ID1406818713を照合。
- ToDo3108の原応答と結果JSONを受領。固定Redソースでコンパイル成功後に7件失敗、2コマンドともexit100。3件はカタログ欠落で後段未到達、通常経路も未到達という限界を保持。
- Red記録: 3108のevidence/todo-3108-red/results.jsonと全文stderrログ。記録コミット49720ff。原応答SHA256 AFF6F16144CA92DDEFDCFBB201B41E6678E036CEFA2507D6CB0F0BA12EFB4018。
- 共通product_identityモジュールに2repo/owner/固定製品ID、厳密な照合、未知値と名前だけの分類を実装。
- 本人指定2641/2737/3093は記録プロジェクトがcc-pc-tools-rsの場合だけαへ結ぶ。題名や自由文は使わない。
- 共通順位、カタログ、通常/保護中API、ジョブの一意な主帰属を修正。作業のproductIdentityとcodeProductを分ける。
- 製品進捗率はnull、各ToDoの実測進捗は保持。CC記録のみをrecorded_active、担当不明をunobservedとし、Native停止を捏造しない。
- テストは最初の7件に加え、コードrepoとの分離、型/方針の矛盾、αの既知3件、表示APIの帰属などを追加。rustfmtとgit diff --checkまで確認済み。親にはCargo割当がなくコンパイルは実行していない。
- 顧客のソース/設定/資料/旧フォルダ/一覧原本/認証情報は未閲覧。UI/CSS/配置は未変更。

## 未完了

PRの正式CIでGreenと利用側のnextest/doctest/Clippyを確認する。親から重いCargoは起動しない。同じ最終ソースで成功したCIを重複実行しない。
PR297: https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/297 。CIは実行順待ち。
別モデルの有限レビューはToDo3119へOpus5.5/maxを指定して登録済み。CLIのモデル引数はスラッシュを受け付けず、claude-opus-5-5という正規aliasで登録した。schedulerは既存anthropic/claude-opus-5-5へ正規化することをsourceで確認した。実行したモデルは実ジョブの対応メタデータで後から照合する。
追加の確認で、直接の主担当がない競合旧リンクを作業番号で選ぶ問題を補強した。製品はunknown、jobは未確認対象に1件保持する。新候補の検査/レビュー結果はまだ受領していない。
稼働APIの変更前読取り: masked_metadata/metadataOk=true。GoDD MCPは未掲載、αの作業記録は5行。新しい固定識別フィールドは未配布でnull。これは人数や現在の製品進捗ではない。
Opus5.5/max（または範囲を絞ったSonnet5.5/max）の固定候補レビューを共有ToDoへ--specで登録する。直接子Codexジョブは起動しない。
Must修正、必要な再検査、CI成功、PR/merge、既存経路の配布、実API読戻し、3084とowner-distinct-godd-mcp-products-20261020.mdへの結果配送が残る。
配布時は現行ソースと並行変更、正式guard、既存UIの保持を再照合し、未承認の別UI候補を一緒に配布しない。
実装/CI/レビュー/merge/実配布/実APIを区別する。owner判断や本人PC操作は現在不要。通常完了通知/メンションは不要。

## 一時データとCLI

前回の2つのsafe-delete所有フォルダは通常削除後にfinish済み。
今回の所有IDは58eb041a7237c992ee9ec928d8c4a547（review-and-pr）。本文はToDo正規specとGitHubへ保存後にplan→別呼出し通常削除→finishする。
Windows GUIサブシステムのCLIはPowerShellが非同期扱いするため、scheduler/todo等の結果が必要な呼出しは `& ... | Out-String | Write-Output` で待ち、直後のLASTEXITCODEと保存後の実記録を照合する。checkpointのrequest-fileは完了前に片付けない。
