# ジョブの起動箇所とモデル（2026-10-07）

Rustの実際の起動箇所を調べた表です。自動で選ぶ場合の強さはすべてhighです。個別に指定したモデルと強さは、それぞれ独立して優先します。保存済みの再開ジョブのモデルやmaxも保ちます。

| 起動箇所 | ジョブ名 | 用途 | 自動で使うモデル | 指定の優先 |
|---|---|---|---|---|
| cc-jobs/src/cli.rs | runで渡された名前 | 共通受付。下記の軽い名前と作業種別を判定 | 軽い作業はgpt-6-luna、実装はgpt-6.1-sol | --modelと--effortがそれぞれ優先。未指定はhigh |
| cc-jobs/src/jobs.rs | 登録済みの同じ名前 | Codexの新規起動・再試行・枠を譲った後の再開 | 登録済みの値 | 保存値を保つ。文脈更新の引き継ぎも同じモデル |
| cc-ask/src/intake.rs → core.rs | req-{スレッド末尾6桁} | 依頼を受け付ける | 実装はgpt-6.1-sol、軽い作業はgpt-6-luna | 保存済み選択を優先 |
| cc-ask/src/intake.rs → core.rs | diag-{スレッド末尾6桁} | 停止した担当の原因を調べる | gpt-6-luna | 保存済み選択を優先 |
| cc-ask/src/resume.rs → core.rs | resume-{スレッド末尾6桁} | 質問への回答を受けて続きを進める | 作業の種別で選ぶ | 保存済み選択を優先 |
| cc-ask/src/steering.rs → core.rs | resume-{スレッド末尾6桁} | 新しいオーナー指示を同じ担当へ反映 | 元のモデル、なければ作業の種別で選ぶ | 元の選択と明示された設定を保つ |
| cc-ask/src/restart.rs → core.rs | resume-{スレッド末尾6桁} | 元のセッションから停止作業を再開 | 元のモデル | 保存済み選択を保つ |
| cc-ask/src/routing.rs → core.rs | followup-{スレッド末尾6桁} | 終了した質問・依頼への追加投稿を受け付ける | 実装はgpt-6.1-sol、軽い作業はgpt-6-luna | 保存済み選択を優先 |
| cc-ask/src/patrol.rs → core.rs | patrol-{セッション末尾6桁} | 旧セッションを引き継ぐ互換処理 | 元のモデル | 保存済み選択を保つ。通常の巡回は廃止済み |
| cc-scheduler/src/backend.rs | todo-{番号} | 共有ToDoの作業を起動する | 実装はgpt-6.1-sol、軽い作業はgpt-6-luna | ToDoのmodelとeffortがそれぞれ優先 |
| cc-shepherd/src/lib.rs | shep-{PR番号} | PRの修正・テスト・仕上げを行う | gpt-6.1-sol | 実装を含むためhighを保つ |
| cc-todo/src/recovery.rs | stall-todo-{番号} | 停止したToDoを再開する | 元のモデル、なければ実装はgpt-6.1-sol | 元のmodelとeffortを保つ |
| cc-live/src/review.rs | rev-{名前} | PRの差分を読むレビュー | gpt-6-luna | 閲覧専用のレビューとしてhighを指定 |

## 名前と作業種別の判定

| 名前または作業種別 | モデル | 強さ |
|---|---|---|
| rev-で始まる、または-reviewで終わる名前 | gpt-6-luna | high |
| diag-、audit-、inspect-、check-、verify-、research-で始まる名前 | gpt-6-luna | high |
| cleanup-、clean-で始まる名前 | gpt-6-luna | high |
| review、code_review、documentation、research、live_debug | gpt-6-luna | high |
| verification、check、confirmation、audit、inspection | gpt-6-luna | high |
| cleanup、housekeeping | gpt-6-luna | high |
| implementation、pr_shepherd、その他 | gpt-6.1-sol | high |

共通の名前・作業種別の判定はcc-job-policyのmodel_profileです。大文字小文字を区別しません。名前にreviewという文字が含まれるだけでは分類を変えません。モデル選択の自動応答がmaxを返してもhighを使います。モデルを除外して再選択する場合は除外を守ります。

調査範囲はD:/CC/Tools/rsのRustソースと、このリポジトリのRustソースです。cc-channel-watchは投稿を受け付ける別ツールの起動だけで、Codexのモデルを指定しません。cc-chromeとcc-task-titleもCodexを起動しません。cc-liveのdriverとmerge_prは元のship/review呼び出しを経由し、独自のCodex起動はありません。
