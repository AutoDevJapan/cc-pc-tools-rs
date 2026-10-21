# ToDo3238: 親3085の固定版を検査する

この担当は検査と未配布の実行ファイル候補の生成だけを行う。
親ソース、親ブランチ、配布済みの実行ファイルは変更しない。

- 対象: AutoDevJapan/cc-pc-tools-rs (repository ID 1406818713)
- 親ToDo: 3085 / この検査のToDo: 3238
- 実行ジョブ: 20261021-100648-todo-3238（割り当て資源: cargo）
- 親ソース: D:/CC/Work/unattended-operations-20261013-wt/todo-3085-capacity-decision
- 固定ブランチ: scheduler/todo-3085-capacity-decision
- 固定HEAD: 3a9910d7f0ceb9dbdc178ccaa6391b26256b2dda
- この担当のブランチ: scheduler/todo-3238-todo（開始時HEAD: 3e1a211ddeaa9d4d27bdabce12ffc3754863a232）
- 生成先: codex-owned-0c7d583da6c03192db12646d4a0493d3（この作業場所内、safe-delete所有IDも同じ）

開始時に親のHEAD・ブランチ・変更なしを照合した。
正規のscrub391判定は親とこの作業場所の両方でok=true、client_resource=false、classification_unverified=false。
GitHub APIで所有者とrepository IDを確認した。

## 実行する検査

通常のCargo共有ゲートを使い、同じ生成先で次の順に実行する。
各コマンドに親のCargo.tomlをmanifest-pathとして渡す。
既存nextest設定の直列指定とignored検査を維持し、新しいskipは加えない。

1. cargo nextest run --locked -p cc-todo -p cc-scheduler
2. cargo test --locked -p cc-todo -p cc-scheduler --doc
3. cargo clippy --locked -p cc-todo --all-targets -- -D warnings
4. cargo build --locked --release -p cc-todo --bin todo

全workspace検査は実施しない。公開CLIとscheduler境界の統合を対象にする。
releaseビルドは親が取り込む配布候補の生成に必要な1回だけ行う。

## 証拠と再開

標準出力・標準エラー・各終了結果はこの作業場所の `.ci-work/3238-*` に全件保存する。
実行中の検査、子プロセス、完了済み検査は `.ci-work/3238-live-status.private.json` に残す。
再開時はそこにあるプロセスと各検査の結果を照合し、実行中または成功済みの同じ検査を重ねない。
親ソースが変わっていたら追従せず、差異を記録して親へ返す。

全検査成功後、release実行ファイルを独立コピーし、次を確認する。

- `.ci-work/todo-3085-integrated.exe` のSHA256がrelease出力と一致する。
- fsutil hardlink listで候補の現在のリンク数が1である。
- 親ソースのHEAD・ブランチ・変更なしが実行前と一致する。
- `.ci-work/3085-cargo-receipt.private.json` にコマンド、結果、件数、出力証拠、候補情報、production_changed=falseを保存する。

候補とreceiptは親3085が読み返すまで保持する。この担当は配布しない。
最終レビューとマージは親3085/既存3090の範囲に残す。

## 2026-10-21の結果

指定した検査4件の実行と原因の切り分けは完了した。全体の検査結果は失敗。
親ソースの修正や配布は行っていない。

| 検査 | 終了コード | 結果 |
| --- | ---: | --- |
| cc-todo / cc-scheduler のnextest | 100 | 447件中446成功、1失敗、skip 0 |
| 両クレートのdoctest | 0 | 両ライブラリとも対象0件 |
| cc-todo のClippy（all-targets、警告をエラーとして扱う） | 0 | 成功 |
| todo のreleaseビルド | 0 | 成功、未配布 |

失敗した検査は `cc-todo::product_review::only_original_review_changes_and_history_is_complete`。
`cc-todo/tests/product_review.rs:41` の2回目の `bind_review` が
「保存済みの割当が変わっています」を返し、unwrapが失敗した。

