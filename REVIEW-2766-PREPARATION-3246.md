状態: 完了

読取レビューだけを完了しました（修正・commit・ToDo更新なし）。Must は0件、Suggestion 3件、Nit 3件です。修正は2766へ返します。

固定 base f01265d（tree fa07fc5）から head 8a68679（tree 55f7902、12ファイル）の Git blob を読みました。worktree の現HEAD 1784c0d には追従せず、行番号も固定head基準です。指定12ファイルは全文読了し、周辺の normalci・ci-guest-2737・windows-ci-runner-2737 は関連部分だけ読みました。実行は固定headの.go 10個への gofmt -l（差分なし）だけで、go test・vet、CI、Linux、原製品の再取得、3つのblob定数の実値照合は0（未実施）です。未読は normal_ci_admission.go 本体、`secure_files*`、source_materialization.go です。

問題なしと確認した点:

- 元MCP α: profile.go:16 で保持し、Draft/base検査は Press のみ（sourcepacket.go:237）。未知product・未知workflow・部分指定・CodeQLは profile.go:19-26 の閉じたswitchで拒否し、Fetch:141・Decode:406・VerifyCheckout:30 がAPI/IOより前に検査。
- 同一性: sourcepacket.go:227-246 が repo ID・owner・名前・private・非fork・admin・open・Draft・base・head・ref・同repoを網羅し、Fetch は前後で再検証。物理proofは選択workflowのパスとSHA-256に束縛（:436、verify.go:113）、Decode:419 が Product/Workflow 込みで完全一致を要求。
- outline: 13 job（10+2+1）、4 shard、2OS、fail-fast false を固定し、job追加・name付与を拒否（outline.go:148-217）。blobID照合と前後verifyがあり、失敗時は stdout なし（main.go:616）。RunnerAdmission と OfficialCISuccess は常に false。main.go の既存分岐と candidate 値は不変。
- 公式資料: [Workflow syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax) の timeout-minutes 省略時360分は outline.go:166 と同じ。[Events that trigger workflows](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows) では pull_request の GITHUB_SHA が merge commit で head.sha と別になり、後続で固定するとした README の説明と合う。matrix job名の表示も公式例（Using jobs in a workflow）と一致。

Suggestion:

1. outline.go:43-47,130-138 と product-ci/README.md:11: events は dispatch の bool だけなのに「events are kept」とある。キー名一覧を足すか、文言を「dispatchの有無のみ」に直す。
2. profile_test.go:66-83: Product=press-2766 で Workflow 空、Head.Ref違い、Head.Repo.ID違い、Head/Base FullName違いの行が無い（コードは対応済み）。4行足す。
3. ci-source-2737/README.md:14-15「既存guestはPress bindingを拒否」は normalci/admission.go:71 のα固定が根拠で、否定テストが無い。validSourcePin（ci-guest-2737/job_contract.go:23）と Plan.Validate（windows-ci-runner-2737/contract.go:39）に Product/Workflow 空を要求し、拒否テストを足す。

Nit: ci-source-2737/README.md:21-24 が ci.yml 前提のまま。main.go:734 の usage に press-ci-outline が無い。outline.go:49 の Repository が stdout に出る（顧客案件ではないので漏えいではないが、codeql.go:44 の慣行は `json:"-"`）。

未確認のリスク（拒否側に倒れる）: 実Press treeが validateEntries と128MiB・20万件の上限を通るか、commit復元が +0000/+0900 だけを試す点（sourcepacket.go:275）は、実Fetchで初めて分かる。