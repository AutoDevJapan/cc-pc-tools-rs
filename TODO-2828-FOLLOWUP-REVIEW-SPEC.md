# 元2828の提案修正だけを別モデルで確認する

読み取り専用の有限レビューです。元2828の実装はgpt-6.1-sol、原レビュー2830は
gpt-6-lunaであり、job.jsonと正規rolloutのturn_context.modelを実装担当が照合済みです。
tools/windows-ci-runner-2737/reviews/receipt.jsonにthreadと確認元、原応答hashを保存。
今回はgpt-6-lunaを指定します。実modelを最終返信に明記してください。

原2830はbec99388の全23fileを読みMust0/Suggestion2でした。今回はその2提案を採用した
新差分だけです。元の23file全体や既存の解決済みM1/M2を再レビューしないこと。
実装/検査/ビルド/VM/SCM/runner/原source/Cargo/GPU/API-key/PR/merge/Discord操作は0件。
子Codexを直接起動しない。結果ファイル/commit/ToDo更新は不要、最終返信で結果を返す。
元2828や親2737の完了を待たない。通常schedulerのreview ToDoとして完了を返す。

## 固定対象

AutoDevJapan/cc-pc-tools-rs repo1406818713 private/admin/nonfork。
実装worktree:
D:/CC/Work/unattended-operations-20261013-wt/todo-2828-godd-mcp-windows-ci-guest
branch scheduler/todo-2828-godd-mcp-windows-ci-guest
base 93c0c42377a47f1e3e01d8162dcebbc1f730b4aa
head b60e85e7f66abb3329264bfc74895a3d3c2be10f
tree e192d0a34d9c546e146575681d3d9365c6185ee0

git show固定head、またはread-only GitHub APIで同じ固定Git blobを読む。
現在のworktree HEADと混同せず、開始/終了に固定head/treeを照合する。
元commit全13fileのうち、機能と説明のレビュー対象は次の6fileだけ:

- tools/windows-ci-runner-2737/host.go
- tools/windows-ci-runner-2737/logbound_windows.go
- tools/windows-ci-runner-2737/review_regression_test.go
- tools/windows-ci-runner-2737/logbound_windows_test.go
- tools/windows-ci-runner-2737/README.md（追加記述）
- tools/windows-ci-runner-2737/HANDOFF.md（追加記述）

その他は原レビュー応答・hash・モデル確認元の保存のみ。原応答は改変せず扱う。
必要な局所参照は同moduleのsession.go/contract.go/session_test.goのみ許可。
AGENTS.md/docs/test-build-scope.mdとD:/CC/AGENTS.mdを適用する。
guardはscrub391 --guard-pathで対象を判定するのみ。client一覧やguard内部を読まない。
元製品source/owner-work/登録manifest/2783 folderや原diskは参照禁止。

## 差分の目的と不変条件

1. 診断ログの探索が空directoryでも無制限にならないよう総2048entryと深さ16で停止。
   ReadDir(64)を用い、callback前に巨大な全entry配列をロードしない。
   entry間と再帰前でctx取消しを確認する。既存の1024file/64MiB制限は保持。
   100msのsamplingなので瞬間的な超過はあり、ディスクquotaとは主張しない。
2. Failed eventの記録前に起動済み段階と実numeric runnerIDを照合。
   timeout/processはDrained=true/Exit値/有効hashが必要。
   drain-unconfirmedはDrained=falseのみ、未取得Exit/hashは空でもよい。
   矛盾したtelemetryはErrProtocolでObserve前に拒否。正しいtimeoutはDeadlineExceeded。
   未確認drainはtimeoutやcleanup ackと扱わず、親がVM/leaseを保持する。

host gate/registration/VM回収は親の責任、guestのenvはgate証拠でない。
source/nonce/RunID/JobID/Role/RunnerID/Inputsの束縛は緩めない。
固定SDK/SCM/COM/TLS、stock runner13工程、正常時hook/runner-exitedは変更していない。
公開Go APIのsignatureは変えていない。正常返り値の意味も変えていない。

## 検証済み

実Windows/Go1.27.0で追加regression成功2.618s、新module全体race成功4.280s、vet成功。
空dir2049件/深さ17拒否、smalltree成功と取消し、実HostSessionを使う合成channelで
誤runner/起動前/排出矛盾/Exit/hash不備は保存0、正しいtimeoutと未確認drainを別々に確認。
旧sourcepacket/SDK/normalciは変更しておらず前回の有効検査結果を再利用。
mockとテスト専用Windows子プロセスだけで、実VM・stock runner・原13工程は未実施。

## 返信

日本語で4000字以内。固定base/head/tree、実model、読んだ6file/未読、静的確認tests0を明示。
Must/Suggestion/Nit、file:lineと具体的trigger/実害/有限な修正案。Must0なら明記。
親2737や製品CIの完了に読み替えず、実機未確認事項は要実機確認として区別する。
