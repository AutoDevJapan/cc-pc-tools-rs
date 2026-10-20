# ToDo3154: 固定版の有限なRed検査

指定4コマンドを各1回実行し、すべてコンパイル後の実exit100を確認した。
選択した計7テストは0合格・7失敗。コンパイルエラーによるRedは0件。
ただし、単独reviewの1件はテスト補助が加えた引数と競合しており、意図した作者判定に未到達。
失敗を修正せず、元ToDo3081 / [Issue277](https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/277)へ返す独立checkの証跡である。

| 実行 | 固定した実HEAD | 選択した検査 | 実exit | 結果 |
|---|---|---|---|---|
| 01 | `52bc2dd900b68bfee86e3f5acf9d7378d0c760db` | cc-job-policy / model_effort / 指定1件 | 100 | 0合格・1失敗・2選択外 |
| 02 | 同上 | cc-jobs / model_effort / 指定2件 | 100 | 0合格・2失敗・2選択外 |
| 03 | 同上 | cc-jobs / parity / 指定3件 | 100 | 0合格・3失敗・388選択外 |
| 04 | `5a82f08a57d0774c4e01fb0e8760a57a750a13c2` | cc-job-policy / finite_review | 100 | 0合格・1失敗 |

04は01〜03の終了後、指定の比較版へ切り替えて実行した。
`52bc2dd..5a82f08`の変更は`cc-job-policy/tests/finite_review.rs`の追加だけ。
01〜03の対象版を変更していない。4回とも同じ専用targetとsccacheを利用した。

| テスト | 実際に失敗した箇所 | 到達していない確認 |
|---|---|---|
| `review_aliases_are_author_identities_and_preview_is_not_review` | model_effort.rs:5。`same_model("opus", "anthropic/claude-opus-5-5")` がfalse | Sonnet別名、preview判定 |
| `managed_legacy_hints_use_max_while_a_recorded_owner_override_survives` | model_effort.rs:45。最初のSolループでmaxの起動引数が見つからない | 同ループのowner override、次のClaudeループ全部 |
| `unknown_claude_keeps_its_identity_and_disables_inherited_gpt_fast` | model_effort.rs:69。`service_tier=default` が見つからない。モデル名を保つ最初のassertは通過 | `features.fast_mode=false` |
| `standalone_review_registers_without_guessing_authors_from_a_shared_cwd` | parity.rs:1947。登録呼出しのunwrapがErr（code1）で失敗 | 登録後のmodel、作者未確定、作者一覧 |
| `queued_capacity_jobs_cannot_hold_replaced_missing_or_unlinked_todos_forever` | parity.rs:1968。最初の`replaced`で実statusが`queued`、期待`failed`。直前のadmitがNoneのassertは通過 | 同ループのspawn無し、`missing`と`unlinked`の全assert |
| `help_and_parser_errors_match_original_python_bytes_and_codes` | parity.rs:11876。`["run", "-h"]` の標準出力が旧baselineと不一致 | 当該ケースのstderr、残り26ケース、末尾の呼出し/ジョブ数assert |
| `finite_root_review_closes_only_its_bound_spec_and_keeps_parent_work_separate` | finite_review.rs:8。`completed(&job, &format!("{COMPLETE}\nMust0。親の検査と配布は未完です。"))` がfalse。COMPLETEの値は「状態: 完了（レビュー作業のみ）」 | spec照合、spec変更拒否、書込担当拒否、有限範囲の紐付け無しを拒否する各assert |

単独reviewの実エラー文は `--model and --auto-model are mutually exclusive`。
同じ固定版の`Test::run`（parity.rs:6585〜6602）が`--model m`を加え、テストが追加した
`--auto-model`と競合する。parserはcli.rs:277〜280で拒否する。
このため、要求にある「作者不明の登録拒否/不正な作者推定」の振る舞いを実証したとは扱わない。
このcheckではテスト補助も修正・再実行していない。

help検査は29ケースであることを確認するassertと、最初の`-h`・`--help`の
code/stdout/stderr比較を通過した。3ケース目の`run -h`ではcode0と、許容する
original-product-retry-keyの2つの出現数assertも通過した。
その許容部分を取り除いた後にも`--review-source-jobs`と`--review-excluded-models`の説明が残り、
旧baselineとのstdout比較で失敗した。後続を実行済みとは数えない。

完全stdout/stderrは各`*.stdout.log` / `*.stderr.log`へ生バイトのまま保存した。
4回ともstdoutファイルは0 byteで、nextestの結果と各テストのstdout/stderrはstderrログにある。
実引数・実HEAD・時刻・gateのPID/作成時刻・実exitは各jsonに保存した。
`--no-fail-fast --status-level all`で全選択結果を回収し、392件の選択外を実行済みにしない。
生ログのSHA256を実行metadataと照合し、Gitの改行変換をこの証跡フォルダー内だけ無効にした。

共有CargoゲートをCreateNoWindowで起動し、stdout/stderrを並行回収してWaitForExitした。
build/testの要求上限は各8、同一targetの4コマンドを直列実行した。
GPU・Civ6・APIキー・追加AI・本物のCodex子ジョブは起動していない。
製品ソース/テスト、元worktree、元ToDoの状態を編集せず、質問・Discord通知も行っていない。
全workspace、Green、Clippy、doctest、release、PR、merge、配布は実施していない。
この証跡は現在の修正後実装のGreenや元ToDo全体の完了を意味しない。mainへマージしない。
