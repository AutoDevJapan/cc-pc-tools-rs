# 親への結果返却とDiscord配送（ToDo3257）

子の終了結果は、仕事の完了と未完了での停止を区別して親へ保存する。
送信予約やCLIの終了コード0を、仕事の成功や報告済みの証拠にしない。

### 2026-10-21の本人による通知条件の訂正

新しい結果ではowner_requiredだけでメンション要否を決める。owner_reply、正常完了、技術停止、保留、依存待ちだけではメンションしない。
新会話と従来のlog/postの両方で、誤って--notifyが付いた報告の先頭が「対応不要」「報告のみ」「本人対応不要」なら無通知で配送する。通常の報告も無通知とする。具体的な「回答が必要：質問」と異なるa/bを持つ質問、または既存の具体質問の判定を通るものだけ通知候補にする。選択肢にある「対応不要」は禁止せず、報告の先頭の対応要否とは分ける。見出しだけ・空の質問・同じ選択肢はPOST前に具体的な入力エラーとして親へ返す。本当に本人が決める質問は正規ask/decisionで用意し、ラベルだけで新しい判断待ちを作らない。
旧未送信のIntentも、元入力・ID・fingerprintを保持し、新しいPending.effectだけを最新条件で決める。送信結果不明のPendingはGETで回復し、再POSTしない。既配送本文や既存の送信予約は書き換えない。
Rustの受領票はrequested_notifyとeffective_notifyを分けて返す。実payloadは無通知時にusers/roles/parse空、replied_user=false、flags=4100となり、GETで本人mentionsが空であることを検査する。Goはeffective_notifyに従って独立のGETも照合し、旧notify_ownerを保持したまま実結果を追記する。GETがallowed_mentions自体を返すとは扱わない。
旧経路では本文の送信payloadにparse空と通知抑制を指定し、返信参照を作らない。本物の質問にeffective_notify=falseが返った場合は確認済みにせず、owner_action_notification_missingを親へ返して自動再送しない。依存待ちの署名には待ち先を含め、時計や回数だけの更新では再投稿しない。旧署名は一度基準移行し、過去の待機を一斉に再報告しない。

## 対象と境界

- 既存unattended-runnerのagent-result入口とCC-DiscordAskTickを使う。
- 親が受領したmulti_agent_v1の結果、子IDが返らない起動失敗、親自身の停止をrecordで保存する。IDがなければ元ToDo・依頼ID・試行IDで対応を保つ。
- watchで明示登録したcodex_jobsの稼働中担当だけを、保存済みjob.jsonから観測する。過去の終端は登録せず、一斉走査・状態変更・再投稿しない。
- この実装の対象製品はcc-pc-tools-rs。別製品の配送先を推測しない。
- 内蔵APIの終端監視、親自身が結果を保存する前に消失した内蔵セッション、未登録の外部担当は対象外。親は起動APIの失敗と子の終端を受領してrecordへ渡す必要がある。全サブエージェントを修復済みとは扱わない。

## 入出力

