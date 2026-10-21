# 親への結果返却とDiscord配送（ToDo3257）

子の終了結果は、仕事の完了と未完了での停止を区別して親へ保存する。
送信予約やCLIの終了コード0を、仕事の成功や報告済みの証拠にしない。

## 対象と境界

- 既存unattended-runnerのagent-result入口とCC-DiscordAskTickを使う。
- 親が受領したmulti_agent_v1の結果、子IDが返らない起動失敗、親自身の停止をrecordで保存する。IDがなければ元ToDo・依頼ID・試行IDで対応を保つ。
- watchで明示登録したcodex_jobsの稼働中担当だけを、保存済みjob.jsonから観測する。過去の終端は登録せず、一斉走査・状態変更・再投稿しない。
- この実装の対象製品はcc-pc-tools-rs。別製品の配送先を推測しない。
- 内蔵APIの終端監視、親自身が結果を保存する前に消失した内蔵セッション、未登録の外部担当は対象外。親は起動APIの失敗と子の終端を受領してrecordへ渡す必要がある。全サブエージェントを修復済みとは扱わない。

## 入出力

record入力はrequest_id、todo_id、project、thread_id、source、parent_id（任意）、child_id（任意）、attempt_id、transition_id、state、reason、remaining、next_action、owner_required、private_ref（任意）。
公開本文は固定の日本語説明と元ToDoから生成し、入力の文章、エラーログ、私的参照、認証値を転記しない。
state/reason/remaining/next_actionは列挙値。blocked by policy等の自由文の引用で拒否を推定しない。
清掃の個別保留はstate=item_deferredとして、item_id、item_kind、capacity_bytes、capacity_scope（complete/partial/unknown）も記録する。具体対象の種類と識別子を公開本文へ示し、対象の場所・元指示はprivate_refへ分離する。partialを対象全体の容量とせず、unknownで容量を推測しない。
同じ依頼・試行・遷移は同じ親の記録へ対応し、内容の違う再送は拒否する。
recordの成功は親への永続受領だけを意味する。Discordのdelivery=verifiedとは別である。

watchは稼働中の外部担当を登録し、以後の終了・依存待ち・利用枠待ちをrecordと同じ保存先へ渡す。
結果を確認できないまま1時間を超えた場合は、成否未確認の結果を親へ返して観測を終える。担当の実行状態は変更しない。必要な継続観測は親が新しい試行IDで明示登録する。
外部担当のstatus=succeededでも、最終回答の先頭が状態: 完了でなければ未完了とする。
終了理由は構造化状態だけから決め、本文中の語句検索は行わない。
登録済みの外部担当がjob.jsonのitem_deferred配列へ新しい個別保留を保存した場合は、status=runningのままでも親へ返す。各項目はitem_id、item_kind、transition_id、reason、capacity_bytes、capacity_scope、next_action、owner_required、private_ref（任意）を持つ。1回の保存は最大32項目。同じ対象・遷移の再観測は同じ結果記録へ戻す。登録時点の既存保留は送らない。
確認した既存cleanup-8hのretained_*とcapacity-audit-3hのcategoryResultは集計値で、個別対象の保留記録ではない。その件数から対象や原因を作らず、この実装だけで既存清掃全入口を接続済みとはしない。担当が個別保留をrecordへ渡すか、上記の構造化進捗へ保存する必要がある。

## 配送と回復

親の受領記録を先に保存する。親や送信処理が終了しても既存1分巡回が未配送を引き継ぐ。
配送処理は20秒、通常処理は従来の50秒、既存CI巡回は15秒を確保する。全体は終了待ちを含め90秒で、既存タスクの2分以内に収める。
配送は製品別の通常conversation logを使い、通知・メンション・添付は付けない。
送信後の通常配送結果とDiscord GETで本文・投稿者・宛先・無通知フラグを照合してからverifiedを保存する。
送信応答や最後の保存を失った場合も同じ配送IDを使い、通常送信側の予約と重複防止を再利用する。
1巡回に送信は1件、1件の再試行は最大4回。失敗と次回時刻を保存し、上限後も未配送を保持する。resumeで同じ遷移の有限な再試行を再開できる。
巡回は件数と時間を制限し、順番を保存する。新しい常駐や監視ループは作らない。

## 受入条件

1. 正常完了、未完了、拒否、異常終了、時間切れ、利用枠待ち、依存待ち、キャンセル、子IDなし起動失敗、親停止が親の記録へ戻る。
2. 引用文だけで拒否としない。CLI successだけで仕事成功としない。
3. 親への受領後に再起動しても配送でき、予約だけでは報告済みにならない。
4. 同じ遷移を重複投稿しない。送信失敗・未確認・再試行上限を保存する。
5. 公開文に原エラー、カード・認証値・受託部内容・私的参照を含めない。
6. 専用の有限停止1件が指定投稿へ届き、通常配送とGETのmessage ID/URLを確認する。
7. 担当がrunningのままでも、新しい清掃の個別保留が親へ保存され、対象・理由・実測容量または未計測・次の処置・本人判断の要否を配送できる。

## 親が使う入口

`unattended-runner.exe agent-result record --input <親が受領した構造化結果JSON>` は親への永続受領であり、配送の成功ではない。
`agent-result status --id <record_id>` のdelivery=verified、message_id、urlで配送確認を照合する。
`agent-result watch --input <外部担当を指定したJSON>` は登録済みの1担当だけを観測する。通常の1分巡回が観測と配送を引き継ぐ。
`agent-result drain` は同じ既存経路を20秒以内で1回処理する。上限後の同じ記録は `agent-result resume --id <record_id>` で有限に再試行する。

正本・AGENTS・agent-job-managementの変更は別の文書担当が扱う。
