# ToDo2593: 親2488の固定版CI確認（途中）

固定版の全体テストは失敗。成功した検査と残る検査を分けて記録する。

確認日時: 10/14/2026 02:07:43
固定HEAD/sourceSHA: 1e9cd8c90deec542d28d671d09fd1b9955d8f2ee
Git tree: ef282f643578d5949b91a7042c97fbef302735a7
PR head ref: codex/unattended-readiness-20261013 / refs/pull/144/head

| Workflow | Run | 状態 | 結果 |
|---|---:|---|---|
| Build speed | [38433222034](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222034) | completed | failure |
| CodeQL PC | [38433222531](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222531) | completed | success |
| Unattended readiness tools | [38433222500](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222500) | queued |  |
| Agent progress tools | [38433222594](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222594) | queued |  |
| Original record recovery staging | [38433222286](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222286) | queued |  |
| Channel reports | [38433222393](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222393) | completed | success |
| ChatGPT browser fixtures | [38433222360](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222360) | in_progress |  |
| Bookmark queue | [38433222089](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38433222089) | completed | success |

Build job115420510423/step9: 2145 tests run / 2144 passed / 1 failed / 5 skipped / 394.949秒。doc(step10)・alltargetsClippy(step11)はskipped。package-toolsは条件外skipped。
失敗: cc-jobs::parity end_to_end::dependency_fixture_does_not_read_its_callers_plugin_configuration。固定sourceのcc-jobs/tests/parity.rs:10597でcancelのstatus.success()がfalse。child成功・parent稼働・otherの容量待機は確認済み。取消のstderrとexit codeはassertionから出力されず、根因未確定。
親の必要範囲: dependency_contractと取消のプロセス寿命・識別照合。取消結果の診断を取得し、parityの依存fixtureと取消関連を先に確認。安全条件を緩めない。修正と必要な再検査は親担当だけ。
先行step8: job_references/verified_resumeの18passed/0failed/0skipped。孤立worktreeとprocess-tree先行も各1passed。
CodeQL4job success。解析ID1945143684(rust)、1945153206(go)、1945161626(actions)、1945179842(javascript-typescript)は同HEAD、error空・results_count0。全4jobのcheckout SHA実行行と解析upload complete行をacceptanceログへ保存。
Go Linux3jobはAPI success。Windows3job+repair-guardは未終端。

証拠: ci-2488-result.private.jsonにworkflow/job/stepのAPI結果と原ログURLを保存。build-2488.acceptance.private.logとbuild-2488-failure-context.private.logは全文ログから抽出した非秘密行。成功workflowもjob別acceptanceログに保存。全ログはGitHubの各job URL/ログAPIで親が確認可能。
旧3581928の2143passed/1failed/5skippedは親の原記録を保持。新固定版の数値と混ぜず、旧failureを成功へ変更しない。

再開: 同じ8run IDだけgh OAuthで確認。2026-10-14 02:27:27 JSTまでに未終端なら2593へwait-retryを登録する。新しい監視・再実行・手動Cargo・source変更は禁止。
親2488のnoteを保持して、初回観測とBuild失敗をCLI editで追記済み。残るworkflowの終端ログ・最終note・コミットが残る。通常のToDo状態更新はスケジューラに任せる。
範囲: 結果検証だけ。親修復完了・マージ・稼働配布完了とは扱わない。