原因は `cc-todo/src/product_resume.rs:202` が実際の現在時刻を
`cc-scheduler/src/product_review.rs:11-12` の固定期限と比較すること。
固定期間は2026-10-13 00:00 UTC以上、2026-10-21 00:00 UTC未満であり、
終了時刻は日本時間の2026-10-21 09:00。今回の検査は期限後の10:12〜10:14に実行した。
テスト中の直接のeligible呼出しは過去の時刻を明示するが、2回目のbind_reviewには
その時刻が渡らず、日付に依存する。本番の受付期限を延長せず、親担当が検査の時刻を
固定して修正し、新しい固定ソースで影響する検査を確認する必要がある。
この担当では同じ失敗の再実行、skipの追加、親ソースの編集を行っていない。

親のHEAD・ブランチ・変更なしは全4件の前後と最終確認でも一致した。
最終の正規scrub391判定も成功し、Cargo.lockのハッシュは開始時と一致した。

テストが失敗したため `.ci-work/todo-3085-integrated.exe` は作成しなかった。
release出力だけを専用生成先の `release/todo.exe` に保持している。
サイズは7,632,384 bytes、現在のリンク数は2。配布候補として扱わない。

- release SHA256: `8231fe28624328ac90fdd9792129ce8783bdc154cc8e3a729ebfe147718d4466`
- receipt: `.ci-work/3085-cargo-receipt.private.json`
- receipt SHA256: `535aeab44a2dd11be892272e70d068483799e52fc776643f29252c6fec164178`
- 原因の記録: `.ci-work/3238-nextest-failure-analysis.private.json`
- 全出力と各コマンドの終了結果: `.ci-work/3238-{nextest,doctest,clippy,release}.*`

この検査の完了は親3085全体の完了を意味しない。親へ結果を返し、修正と最終レビューを残す。
ソースや依存のキャッシュを含む専用生成先と全証拠は保持し、親の成果物を削除していない。

## 2026-10-21の再検査計画

元3238を有限な検査担当として再開する。実行ジョブは
`20261021-105443-todo-3238`、本人からの割り当て資源はcargo。
親3085の新しい固定HEADは `743613b95c9137d11b0b0c6562cfc569061976ce`。
親の作業場所とブランチは前回と同じで、開始時に変更なしとGitHubへの保存を確認した。
正規の保護判定もこの作業場所・親・元登録場所のすべてで成功した。

前回との差分は `cc-todo/src/product_resume.rs` と
`cc-todo/tests/product_review.rs` のみ。時刻を明示できる関数と回帰検査が変わり、
本番の呼び出しは引き続き現在時刻を渡す。受付期限は変更されていない。

既存の所有ID `0c7d583da6c03192db12646d4a0493d3` の生成先を使い、
通常のCargo共有ゲートで次を順に1回ずつ実行する。

1. cc-todoの `product_review` 結合検査3件をnextestで確認する。
2. cc-todoをall-targetsのClippyで確認し、警告もエラーとして扱う。
3. 新しい固定ソースから配布候補用のtodoをreleaseビルドする。

前回成功した残りの検査・対象0件のdoctest・Go検査は繰り返さない。
旧receiptと全ログは上書きせず、旧release実行ファイルも独立したコピーで保持する。
新しい出力・終了結果・実行中の状態は `.ci-work/3238-fixed-*` に保存し、
正式な検査記録は `.ci-work/3085-cargo-receipt-fixed.private.json` に作る。
再開時はその記録とプロセスを照合し、実行済みの検査を重ねない。

すべて成功したら、新しいrelease出力と同じ内容の独立した候補を
`.ci-work/todo-3085-integrated.exe` に作り、SHA256・サイズ・リンク数1を確認する。
各検査の前後と最後に、親のHEAD・ブランチ・変更なしを再確認する。
親ソース・親PR・現在の配布物は変更しない。親全体の最終レビューや完成を代行しない。
この担当のブランチ名は変更しない。

## 新しい固定版の検査結果

新しい固定HEAD `743613b95c9137d11b0b0c6562cfc569061976ce` に対する
指定の再検査と候補の生成が完了した。結果は成功。検査担当3238の完了であり、
親3085の実装全体・最終レビュー・配布の完了を意味しない。

