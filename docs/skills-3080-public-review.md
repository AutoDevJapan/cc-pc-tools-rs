# 公開入力に結ぶ読み取り専用レビュー

`--receive-skills3080-result`の新しい3080子レビューは、起動前に保存した仕様の
**全体**を`skills-3080-public-review-v1`のJSONにする。後からrequestへscopeや
public boolを足す方法ではない。原authorと現在のconsumerは別々に照合する。

JSONは`repair-guard/skills3080_public_review_windows.go`の
`skillsPublicReviewScope`の全fieldだけを持つ。`instructions`は同ファイルの
`skillsPublicReviewInstructions`と完全一致させる。任意の追加指示、未知field、
JSON後の文章、重複keyは拒否する。実データとGitHubの証明が必要なため、以下の
説明用placeholderをそのまま登録しても資格は得られない。

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

filesはmanifestを必ず含み、`.gitattributes`または既存のSkills3080文書・公開証拠の
名前空間に限定する。各fileの相対path、Git blob ID、SHA256、byte数を宣言する。
最大128file、各16MiB、合計32MiB。一般の公開repo・他製品・私的scopeには流用しない。

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
