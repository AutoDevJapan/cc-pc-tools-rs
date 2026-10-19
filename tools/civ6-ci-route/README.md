# PR149の検査経路の反映

ToDo2962の3workflowだけを変更する有限Goツール。対象repo/PR/head/base/branchを固定し、
共有contract.Proposeが生成した検査経路とdoctestだけを反映する。
runner登録、dispatch、status作成、予算変更、製品source全体の展開は行わない。

`prepare <safe-delete所有root>` は元repo/PR/HEAD/base/branch/treeとworkflow blobを
再照合し、三candidateと指紋を保存するだけ。GitHubへの書込はゼロ。
`publish <同root>` は保存bytesと現在の固定sourceを再照合し、三workflowのtreeと
sole-parent commitを作る。全recursive tree/path/mode/typeを比較した後だけ、
元branchへ非forceで1回更新する。古いsourceや候補変更は更新前に拒否する。

各変更前にexclusive journalをfsyncする。失敗・不確かな結果の再実行・revert・
別経路への切替をしない。ref応答が失われた場合は1回読み取って結果を保存するが、
成功とは返さず原journalを保つ。同rootの再publishは書込前に止まる。

親は別モデルレビューとOS/queueの供給・回収が成立してからpublishを実行する。
このツールの成功はworkflow反映だけで、CI成功やmergeの証拠ではない。
正式PR149最大6job、merge後最大6jobの予算は既存queue台帳が別途保持する。

既存gh_gate.exe/OAuthを利用。APIキー環境変数を除去し、secretやstderr本文を保存しない。
実行はCREATE_NO_WINDOW。新tempはsafe-deleteの所有手順で保持・回収する。
`go test -race ./...`、`go vet ./...`、`go build ./...`で関連検査を行う。