| 検査 | 終了コード | 結果 |
| --- | ---: | --- |
| cc-todoのproduct_review結合検査 | 0 | 3件すべて成功、失敗0、skip 0 |
| cc-todoのClippy（all-targets、警告をエラーとして扱う） | 0 | 成功 |
| 新しい固定ソースのtodoのreleaseビルド | 0 | 成功、未配布 |

3件には、前回失敗した再入の検査と、期限後の再入拒否・DB不変の検査を含む。
次の3件の実行結果を全ログに保持した。

- `only_original_review_changes_and_history_is_complete`
- `changed_scope_stops_dependencies_workers_and_preparation_never_write`
- `prepared_binding_cannot_be_reused_with_other_inputs`

前回447件中446成功のうち、今回の3件に含まれる成功2件を除いた444件は
影響範囲外の既存証拠として保持した。合算して449件成功とは数えていない。
対象0件のdoctestと以前のGo検査は繰り返していない。

検査の前後・候補の作成前・最後に親のHEAD、ブランチ、変更なしを照合した。
Cargo.toml、Cargo.lock、nextest設定のハッシュは前回と一致した。
最後の正規の保護判定も成功した。親のソース・PR・配布済みの実行ファイルを変更していない。

候補は `.ci-work/todo-3085-integrated.exe`。新しいrelease出力とSHA256・サイズが一致し、
候補のリンク数は1、サイズは7,632,896 bytes。release本体はリンク数2のまま保持した。

- 候補SHA256: `45449d6fa5a6553013814c71d7988921ca6843b224ebe30633b215a5ee82dbcb`
- 正式な検査記録: `.ci-work/3085-cargo-receipt-fixed.private.json`
- 検査記録SHA256: `7fb5c67e86d0b2aad339ecc68efe925e2afadae108979acc063dbd5631be508c`
- 新しい全出力・終了結果・ソース照合: `.ci-work/3238-fixed-*`
- 再開情報: `.ci-work/3238-fixed-resume.private.json`

旧receiptのSHA256は `535aeab44a2dd11be892272e70d068483799e52fc776643f29252c6fec164178` のまま。
旧ログ全件のハッシュも一致した。旧releaseはビルド前に
`.ci-work/todo-3085-previous-3a9910d.exe` へ独立してコピーし、
旧SHA256 `8231fe28624328ac90fdd9792129ce8783bdc154cc8e3a729ebfe147718d4466` と
サイズ7,632,384 bytes、リンク数1を確認した。旧記録は上書きしていない。

親3085はこの正式な検査記録と候補を読み返して取得する。
両方と既存生成先はそのために保持する。この担当は新しいPRや監視担当を作成せず、配布もしない。

## レビュー作業場所の修正に対する有限な検査（2026-10-21）

元3238をジョブ `20261021-132803-todo-3238`、割り当て資源cargoで再開した。
親3085の固定HEADは `428adc60e998f55c93aa79baab200f3d267fbca2`。
親の場所・ブランチは従来どおりで、開始時に変更なしとpush済みを確認した。
親・この作業場所・既存生成先は正規の保護判定に通過した。
この担当は親のソースを編集せず、新しい担当やレビュー、PR、配布を実行しない。

所有ID `0c7d583da6c03192db12646d4a0493d3` の既存生成先を再利用し、
通常のCargoゲート、sccache、親のnextest設定を使い、次を1本の順序で実行する。

1. cc-schedulerのparityで `test(review_workspace_)` の新しい3件を確認する。
2. 成功後、同じparityで `not test(review_workspace_)` の既存検査を確認する。
3. cc-todoのsubwork・capacity_cycle・verified_resume結合検査を確認する。
4. 両クレートのdoctest、両クレートのall-targets Clippyを順に確認する。
5. すべて成功後、両クレートのbinsをreleaseで1回生成する。
6. todoとschedulerを独立したリンク数1の候補へコピーし、内容を照合する。
   schedulerは全体のhelpとdispatch-subworkのhelpだけを起動する。

最初の3件が失敗したら同じ失敗を反復せず、全出力と原因を親へ返す。
後続の失敗も親のソースを修正せず返し、失敗したまま成功候補を作らない。
全workspaceや今回の変更に影響しないproduct_reviewは実行しない。
旧receipt・旧候補・旧全出力を保持し、旧成功と今回の成功を混同しない。

