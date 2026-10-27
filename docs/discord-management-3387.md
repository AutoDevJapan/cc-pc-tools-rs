# Discordの対応漏れと新しい依頼を元作業へ接続する（ToDo3387）

読取修復: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/396 （PR397配布済み）

通常受信の修復: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/400

## 元依頼と担当

2026-10-23の本人依頼は、直近1か月のDiscordから対応漏れを調べ、
全プロジェクトで漏れと新しい依頼を元の作業へ結び、実対応を追うこと。
継続監督は既存OMB、3387は有限の整備担当とする。
3327の原票・索引・manifest・scopeと単一の照合担当を保持する。
照合済みの件数を実装完了件数と呼ばない。

共有ToDoと既存の会話受信記録を正本とし、別のregistry、常駐、定期予定、
司令塔を追加しない。本人の投稿ごとに新taskを作らず、追記・回答・訂正は
元の質問世代と対象を確認して元taskへCASで結ぶ。
本人の停止、後の訂正、同じ画像・範囲への回答、正当な依存を維持する。

## 新しい期間の読取

3331の `monthly-history-inventory` / `monthly-history-read` を再利用する。
元3327の資格は従来の固定期間・固定IDのまま変えない。
3387では同じdomainとguild/本人IDに、`todo_id:3387` を指定する。

- `since` は本人が指定した2026-09-23 00:00 JST以降とする。
- `until` は明示的に固定し、未来を指定しない。
- 1scopeは空でない最大31日間とする。次の差分は別scopeで記録する。
- 元のscope/cursorを変更・上書きせず、同じscopeの正常な前pageだけを
  次pageのcursorに使う。覆域とwatermarkは実取得した区間に限る。
- GET限定、現在の保護判定、実bot/権限/所属確認、本文の秘密検出、
  私的な非上書きreceipt、1回100件/最大10channelの制限は再利用する。
- 403/404/権限不明/削除か不明な対象は取得不能として残す。
  空の原票や全guild完了に置き換えない。

新しい期間への資格追加は、過去のscopeや旧cursorを新期間へ移す許可ではない。
APIの原票・本人本文・秘密・顧客の内容は公開リポジトリへ入れない。
取得した本人本文だけでは判断できない場合、元bot質問等の文脈は元3327/3331の
既存担当へ正確なmessage/channel IDでつなぎ、a/bや題名から推測しない。

## Given / When / Then

1. 元3327の固定scopeがあるとき、新旧CLIで一覧/本文を読むと、
   同じ資格条件が成立し、元scope・原票・cursorのバイト列は変わらない。
2. 3387の独立した直近1か月または終端後のscopeがあるとき、
   同じ正規readerで取得すると、その期間/担当/版に結び付く新receiptが得られる。
3. 未来・空・逆順・31日超・下限前・別本人/guild/ToDoのscopeでは、
   認証を読む前に失敗し、正常receiptを作らない。
4. 別scopeのcursorを使うと、認証を読む前に失敗し、前receiptを保つ。
5. 同じ取得済み入力を再読すると、調査のreceiptを作るだけでは
   task/job/質問/外部投稿を増やさない。実際の元taskへの配送・担当実読・
   操作・テスト・親受領・製品別配送はそれぞれ別の証拠で確認する。

## 継続する受入

受付の発火は既存CC-DiscordAskTickとOMBの既存15分予定に置く。
新しい投稿・編集・遅延・同じIDの再読・保存後の中断・担当再開を、
既存受信とCAS/版hash/元task/依存を使って扱う。
未処理を少数から実際に配送し、同じ担当が読むことと実操作・テスト・結果の
親受領・製品別の無通知配送を確認してから残りを通常の優先順位へつなぐ。
設定保存、send成功、PID存在だけでは運用受入にしない。

OMBへは元の本人依頼と元3327/3331/3110/3153/3259/3392の役割・未完を
重複抑止ID付きの通常sendで伝える。元の受領後の差分だけを追記し、
既存予定の実runが3387を追跡したことをrun ID付きで読み返す。
正常完了、技術停止、依存、保留を親へ返し、本人の対応が不要なら
mention・返信ping・notifyを付けない。同じ状態を再送しない。

