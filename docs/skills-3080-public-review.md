# 公開入力に結ぶ読み取り専用レビュー

`--receive-skills3080-result`の新しい3080子レビューは、起動前に保存した仕様の
**全体**を`skills-3080-public-review-v1`のJSONにする。後からrequestへscopeや
public boolを足す方法ではない。原authorと現在のconsumerは別々に照合する。

JSONは`repair-guard/skills3080_public_review_windows.go`の
`skillsPublicReviewScope`の全fieldだけを持つ。`instructions`は同ファイルの
`skillsPublicReviewInstructions`と完全一致させる。任意の追加指示、未知field、
JSON後の文章、重複keyは拒否する。実データとGitHubの証明が必要なため、以下の
説明用placeholderをそのまま登録しても資格は得られない。
keyはjson tagで定めた綴り・大文字小文字へ再帰的に完全一致させる。
Go標準decoderのcase-insensitive照合だけには依存せず、`instructions`と
`Instructions`のような別名や、nested author/filesの大小文字違いも拒否する。
受入markerのaccepted/must/headにも同じ厳密さを適用する。原記録のkeyを書き換えて
資格を作らず、正常な起動前入力をそのまま保つ。

```json
{
  "domain": "skills-3080-public-review-v1",
  "instructions": "skillsPublicReviewInstructionsの文字列をそのまま使う",
  "todo_id": 3080,
  "repository": "AutoDevJapan/cc-pc-tools-rs",
  "repository_id": 1406818713,
  "commit": "公開済み固定40桁commit",
  "author": {
    "task": 3080,
    "job": "実際の原author job",
    "spec_sha256": "原authorが受け取った保存specのSHA256",
    "workspace": "原3080の登録worktree"
  },
  "author_spec_path": "D:/CC/Tools/todo/specs/3080-保存済み32桁ID.md",
  "manifest_path": "docs/skills-3080-operations/対象.json",
  "manifest_sha256": "Git blob生bytesのSHA256",
  "manifest_declaration": "SKILLS3080_MANIFEST_SHA256 同じSHA256",
  "files": [
    {"path":"docs/skills-3080-operations/対象.json","blob":"固定Git blob ID","sha256":"同じSHA256","bytes":123}
  ]
}
```

filesはmanifestを必ず含み、`.gitattributes`、既存のSkills3080文書・公開証拠、
または元3080の検査器`tools/skills-3080-catalog`直下のGo source/testと`go.mod`に
限定する。検査器の子directory、他のtool、実行物、設定秘密は含めない。
各fileの相対path、Git blob ID、SHA256、byte数を宣言する。
最大128file、各16MiB、合計32MiB。一般の公開repo・他製品・私的scopeには流用しない。

変更前後の比較が必要なら、同じ起動前JSONに任意の`comparison`を1つ含める。
`instructions`は`skillsPublicReviewComparisonInstructions`と完全一致させる。

```json
"comparison": {
  "commit": "固定baseの40桁commit",
  "files": [
    {"path":"head filesと同じ選択path","blob":"baseのblob ID","sha256":"baseのSHA256","bytes":123},
    {"path":"head filesにある新しい選択path","absent":true}
  ]
}
```

base filesはheadで選択した全pathを、同じ綴りで1回ずつ宣言する。新規pathだけを
`absent:true`とし、その場合blob/SHA/bytesは指定しない。公開GitHubの比較metadataで
baseがheadの正規祖先であること、両固定treeのregular blobと正確なbytes、又は
baseでの不在を照合する。baseの宣言が別pathの読取範囲を増やすことはない。
合計32MiBにはbaseも含める。変更と必要な支持を読む範囲を分けて報告し、変更が
ない部分を全読したとはしない。受領条件は未mergeでも同じで、comparison結果を
全出力のCreateNew用内容受入へ転用することは実装でも拒否する。

照合は次の順で行う。

1. 既存の本人scopeと、現在の最新・生存・未停止consumer3080を確認する。
2. 登録されたreadonly child、admission、原job/spec/cwd、実origin authorとその
   保存spec・最初の依頼を含むlaunch prompt・実launch receiptを照合する。
3. GitHubの実repo ID/owner/PUBLIC、固定commitのtreeを照合する。宣言した各fileを
   そのtreeのregular blobへ結び、GitHubのblob ID/sizeと保持するGitの生bytesの
   Git object hash/SHA256/sizeを照合する。既存保護を優先し、linkを拒否する。
4. 原応答ファイルを開く前に、native sessionの実ID/cwd/厳密な開始終了窓で、
   同じlaunch promptと`final_answer`の期待digest、実model/effortを照合する。
   合うuser入力がないassistant本文は射影へ取り込まない。その後だけ原応答を
   読み、同bufferのfinal digest・job metadataと一致させる。
5. 返却直前にも実consumerを再確認する。返すのはexact原応答と限定metadataだけ。

型付き仕様の実入力は`prompt.md`だけとはしない。正規schedulerの登録promptと
実specの境界、実subworkの番号/kindから作る固定文、実sandboxの固定guidanceを
照合する。その上で、正常ランナーが保存する`execution-prompt.md`が、同じpromptと
正規の運用方針・固定site文だけからできていることを確認し、nativeへ完全一致させる。
任意の先行user・仕様後の自由文・追加画像・別の後続userを拒否する。

