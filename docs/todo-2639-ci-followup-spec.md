# 元ToDo2639・必要修正後の固定headに正式CIを完了させる

元2639の有限な独立検査工程。元1636/2016/2274と他3製品の停止・依存・未完了を変更しない。既存OAuth枠だけを使い、APIキーAI/新費用/Hosted予算増額/GPU/Civ6/本人PC操作/Terminal/UACは禁止。割当Cargoは必要なCI・既存ソースの必須検査だけに使い、同検査を重複しない。

## 固定対象と実確認

repository ID1285649124、PR1002、新head20c441df545a88f4a839d7e906b5aeb4f1708ae3、tree007b84082008ea9effd5052f63856273aabbfb17。旧b93e126b172adbb29ad2081af4732f4b42f8bc95を唯一の親に持つ同branchの通常commit。AutoDevJapan/private/admin/nonfork/同repo/branchhead/PRheadをAPIで再確認する。名前はメモリ内API経路だけ。別head/merge-refへ追従しない。原PC製品folder/除外一覧/秘密/guard/registry/owner-work登録は操作しない。拒否操作の代替実行は禁止。

新headは、原Sonnet2735 Must0のSuggestion対応。アラート表・復旧手順・required testの実行機説明を正し、PostgreSQLのhealthy heartbeat text変換を3timezoneで検査するtestを追加した。b93との差分はCLAUDE.md、docs/runbooks/observability-slo.md、docs/runbooks/github-required-check-boundary.md、backend/tests/test_observability_postgres.pyの4ファイルだけ。backend/appと原workflow/scriptsのbyteは変えていない。

固定b93archive+新4ファイルの検査で、locked依存/Ruff/format/mypy/元監視テスト+新PostgreSQLテスト計50passed/skip0。uv/Postgresの隔離containerのPID0/rm -vを実確認。原log2857bytes/SHA256 f3740324680cf9f7bf79e99fb332d34cbd3193f80b21f1cda3e15a958821f570。tested4filesのSHA256・upload GitBlobSHA1・treeを照合。元956のbackend2236passed/残3結合passedと、b93のNode185/CodeQL/secretの実成功は古いsourceの証拠として保持する。新headの正式CI成功に読み替えない。

親の所有証拠root（削除禁止、まだ作業中）：C:/Users/makhmeto/AppData/Local/Temp/codex-owned-d84c967c6eb3b9abf993ad85a20fb9b9。acceptance.private.json/log、commit-proposal.private.json、review2735-original.private.md、元blob/4file proposal/Go操作補助を保持。生ログ・生ソース・顧客名をstdout/会話/報告へ表示しない。確認した値だけを返す。

## 実施すること

1. D:/CC/Tools/product-ci/product-ci.exe と元2648の最終配送・PR187の正式配布元を確認する。既配布toolが旧b93を固定して新headを拒否するのは正常。固定拒否を迂回する別実行機・即席tool・mock successを使わない。
2. 正規の修復source repository ID1406818713（実ownerAutoDevJapanを確認）の最新mainに対し、product-ciのこの候補SHAだけを旧b93から新20cへ更新する。元PR173/174/183/185/187を再実行しない。他3候補とSHA、未知repo/fork/owner/workflowbytesの拒否条件、復旧/排他設計、guard、queue設定を変更しない。変更は必要なpin1行とその検査だけ。source commit/通常PR/適切な別モデルレビュー/Must0/必要CI成功/通常merge/既存toolへの正式配布の順を保持する。単一ファイル読取レビューならHaiku5.5を共有ToDoへ--spec付きで登録し、子Codexジョブを直接起動しない。自分の作業ブランチを変更したら終了・待機前に正式checkpoint-workspaceで保存する。
3. 最新sourceの正式toolを配布後、同じ新headの既存queued jobを正規のproduct-ciで処理する。CodeQL/secret/Node等、同候補の工程は必ず直列。元workflow bytesは不変なので既存のworkflow固定条件を緩めない。新CIをむやみにdispatch/rerunせず、最初にGitHubの現run/job/headを確認する。原source SHAとGitHub eventのmerge contextを区別する。
4. 新headの公式CodeQL4言語と通常secret/testを実successにする。Node script契約も必要なら既存toolで固定新headを検証。Rust/他3製品の既存検査を重複しない。failureは原因を突き止め、範囲内の必要修正は根拠付きで親へ返す。scope/秘密/予算を増やさない。
5. 実source/head/tree/run/job/analysisID/check供給元と実成功・失敗・未実行、全生logのbytes/SHA256、containerのPID0/回収/runner登録不在/排他解放を保存する。新しいtempはsafe-delete createで所有登録し、終了後はplan→別呼出し通常削除→finish、拒否は正規quarantine。指定された操作証拠だけを従来のevidence保管先へ保存し、親へ正確な保存先を返す。原資料は保持する。

親が新4ファイルの別モデルレビューを別工程で進める。製品PR1002のdraft解除・merge・Prd変更はこの検査担当は行わない。全必要検査が揃った実結果を親2639へ返し、この有限工程だけ完了する。所有元や元製品全体をdoneにしない。待つだけならtodo wait --onで依存を保存し、技術的な待機をowner blockにしない。
