状態: 完了

読取レビューだけを完了しました（修正・commit・ToDo更新・子ジョブ・Discord送信なし）。Must 1件、Suggestion 3件、Nit 2件で、修正は2766が進めます。

対象は base 1784c0d / head 68ac605 / tree 72cc7c6 の32ファイルで、固定head blobを全文読了しました（行番号は固定head基準）。追加で全文読んだのは normalci の4ファイル、ci-guest の guest_linux・gate_windows・protocol・job_rust_probe_linux、windows-ci-runner の host・channel・session です。未読は secure_files*、source_materialization、normal_ci_admission本体、worker_*、windows_clone、テスト補助ほかです。

**Must**

1. 元timeoutの外側時間が一部しか動的でなく、実行が約40分（Linux）か26分（Windows）を超えるとPressのjobが打ち切られます。13 role中12 roleの元timeoutは90または360分です。

   - Linux: `guest_linux.go:78` の接続期限40分と `:345` のguest寿命45分が固定です。`job_linux.go:169` はctxだけ伸ばすため、期限切れで読取goroutine（`job_linux.go:216-218`）がcancelしrunnerを止めます。
   - Windows: `host.go:40,51` と `channel.go:96` が26分固定で、`guest_windows.go:268` の `timeoutMinutes()+3` は親ctxを超えられません。
   - 修正: 期限をplanのrole timeoutから導出します（Linuxはlive-job受理時、Windowsはseed.Planから）。α側の26/40/45分は保ち、13 role全てで実効期限 ≥ role timeout を確かめる表テストを足してください。

**Suggestion**

1. PR実行の `run.path` と `head_sha` の形が未確定です（`admission.go:121,153`、`job_windows.go:458`。テストfixtureは自作）。検索結果が挙げる [Events that trigger workflows](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows) と [REST workflow runs](https://docs.github.com/en/rest/actions/workflow-runs) の要約では、pull_request の run の `head_sha` は通常マージコミットで、`path` は `…yml@refs/pull/N/merge` 形になり得ます。コードは候補headと素のpathを前提にするため、この要約が正なら3つのPR実行roleは全て拒否されます（安全側）。要約は二次情報で本文は未確認です。αのdispatch runは素のpath比較（`normalci/admission.go:103`）を通過済みと読めます。route公開前にPR175の既存pull_request runを読取GETで1件確認し、期待値を1通りに固定してfixtureを実形に直してください。
2. `job_windows.go:101` と `contract.go:87` は `JobName != ""` だけです。Pressは `b.JobName == policy.Name` まで束縛し、`job_policy_windows_test.go:53-60` に改変の拒否を足してください。
3. `admission_test.go:92-95` は workflowごとに1 roleしか `plan` を通さず、shared-shard（`:227-229`）の中身はattempt違いです。13 role全てとshard間のlabel入替えを足してください。

**Nit**: `sourcepacket.go:166-169,171,176` と `signed_payload_test.go:9-20` はgofmt未整形に見えます（gofmt未実行）。`job_rust_probe_linux.go:19` はα作業場所とRust 1.97.1固定で、Pressの診断が失敗します。

**問題なしと確認した点**: 空profileはmerge値を拒否し（`profile.go:18,24`）、未知productはα経路に入りません（`job_policy.go:10-21`、`admission.go:51`）。2親の順序はAPI・Fetch/Decode・guest物理Git（私的snapshot）の3箇所で照合します（`remote.go:23`、`sourcepacket.go:467`、`verify.go:91-95`）。routeは置換した `runs-on` 以外を連結で保ち（`route.go:171-187`）、cleanup（`job_windows.go:123-137`）とACL（`identity_windows.go:120-131`）はPress側のrepoとpathに切り替わります。`route.go:112` の github・matrix・vars は、検索結果の Contexts reference でも `runs-on` で使える context です。`format()` と、PR実行の `GITHUB_WORKFLOW_SHA` がマージコミットになる点（`job_contract.go:78`、`contract.go:106`）は検索で確認できず（4件目は上限）、私の知識に基づくため古い可能性があります。

**検査**: 実行は固定head blobへの git show・diff・grep と Web検索3件だけです。go test・vet・gofmt、実API取得、route公開、dispatch、runner登録、公式CIは0（未実施）です。この68acはlive利用や公式CI成功の根拠になりません。