起動時のAGENTS/環境の構造化メッセージは作業のuser入力と分ける。元の登録authorの
同じrepository/cwdに属する正規sessionの初期指示digestと、callerの厳密な環境形式を
照合し、最初の1件だけを認める。本文を返したり、元のAGENTSファイルの分類を解除する
ものではない。sessionの初期metadataは再開authorより前のことがあるが、作業の入力と
finalの開始終了窓を広げる証拠には使わない。運用方針のsnapshotを現在の正本へ
照合できない場合は保留し、元記録を現在版へ書き換えない。
caller自身の初期指示は、作業入力の射影より前に独立した必須条件として照合する。
bootstrapが欠落した記録を、完全一致する作業入力だけで代用することはできない。

公開済みの固定入力は未mergeでもこのread-only経路で確認できる。`main`との
compareを読取条件にしない。CreateNewは従来どおりmain merge、個別notice、内容review、
正確なmanifest、現在writer、不在target等を別途要求する。raw受領だけで配置受入に
しない。JSON内の`manifest_declaration`は従来の正式受入markerへ同じhashを渡す。

通常登録の例（モデル指定は元3080の既存限定回答の範囲内）:

```powershell
todo.exe add cc-pc-tools-rs "元3080の固定公開入力をレビューする" --spec <新しいJSON仕様の絶対パス> --subwork-of 3080 --parent-job-id <実authorのjob> --subwork-kind review --model gpt-6-luna --model-origin owner_request --effort max --effort-origin owner_request
```

実起動後のjob/spec/final digestから、従来のoperation requestを作る。writerは
現在consumerの実job/spec/workspaceとする。`result`に元reviewのtask/job/spec/final
を渡し、`scrub391.exe --receive-skills3080-result <requestの絶対パス>`で受領する。
requestへ公開scopeの追加fieldを渡さない。異なる原authorをcurrent writerへ偽装しない。

3847は既存の固定task/job/spec/final移行を維持する。3850は別の固定移行として
task3850/job20261025-224324/spec5a995d12/finalc1b429d6（4020B）と実起動metadata、
公開commit3b15b7a5/tree9bf31d93の7入力へ結ぶ。両者が起動前から新typed仕様を
持っていたとは記録しない。3850の固定入力は元のpublic proofと、GitHubの同tree/
Git blob生bytesを独立に照合したもの。元の不完全な3847の仕様は変更しない。

旧3686/job211205は別threadのまま保持する。実CLI0.163.0の生成schemaと
限定native metadataではterminal phaseは`final_answer`。原431Bは元job終了の
1468ms前に記録されており、元の開始終了窓を広げない。job metadata containerに
final fieldのbytesが含まれることと、原文をモデルへ表示・意味読了したことを分ける。
この固定経路でも、保存prompt/receiptとnativeの入力・期待final digest・model/effort・
厳密時間窓を先に照合し、その後だけ原応答fileを開く。native不一致では原応答を
開かない。file/job metadataのco-alterationもnativeの原digestとは一致しない。

合成fixtureは`skills3080_public_review_windows_test.go`と
`skills3080_history_windows_test.go`。未mergeの正例、private/別source/author、
後付けscope、未登録/別consumer、欠落/改変/aliasと時刻窓・phaseの負例を確認する。
原台帳・一般guard・未知9・旧14/28hold・647拒否・既存成功を変更しない。

## 同じ公開修復担当の履歴継続

原003920の310Bも、原spec4323・prompt081ed6b9・実job/launch/thread/cwd/元の
開始終了時刻・原final SHA2a8a0768を保持する固定移行とする。旧211205とは別で、
両者を起動前からtypedだったとはしない。現在の023556も文章仕様のまま保持する。

**今後の起動前**に元3686の仕様全体をJSONへする場合は、public reviewと同じ
repo/commit/author/author_spec_path/manifest/filesの構造を使い、domainだけを
`skills-3080-public-repair-v1`、instructionsを`skillsPublicRepairInstructions`の
完全一致にする。元3080のauthorとmanifestをanchorにし、入力は同じ公開namespaceと
`repair-guard`直下のGo source/test・go.modだけ。comparisonは使わない。
任意の自由文や私的資料を足すことはできない。

受領requestのresultには元job/spec SHA/final SHAに加えて、原の保存済みJSON仕様を
持つ`spec_path`（正規の`D:/CC/Tools/todo/specs/3686-32桁ID.md`）を指定する。
このpathは新しい一般読取先ではなく、実起動promptと同一のtyped仕様を検証する
ための限定metadataである。現在のToDo specが進んでいても、同じ登録履歴内の
author・実launch・PUBLIC固定入力・native入力/期待final/厳密時間窓を先に照合し、
その後だけ元結果を開く。異なる登録済みjobでも、正しいtyped起動ならjob別の
固定表を追加しない。未登録・未来世代・private/後付けscopeは拒否する。
既存の文章仕様をこのJSONへ事後置換して資格を作ることはできない。
