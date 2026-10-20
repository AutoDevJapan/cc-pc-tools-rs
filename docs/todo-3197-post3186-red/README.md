# ToDo3197の固定ソース測定

指定された2コマンドを各1回実行し、コンパイル後に合計6件の比較が失敗した。
両方の終了コードは100。コンパイル失敗や起動失敗ではない。
この記録はToDo3103の追加受入条件を修正前ソースで測った結果である。
親の修正、最新候補の合格、レビュー承認、マージ、配布を完了したという意味ではない。

対象はAutoDevJapan/cc-pc-tools-rs、実repository ID1406818713、Issue289／親PR302。
通常のGitHub OAuthで本人loginと実所有者を照合した。保護判定は作業領域・原登録範囲・
共有Gitオブジェクト領域とも対象外だった。親の現在ソースや顧客側の資源は開いていない。
作業枝は `scheduler/todo-3197-3103-source`、実行jobは `20261021-061341-todo-3197`。

固定ソースは **f85630d54888e0030692d6a7823cb8d6d99154fe**。
943e5f6ae931b886807aa6bd6ba8bd2850baac8aとの差はRust受入テスト2ファイル、
Go受入テスト1ファイル、仕様文書1ファイルだけである。
固定ツリーのIDは [fixed-tree.txt](fixed-tree.txt)、差は [fixed-source-diff.raw.txt](fixed-source-diff.raw.txt) に保存した。

## 実行結果

日時はすべて2026年10月21日、日本時間。2件は同じ専用targetで順番に実行した。
通常の `cargo_gate.exe`、sccache、専用TEMPを使用した。ビルドの要求上限は10、
テスト並列の要求上限は8で、ゲート設定のビルド上限8を保持した。
実行プロセスは `CreateNoWindow=true`。GPU・Civ6は起動していない。

|実行|日時|コンパイル|選択／除外|成功／失敗|終了コード|
|---|---|---|---|---|---|
|01 cc-job-policy|06:19:58.146–06:20:08.124|成功、6.57秒|1／9|0／1|100|
|02 localai-web-rs|06:20:24.698–06:20:57.225|成功、31.50秒|5／91|0／5|100|

実行01:

```text
cargo nextest run --locked -p cc-job-policy --test product_identity -E 'test(mcp_business_jobs_preserve_dependency_urgency_without_borrowing_history_priority)' --no-fail-fast
```

実行02:

```text
cargo nextest run --locked -p localai-web-rs --lib -E 'test(mcp_job_business_identity_keeps_the_more_urgent_product_or_dependency_rank) | test(mcp_raw_private_project_cannot_be_unmasked_by_a_display_override) | test(mcp_private_job_priority_keeps_its_linked_task_details_closed) | test(mcp_private_names_in_related_ids_hide_both_task_and_job_details) | test(mcp_api_reports_the_presence_of_runtime_binding_overrides)' --no-fail-fast
```

|検査名（`mcp_`以降）|最初に失敗した比較|到達範囲と限界|
|---|---|---|
|business_jobs_preserve_dependency_urgency_without_borrowing_history_priority|順位の期待30、実際60。145行、`false/wait_on/30`|declared=false／wait_onの10・20は通過。30で停止。40・50・61・100、work_wait_on、依存なし、declared=trueは未到達。|
|job_business_identity_keeps_the_more_urgent_product_or_dependency_rank|順位の期待30、実際60。540行、`masked=false/parent=30`|通常経路の受託製品は全7順位を通過。αは10・20を通過し30で停止。αの後続順位とmasked=trueは未到達。|
|raw_private_project_cannot_be_unmasked_by_a_display_override|期待Err(404)、実際Okで架空本文を返す。563行|owner_projectによる上書き（from_policy=false）の詳細で失敗。盤面の検査とpolicy経由の上書きは未到達。|
|private_job_priority_keeps_its_linked_task_details_closed|期待Err(404)、実際Okで架空本文を返す。584行|jobだけに受託順位10がある条件の詳細で失敗。盤面の検査は未到達。|
|private_names_in_related_ids_hide_both_task_and_job_details|期待はtask/job両方Err(404)、実際は両方Okで架空本文・報告を返す。609行|taskとjobの詳細は両方実行済み。組合せのassertで停止し、盤面の検査は未到達。|
|api_reports_the_presence_of_runtime_binding_overrides|`productBindingsOverridden`の期待false、実際Null。628行|masked=false／policy={}で停止。キーありの空object・nullとmasked=trueは未到達。|

すべて合成の識別子・非公開名・本文であり、実際の顧客本文や認証値ではない。
未到達の分岐を失敗・成功のいずれにも数えていない。元のテストやassertは変更していない。
全workspace・release・Clippy・修正後候補の検査・成功検査の再実行は行っていない。

## 生データとソースの一致

通常の `git archive` でもこのPCの `core.autocrlf=true` と属性が作用し、
初回は1,183ファイル中1,164件に差があった。この段階ではCargoを起動していない。
失敗した全件ハッシュと照合は `source-initial.*` に保持した。

所有記録付き一時領域内に専用bare Gitを作り、固定コミットのオブジェクトだけを読む
alternatesを設定した。専用の `info/attributes` の `* -text -filter -ident -working-tree-encoding`
と `core.autocrlf=false` により変換を止めて再度archiveし、別ディレクトリに展開した。
親のGit設定や原資料は変更していない。

