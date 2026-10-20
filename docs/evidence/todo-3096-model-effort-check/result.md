# ToDo3096 独立検査の結果

指定の有限検査を完了した。比較用Redは2件とも期待したassertで失敗し、最新Greenは58件すべて合格した。exampleのコンパイルも成功した。これは検査だけの完了であり、ToDo3081 / Issue277の製品全体の完了ではない。

対象リポジトリは実GitHub所有者をAPIで確認したAutoDevJapan/cc-pc-tools-rs。元の作業範囲と専用worktreeはguardでclient_resource=falseを確認した。

- 比較用Red: `8b70dd71c400ef449d0216d0075aff6bc253c527`
- 最新Green: `170bf90635ddb1b7648531387c1e19d5816b3b69`
- Red証跡の中間commit: `ce9af5a93b15ef9c4736eaf37b2cab1cae9ec0b8`。専用Red branchへpushし、ls-remoteとの一致を確認した。比較用ソースはmainへmergeしない。
- 最終証跡branch: `scheduler/todo-3096-3081-green`。Green固定版に証跡だけを追加する。

| 選択した検査 | 実終了コード | 実行件数 | 合格 | 失敗 | 選択外 |
|---|---:|---:|---:|---:|---:|
| Red: cc-job-policy / review_qualification_red | 100 | 1 | 0 | 1 | 0 |
| Red: cc-jobs / 指定した実process E2E | 100 | 1 | 0 | 1 | 384 |
| Green: cc-job-policy / model_effort | 0 | 2 | 2 | 0 | 0 |
| Green: cc-jobs / model_effort・pool_retry | 0 | 13 | 13 | 0 | 0 |
| Green: cc-jobs / 指定parity選択集合 | 0 | 34 | 34 | 0 | 355 |
| Green: cc-scheduler / 指定parity 2件 | 0 | 2 | 2 | 0 | 179 |
| Green: cc-ask / 指定intake 2件 | 0 | 2 | 2 | 0 | 68 |
| Green: cc-live / 指定review集合 | 0 | 5 | 5 | 0 | 120 |
| Green: cc-jobs / examplesのcargo check | 0 | コンパイルのみ | — | — | — |

完全な引数はplan.json、実HEAD・開始/終了時刻・実exitは各runのJSON、実テスト名と件数はresults.jsonに保存した。nextestには結果回収用の`--no-fail-fast --status-level all`を追加した。選択外のテストは未実行であり、失敗や未回収ではない。Greenの指定7コマンドはすべて実行し、未開始はない。

Redの実失敗は次の2件。構文エラー・資源待ちをRedと数えていない。

1. `automatic_review_default_differs_from_the_normal_sol_writer`: review_qualification_red.rs:6:5。実モデルは`gpt-6.1-sol`、期待は`anthropic/claude-opus-5-5`。後続assertは未到達。
2. `end_to_end::real_capacity_retry_preserves_prompt_cwd_and_session_and_stops_at_limit`: parity.rs:10195:25。実catalog_modelはNull、期待は`p000002/gpt-6-luna`。最初のcapacity-onceケースで失敗し、後続capacity-alwaysケースは未到達。

Greenの同E2Eは66.952秒で合格した。fixtureが実子プロセスを起動し、5/15/30秒の待機、元の入力・作業場所・セッション、別枠のcatalog、保存済みの回数制限を確認した。条件期限120秒を維持した。nextestの60秒SLOW表示は観測用であり、終了はPASSだった。

共有CargoゲートをProcessStartInfoのCreateNoWindowで起動した。stdout/stderrを並行して完全保存し、プロセス終了と両ストリームの完了後に実exitを記録した。同じ所有付き専用targetを直列で再利用し、sccacheとbuild/test各10以下の上限を使用した。全9コマンドで固定HEADと追跡対象の差分なしを実行前後に確認した。

実AI・GPU・Civ6・example本体は起動していない。APIキー・認証ファイルの読取・新費用は使っていない。Fastやeffortの実upstream適用は今回のfixture検査では確認していない。これは実装と同じモデルでのcheckであり、別モデルレビューではない。

全workspace、各crate全件、doctest、Clippy、release、PR、merge、配布はこのcheckでは実行していない。元担当は同じ最終ソースの正式CIで逆依存/doctest/Clippyを1回確認し、別モデルレビュー・必要な修正・merge・配布を継続する。元担当の作業場所とソース、元ToDoの状態は変更していない。オーナーへの質問・Discord通知は行っていない。

自分の検査生成物4,260,664,824 byteは全プロセス終了後、safe-deleteの所有IDとplanを確認した。planはsize退避を指定した。quarantineはCドライブ宛先のため事前検査でQUARANTINE_CROSS_DRIVEとなった。Skillの既定手順で同じDドライブの削除希望へMove-Itemし、元の不在・3,855項目・総byte・相対パス/型/サイズ/更新時刻の一覧SHA256一致を確認した。所有記録は手で変更していない。移動なので空き容量の回収量は0。通常削除の拒否や自動承認レビューの拒否ではない。

検査ログと最終結果をcommit/pushした後、schedulerの正規checkpoint-workspaceで実HEADを保存する。最終request/responseはGit対象外の`checkpoint-final-*.private.json`に残す。saved=trueを確認してから担当を終了する。
