# 対象repoのCI受付契約

`cc/civ6-ci-contract` はToDo2962で指定された1 repoだけの読み取り専用Go package。
runner登録、source展開、status作成、Cargo実行、予算変更を行わない。
受入条件と分担は [実装契約](../../docs/todo2962-ci-contract.md) を参照。

`Plan.Validate` は固定source/run/attempt/jobとイベントの形を確かめる。
`Verify` は呼び出し元が渡す `ReadAPI` によって実repo・current source・全workflow・
元検査のOS matrix・CodeQL languages/settingsを照合する。初回PR149では
元f3e26f4を唯一の親とし、三workflow以外の全tree/path/mode不変を要求する。
merge後は実PR149のmerged記録とそのmergeを含むmain lineageも必要。
以後のworkflow変更は新しいtemplateレビューを行うまで拒否する。

`CheckEvent` は実SDK環境とイベント本文を照合する。PRのheadとmerge contextは別。
CodeQLの生成イベントには公表された安定workflow-ref本文が無いため捏造せず、
`workflow_ref` と `workflow_digest` を空、`workflow_sha` を検査sourceにする。
この場合も実repo/source/run/attempt/job/OSとlive APIの照合は必須。

matrixで共有される `GITHUB_JOB` だけではjob IDを区別できない。
helperは固有のinstance runner名で `VerifyAssigned` を開始hook/終了hookから呼び、
`RequireSuccess` でそのrunnerの正式completed/successと実stepの存在を別途確認する。
その後も実source HEAD/tree・認証不在・namespace回収の証拠が必要で、
このpackageの成功だけではCI全体の完了にならない。

`ReadAPI` の実装は既存GitHub CLI/OAuthを使い、レスポンス/出力の上限と期限を持ち、
エラー本文・secret・tokenを会話や記録へ転記しない。全API操作はGETだけ。
`Propose` は元workflow SHA256を要求し、routingとcheckout source/authを変更する。
元のall-targets検査では実行されない、依頼で必須のworkspace doctestも独立stepで補う。
元command bytesと親のconcurrencyを保持し、意図した差分を除いたYAML ASTも比較する。
`testdata` は明示的に許可された三workflowだけの元bytesで、製品source全体ではない。

検証: `go test -race ./...`、`go vet ./...`、`go build ./...`。
fixture検証と公式Actionsによる実受入は別に記録する。