全出力・途中結果・再開情報は `.ci-work/3238-review-workspace-*` に残す。
正式な検査記録は `.ci-work/3085-cargo-review-workspace.private.json`。
新候補は `.ci-work/todo-3085-review-workspace.exe` と
`.ci-work/scheduler-3085-review-workspace.exe`。旧候補を上書きしない。
親のHEAD・ブランチ・変更なしを各コマンドの前後と最後に再確認する。

再開時は正式な記録、途中結果、実行中プロセスを照合し、成功済みの検査を重ねない。
作業ブランチ名は `scheduler/todo-3238-todo` のまま変更しない。
検査結果は親3085が受領し、3255への永続記録と既存Discord話題への配送を担う。
この担当の完了は検査だけであり、親3085・3090全体の完了とは区別する。

## レビュー作業場所の修正に対する結果

指定された有限な検査の実行と原因の切り分けは完了した。検査結果は失敗。
実担当はジョブ `20261021-132803-todo-3238`、モデル `gpt-6.1-sol`、
推論 `max`、割り当て資源 `cargo`。通常CLIの該当ジョブだけを抽出して確認した。

固定HEAD `428adc60e998f55c93aa79baab200f3d267fbca2` に対して、
新しい3件を13:32:44〜13:33:12 JSTに1回実行した。
結果は終了コード100、3件中2件成功・1件失敗。
既存192件は指定フィルターの対象外であり、実行した件数へ含めない。

失敗は `review_workspace_refuses_unverified_provenance_and_running_or_queued_review` の
`still_alive` 条件。`cc-scheduler/tests/support/review_workspace.rs:175` の
`prepare(...).is_err()` が成立しなかった。
この固定版のテストは終了済みのjobへ `pid=7` を追加し、作成時刻を持つprocessを置くが、
jobの `pid_created` を設定していない。既存の `policy::alive` はPIDの再利用を区別するため
作成時刻の一致も要求し、このjobは生存中と判定されない。
実行中の同一processを表すテストデータになっていないことが、失敗の直接原因である。
正しく作成時刻を照合できる実processでも本番の拒否を通り抜けると証明した結果ではない。

指定の停止条件に従い、既存parity、cc-todoの関連3統合、doctest、Clippy、release、
候補の包装、候補schedulerのhelpは未実行。同じ失敗を反復していない。
新候補2個は存在せず、旧候補を今回の成功候補として流用していない。
標準出力・標準エラーを省略せず保存し、実コマンドのargv・終了コード・件数・SHA256を記録した。

実行直前・直後は親の固定HEAD・ブランチ・変更なしが一致していた。
その後、親担当の修正コミット `b392804d00307c171bbfdbba370c7c91fc460bb2` を観測した。
この担当は新しい版へ追従して検査せず、元の固定版の失敗結果を保持した。
この担当による親sourceの編集、本番変更、共有exeの更新はない。

- 正式な検査記録: `.ci-work/3085-cargo-review-workspace.private.json`
- 記録SHA256: `84335c0b6a52a6054fe58561bf4e88691df87a6de1df1119239d3952fcf2f1c6`
- 完全な出力: `.ci-work/3238-review-workspace-new-three.{stdout,stderr}.log`
- 原因の記録: `.ci-work/3238-review-workspace-failure-analysis.private.json`
- 実担当の照合: `.ci-work/3238-review-workspace-assignment.private.json`
- 再開情報: `.ci-work/3238-review-workspace-resume.private.json`

旧receiptと旧全出力・旧候補は全件のSHA256を照合して保持した。
旧補足記録のSHA256も `f224bede5775f39b26b8a4ee2dfafb9ea96ba902d2640aaa2617840679bb5ebb` のまま。
検査用processは終了し、生成先内の実行中exeは0、検査runnerの排他も解放済み。
生成先と証拠は親の受領と次の有限な検査に必要なため保持する。

