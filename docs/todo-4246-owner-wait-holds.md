# ToDo4246：本人の保留を所有待ちの再評価で保持する

親3387の有限なMust M1、同じ境界のS1/S2だけを処置する。
新しい司令塔・巡回・reader・全体設定は作らず、実タスクの解除、別モデルレビュー、
CI、PR、配布、Discord送信、元4023の受入は親が行う。

## 再開情報

- 作業：`scheduler/todo-4246-3387`、job `20261030-085825-todo-4246`。
- 開始HEAD：`0d919ed9d0bd68e7005ce2168b6aeaa5225e5a0e`。
- 固定実装 `05a18daddcc4a47eb0ffeb3d748d26c5c594755f` を
  `0abca7fb` として通常のcherry-pickで取り込んだ。
- 指定BASE `9c9a1f30ee0dc5dde0fd1af99035cbc76adaa4e5` のtreeは
  `f8d17addb2fe9c0f77773adee02f3467af721e45` と一致し、固定実装とのコード差分は0
  （文書の履歴追記だけが異なる）。
- 回帰テストを先に追加し、Redを実測（nextest exit100、1件失敗）。
  `751d1851` にRedのテスト・入力hashを保存した。
  312条件中301条件が `released=[1]`、保存callback1、store bytes変化になった。
- 最終source commitは `5443df48b2d1de7913ae61d0376deb59f2792423`。
  子の修正・テストを終え、差分・入力と清掃結果を証拠commitに保存した。
  通常checkpointの実際の保存結果は下記の実行記録を参照し、親のレビュー等は別工程とする。
- 一時物の所有ID：`fb358fdfedb926f235710748ada866ad`（Cの一時補助）、
  `ca355ffba5561fca35a0723685ed6ebd`（このworktree内のCargo target）。
  清掃結果は下記に記録する。

原保護一覧・失われたregistry・原prompt・秘密・実SDK・実タスクのJSONには触れない。
実行する再評価は架空fixtureだけ。現用実行物や設定を更新しない。

## M1とS1/S2の処置

`direction_record::HOLD_FIELDS` だけをcrate内へ最小公開し、候補行とowner行の直下および
scheduler内にある同じ項目を再利用して確認する。
null、false、空文字以外を明示holdとして保持し、0、空配列、空objectなどの型不正値も保持する。
`owner_stop_job/pending_cancel/question_close_pending/question`、不正なscheduler・stall_recovery、
未経過・不明の延期時刻も保存0とする。
owner側のblock・未確認状態・本来の未完了依存も解除せず、doneのownerに保留がなければ通常解除を維持する。

登録project root、選択行とowner行の非空scheduler.worktree、正確な参照jobのcwdを
正規Hostで判定する。選択行・owner行は元sourceに含まれるため、scheduler.worktreeが
途中で変わっても最後のCAS比較で拒否する。root/cwdをworktreeの判定で代替しない。
重複pathの判定結果は各pass内だけで利用する。2回目のpassは空の集合から始め、すべて新たに確認する。
異なるscheduler.worktreeを含む正常fixtureの実呼出順を基準として、すべての失敗位置をテストする。
元の固定 `0..6` には依存しない。

初回のToDo lock待ちとsource取得が終わってからHostを作る。
SystemHostの単一の単調時計期限とwall-clock proofを同じscope開始へ揃え、passを跨いでも更新しない。
45秒以上・時計逆行・不明時刻では保存0、45秒未満では通常解除を維持する。
Hostの生成がsource snapshotより後で、共有lockの外であることを、待ち時間を使わず
並行writerとCASで確認する。
job state lockから即時ToDo lockの順、job bytesの再照合、fresh liveness、他行の変更保持は維持する。

## S3〜S7として残す前提と未対応

- owner行のprojectが選択行と異なるedgeは、正当な所有競合であっても拒否する。
  別projectのrootを補完して対象を広げる変更は行わない。
- 参照するjobは選択行のjobs・scheduler.job・work_owner_jobに記録されたものだけである。
  未記録worker、別job一覧、すべてのowner jobsを列挙する機能は追加しない。
  livenessの確認もその正確な参照jobのPID/birthとstatusに限る。
- 通常のjob state lock → 即時ToDo lockの順を保ち、queue gateを追加しない。
  この入口はdispatchやqueueの変更を行わず、全queue writerとの共通トランザクションを新設する工程ではない。
- guardの二度の新規確認とsource/CASは維持するが、外部guardの資格変更、Git所有情報、
  queueの全状態をまたぐサービス共通の原子性を保証しない。