## 完了の境界

独立した期間の読取修復だけで3387全体を完了にしない。
未解決のcaseには元task/責任担当/次の手または具体的な取得不能理由を残す。
旧592件、追加分、他チャンネルの未確認、新しい入力、元projectの未完了を保つ。
コード変更は関連テスト、別モデルレビュー、CI、マージ、並行配布の照合、
現用版での少数実API読取を経て採用する。GPU/Civ6/Cargoは使わない。

## 今回のコード確認

変更前、3387の月間/終端後scopeを使う公開CLIのテストはscope段階で失敗した。
最小修正後、`go test -race -run '^Test(Management|Monthly|History)' -count=1
-timeout 120s .` と `go vet .` は成功した。
固定3327、境界時刻、別scopeのcursor拒否、前receiptの保持を含む。
読取修復はPR397のマージ・配布と、現用CLIによる一覧・本人返信の実読取まで
確認した。元担当への配送・実反映とOMB監督の受入は、その読取結果と分ける。

## 普通投稿の版と元作業への結合（Issue400）

発火は既存の通常tickだけとする。既存の`unattended-state.json`にある
Request、Threads、Scans、Discoveryに状態を持ち、別の受信registryを作らない。
従来の質問回答と限定製品の配送を残し、新方式の会話はその専用処理で扱う。

- 普通投稿は完了・報告済みも含めて、`Last`や`After`とは独立に1tick最大2件を
  ID指定で再読する。試行するIDを先に保存し、失敗する先頭だけを繰り返さない。
- 同じIDの編集は元版の私的receiptと本文hashを保存し、同じ元ToDoを保持する。
  現在版のhashはguild/channel/message/author、作成・編集時刻、本文、返信先、
  message型と添付metadataの既知/不明に結び付く。旧版の反映票を流用しない。
- 未取得のIDは旧watermark以下でも捨てない。通常受信の`After`は巻き戻さず、
  別の`replay_before`でcutoverまでを最大1page/tick再読する。深いpageの遅延入力も
  保存した継続位置から回収する。月間監査のscope/cursorには触れない。
- `management_intake: true`を既存設定へ反映すると、同じguildのchannel一覧と
  active threadを既存monthly metadata readerで通常受信へ接続する。
  第三forum、通常text/news、voice/stage内text、public/private threadを含む。
  archiveは親channelと公開/private/joined-privateのendpointを1tickに1つずつ巡回する。
  private系はGUILD_TEXTだけとする。権限とprivate所属は既存`monthlyReadable`で
  本文取得前に確認し、拒否時は対象ID・段階・HTTP状態を既存Discoveryへ残す。
  timestampの同値境界は未確認のまま表示し、全件完了と呼ばない。
- message型と添付は`historyReadMessage`を再利用する。欠落は未知であり、
  `attachments: []`と区別する。添付URLやファイル名を共通metadataへ出さない。
  旧receiptが観測していない型・添付は未知を保持し、項目の追加だけを理由に
  完了済みtaskを再配送しない。

`intake status [--after <message-id>]`は最大50件のmetadataだけを返す。
元投稿ID、本人ID、版hash、元ToDo、段階、`pending_owner: OMB`と次の手、
配送版を含み、本人本文は出さない。設定と登録だけを実運用成功としない。

旧形式の`origin_pending`で担当や次の手が空なら、状態の読取結果にだけOMBと
元の文脈・既存taskとの対応の確認を補う。明示済みの担当と次の手は保持する。
これは元taskの待ち・停止・完了を解除する指示でも、再配送や新規結合の指示でもない。
元Request、source版、ToDo、担当、待機、質問や投稿はこの読取で変更しない。