残る作業は、親3085が修正後の固定版を指定して元3238を再開し、必要な検査と
候補生成を完了すること。その後の3090の別モデルレビュー・配布も親の未完了作業である。
今回の失敗結果は親3085が受領し、3255への永続記録と既存の話題への配送を行う。
本人の判断やPC操作は不要。この担当の検査だけの完了を、親全体の成功や完了へ広げない。

## 作成時刻を修正した固定版の再検査計画（2026-10-21）

元3238をジョブ `20261021-135545-todo-3238` で再開した。
通常CLIのjobs配列からこのジョブとToDoだけを抽出し、モデル `gpt-6.1-sol`、
推論 `max`、割り当て資源 `cargo` を確認した。
親の固定HEADは `b392804d00307c171bbfdbba370c7c91fc460bb2`。
同じ親ソース・ブランチで変更なし、remoteの同じブランチへのpush済みも確認した。
前回 `428adc6` との差分は、失敗したstill_alive条件の検査用jobへ
processの作成時刻を渡すテストデータの3行追加・1行削除だけである。

初回の3件中2件成功・1件失敗と固定HEADは書き換えない。
初回receipt、全出力、stderr SHA256
`5286c665cff94fcaa3b90ff5bbf2073b2e365f57725770592d06edfbd3b0fe39`
を保持し、同じproduction sourceで成功した2件を再利用する。

既存の所有記録付き生成先と通常のCargoゲートを使い、次を順に実行する。

1. 失敗した `review_workspace_refuses_unverified_provenance_and_running_or_queued_review` だけを確認する。
2. 成功後、未実施の既存parity192件を `not test(review_workspace_)` で確認する。
3. 未実施のcc-todoのsubwork・capacity_cycle・verified_resume結合検査を確認する。
4. 未実施の両ライブラリのdoctestと両クレートのall-targets Clippyを確認する。
5. すべて成功後、両クレートのbinsをreleaseで1回生成する。
6. todoとschedulerを新しい独立候補へ保存し、SHA256・サイズ・リンク数1を確認する。
   候補schedulerの全体helpとdispatch-subworkのhelpだけを実行する。

新しい全出力と途中結果は `.ci-work/3238-review-workspace-fixed-*`、
正式な記録は `.ci-work/3085-cargo-review-workspace-fixed.private.json` へ保存する。
候補名は `todo-3085-review-workspace-fixed.exe` と
`scheduler-3085-review-workspace-fixed.exe`。旧receipt・旧候補を上書きしない。
親ソースのHEAD・ブランチ・変更なしを各コマンドの前後に照合する。
失敗時は後続と候補作成を止め、全出力を親へ返す。親ソースは修正しない。

この担当は元の検査範囲だけを完遂する。新しいToDo・子ジョブ・レビュー・PRを作らず、
本番や共有実行ファイルを更新しない。親3085と既存3090の完了を前提にしない。
作業ブランチ名は変更しない。結果の受領、3255への永続記録、Discordへの配送は親が行う。

## 作成時刻を修正した固定版の検査結果

固定HEAD `b392804d00307c171bbfdbba370c7c91fc460bb2` に対する指定の検査と
未配布候補2個の生成が完了した。検査結果は成功。
実担当は `20261021-135545-todo-3238`、モデル `gpt-6.1-sol`、推論 `max`、資源 `cargo`。
実コマンドは2026-10-21 13:59:23〜14:01:50 JSTに実行し、すべて終了コード0だった。

| 検査 | 実結果 |
| --- | --- |
| 作成時刻を修正した拒否条件の1件 | 1件成功、194件はフィルター対象外 |
| 既存parity | 192件成功、追加3件はフィルター対象外 |
| cc-todoのsubwork・capacity_cycle・verified_resume | 3実行ファイルの23件成功、skip 0 |
| 両ライブラリのdoctest | 対象0件と0件、両方成功終了 |
| 両クレートのall-targets Clippy | 警告もエラーとして扱い、成功 |
| 両クレートのbinsをrelease生成 | 1回実行して成功 |
| 候補schedulerの全体helpとdispatch-subwork help | 両方成功。実dispatchは未実行 |