- bounded job.json読取は1MiB以内の本体全体をread・parseする。
  別promptファイルを開かないことを「prompt読取0」と呼ばない。
  JSON内にpromptなどが含まれれば、その値も読取・parseされる。

実4023がこれらの前提を満たすかは親の受入工程で確認する。
この工程のfixture成功から実4023の解除・レビュー完了や元3387全体の完了を推定しない。
新しいsource reader、資格の迂回、空原本、別rootへのcopyは作らない。

## テストの範囲と候補

共有ゲートとsccacheを使い、同じ専用targetで順次実行する。
入口の回帰、共通HOLDを使うdirection-record、会話待ちとrecoveryの保留保持に関係する
CLI 10件を選び、全workspaceや旧成功件数を再実行・加算しない。
外へ公開するAPIの変更はない（共通定数のcrate内公開だけ）ため、cc-jobs/localai全体のテストは選ばない。
既存4142から取り込んだSystemHostのstrict wire-formatテストは確認する。

配布候補は今回作らず、`release_eligible=false` とする。
テストで作るdebug実行物はfixture用であり、配布候補に昇格しない。
既存07b6候補を今回の最終版として扱わない。
別モデル確認・必要CI・通常merge・実配布・元4023の正式再評価は親の未完了工程として返す。

## 最終結果

| テスト | 結果 | 証拠 |
|---|---|---|
| 元9cと同じ入口実装へのM1回帰 | 期待したRed、312条件中301条件が保存・解除 | `red-m1.*`、`red-inputs.json` |
| 最終sourceの入口 | 22件成功（M1の312条件を含む） | `green-final-owner-wait.*` |
| 関係するCLI | 10件成功 | `green-related-cli.*` |
| SystemHostの期限とstrict guard | 3件成功 | `green-system-host.*` |
| cc-todo/cc-schedulerのClippy | 各成功、warningsをエラー扱い | `clippy-todo.*`、`clippy-scheduler.*` |
| cc-todo/cc-schedulerのdoctest | コマンド成功、各0件 | `docs-libraries.*` |
| 変更したRust 3ファイルのformat | 成功 | `format-check.*` |

すべて [証拠ディレクトリ](evidence/todo-4246/) に保存する。
最初のGreen試行には、テストのString配列にslice::repeatを使ったコンパイルエラーがあり、
期待値の配列を直して解消した。これはM1のRedとは別の実装途中の失敗として残す。
21件の途中Greenと期限/CAS 2件は履歴であり、最終22件へ合算しない。
旧4142の311/13/31件やClippy/doc成功も最終件数へ加算しない。
CLI 10件は最終sourceをcommitする前に同じ実装で実行したもので、以後その実装を変更していない。
最終22件、SystemHost、Clippy、doctestは上記のsource commitで実行した。

`final-inputs.json` は実sourceのraw SHA256・bytes・filtered Git blob・commit blob、
Cargo.lockと必要manifest、toolchainと引数の証拠を指す。
`must-from-fixed.patch` は固定05a18から今回の処置だけ、`full-from-start.patch` は開始HEADから
固定実装の取込みを含む差分であり、いずれも明示した関連ファイルだけを含む。
配布候補用の完全なdepinfo入力閉包ではなく、候補作成もrelease buildも行っていない。
最終証拠を追加するcommitで実行ソースは変えない。

## 後片付けと親への保留報告

- Cargo生成物：2,295,336,794 bytes（約2.14GiB）、3,411 entries。
  safe-deleteのplanが容量を理由に削除せず退避するよう指定したため、通常のquarantineを実行し、
  `D:/削除希望/20261030-todo-4246-build-target-ca355ffb` への移動と元フォルダ不在を確認した。
  状態はquarantinedで、容量はまだ回収されていない。必要ソースとテストの証拠はGitへ残す。
  次は既存の清掃工程でこの所有記録と退避物を扱う。本人の判断は不要。
  親はこの新しい保留だけを通常の記録へ反映し、同じ保留を繰り返し送らない。
- Cの一時補助：1,977 bytes、1 entry。
  planの通常削除命令を別呼出しで実行し、finishのsource_absentを確認した。
- 既存4142の5.66GB退避は他担当のデータとして保持し、操作・再報告していない。

checkpointの入力と返却は、この証拠ディレクトリの
`checkpoint-request.private.json` / `checkpoint-result.private.json` に残す。
自己参照でHEADが変わらないよう、これらはGit管理外の実行記録とし、実HEADで正規CLIへ保存する。
本有限工程の成功を元3387全体の完了として登録しない。