元task不明の追記・返信、他channelの入力、元taskが複数ある入力は元Requestに
pendingを保持する。従来の依頼forumで本人が作った新規starterだけは従来の
新規登録を使える。同じthreadの停止・完了済み元ToDoが分かる補足を、投稿ごとの
新規ToDoへ変えない。既存で検証済みの「完了した進捗元への新しい作業世代」は
従来の明示Originと世代markerで管理し、その世代の後続返信を同じtaskへ集める。

OMBまたは元担当は原文、後の訂正、元仕様と実対象を読み、元taskを確定した後に
`intake bind --input <private JSON>`を使う。JSONのdomainは
`discord-intake-binding-v1`、必須項目は`message_id, thread_id, author_id,
source_sha256, todo_id, project, evidence`。新taskが必要な実依頼だけを、
既存の通常ToDoへ適切な正式project・spec付きで登録してから結ぶ。
本文の言い換えやa/bだけで元taskや判断世代を推測しない。

bindは同じrunner.lock内で現在のAPI版とsource hashを照合し、既存のguardで
元taskのproject/rootを確認する。通常ToDoのnote/stateのCASと読戻しを使い、
元taskへ版ごとに1つのsource参照を記録する。保存後の中断、CAS競合、配送成功後の
応答消失は同じmarkerと現在のnoteを照合して再開する。待ち・停止・done・担当・
job・依存は変更せず、別workerを起動しない。短いa/bや既存の質問threadをbindで
解釈せず、元の質問回答経路で同じ世代へ接続する。

配送済みと実反映は別の段階である。担当は私的sourceと元仕様を読み、実操作と
テストを行い、`applied/<message-id>.json`へ現在版の`source_sha256`、
`source, todo_id, message_id, thread_id, author_id, job_id, revision,
applied_at, verification`を保存する。runnerは版・本人・元task・実job所有・時刻を
照合する。照合後も親受領と製品別配送は未確認としてOMBへ残し、全体doneにしない。

### 追加のGiven / When / Then

1. 登録・報告済み投稿に新しいLastがなくても、通常tickの再読で編集を保存し、
   同じ元taskへ新版を配送する。再読と再起動で旧版履歴やnoteを増やさない。
2. 旧watermark以下や、最新100件より深いpageの未取得本人投稿を後から読めたら、
   受信記録へ一度保存し、旧watermarkを保持する。
3. CAS競合なら未配送を保持し、成功後に応答だけ消失した場合はnoteの読戻しで
   配送を回収する。どちらも元のwait/依存/担当を変えない。
4. 他channelの権限確認が403なら、空pageや調査完了を作らず、本文取得と
   watermark更新を行わない。対象と拒否理由を保存する。
5. 未結合の補足はpendingのまま責任と次の手を持ち、登録件数に加算しない。
   古いsource hashやa/bだけの結合要求はtaskへの書込み前に拒否する。
6. 同じ返信の編集前のapplied票では新版を完了しない。実担当と新版が一致した
   反映票を確認できても、親受領・製品別配送まで成功したと推測しない。

合成テストは実入力での実操作・現用設定の有効化・実定期runの代わりにはならない。

### レビュー3465による互換性と公平性の修正

元taskを確定する必要がある補足は`task_binding_pending`に置き、新規登録の
4件枠を消費しない。依頼forumの走査8件と追加channelの走査2件は別cursorを使う。
一覧の`last_message_id`を保持し、通常archiveはcutover以前の閉鎖時刻で打ち切る。
設定をfalseへ戻すと、保存済みの追加channelも通常走査・replayの対象から外れる。

検証済みの旧質問は専用の原質問照合を経て従来の登録へ戻す。bindは同じlock内で
質問receiptを読み、長文回答とcutover前に作られた質問も一般結合へ流さない。
限定製品のOriginは既存の有限steering配送だけを使い、一般bindからは拒否する。
型・添付の項目追加だけでは未登録や完了済みのphaseを変更しない。
登録前の実編集は登録待ちを保ち、一時的なToDo読取失敗は再試行可能なまま残す。

