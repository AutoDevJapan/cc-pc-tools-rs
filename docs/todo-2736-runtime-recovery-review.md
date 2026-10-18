# 両OSの正式runner修復差分の有限レビュー

root2736の新差分だけを既存OAuthサブスクのSonnetで読み取り専用レビューする。
原2797とレビュー2832は完了済みで、固定spec/モデル/結果を変更・複製・再実行しない。
今回は正式実行で新たに判明した別の失敗を修復した追加差分。
編集、ToDo更新、commit/push/PR/外部送信、テスト/Cargo/Docker/GPU/Civ6、子ジョブ起動、
認証・秘密・原製品source・旧profileの読取は禁止。結果は最終応答だけに返す。

## 固定範囲

worktree D:/CC/Work/unattended-operations-20261013-wt/todo-2736-pr148-ci
base 52dd6a616ad18f7b40ac110bbd87387dc7bc7e04
head be86ae33eee6ce0e6ac96b29b551ab20034227f9

次の追加/変更箇所と必要な関数だけを読む。広い履歴や他のrepoを探索しない。
- tools/product-ci/pr148_linux_gate.go: pr148HostEnvとその受渡し先
- tools/product-ci/pr148_linux_run.go: listener終了後、既存cleanup defer、terminal job照合
- tools/product-ci/pr148_listener_result_test.go
- tools/product-ci/pr148_finite_test.go: normal Rust fixtureの並列数検証
- tools/product-ci/pr148_review_regression_test.go: HostEnv capの追加検査
- tools/pr148-windows-runner/identity_windows.go: API読取・capture・retry・deadlineのみ
- tools/pr148-windows-runner/github_read_retry.go と github_read_retry_test.go
- tools/pr148-windows-runner/materialize.go と materialize_parallel_test.go
- 両module READMEの今回追記と、TODO-2736-CHECKPOINT.mdの最新01:15節

規則はD:/CC/AGENTS.mdと当worktree/AGENTS.md。新しいfull testは実行しない。
gitを起動できない場合は元2736の上記絶対pathの作業コピーだけを読み、固定headとの
一致を自分で確認できなかったことを明記する。親が原応答と読取pathを照合し直す。
原listener logや他の恒久evidenceは読まない。必要な事実は以下に限定して提示する。

## 確認済みの事実と境界

1. 正式Linux jobは03c7cc6/a6851bd固定でcheckout/Rust/fmtに成功したが、Rust/Lua検査は
   linker SIG9でexit101。8GiB/2CPU namespaceへhostの最大10並列が渡る不整合を発見。
   OOMのkernel実測は残っておらず断定しない。修正はCARGO_BUILD_JOBS/RUST_TEST_THREADS/
   NEXTEST_TEST_THREADSの上限2を通常の既存gateへ要求するだけ。gateは低い要求をclampし、
   実割当をguestへ渡す。隔離のFS/権限/容量、検査コマンド/OS/sourceを変更しない。
2. 実SDK・kernel・共有gateの認証なしnormal Cargo fixtureは20.51秒でPASS。実guestの
   granted値1〜2と子孫排出を確認。製品全件成功を意味しない。
3. 同job後、公式listenerはread-only /runner/.credentialsのSDK unlink拒否でexit2。
   内容は未読。既存のwhole-namespace teardownと登録削除・lock解放は成功済み。
   今回はその正確なSDKメッセージとexit2だけを別記し、両source hook・全source/run照合・
   APIの当該owned runner/job/run/attempt・completed/successを必須にしている。
   それ以外のlistener異常/failed/skipped/cancelledは受入不可。SDK file削除の別経路や
   FS権限拡大は追加せず、既に存在する全namespace破棄だけを維持。deferのcleanup失敗も
   親へ返す。新コードを不適切と判断したら、その根拠と具体的なMustを明示してほしい。
4. Windowsのread-only診断で異なるblob GETが約10.13秒/exit1/応答0byte/TLS timeout。
   同じGETは後に成功。通常CLIでもTLS handshake timeoutを実測。auth/rate不在は推測しない。
   1109 unique blobを逐次取得する時間がprepareの8分上限を圧迫するため4readerへ限定。
   元のblob/hash/tree/256MiB/期限検査、同repo/同source、GitHub gate/OAuthは維持。
5. 各GETはTLS handshake timeout/EOF/resetだけ最大3試行・有限backoff。HTTP4xx/auth/policy/
   size/JSON不正はretryしない。parent deadline後の成功も拒否。stderrはRAMの8KiB preview
   だけで、生本文/秘密/製品sourceを保存・表示しない。errorはrequest hash/bytes/exit/status。
   取得workerは成功・失敗どちらも全排出後に返り、全byte検証前のfile書込はしない。
6. Windows race検査78pass/2skip、関連Go53pass/4skip、その後のclassifier/terminal追加回帰、
   両module vetとWindows/Linux buildは親が確認。これはレビュー者自身の検査ではない。
   新版の正式job/Windows prepare再試行はまだ実施していない。旧prepare49ffは再実行しない。
   Linux startupの旧2832レビューはGit失敗を親が補足した静的Must0で、今回の追加差分を
   無条件に覆うとは扱わない。

## 観点と最終応答

上限要求がゲートの代用にならないか、既存cleanup refusalを誤って成功化しないか、
実GitHub結果/両hook/sourceの条件が必須か、deadline後の成功、retryの拒否条件、4readerの
race/失敗時排出、元のbyte/RAM/tree境界の欠落、原証拠の資格を点検する。
今回の共有CI側Go CodeQL bridge403や旧フィルター修復を設計し直さない。

日本語3000字以内。base/head、読んだfileと範囲、未読、実検査0を明記。
Must/Suggestion/Nit、file:line、根拠と具体的修正に分類し、Mustなしも明記。
根拠のない成功証明や本人への技術質問を追加しない。
