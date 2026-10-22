# schedulerの起動準備と正規再開

起動準備は登録されたプロジェクトの作業範囲を保つ。資料フォルダにはGitを初期化せず、原資料を残して従来の `<登録場所>-wt/todo-<番号>-<説明>` に専用作業場所を用意する。Gitが祖先の別リポジトリを見つけても、その資料フォルダをリポジトリとして扱わない。起動指示には原資料の登録場所を渡す。

Gitの再開では、保存された現在・前回の担当、または元ToDoのジョブ履歴にある正確な `todo_id` / `related_todo_ids` / 担当receiptを使う。指示文の部分一致やPIDは所有の証拠にしない。前担当の既存cwdを選び、Gitのルート、共通管理領域、ブランチを照合する。保存済みcwdが同じ場合は保存ブランチも照合する。元worktreeの未コミット、途中コミット、ブランチをそのまま保持し、reset、checkout切替、削除をしない。

担当が意図して別の作業ブランチへ移った場合は、終了・待機の前に `D:/CC/Tools/scheduler/scheduler.exe checkpoint-workspace --request-file <JSON>` で明示的に記録する。JSONには `id`、`job`、`expected_branch`、`branch`、`head` を指定する。`job` は起動処理が渡す環境変数 `CC_JOB_ID` から取得する。保存前後とも `scheduler/todo-<同じ番号>-` の作業ブランチに限る。元ToDoと実ジョブの所有・cwd、現在の保存値、実Gitルートと共通管理領域、現在のブランチと完全なHEAD、両パスの既存保護判定を照合する。別担当が実行中か、生存過程の役割を確認できない場合は保存せず、対象ToDoの行全体とproject登録を共有ロック内で再照合する。出力には件数と番号だけを返し、名前やパスは出さない。

この操作は保存されたブランチと検証receiptだけを更新する。ソース、Gitのref、ジョブ・会話、本人停止、依存、外部待機、起動期限、資格、許可、進み具合は変更せず、ジョブも起動しない。実行時には従来どおりブランチを再検査する。任意の不一致を自動受容したり、別ブランチへのcheckout/resetで合わせたりしない。同じ要求をもう一度送る場合も実状態を再検証し、保存済みなら変更しない。

正規の`work_owner`待機理由が現在の生存担当を正確に指し、同じ物理作業場所・保存済み所有情報を確認できるqueued担当だけは、受付と共通のqueue lockを保持して保存時の競合から除く。監督過程（pid）は誕生時刻の照合が必要で、実行過程（codex_pid）や未確認の過程は許さない。queuedという表示だけでは除外せず、再起動予約、別ToDo/cwd、欠落・矛盾した待機証拠も拒否する。条件は[ToDo3360の仕様](workspace-checkpoint-3360.md)に記す。

保存前に同じコマンドへ`--check`を付けると、元ToDoを変更せず、`eligible`と固定の拒否理由・比較の真偽を確認できる。`CheckpointCompetingWorker`は他担当の実行/生存、`CheckpointWaitUnverified`は待機先の不一致、`CheckpointWaitIdentity`は所有情報の不一致、`CheckpointExpectedBranch`は保存前branchの不一致を表す。共有ToDo本文、他の案件名やパス、ジョブ本文は出力しない。検査の成功は保存を意味せず、通常保存と結果の読み返しを別に行う。排他用のディレクトリやlockファイルは必要時に作るが、ToDoとGitのデータは変更しない。

ほかに、`CheckpointItemMissing`は元番号不在、`CheckpointTaskState`は完了・本人停止などの対象外状態、`CheckpointSourceMissing`は元job不在、`CheckpointSourceOwnership`/`CheckpointSourceCwd`/`CheckpointSourceNotCurrent`は元担当の所有・場所・現在担当の不一致を示す。`CheckpointHeadMismatch`は実HEAD不一致、`CheckpointRowChanged`は途中の行またはproject変更、`CheckpointQueueBusy`/`CheckpointQueueUnavailable`は受付ロックの取得不能を示す。入力不正は`InputRejected`、既存保護とGitの拒否は`ClientResource`/`NonGit`/`RepositoryMismatch`/`BranchMismatch`など既存の理由を保つ。

専用作業場所以外のcwdは、保存receiptと元ジョブの所有をもう一度照合してから使う。別リポジトリや不存在の元cwdは別の空worktreeで置き換えない。Gitの新規作成だけがorigin/mainの取得を必要とする。稼働時は原場所、再開元、専用作業場所を `scrub391 --guard-path` で判定し、案件資源を除外する。判定不能も書込みへ進めない。

準備失敗は元ToDoの `scheduler.preparation_failure` に固定の理由、時刻、再確認期限を残す。理由はcwd不在、Git確認不可、origin不在、ブランチ不一致、管理領域不一致、ツール不在、入力拒否、資源待ち、対象除外、処理失敗。stderr、案件名、コマンドライン、資格情報は残さない。通常は5分待たせ、独立した項目の起動を同じ回に続ける。失敗をオーナーの判断待ちへ転用しない。

Claudeのモデル名は起動選択で `anthropic/` を付ける。指定したHaikuを別モデルへ置き換えない。保存された利用枠証拠が全対象アカウントの100%消費と将来のresetを示す場合、その期限まで待つ。新しい利用枠問い合わせは起こさない。HTTP429を記録した実行中ジョブは既存の終了経路でツリー終了を確認してから資源待ちへ戻す。通常の30分CPU静止判定は変更しない。

待機期限はモデル選択の更新、準備、取消結果の再反映、担当なしの明示再開で短縮しない。資源待ちの取消結果は元ToDoの結果台帳へ一度だけ記録する。起動直前には共有ToDoのOS lock内で保存した仕様、資源、所有、作業場所、待機期限の変更を再確認する。

回帰検査は `cc-scheduler/tests/parity.rs` の `preparation2164_*` と、cc-todoの再開・結果台帳検査を使う。実Gitで原worktree、途中コミット、未追跡ファイルの保持を確認する。共有ToDo全体の再受付、他担当の既完了処理、依存waitの解除はこの修復の範囲に含めない。