通常の編集再読では受付用のphase判定を繰り返さず、発見失敗のtickでは
再読・replayを行わない。配送待ちと反映票の確認は履歴再読とは別の最大2件枠で
処理する。新しいsource参照は既存workerが読む`steering/<todo>-<message>.json`にも
届ける。未編集の旧steeringには担当が指示どおり作った旧形式の反映票を認め、
実編集後には認めない。反映済み編集の完了報告はその版ごとの重複抑止IDを使う。
原票の版ファイルには元投稿の事実だけを入れ、可変なOriginや進捗先を入れない。

### 受信開始前の投稿の編集をつなぐ

月間原票の全投稿を新規依頼として取り込まず、3327の照合で必要と分かった
原票と投稿を1件ずつ`intake watch --input <private JSON>`へ渡せる。
domainは`discord-intake-history-watch-v1`、必須項目は
`message_id, receipt_file, receipt_sha256`。原票は3327または3387の正規monthly
read-pageだけを認め、元ファイルの保護判定・SHA・期間・本人・guild・botを確認する。
現在のbot、権限/private所属、本文と返信先・作成編集版も実APIへ照合する。
当該月の下限9/23より前、原票変更、版違い、権限不明は取り込まない。
作成時刻が受信開始`State.Since`以上の投稿も拒否し、通常受信の対象を
受動的なwatchで隠さない。保存したwatchの再読でも同じ境界を確かめる。

確認した基準版を既存Requestの`history_watch`へ保存する。この登録でtask、質問、
元作業のnoteや担当を作り直さない。既存の通常受信があるIDの経路は置き換えない。
未編集のwatchはbindを拒否し、OMBの対応待ちとして表示しない。
原scope/cursor・原票は変更せず、その私的参照とhashを同じRequestへ保持する。
本文が変わらなければ通常の登録へ流さず、元の短いa/bも再解釈しない。
後で実編集を検出した時だけ`task_binding_pending`へ置き、元の質問と元作業を
確かめる責任・次の手をOMBへ残す。配送は従来の正確なbindまたは質問専用経路を
使い、限定製品の資格確認を飛ばさない。
既知質問や短い回答の編集は、一般bindではなく正確な質問世代の確認へ置く。
担当向けの一投稿の版ファイルとsteering票には、月間原票のパスを渡さず、
原票hashとscopeを保持する。元の原票パスは受信側のRequestだけに残す。

受入では、cutoverより前の基準版を登録・再起動・再読してもtaskが増えず、
実編集で初めて結合待ちになること、原票のhash不変、改変原票・現在版違い・
不明な権限で保存されないことを確認する。過去全592件の無条件登録は行わない。

レビュー3476の修正では、登録後の質問編集を`source_context_pending`へ渡し、
元の質問世代を確認する次の手を残す。旧`origin_pending`の質問回答は専用の
質問照合へ戻し、旧形式の一般結合待ちは新規受付の枠から外す。
archiveの同値境界は同じ巡回の最終pageや下限到達後も未確認として保持する。
Lastは通常発見のメモリ内だけで使い、monthly v1原票のJSON形式は変えない。
replay拒否はreplay自身の覆域へ記録し、元taskの不存在と一時的な読取失敗を分ける。
取得前から編集済みだった旧steeringも、現在receiptのhashが一致するときは受理し、
反映確認と完了報告を別の段階に残す。

設定falseは追加channelの発見・通常走査・履歴再読を止める。
すでに受け付けて配送待ちになった指示の回収と反映票の確認は継続する。
必要な旧投稿を明示して登録したwatchは、既存の同じ再読枠で継続する。

一般配送から除く理由と、保存する段階・責任・次の手を共通の遷移で扱う。
設定済みの製品報告や限定scopeの編集は
`source_context_pending`へ置き、元scopeを照合した有限steeringを次の手とする。
配送済みだと扱ったり、実行できない配送を毎tick繰り返したりしない。
質問照合や登録に成功した後は、古い結合待ちの次の手を消す。
取得時点から編集済みの旧steeringは、現在のsteering票の元投稿版が一致すれば、
元の指示どおりの旧形式の反映票も受け付ける。実編集の履歴がある票には流用しない。