今回新しく実行したnextestは合計216件で、216件成功・失敗0件。
初回の同じproduction sourceで成功した2件は再実行せず再利用した。
初回の固定HEAD、3件中2件成功・1件失敗、旧receipt・全出力・旧候補はそのまま保持した。
76個の既存証拠ファイルはSHA256がすべて一致した。

最初の1件は実行自体が終了コード0・1件成功だったが、この担当の記録処理が
nextestの単数形 `1 test run` を認識せず一度停止した。
その停止receiptと元の結果ファイル・全出力を保持し、読取処理だけを修正した。
SHA256を照合した同じ成功出力から件数を確認し、その1件を繰り返さず後続を実行した。
この停止をテスト失敗や親ソースの不具合へ分類していない。

新しい独立候補は両方とも生成元とSHA256・サイズが一致し、現在のリンク数は1。

- `.ci-work/todo-3085-review-workspace-fixed.exe`: 7,650,816 bytes。
  SHA256 `3ec3ffb1de995d1f900398386004b69a4aed109fb9f0c91319ac1b32ba467b44`。
- `.ci-work/scheduler-3085-review-workspace-fixed.exe`: 6,214,144 bytes。
  SHA256 `483e498eb8fd64557e4d51cc9306f34d66bed8034b7edc07f1b32aac0c8fc193`。

正式な記録は `.ci-work/3085-cargo-review-workspace-fixed.private.json`。
SHA256は `db66f8e29f0ae824a7120d4bead1e5138fdfd339017721711040a5e325199501`。
各コマンドのargv・開始終了・終了コード・実件数・完全なstdout/stderrとそのSHA256、
生成元の指紋、候補の絶対パス・SHA256・サイズ・リンク数を保存した。
最終照合は `.ci-work/3238-review-workspace-fixed-final-verification.private.json`、
再開情報は `.ci-work/3238-review-workspace-fixed-resume.private.json` に残した。

各検査の前後と最後に親の固定HEAD・ブランチ・変更なしを照合した。
正規の保護判定も成功。検査用runnerは終了し、生成先と候補の実行中プロセスは0、
runnerの排他も解放済み。元の生成先と証拠・旧候補・新候補は親の受領のため保持する。
親ソース、本番、共有実行ファイルは変更していない。作業ブランチ名も変更していない。

この担当3238の有限な検査は完了した。親3085・3090の作業全体は完了していない。
親が結果と候補を受領し、3255への永続記録、既存話題への通知なしの配送と読み返し確認、
既存3090の別モデルレビューと実dispatch・配布を進める。本人の判断やPC操作は不要。

## 保存済み計測状態を保持する追加差分の検査計画

同じ3238をジョブ `20261021-150054-todo-3238` で再開した。
正規CLIから当該ジョブだけを抽出し、`gpt-6.1-sol` / `max` / `cargo` を確認した。
親の対象は固定HEAD `dce5c2042ef9ffecad96281c7cb2a988ae24406a`、
ブランチは `scheduler/todo-3085-capacity-decision`。変更なし・push済みを確認した。

今回の追加差分は、対象1件の起動入口がロック取得後に保存済み状態を読む修正、
保存済みの起動間隔・観測と不正状態の保持を確かめる1件、返却理由の追加と説明だけ。
前回の成功216件を新規実行として数えず、影響のない既存結果を再利用する。
旧receipt・候補・出力は保持し、同じ所有記録付き生成先と通常の共有ゲートを使う。

1. `parity` の `review_workspace_dispatch_preserves_saved_state_and_launch_interval` と
   `review_workspace_dispatch_keeps_normal_capacity_model_and_admission_checks` の2件だけを実行する。
2. 成功後、`cc-scheduler` / `cc-todo` の `clippy --all-targets -- -D warnings` を実行する。
   公開面やdoccodeに変更がなく、doctest各0件は前回結果を再利用する。
3. 両クレートの `build --release --locked --bins` を1回だけ実行する。
4. 終了と所有・内包を確認し、新しい独立候補 `todo-3085-state-preserved.exe` と
   `scheduler-3085-state-preserved.exe` を `.ci-work` に保存する。
   生成元とのSHA256・サイズ一致、リンク数1を確認し、新schedulerのhelp2種だけを実行する。