record入力はrequest_id、todo_id、project、thread_id（元投稿）、source、parent_id（任意）、child_id（任意）、attempt_id、transition_id、state、reason、remaining、next_action、owner_required、owner_reply（任意）、private_ref（任意）。親が本人依頼への返答を保存するときはowner_reply=trueとする。この区別は記録用であり、返答だけを理由にメンションしない。
公開本文は固定の日本語説明と元ToDoから生成し、原エラー、私的参照、認証値を転記しない。本人の対応要否を先頭に書く。owner_required=falseなら「対応不要です。AI側で…」と具体的な次の処置を示し、返信やPC操作を求めない。
owner_required=trueではowner_questionにquestion、choice_a、choice_b、urlを指定する。親が既に通常経路で用意した本人質問の公開用・秘密なしの文と選択肢、回答先のDiscordメッセージURLだけを使う。質問には本人が決める必要のある点を具体的に書き、技術復旧だけを本人へ求めない。各文は空でない1行・UTF-8で300bytes以内、制御文字・mentionなし、a/bは異なる内容。URLの書式と設定済みサーバー内であることを検査する。実際の既存質問との対応は呼出側が正規askの受領証で確認する。URLだけで実在・ToDo対応を検証したとは扱わない。
直接recordする新規入力で質問か選択肢がなければ受領エラーとして親へ返し、曖昧な依頼を配送しない。false時の質問指定も拒否する。本文先頭に「回答が必要：」と質問・a/b・回答先、「この対象だけ回答を待ち、AI側はほかの作業を続ける」を示す。新しい質問を結果投稿から登録する入口ではない。公開用の質問も通常senderの秘密/mention検査を通す。サーバー外URLはerror_code=owner_question_scope_invalid/delivery=invalid_inputで保持し、自動再試行しない。
state/reason/remaining/next_actionは列挙値。blocked by policy等の自由文の引用で拒否を推定しない。
清掃の個別保留はstate=item_deferredとして、item_id、item_kind、capacity_bytes、capacity_scope（complete/partial/unknown）も記録する。具体対象の種類と識別子を公開本文へ示し、対象の場所・元指示はprivate_refへ分離する。partialを対象全体の容量とせず、unknownで容量を推測しない。
同じ依頼・試行・遷移は同じ親の記録へ対応し、内容の違う再送は拒否する。
recordの成功は親への永続受領だけを意味する。Discordのdelivery=verifiedとは別である。

watchは稼働中の外部担当を登録し、以後の終了・依存待ち・利用枠待ちをrecordと同じ保存先へ渡す。
結果を確認できないまま1時間を超えた場合は、成否未確認の結果を親へ一度返す。担当の実行状態は変更せず、その後の終了や新しい個別保留の観測も続ける。再登録の間に保留を失う隙間を作らない。
外部担当のstatus=succeededでも、最終回答の先頭が状態: 完了でなければ未完了とする。
終了理由は構造化状態だけから決め、本文中の語句検索は行わない。
登録済みの外部担当がjob.jsonのitem_deferred配列へ新しい個別保留を保存した場合は、status=runningのままでも親へ返す。各項目はitem_id、item_kind、transition_id、reason、capacity_bytes、capacity_scope、next_action、owner_required、private_ref（任意）を持つ。1回の保存は最大32項目。同じ対象・遷移の再観測は同じ結果記録へ戻す。登録時点の既存保留は送らない。
確認した既存cleanup-8hのretained_*とcapacity-audit-3hのcategoryResultは集計値で、個別対象の保留記録ではない。その件数から対象や原因を作らず、この実装だけで既存清掃全入口を接続済みとはしない。担当が個別保留をrecordへ渡すか、上記の構造化進捗へ保存する必要がある。

## 配送と回復