`git ls-tree -r -z` で固定ツリー全1,183ファイルのblob IDを取得し、
`git cat-file --batch` の生バイトすべてからSHA256を計算した。
実行直前・直後の各2回を含む全5回の照合で、ファイル数1,183、差0、reparse 0だった。
比較にはロックファイル・テスト・設定・文書も含む。targetとTEMPはソースの外に置いた。

- [git-blob-sha256.json](git-blob-sha256.json): 全blob ID・サイズ・SHA256。
- [git-ls-tree.txt](git-ls-tree.txt): 固定ツリーの全エントリ。
- [preparation.json](preparation.json): 生archiveのSHA256、改行数、専用領域と生成方法。
- `source-initial-raw.*`、`source-before-01.*`、`source-after-01.*`、`source-before-02.*`、`source-after-02.*`: 展開ファイルの全SHA256と照合結果。
- [results.json](results.json): 選択数、個別結果、到達範囲、日時、終了コード。
- [run-01.json](run-01.json)／[run-02.json](run-02.json): 正確なargv、PIDと生成時刻、終了確認、出力ハッシュ。
- [run-01.stdout.log](run-01.stdout.log)／[run-01.stderr.log](run-01.stderr.log)、[run-02.stdout.log](run-02.stdout.log)／[run-02.stderr.log](run-02.stderr.log): 切り詰めない全文。stdoutは両方0 byte。nextest内のテストstdoutもstderr側の全文に含む。
- [process-exit-check.json](process-exit-check.json): 2つの元ゲートPIDの終了確認。出力の排出後に保存した。後から取得した `process-observation-02.json` は終了後の空の観測で、実行中の並列数を測った証拠にはしない。
- [provenance.json](provenance.json): 実所有者・OAuth本人login、ゲート設定、使用した実行ファイルのSHA256。秘密値は含まない。
- [measurement.ps1](measurement.ps1): この有限測定に使った一度限りのPowerShell手順。各runの起動記録が既存なら重複起動を拒む。

ログとJSONを含むこのディレクトリは `-text` 属性で保存し、コミット時も改行変換を止める。
証拠に含まれるCRLFを末尾空白と誤判定しないよう、このディレクトリだけに
`whitespace=cr-at-eol` 属性を付けた。保存したログのバイト列は変えていない。

## 一時データの退避

safe-deleteの所有IDは `8daae585cac606ae8a496ca2af6b9169`。
planは1,460,365,649 byte／5,025項目について、sizeを理由にquarantineを指定した。
通常削除のコマンドは返されなかったため、削除は提出していない。
通常のquarantineは `QUARANTINE_CROSS_DRIVE` でpreflight中に止まり、元データを保持した。
これは実行審査の拒否ではない。削除の再試行や別手段での削除は行っていない。

本人指定の同一ドライブ退避として、新規の
`D:/削除希望/20261021-todo-3197-post3186-red-8daae585/data` へ通常のMove-Itemで移動した。
移動前に、元の所有記録、絶対パスと境界、両方の測定プロセスの終了、
他の実行プロセスによる参照がないこと、祖先と配下のreparse不在を照合した。
通常ファイル4,383件は、NTFSのリンク数と領域内の同じIDの名前数も一致した。
他のデータへのハードリンクはない。

根を含む全5,026項目のvolume/file ID、属性、リンク数、サイズ、作成・更新時刻は、
移動直前と移動後で一致した。移動先は新規で、既存の削除希望データは読取り・変更していない。
元パスの不在を確認後、正規の `finish --id` は **state=source_absent** を返した。
退避先には `理由.txt` も保存した。データは退避先に残っており、Dドライブの空きが増えたとは主張しない。
定期清掃側が扱うための退避を終えたのであり、元のGitオブジェクトや原資料は保持している。

- [cleanup-create-receipt.json](cleanup-create-receipt.json)、[cleanup-plan.json](cleanup-plan.json)、[cleanup-quarantine.json](cleanup-quarantine.json): 正規CLIの所有・計画・停止応答。
- [cleanup-stopped-check.json](cleanup-stopped-check.json)、[cleanup-inventory-before.json](cleanup-inventory-before.json)、[cleanup-pre-move-verification.json](cleanup-pre-move-verification.json): 停止確認と移動前の全ID。
- [cleanup-inventory-after.json](cleanup-inventory-after.json)、[cleanup-move-verification.json](cleanup-move-verification.json)、[cleanup-finish.json](cleanup-finish.json): 全ID一致、元パス不在、所有記録の終了。
- [cleanup-inventory.go](cleanup-inventory.go): この移動のメタデータ照合にだけ使ったGo手順。
- [committed-log-verification.json](committed-log-verification.json): 全ログのSHA256と、コミット済みGit生blobとの一致。

最終の実HEADを使うcheckpoint requestと実応答は、同じ作業場所の
`.ci-work/todo3197-checkpoint-final.*` に保存する。コミット自身のHEADをそのコミットの
本文へ埋めることはできないため、これはGit外の実行記録として保持する。
各証拠ファイルの一覧とSHA256は `CHECKSUMS.sha256` に保存する。
