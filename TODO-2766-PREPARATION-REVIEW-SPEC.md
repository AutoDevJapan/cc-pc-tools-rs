# GoDD Pressのソース照合と元検査一覧を確認する（元2766）

あなたは共有ToDoから割り当てられた別モデルの読取レビュー担当です。子ジョブを起動せず、既存OAuthの割当モデルで読んでください。元2766・親2642を依存先にせず、読了結果を最終返信へ返してください。

## 対象と固定版

- 修復repo実ID1406818713 / AutoDevJapan/cc-pc-tools-rs。修復worktree D:/CC/Work/unattended-operations-20261013-wt/todo-2766-godd-press-ci-2642。guard確認済みclient_resource=false。原製品のPCフォルダは対象外。
- base f01265d40618dc45a0e9747a60c5ef4ecc62b61dからhead 8a686793f397b1fa0d473e4b673332990228621dへの12ファイル差分。固定headのGit blobを読み、実装者が後で進めたworktree HEADへ追従しない。
- 全文を読む対象:
  - tools/ci-source-2737/profile.go
  - tools/ci-source-2737/profile_test.go
  - tools/ci-source-2737/sourcepacket.go
  - tools/ci-source-2737/sourcepacket_test.go
  - tools/ci-source-2737/verify.go
  - tools/ci-source-2737/README.md
  - tools/product-ci/pressci/outline.go
  - tools/product-ci/pressci/outline_test.go
  - tools/product-ci/main.go
  - tools/product-ci/main_test.go
  - tools/product-ci/merged_job.go
  - tools/product-ci/README.md
- 必要な局所だけ追加参照可: sourcepacketの既存Encode/Decode/Materialize/validateEntries・secureRoot・cleanGitEnv、normalci.ParseYAML/Field/Verify、既存normal_ci_admission.goとnormalci/admission.go、mainのget/command、merged_jobの元検査。新たな製品全体監査や別製品の調査を行わない。
- 継承した基盤は2737の最終7b8dbb6。今回はそのVM/SDK/runnerを複製せず、修復branchへmergeして再利用した。mainの競合では既存merged-inspect/merged-codeql-jobとMCP通常CIの分岐を両方保持した。別製品candidateの値は2766開始版から変えていない。型alias後のunkeyed literalをkeyedにした変更は値不変。

## 仕様

この段階はPressの有限な準備だけで、通常CI成功・実guest・runner admission・route公開を完了する差分ではありません。

- 空Product/Workflowの元PinはMCP αのまま。
- Product=press-2766では実repo1386702183/AutoDevJapan/GoDD-Press/private/admin/nonfork、Draft PR175、固定ref fix/todo2642-schedule-predecessor-overflow、固定base4dc5c332d54d19325f122df1f0e4a52e9dd3a629だけを許す。head/tree/workflow digestは明示pin。
- Workflowはlocal-checks.yml/rust-v2-https.yml/windows-hidden-fixtures.ymlの3つだけ。未知profile/部分指定/CodeQL/任意pathをAPI前に拒否する。全blob/tree/commitと選択workflowの物理照合は元実装を共用する。
- press-ci-outlineは元b2b6d47b7f9e17617d16a9079a70de272bc3c04c/tree dac21168dfdb55b007da5c06f36c05e66de0a9f7の3元Git blobを完全hash照合し、前後の元PR/ref/base/treeを再検証。metadataだけを返し、runner admissionとofficial successはfalse。
- 元13 job（local-checks10、V2 HTTPS両OS2、Windows hidden1）を落とさない。4shard・2OS matrix・元timeout・eventを保持する。local-gatesのtimeout省略は元GitHub default360分。
- 2つの元PR専用workflowにはdispatchがない。今段階でevent/checkoutを追加・変更したり、未接続のPress bindingを許していない。後続で元PR merge SHAとcandidate SHAを正しく固定する技術作業が残る。未実装の後続を、この限定準備の実装済み主張へ読み替えない。
- API要求の製品sourceはRAMのみ。元製品folder/名簿/保護一覧/guard/manifest/秘密値は読まない・変更しない。APIキーAI/GPU/Civ6/新費用/予算/本人PC操作/Discord送信/製品PR merge/配布は不可。

## 確認済み事実

- 実APIで元repo/PR175 open Draft/同repo/ref/head/baseと3workflow blobを照合。原AGENTS全文と全workflowをRAM読取。CLAUDE.mdは404。製品sourceのホストcheckoutは作っていない。
- 2737はdone、同固定sourceで公式run39201344542 attempt9成功を元note/checkpointで受領。元レビュー3230のMust0とWindows13/最終Gate/sourceproof/回収の記録を読んだ。これをPressの成功とはしない。
- Windowsのsourcepacket全module race成功57.438s。新3workflowの公開合成Git fixtureを実Materialize/VerifyCheckoutし、正確なhead/tree/all blobsと改変拒否、別workflow pin拒否を確認。原製品の実source Fetch/guestは未実施。最初の直接起動は終了コード取得がnullのままだが、完全stdoutはパッケージokを返した。このnullをexit0と捏造しない。vet exit0。
- product-ci全module race: main1.622s / normalci1.066s / pressci1.066s、exit0。静的検査の初回はmerge後の型aliasと古いunkeyed literal9箇所により失敗。値不変でkeyedへ修正しvet exit0。型付けだけの変更で同じ成功検査を重複していない。
- 利用側ci-guest race14.360s/exit0、Windows runner race7.483s/exit0。実VM/公式job/元製品Rustを回した結果ではない。
- press-ci-outlineを実APIで1回実行しexit0。3元blob/10+2+1job/dispatch true,false,false/SourceRevalidated=true/RunnerAdmission=false/OfficialCISuccess=falseを確認。workflow bytes/source commandsはホストへ保存・stdout出力せずmetadataのみ。
- git diff --check成功。作業branchの8a686793をpushしremote ref一致。独立PR/merge/配布は行っていない。

## 観点

profile混同、元MCP αの後退、未知workflowの受付、API前拒否、Draft/base/ref/owner/同repoの取りこぼし、物理proofの別repo/別workflow読み替え、13jobの欠落や元条件改変、partial outlineを成功へ昇格する経路、source/秘密値の保存や出力を確かめてください。後続executor未接続は明記した範囲であり、同じ指摘だけで全製品監査を追加しないでください。

## 一次資料と規則

このworktreeのAGENTS.mdとD:/CC/AGENTS.mdを読む。原製品の新規API取得/CLI実行/guest起動/保護一覧読取は不要。
GitHub公式: https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#pull_request 、https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idtimeout-minutes 。

## 返却

全指定範囲を読み指摘を返したら先頭は「状態: 完了」。次の段落で読取レビューだけの完了と書く。固定base/head/tree、全文読了ファイルと追加局所、未読を明記し、Must/Suggestion/Nitをfile:line・具体的修正とともに返す。実行していない検査は0/未実施とする。Mustがあってもレビューを終え、修正は2766へ返す。結果ファイル保存・commit・ToDo更新・子担当起動は不要。最終返信2500字以内。実modelは実装側が実job/対応turn_contextで照合するので自己推測しない。
