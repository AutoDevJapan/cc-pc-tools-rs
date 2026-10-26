# 完了したPCツール作業のビルド生成物を1時間清掃で回収する

元3942。既存Rust cleanup-8hを拡張する。新しい常駐・Windowsタスク・所有登録は作らない。
3757は2026-10-26 16:25にdone。旧jobの不要なwait再登録で依存世代が変わったため、
通常のcancel/再割当で復旧した。現在job20261026-164814-todo-3942とRootの世代2が一致。
完了済み3757へのwaitは再登録しない。
safe-deleteのソース・配布対象は本件で変更しない。

## 受入条件

- 通常の共有ToDoストアでproject=cc-pc-tools-rs/status=doneの登録worktreeを選ぶ。
  schedulerの元job、CWD、work_owner、実Git remoteの所有者・branch・commonを照合する。
  新しい任意pathの所有登録や、コピーした未知データの資格化は行わない。
- 最初の対象はこのPCの自社リポジトリのignored `target/debug`だけ。C/D内の絶対パスと
  worktree境界を検証し、祖先・子のreparse、外部hardlink、未保存ソースを保持する。
  source/Git/release、原receipt・検証記録、現用の実行物、他担当の領域を削除しない。
- queue/ToDoとCargoの既存OS lockを資格確認から削除まで保持する。原状態を再照合し、
  別の実行中・待機ジョブ、未完依存、保持・現用のファイルがあれば当該対象を保持する。
  profileのlockとそのディレクトリはCargoの並行開始を防ぐため残し、成果の実不在と区別する。
- default/--dry-runはファイル変更なし。--applyだけ既存ハンドル削除を使う。全体期限・件数・
  1回の対象数を制限し、新入口の失敗で従来の削除希望・ごみ箱処理を失わない。
- 候補論理bytes、実削除bytes、free before/after、実不在、保持・未測定・部分失敗を分ける。
  私的なファイル名や元仕様をログへ出さない。既削除8件の51,237,263,964Bを再加算しない。
  一部の走査終了を全PC清掃完了としない。同じ保持を毎回Discord送信しない。
- 境界、並行開始、元状態変化、未知guard、ソース・現用保持、実削除・部分失敗を回帰テストする。
  変更と利用側だけテストし、別の実モデルのレビュー、同headのCI後に通常PRをマージする。
- 元配布票/CAS/ACL/既存AdminBrokerでcleanupだけ反映する。fingerprintと既存1時間タスクの
  実発火・結果を照合する。不要な本件生成物を回収し、必要な証拠とソースを保持する。

## 実装・検証記録

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/545 。
変更範囲のWindows nextestは35件成功、Clippyも成功。実行中exe、別プロセスのCargo lock競合、
未知guard、未pushソース、原状態変化、junction/hardlink、置換・部分失敗を含む。
cleanup-8hはbinaryのみでdoctest対象なし。既存cc-jobs依存を追加したため、最終CIはロック変更の
通常選択規則に従って全workspace/doctestを1回実行する。PCでは同じ全件を重ねない。
元の管理caseと3757の原記録は変更しない。詳細な原結果は本担当の所有証拠フォルダーへ保存する。