親の受領記録を先に保存する。親や送信処理が終了しても既存1分巡回が未配送を引き継ぐ。
recordの受付は長い巡回のrunner.lockやDiscord認証・状態に依存しない。受付同士の短い排他だけを最長2秒で待つ。混雑時は終了75とbusy_retry_same_inputを返し、親は同じ入力を保持して再試行する。設定・ディスク等の失敗を受領済みとはしない。
親の同一入力の再試行は200ms間隔で2回までとし、なお未受領なら入力と具体的な失敗を元作業に保持して既存の再開手順へ返す。Windowsではロック競合33だけを混雑として扱い、開けない・権限不足・共有違反を混雑としない（[Microsoftのエラー定義](https://learn.microsoft.com/en-us/windows/win32/debug/system-error-codes--0-499-)）。受付待ちは親の期限にも従う。
配送処理は20秒、通常処理は従来の50秒、既存CI巡回は15秒を確保する。全体は終了待ちを含め90秒で、既存タスクの2分以内に収める。
配送は製品別の通常conversation logを使う。2026-10-21の最新本人訂正「対応不要なら俺にメンションしないで。対応が必要な時だけ俺をメンションして。」に合わせ、notify_ownerはowner_requiredだけで決め、親の受領時に保存する。出所は主チャットの本人直接入力として親から渡された原文、元ToDo3257/3264と共通正本先頭のOwnerMention:20261021:ActionRequiredOnly:MainChat。旧「返答や停止なら対応不要でも必ずメンション」は履歴であり、今後の送信条件には使わない。
本人の作業・確認・回答・判断が必要で、具体質問と回答先を持つ場合だけ通常senderへ--notifyを渡す。対応不要の依頼返答・正常完了・技術停止・拒否・ID返却前の起動失敗・異常終了・時間切れ・利用枠待ち・依存待ち・キャンセル・親停止・個別保留・定期観測は、親へ永続保存してメンションも返信pingも通知もない記録として配送する。停止/保留の報告自体を省略せず、技術停止を通知のためにowner_required=trueへ変えない。添付は付けない。
新規の本人通知は通常の「【PCの道具】担当の結果」話題へまとめる。thread_idは元投稿として記録に保持し、質問の進捗スレッドへ通知を強制しない。通常senderの話題の対応と配送IDを使い、実際の配送先はstatusのurlで親が確認する。旧保存記録は元の明示配送先も維持する。
notify_ownerのない旧保存記録は従来の通知なし指定を維持する。既存verifiedの再投稿や過去全件のメンションはしない。CLI statusにもnotify_ownerを返す。
送信後の通常配送結果とDiscord GETで本文・投稿者・宛先・通知指定を照合してからverifiedを保存する。新規通知では本文の本人mentionが1回、mentionsが設定済み本人1名のみ、everyone/roleなし、通知抑制4096なし、埋め込み抑制4ありを必須とする。対応不要の新規記録と旧記録は本文mentionなし・通知抑制ありを確認する。
通常送信側が本文末尾へ付けるrecovery識別行も形式を照合する。本文は親の受領時点で固定し、将来の文言変更で未配送の入力を変えない。
通常senderはrequest_idが違っても本文等の表示が同じlogを同じ送信へまとめる。そのため新規結果の本文末尾には実際の親受領IDを短縮した結果記録番号を固定し、同じ説明になる別の停止・保留も区別する。同じ遷移の再試行では番号も本文も変えない。番号は原文や秘密値ではなく、元ToDo/依頼/担当/試行/遷移から得た不透明な識別子。既存の固定済み本文は書き換えず、過去分を再投稿しない。
送信応答や最後の保存を失った場合も同じ配送IDを使い、通常送信側の予約と重複防止を再利用する。
1巡回に送信は1件、1件の再試行は最大4回。失敗と次回時刻を保存し、上限後も未配送を保持する。resumeで同じ遷移の有限な再試行を再開できる。
巡回は件数と時間を制限し、順番を保存する。新しい常駐や監視ループは作らない。
確認済み記録と終端済み監視は履歴用の別フォルダへ移し、未配送・稼働中の処理枠を消費させない。statusと同じ入力の再試行から履歴を引き続き参照する。既存の通常進捗とは別の枠なので、同じ巡回で通常進捗1件と結果配送1件の計2件になる場合がある。
不正なitem_deferredはその項目だけを除いてhealthへ記録する。正しい項目と担当の終了結果は失わない。状態を観測できないだけのときは、担当の停止・失敗と断定しない。
質問が不足したowner_required=trueの個別保留も不完全な入力として扱う。watchは親へitem_progress_invalidの技術記録を一度保存し、元jobを変更せず、親が具体質問を準備する。具体質問を欠く本人要求を配送せず、正常な別項目と終端は継続する。
担当の状態遷移、観測不能、結果待ち期限の3つを分離して重複を抑止する。観測が一時復旧しただけで同じ待機を再投稿しない。観測できない対象の成功・失敗や消失を推測して監視を終了させず、未確認として親へ返す。
同じ連続した待機では再試行予定時刻や回数が変わっても再メンションしない。runningへ戻ってから新しく待機した場合は新しい遷移として報告する。旧方式の署名で保存された待機は初回観測を基準に移行し、署名方式の変更だけを理由に過去の待機を再投稿しない。

## 受入条件

1. 正常完了、未完了、拒否、異常終了、時間切れ、利用枠待ち、依存待ち、キャンセル、子IDなし起動失敗、親停止が親の記録へ戻る。
2. 引用文だけで拒否としない。CLI successだけで仕事成功としない。
3. 親への受領後に再起動しても配送でき、予約だけでは報告済みにならない。
4. 同じ遷移を重複投稿しない。送信失敗・未確認・再試行上限を保存する。
5. 公開文に原エラー、カード・認証値・受託部内容・私的参照を含めない。
6. 専用の有限停止1件が指定投稿へ届き、通常配送とGETのmessage ID/URLを確認する。
7. 担当がrunningのままでも、新しい清掃の個別保留が親へ保存され、対象・理由・実測容量または未計測・次の処置・本人判断の要否を配送できる。
8. 全状態で、本人対応が不要なら返答や停止も通知なしで配送し、本人対応が必要なら具体質問/選択肢/回答先と本人1名のメンションを確認する。owner_replyはメンションを発生させない。

## 親が使う入口

`unattended-runner.exe agent-result record --input <親が受領した構造化結果JSON>` は親への永続受領であり、配送の成功ではない。
`agent-result status --id <record_id>` のdelivery=verified、message_id、urlで配送確認を照合する。
`agent-result watch --input <外部担当を指定したJSON>` は登録済みの1担当だけを観測する。通常の1分巡回が観測と配送を引き継ぐ。
`agent-result drain` は同じ既存経路を20秒以内で1回処理する。上限後の同じ記録は `agent-result resume --id <record_id>` で有限に再試行する。

record入力の例（IDと宛先は実際の受領に合わせる）:

```json
{"request_id":"todo3257-result","todo_id":3257,"project":"cc-pc-tools-rs","thread_id":"1562283280416379024","source":"launcher","attempt_id":"attempt-1","transition_id":"initialization-1","state":"start_failed","reason":"initialization_rejected","remaining":"work","next_action":"retry","owner_required":false}
```

watch入力は同じ書式でsourceをcodex_jobs、child_idを実担当IDへ設定する。state/reason/remaining/next_actionは観測時に上書きするテンプレートで、stopped/work_incomplete/unverified/review_resultを指定できる。登録自体は終了や配送を意味しない。
watchから自動で作る内部結果や技術記録ではowner_reply=falseとする。親が本人への正常な返答を行う場合は、source=parent、別の親返答transition_id、state=completed/reason=completed/remaining=none/next_action=none/owner_required=false/owner_reply=trueで保存する。子の内部完了の既存遷移を後から本人返答へ書き換えない。

正本・AGENTS・agent-job-managementの変更は別の文書担当が扱う。

## 通常log/postで本人へ通知するとき

本文へ手書きのmentionを入れず、通常CLIの--notifyだけを指定する。既存senderが設定済み本人のmentionを本文へ1回追加し、allowed_mentionsは本人だけに限定する。本文を分割しても通知は先頭の1件だけ。GETは最終本文・本人だけのmentions・flagsを照合する。allowed_mentions自体をGETから取得したとは扱わない。

本文へ<@...>を手書きした入力は、senderのno_mentions検査で送信前に拒否される。外側のCLIは「新しい質問・報告の処理が完了していません」と表示するため、一般文だけで配送遅延や上位実行審査の拒否と判定しない。原文と予約を履歴に保持し、実台帳を確認できない場合は配送有無を未確認とする。誤入力を自動整形して将来送信する互換処理は加えない。親が正しい本文+--notifyで通常送信し、誤入力との対応を記録する。質問用の進捗スレッドは引き続き通知できない。
