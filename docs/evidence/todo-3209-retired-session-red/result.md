指定の3コマンドを各1回だけ実行し、製品の不具合による失敗2件、陽性対照4件の合格、fixture失敗1件を確認した。コンパイル失敗は0件。
これは元ToDo3081/Issue277の有限な比較検査の結果であり、製品の完成や修正後の合格を示さない。

比較sourceは`565ec9bf6d0145721884978eb73a05b03cf3b623`、treeは`5b5916dd3decb88b12255c19cd983f744ccd1621`。
実行時HEADは3回とも同source。製品基準`04e02ba04e3e6e635e1954295f88ce24e170c3b9`との差分はテスト2ファイル65行だけ（comparison.patch / comparison.numstat.txt）。
自分の作業ブランチは`scheduler/todo-3209-retired-session-check`。親・他担当の作業場所を変更していない。

| コマンド | 開始〜終了（2026-10-21、日本時間） | 終了コード | 選択した検査の実結果 |
|---|---|---:|---|
| cc-scheduler / parity / 指定filter | 07:34:18.6761481〜07:34:53.1489147 | 100 | 1合格、製品Red1、fixture失敗1、187選択外 |
| cc-ask / resume / 指定filter | 07:34:53.3442388〜07:35:17.9814277 | 100 | 1合格、製品Red1、83選択外 |
| cc-job-policy / resume_policy / 指定filter | 07:35:18.2053778〜07:35:24.3329091 | 0 | 陽性対照2合格、7選択外 |

正確な小数秒は各`run-0N.result.json`のstart/endを正本とする。完全なstdout/stderrは対応する`.stdout.log`/`.stderr.log`に保存した。
stdoutは3回とも0バイトで、Cargo/nextestの出力と各テストのstdout/stderr区画は親のstderrログにある。
runごとのstarted/running/resultに要求引数、ソース、PID、実時刻、終了コード、資源を保存した。再実行はしていない。

| 検査 | 分類とassertの到達 |
|---|---|
| retired_native_conversation_is_not_a_successful_continuation_either | **製品Red**。parity.rs:3723の正常な継続候補ありは合格。退役印を付けた後の3725へ到達し、候補なしの期待に反して失敗した。 |
| retired_native_conversation_cannot_cross_a_cancelled_prestart_handoff | **fixture失敗**。parity.rs:3707へ到達し、期待todoに対して実blockで停止。3709のprepareと、3710（旧会話の不使用）/3712（model）/3713（effort）/3714（spec）/3715（元job）/3716（保存ファイル）のassertはすべて未到達。この取消ケースで製品Redを実証したとは数えない。 |
| completed_capacity_history_does_not_exempt_an_ordinary_discord_resume | **製品Red**。resume.rs:770の起動数1は合格。771へ到達し、期待gpt-6.1-solに対して実gpt-6-lunaで失敗。772（effort）/773（会話ID）/774（元の予算と証拠の保持）は未到達。 |
| preparation2164_reuses_owned_terminal_session_only_without_other_worker | **陽性対照合格**。parity.rs:3673、3679、3680、3682の全4assertへ到達し合格。正しい所有・同じフォルダでの会話保持、所有違い・別フォルダ・他worker使用中の除外を確認した。 |
| every_discord_resume_launcher_applies_policy_before_passing_a_native_session | **陽性対照合格**。resume.rs:718〜723の6assertがpolicy/owner_requestの両方で到達し合格（計12）。policyはSol/max/新会話、本人個別指定はLuna/high/元会話。起動数・権限・作業場所も確認した。 |
| an_eligible_same_model_capacity_attempt_keeps_its_context_and_durable_budget | **陽性対照合格**。resume_policy.rs:56のjob全体一致に到達し合格。現在の混雑再試行の会話、時刻、予算、履歴を保持した。 |
| individual_owner_selection_keeps_the_model_effort_and_compatible_context | **陽性対照合格**。resume_policy.rs:44のjob全体一致に到達し合格。本人の個別model/effortと互換会話を保持した。 |

fixture失敗の理由は読み取りで特定した。parity.rs:3697は`recovery_cancel.committed_at`へ`"saved"`を入れている。
cc-job-policy/src/cancellation.rsのrecovery_cancelはRFC3339の時刻を要求する。この入力には代わりのpending_cancel/stall_recoveryも無く、取消の理由が確認できないためblocks_recoveryがtrueになる。
cc-scheduler/src/engine.rsのreconcile_itemはその場合にstatusをblockへ設定する。製品やfixtureは変更しておらず、この検査の後半を追加実行していない。

製品Redの静的な原因も比較sourceで確認した。continuation_session（cc-scheduler/src/policy.rs）はthread_idを返す前にpolicy_resume_resetを見ていない。
recovery_sessionの取消経路（preparation.rs）にも同じ照合が無いが、取消ケースの動的な証明は上記fixtureで未到達のため未確認のまま残す。
capacity_attempt（cc-job-policy/src/launch_context.rs）は終了後も残る再試行回数・capacity_historyを条件に含む。
その結果、通常Discord起動の共通入口から呼ばれるapply_managed_launch_policyがモデル移行を除外する。実観測は771の旧モデル残存までで、後続の会話ID等を観測済みとはしない。

割当資源はcargoだけ。共有cargo_gate.exeをCREATE_NO_WINDOWで呼び、同じ所有付き専用targetと既存sccacheを3回とも直列で使った。
要求はbuild/testとも最大10並列。保存したゲート設定はjobs_per_build=8で、ゲートが要求をさらに制限する。実プロセスの同時数やCPU使用率を別途実測したとはしない。
各runの前後に32論理CPU・物理メモリ容量/空き容量を実測して保存した。GPUモデル・Civ6・実AI要求・APIキーは使っていない。

Green、Clippy、全workspace、release、配布、過去2362件や既存Redの再実行は行っていない。独立PR・mergeも行わない。
本件は別モデルによる製品レビューではない。修正とその後の検査・別モデルレビュー・統合判断は元3081が継続する。
所有生成物の後片付けと最終checkpointの記録はRESUME.mdとcleanup記録を参照する。
