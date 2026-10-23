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

`intake status [--after <message-id>]`は最大50件のmetadataだけを返す。
元投稿ID、本人ID、版hash、元ToDo、段階、`pending_owner: OMB`と次の手、
配送版を含み、本人本文は出さない。設定と登録だけを実運用成功としない。

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
