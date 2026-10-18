# 正式runnerの出力待ちと並列上限の追加レビュー

既存OAuth Sonnetによる有限read-onlyレビュー。原2797/2832/2840はdoneで再実行しない。
編集、保存、ToDo操作、commit/push、外部通信、認証・秘密・製品source読取、
テスト/build/Cargo/Docker/GPU/Civ6、直接子ジョブは禁止。最終応答だけで結果を返す。

元worktree D:/CC/Work/unattended-operations-20261013-wt/todo-2736-pr148-ci
固定base 41362dce88f69014e42cff793745a849b8ef45f4
固定head 0625cf259435c6784eb57c8eddc7e66cd61690a7
規則 D:/CC/AGENTS.md と元worktree/AGENTS.md。

対象は次の関数とその直接利用者だけ。広い履歴や別repo/製品へ進まない。
- tools/pr148-windows-runner/github_read_capture.go 全文
- tools/pr148-windows-runner/github_read_capture_windows_test.go 全文
- tools/pr148-windows-runner/identity_windows.go: apiErrorPreview, apiReadAttempt, apiReadLimit
- tools/pr148-windows-runner/github_read_retry.go と github_read_retry_test.go
- tools/pr148-windows-runner/README.md: Immutable source blobs段落
- tools/product-ci/pr148_linux_gate.go: pr148TranslateGate と pr148HostEnv
- tools/product-ci/pr148_linux_guest.go: pr148ExecChild のhandshake/caps
- tools/product-ci/pr148_finite_test.go: GateTranslation と GuestRejectsExcessiveCaps

git起動不可なら上記元worktreeの絶対pathを読む。固定head未照合と明記する。
同時に進行する子のworktreeの古いfileを元headの代用にしない。親が原応答と実pathを照合する。

## 修復理由と確認済み事実

2840は固定be86ae3をMust0/実検査0とした。git起動失敗があり、親が12対象のbe86→413差分0を確認。
Suggestionの実害を直す追加差分だけを今回確認する。原応答や証拠rootを開く必要はない。

1. 元StdoutPipe→boundedRead→Waitでは孫がpipeを保持すると期限後もEOFを待つ。
   現在はCmd.Runにコピー排出を任せ、WaitDelay=2秒、stdout上限超過でcontext取消し、
   stderrは8192bytesだけRAM保持。pipe timeoutはretryしない。開始API/OAuth/30秒期限は同じ。
2. 最初の実回帰では40,960,000bytesをlimit8で受け入れる失敗を検出した。
   埋込bytes.BufferのReaderFromへio.Copyが進みWrite上限を通らない原因を直し、
   stdout/stderrとも名前付きbufferへ変更。原失敗はジョブeventsに保持、最初の検査ファイルは
   同名の再実行で上書きされており、独立した原log保存とは主張しない。
3. 実Windows子と30秒保持の合成孫を使う回帰でErrWaitDelay/期限内復帰を確認し、
   合成孫をPIDで停止してWait完了。stdout超過の取消しと子排出、実stderr8192上限も成功。
   関連TestGitHubRead race10pass、PR148関連race67pass/4明示opt-in skip、両module vet成功。
   これらは製品の正式CI成功ではない。
4. 8GiB/2CPU専用namespaceは元gateへ上限2を要求済み。今回host変換とguest開始の両方で
   2を超えるcompiler/test並列数を拒否する。gateの実割当をclampして偽装しない。
   WindowsにこのLinux専用上限を適用しない。新しいkernel fixtureの重複実行はない。
5. Windowsの実reset文言 forcibly closed by the remote host を既存の有限GET retry分類へ追加。
   HTTP4xx/権限/サイズ/JSONをretryしない。transport失敗の安全なbytes/exit/status情報を保持。

全RequiredChecksSuccess=falseは正常な限定結果。正式4jobの集約成功と別物なので変更しない。
前レビューで問題なしだったsource/hooks/実job成功の必須照合/4reader素材取込は変更していない。
新sourceの正式Linux/Windows製品検査、全4job成功、PR219 mergeはまだ未実施。

## 返答

実害のあるMust / 有益なSuggestion / Nitに分類し、位置・根拠・最小修正案を書く。
読んだ全対象、固定head照合可否、実検査0を明記する。既確認の全実装を再レビューせず、
今回の追加差分の期限・排出・出力量・上限拒否・テストの有効性を確認する。