完全なstdout/stderr、argv、実件数、終了コード、各SHA256、実ジョブ、開始終了、
固定ソースと候補の絶対パス・指紋・生成元を
`.ci-work/3085-cargo-state-preserved.private.json` に保存する。
途中結果と再開情報は `.ci-work/3238-state-preserved-*` に残す。
各工程の前後に固定ソースを照合する。失敗時は後続を止め、同じ失敗を繰り返さない。
親ソース・共有設定・本番は変更せず、新規ToDo・子Codex・PR・マージは行わない。
この有限検査の結果を親へ返し、3238だけを完了する。親3085・3090は完了しない。
作業ブランチ名は変更しない。親が受領・永続記録・既存話題への無通知配送を担当する。

## 保存済み計測状態を保持する追加差分の検査結果

固定HEAD `dce5c2042ef9ffecad96281c7cb2a988ae24406a` に対する有限な検査は成功した。
実担当は `20261021-150054-todo-3238`、`gpt-6.1-sol` / `max`、割り当て資源は `cargo`。
実行は2026-10-21 15:06:04〜15:07:07 JST、最終照合は15:08:06 JST。

| 今回実行した範囲 | 実結果 |
| --- | --- |
| 保存済み状態の保持と通常起動条件の2件 | 2件成功・失敗0、対象外194件、終了コード0 |
| cc-scheduler / cc-todoのall-targets Clippy | 警告もエラーとして扱い、終了コード0 |
| 両クレートのrelease bins生成 | 1回だけ実行し、終了コード0 |
| 新schedulerの全体help / dispatch-subwork help | 両方終了コード0、実dispatchは未実行 |

今回新しく実行したnextestは2件だけ。前回の成功216件を今回の実行件数へ足していない。
doctestは公開面・doccodeに変更がないため、前回の各0件・終了コード0を再利用した。
全workspace検査、親のソース修正、共有設定変更、本番配布は行っていない。

新候補は生成終了後にコピーし、生成元とSHA256・サイズが一致し、リンク数が1であることを
再確認した。生成元の全リンクも同じ所有記録付き生成先の内側にある。

- `.ci-work/todo-3085-state-preserved.exe`: 7,650,816 bytes。
  SHA256 `0d3768d007097f995642da9019dc616737c8c07372fbf60292d3948775349ab4`。
- `.ci-work/scheduler-3085-state-preserved.exe`: 6,216,704 bytes。
  SHA256 `869a2b108029b4212c5a4e9f4f0f599427842507fa3efbfe8f94802b4315998b`。

正式な記録の絶対パスは
`D:/CC/Work/unattended-operations-20261013-wt/todo-3238-todo/.ci-work/3085-cargo-state-preserved.private.json`。
SHA256は `33661a3809c109c4cec98a84dde9f28b13f44a10205edb9590fa7b726ff79506`。
実コマンド5件とrunnerの完全stdout/stderr計12ファイル、argv、開始終了、終了コード、
実件数、実モデル・資源、候補の絶対パス・生成元・指紋を記録し、全出力SHA256を照合した。
最終照合は `.ci-work/3238-state-preserved-final-verification.private.json`、
再開情報は `.ci-work/3238-state-preserved-resume.private.json` に保持する。

旧receipt・旧候補・旧出力など141ファイルはサイズとSHA256がすべて一致した。
親は最後まで同じ固定HEAD・ブランチで変更なし。元のrunnerとゲートの各プロセスは
作成時刻を含めて終了を確認し、生成先と新候補の実行中プロセスは0、runnerの排他も解放済み。
既存生成先と旧・新候補は、親の受領と後続作業に必要なため保持する。
この保持は不要物の削除失敗やオーナー判断待ちではない。

3238の有限検査だけが完了した。親3085・3090の受領、別モデルレビュー、実起動・配布は
未完であり、この検査成功を親の全体完了へ読み替えない。
親が永続記録と既存の話題への通知なし配送・読み返し確認を行う。本人の対応は不要。
旧Issue追記の審査拒否は保持し、再試行や別経路での送信を行っていない。
