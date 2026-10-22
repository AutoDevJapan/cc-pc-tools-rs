# native実行の操作結果とCC jobを区別する

元3103/3084/3137の「実際に作業しているものだけ」の受入。UUIDや親からの状況文をCC job IDへ変換しない。登録・委譲・監査は製品開発の成果へ加算しない。

既存loopback専用APIに、ローカルの担当実行環境が実際に受け取ったTool結果の限定メタデータを渡す。APIは結果を独自に実証するOS監視器ではない。登録されたsourceだけの部分観測であり、全native数、継続heartbeat、全製品の自動監視が実装済みとは扱わない。過去の1回のsnapshotから継続稼働を作らない。原rollout・会話・コマンド本文・環境変数・秘密を受信/走査しない。

1. `POST /api/agent/native/register`: `session_id`（実際のnative UUID）, `todo_id`（主作業1件）, `role` (`work`/`review`/`audit`/`supervision`), 任意の `business_project`（既存公開カタログの製品）。応答の `generation` を保持する。登録だけでは実作業にならない。製品はコードの編集先repoと区別するが、本文の保護や元の製品identityを解除しない。
2. 実編集・検査・ビルド・調査などのToolが実行された結果だけを `POST /api/agent/native/activity` へ送る。`session_id`, `generation`, `actual_at_ms`（実結果の元時刻）, `event_id`（実call/resultの一意な対応）, `tool` (`exec_command`/`apply_patch`/`cua_repl`/`runtime_tool_metadata`), `kind` (`edit`/`test`/`build`/`inspect`/`publish`/`review`), `executed: true`, 任意の `exit_code`。上位審査で未実行なら `executed: false` とし、これは受理・計数しない。登録やこの受信APIへの書込み自体を作業結果にしない。時刻を後の受信時刻に置き換えない。
3. 正常終了・依存待ち・失敗・取消し・拒否で担当を閉じるときは `POST /api/agent/native/close` へ `session_id`, `generation`, `ended_at_ms`, `outcome` (`success`/`waiting`/`failed`/`cancelled`/`refused`)。即座にliveから除外し、結果は保持する。これはプロセス終了やToDo完了の偽装ではない。別世代の終了を現世代へ適用しない。

30秒以内の実Tool結果だけをliveへ投影する。監査・監督roleは実作業数へ含めない。通常start済みdoingのToDoだけを対象とし、wait/block/recovery_unverified/doneの記録を自動再開しない。CC jobと同じ元ToDoは二重加算しない。複数nativeはsessionとgenerationの異なる実行証拠を別々に数え、元のToDo状態は最初の1回だけ保持する。再登録やファイル更新だけでは期限を延ばさない。`GET /api/agent/native` は接続済み担当の観測・非可逆な実行キー・確認/終了時刻・結果を返し、書込みを行わない。API/画面にはnative UUIDをCC jobとして出さない。

CCのprocessingJobKeyの厳密一致は保持する。nativeは同じToDoに結び付いたnative実行キーと実証拠が一致した場合だけ別経路で投影し、CCのキーを借りない。終了済み・監査・監督のbindingで所属を移さず、unknown/MCPの既存identityはnativeの表示先指定で救済しない。通常のボードに全binding履歴は載せず、投影できた件数と観測範囲だけを載せる。

現用保存は固定の `agent-progress/native-activity.json` とし、job/ToDo原本には書かない。破損を空の正常結果に置き換えず、取得不能とする。登録変更/再開は世代を進め、古い結果と終了を再利用しない。全結果は元の担当から親へ返し、親が既存製品topicへ永続記録・配送確認する。本人対応不要の結果にメンションを付けない。

終了後に再利用する世代、および2048件の現用上限に達した際の最古の終了128件は、先に `agent-progress/native-activity-closed/<内容SHA>.json` へ不変保存する。保存成功後だけ現用枠を再利用し、終了結果を捨てない。現用全件が未終了なら503を返す。書込みとfsyncはblocking処理へ分離し、読取用状態lockをI/O中に保持しない。

仕事量の観測cacheはキー変更に合わせて `workload-observations-v2.json` に分ける。元v1ファイルと過去結果を上書きせず、古いセッションキーを新計数へ混ぜない。

作業記録（ToDo行）と実作業は別の単位とする。部署・製品の確認した実作業数は、期限内のCC/native実行キーを重複除去した件数で、状態別のToDo件数とは区別する。未知の観測を0件へ変換しない。

各世代の登録時刻を実結果の下限とする。runtimeは操作前に登録し、圧縮・再開・初回登録より前の結果を新しいscopeへ付け替えない。native台帳が取得不能ならnativeの件数はnull/未確認とし、取得できた0件と区別する。

## 2026-10-23の正常0件と取得失敗（元3084/3137）

本人の主チャット直接要件（受領観測07:14 JST）:「一部取得とか未観測ってなってる部分の修正はできないの？0なら0でいいと思っている。」既存配置と画像aの承認範囲を保ち、取得・表示を修復する。

正常に読み取れた接続済み担当が空なら0件を返す。`state/complete` はこの有限な取得の成否であり、PC全体の担当を発見できた意味ではない。`scope=connected_sessions`、`globalInventoryComplete=false` とし、全PC人数の `processing.total` は未確定のまま。未接続の担当を0件と断定しない。本文保護は取得状態の分母から分け、正常な実行状態・ビルド0件を隠さない。

手動の結果投稿がないnativeを落とさないため、登録した実sessionへ `POST /api/agent/native/connect` を接続する。入力は `session_id/generation/rollout_path`。本人の通常Codex session保管先内の、同じUUIDを持つ元記録だけを検証し、接続時の末尾から読む。全ディスク・全sessionの走査や別ToDoの原文読取はしない。HTTP GETはファイルを走査せず、既存常駐内の2秒周期・最大8接続/回・1MiB/接続/回の背景処理が新しい呼出しIDと結果ID・元時刻を照合する。1MiBを越す単一記録、破損、切詰め、読取失敗、取得遅延は成功0へ変換しない。引数・メッセージ・Tool出力本文は保存せず、API/画面にも出さない。

実行コマンド/編集/UI操作の結果だけを既存世代の証拠へ反映する。登録、時計取得、状態文、監督/監査役、単なる待機、拒否、未対応の結果、接続前の結果を数えない。元taskの終了はliveから外し、ToDo全体の完了にはしない。正常なclose、CCとの同一ToDo重複除去、古い世代の拒否を維持する。

単一の取得に失敗しても独立した正常取得の件数と有効0は保持する。失敗した担当の種類、理由、最後の成功時刻、自動再試行を普通の言葉で表示する。内部source名の羅列や、本文保護を全データの取得失敗へ変換する表示は使わない。正常0/実1/終了0/失敗/重複/古い記録/遅延を関連Rust・実JavaScriptのGo検査で確認し、別モデルMax review・CI・正式Prd・本人Chrome受入を元ToDoへ記録する。登録だけ、検査だけで完成としない。
