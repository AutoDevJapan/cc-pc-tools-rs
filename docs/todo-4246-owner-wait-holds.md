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
- M1/S1/S2の修正後、入口21テストと追加の期限/CAS 2テスト、関係するCLI 10テストが成功した。
  同じテストを重ねて合算しない。旧4142の311/13/31件も加算しない。
  最終sourceに対する22件の入口、SystemHost、Clippy、doctestの結果と清掃・checkpointは後続記録に残す。
- 一時物の所有ID：`fb358fdfedb926f235710748ada866ad`（Cの一時補助）、
  `ca355ffba5561fca35a0723685ed6ebd`（このworktree内のCargo target）。
  清掃はsafe-deleteのplanを別呼出しで実行してfinishする。

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

