ToDo2722 再開記録（2026-10-16）
対象は隔離されたCCソースのWindows取消しだけ。親のtools/product-ci、製品ソース、runtime、runnerは変更しない。
元HEAD 3002f5eceaffb1a1534f289a0e6e0740270d7193。branch scheduler/todo-2722-work を保持。
元の失敗原ログSHA256 0E71B444EBF117E6A37544EEFC0E86B23A614A79DFF800B867D6538E2CA5A601。
同じRustソースの関連8件は作業場所では全部成功（baseline.log）。仮説の単純な同名列挙はsourceと一致せず、walkは数値の親PIDで絞ってからOpenProcessする。ただし生成時刻の下限を確認する前にOpenProcessし、古い親PID関係の保護プロセスで失敗し得る。原ログに候補の生成時刻はなく、元候補が実際に無関係だったかは未確定。
現在の修正案: kernel snapshotにある生成時刻と親子関係を同時取得し、親より古い候補を開く前に除外。対象候補の不明さ、取得失敗、保持済み世代不一致は失敗のまま。snapshot parserを新設中、pause.rsへの接続と回帰テストは未完。
検査生成先: このworktreeのtarget。共有cargo.cmd→cargo_gate、sccacheは既存設定。baselineは終了した。
所有一時領域: f77d57a1dfc7225219380a22f27a30aa（C:/Users/makhmeto/AppData/Local/Temp/codex-owned-同ID）。baseline.logと以後のログをここへ保存。safe-delete plan→別delete→finishで回収。保持すべき根拠は作業記録へhashを残す。
未完: 決定的RED/GREEN、実ツリー消滅、関連検査、別モデルレビュー、最終workspace/doc/Clippy、commit/push、親への結果配送、生成物回収。
依頼の具体specは独立PR/mergeを要求する一方、実行メッセージの後段は独立PR/mergeを禁止する。後段の制限を優先し、独立PRを作らずcommit/pushと検査結果を元2648へ返す。親ソースの編集もしない。

実装接続と関連52件のGREEN、cc-jobs全targets Clippy成功。DACLを空にした所有fixtureとスレッド限定SeDebug無効化で、実OpenProcess拒否→kernel birth取得→古い候補非inspect/整合候補拒否を確認。失われたsupervisorのE2Eは全fixtureを事前保持し、同じハンドルのシグナルで全終了を確認する。REDは旧walkでforeign candidates were opened: [2,1,3]を確認。
残りは別モデルレビュー、最終workspace/doc/Clippy、親への配送と回収。

2026-10-16 18:18 JST 追加記録:
固定source 1c0ef903ee3dbdc1bd5f7f339fd89ee92500e4b4 を通常push済み。branchは変更なし、checkpoint-workspace成功。
別モデルレビューは sibling subwork（元2648配下）ToDo2723としてspecを保存した。モデルclaude-sonnet-5-5をschedulerがanthropicへ正規化することはsourceで確認。初回slash付きmodelのaddはCLIバリデーションでValueError、sourceで原因を確認し、受理されたaddは1回だけ。
全workspace nextest --locked --workspace --no-fail-fast を同じsourceで1回実行中。専用target、共有gate/sccache。ログは所有一時領域のworkspace.log（終了コードは既存exec session18458が返す）。重複実行しない。終了後workspace doc/全targets Clippyを直列実行する。
review2723の結果を受領するまで実装/全件の最終成功とは扱わない。独立PRは後段の制限により作らない。

関連ログ: related-final.log は52pass/0fail、clippy-related.logはexit0。workspace.logは実行中でハッシュ未確定、実行中ログをhashしない。レビュー2723は通常の準備を通りpreparedになった。
レビューの実受付: ToDo2723 / job20261016-182523-todo-2723 / anthropic/claude-sonnet-5-5。job statusはqueued、queue_wait_reason.code=child_limit、同元2648の上限2。同じ親の子2722が検査を終えたら、todo wait 2722 --on 2723を記録して枠を返す。レビュー終了後は同2722をschedulerが自動再開する。上限設定や他担当は変更しない。受付だけをレビュー完了としない。

2026-10-16 最終の実検査結果（レビュー前）:
- コードの固定HEAD: 1c0ef903ee3dbdc1bd5f7f339fd89ee92500e4b4。cc-jobs Git tree: 6a1447bb0dbea1ffce979d814a4cc6b94f542973。以後のこの記録への追記はコードを変更していない。
- 関連nextest 52 passed、0 failed。原8件のbaselineは8 passed。決定的REDは1 failedを確認し、修正後に成功。
- 最終コードの全workspace nextestは1回だけ実行。2198 passed、0 failed、既存5 skipped、590.997秒。実run ID 7a96352e-5dc7-4024-9f12-8564d8f8cf8f。実lost-supervisor取消しは1.424秒で成功し、事前保持した全fixtureハンドルの終了を確認した。
- workspace doctest exit0、16 summaryすべてテスト0件。テストが存在したようには扱わない。
- workspace全targets Clippy -D warnings exit0。cc-jobsのfmt --check exit0。
- 追加のworkspace fmt --checkだけexit1。先頭はRust2021のcc-askにあるimport順などで、base3002との該当3ファイル差分0を確認。元CIはfmtを要求しておらず、有限の取消し修正に無関係な全体書換えを加えない。raw出力fmt-final.logを保持し、この任意検査を成功とは報告しない。

所有領域 f77d57a1dfc7225219380a22f27a30aa の原ログとSHA256:
- related-final.log: 6667 bytes / 589EE25045B4E358A3DE6DA59990CA60B3D48ABA099CFBA9C2B6489B6D843F1A
- workspace.log: 285060 bytes / 988AB79B5B5EDA5787327355B68BED1DBD0BECA027E644D708538547F3D04F65
- doctest.log: 2380 bytes / 454E25E528DD4D2B6AAFE04946D5099FAE8E6CB1E86D9340BCDFF391B56036CB
- clippy-workspace.log: 4951 bytes / 0BDBE26E108F567C767EE0DA856A6CBAA6D68B1FEFB5A651100BAA365D813518
- checks.jsonにrun/source/log size/hash、追加fmtの失敗も区別して保存。

再開する担当への残作業:
1. ToDo2723の実レビュー原文を取得。元job20261016-182523-todo-2723、model anthropic/claude-sonnet-5-5、fixed1c0ef90を照合。Mustがあればこの隔離worktreeだけで修正し、影響する検査を再実行。別モデル結果を受付や待ちと混同しない。
2. Must0を証拠として保存し、元2648へコードcommit、実検査とloghashを配送する。親ソースは編集しない。このcheckの後段禁止を優先し、独立PR/mergeは行わない。原CI失敗の候補birthは原logになく、元候補が無関係だった断定や同headCI成功の主張はしない。GitHubの必要CIは親の取込み後に実確認する必要がある。
3. レビュー対応に使う専用targetを保持している。現時点で配布runtimeや既存runnerを変更していない。全作業が終わったら、自分が生成したtargetだけを所有/リンク/使用終了確認後に回収する。所有一時領域の原ログは親の検証根拠として保持中で、不要化前に消さない。safe-delete create記録の対象だけplan→別delete→finish、拒否はquarantine。既存targetに所有記録を偽造しない。
4. 自分のbranchを変更した場合も変更しない場合も、終了前に実HEADのcheckpoint-workspaceを保存。未完了のままレビュー待ちに入るときはtodo wait 2722 --on 2723を必ず記録する。新たな担当や監視ループを直接起動しない。
