# 新しいDiscord質問の待機と回答の適用

ToDo2823の新しい会話だけを既存の共有ToDoへ接続する。
失われた旧質問台帳、旧質問の解除、元2467、原保護設定の復旧には使わない。
対象は共有ToDoの `projects` に文字列のパスが登録されたプロジェクトとする。
`owner`、未登録、空・不正な登録パスを拒否する。
親Goが質問と回答の各呼出し前に元のプロジェクトの正確なrootをscrub391で確認し、
`client_resource=false` かつ `classification_unverified=false` を要求する。
この資源照合はGoの境界であり、Rustの対象拡張でguardや旧台帳の制約を緩めない。

```text
todo question-wait <id> --question-file <JSONファイル>
todo question-answer <id> --answer-file <JSONファイル>
```

どちらも既存CLIの経路で `todo.lock` を取得し、最新の全ToDoを読んで検査する。
成功した変更だけを既存の原子的保存へ渡す。拒否では保存しない。
同じ入力の再試行も保存しない。ロック外の古いスナップショットで解除を決めない。

## GoとRustの分担

親Goは元のプロジェクトの資源guardと、Discord API v10で質問、本人、bot、guild、返信、判断範囲を確認し、
私的な質問・返信の原本を永続化してから以下のメタデータを作る。
Rustはネットワークへ接続せず、旧台帳も私的返信原本も開かない。
現在の元担当に対応するjob.jsonが存在するときだけ、その停止記録も確認する。
過去のジョブ全件を走査しない。読めない・破損した現担当記録があれば保持する。
SHAだけではAPI取得の真正性を証明できない。API照合は親Goの責任である。

1. 親Goが質問APIの取得結果からquestion-fileを生成し、生bytesのSHA256を保存する。
2. `question-wait` を呼び、元ToDo内の待機記録と `status=block` を読み返す。
3. 本人回答の原本を保存し、元メモと質問マーカーを保持した標準 `edit --note-file`
   で回答・元質問・取得元を追記する。
4. answer-fileを生成して `question-answer` を呼び、回答済みreceiptと `status=todo`
   を読み返す。CLIの起動成功だけでは適用成功としない。

Rustは作業を起動せず、`start`、`launch`、`done` を実行しない。
既存のworker、jobs、spec、scheduler、進捗、worktree、未完了成果を保ち、
通常のスケジューラーが元の作業を扱う。関連する別ToDoも変更しない。

## 入力契約

ファイルは1 MiB以下の通常ファイルに限る。シンボリックリンク、親のリンク、
Windowsのreparse point、ディレクトリを拒否する。読み取りは上限付きで1回行い、
ハッシュとJSON解析に同じbytesを使う。パスや本文をエラーへ含めない。

JSONは単一の平坦なオブジェクトで、全フィールド必須、全値が文字列。
未知のキー、重複キー（エスケープで綴った同じキーも含む）、null、数値、
入れ子の値、末尾の別JSONを拒否する。BOMを付けない。
以下のほかのフィールドを追加しない。

| 値 | 条件 |
| --- | --- |
| 全ID（`todo_id`も含む） | `[1-9][0-9]*` の文字列。u64の範囲。`todo_id`はCLIの正のi64と一致 |
| `request_id` | 1〜96文字のASCII英数字、`_`、`-` |
| SHA256、fingerprint | 64文字の小文字16進数 |
| 時刻 | RFC3339。UTCオフセットと小数秒を許可。未来時刻、UNIX epoch以前を拒否 |

本番のToDoへ適用するときは、既存の `CC_ASK_CONFIG`、なければ本人ホームの
`.godd/discord-notify.json` の読める設定値も照合する。
`guild_id`、`owner_user_id`、botの `application_id`（または `bot_user_id` / `bot_id`）
を照合する。設定がある場合、この3種類のIDは必須。botの別名も存在するなら全て一致を要求する。
設定が存在するのに破損・読取不能なら拒否する。設定が無ければ親GoのAPI照合と
保存待機内の本人IDを使う。明示した `CC_ASK_CONFIG` は隔離したToDoでも照合し、
その設定ファイルが無い場合は拒否する。テストは本人・bot・guildが揃う専用fixture設定を使う。
時刻の検査は既存Rustの `time_s()` を使用する。

### question-file

| フィールド | 固定値・対応 |
| --- | --- |
| `domain` | `discord-conversation-question-v1` |
| `request_id` | 新しい質問の固定ID。Goは `conversation-v1-` と32文字のhexを使用 |
| `todo_id` | 元ToDoの番号を文字列で指定 |
| `guild_id`, `owner_id`, `bot_id` | APIで照合した送信先・本人・bot。本人とbotは別ID |
| `thread_id`, `question_message_id` | APIの質問スレッドとstarter。両者は同じID |
| `progress_thread_id` | 質問とは別の、通知しない進捗投稿のスレッド |
| `question_fingerprint` | 親Goが照合した質問のfingerprint |
| `created_at` | 送信済み質問のAPI timestamp |

有効な構造のfixtureは `cc-todo/tests/conversation_wait.rs` の `question()` にある。
例ではIDが `100` などの小さな値でも受け付ける。
`todo_id` を `1` というJSON数値に変えると無効。`"1"` を使う。

待機を作れる元の状態は `todo` または `doing` だけ。
本人の停止・保留、未完了/不明/重複した依存先、外部の再開条件、decision wait、
既存scheduler質問、未来の再開期限などがある場合は拒否する。
完了済みの依存先は残したまま許可する。

待機時は元メモに次の独立した1行を追記する。

```text
[discord-conversation-question-v1 request_id=<request_id> sha256=<question-file生bytesのSHA256>]
```

`owner_question_wait` の構造は以下のとおり。

| フィールド | 保存する値 |
| --- | --- |
| `domain` | `todo-owner-question-wait-v1` |
| `question` | 検査済みquestion-fileの全フィールド |
| `question_receipt_sha256` | question-fileの生bytesのSHA256 |
| `previous_status` | `todo` または `doing` |
| `previous_note` | 元のメモ。存在しない場合はnull |
| `previous_note_present` | メモフィールドの有無 |
| `block_note` | 元メモと専用マーカーを結んだ文字列 |
| `blocked_at` | 待機を保存したRFC3339時刻 |

同じ生bytesでの再試行は変更なし。
既存待機を別質問に置換しない。同じrequest IDや質問threadを別のToDo・質問へ
再割当てしない。過去の回答済み記録にも同じ制約を適用する。

### answer-file

| フィールド | 固定値・対応 |
| --- | --- |
| `domain` | `discord-conversation-answer-v1` |
| `request_id`, `todo_id`, `thread_id`, `question_message_id`, `question_fingerprint` | 保存済み待機と完全一致 |
| `question_receipt_sha256` | 待機に保存したquestion-fileの生bytesのSHA256 |
| `message_id` | 本人回答のメッセージID。質問starterより後のID |
| `author_id` | 待機内の `question.owner_id` と完全一致 |
| `source` | `discord-api-v10` |
| `reply_sha256` | 親Goが確認した返信本文のSHA256 |
| `acquired_at` | 正規APIで返信を取得した時刻。質問作成時刻以後 |
| `receipt_file_sha256` | 親Goが永続化した私的Requestファイルの生bytesのSHA256 |

有効な構造のfixtureは同じテストの `answer()` にある。
質問fileを保存した後、実際の生bytesのSHAで `question_receipt_sha256` を作る。
再整形・改行の変更でもこのSHAは変わる。
`receipt_file_sha256` は親Goの私的原本への参照であり、パスは受け取らない。
Rustに私的ファイルの存在検査や本文読取を追加しない。

解除には、検査済み待機との一致、元ToDoの `status=block`、その待機の専用マーカーが
元メモ内に独立した1行として1回だけ存在すること、他の停止・依存・再開条件が
ないことをすべて要求する。blockという状態だけから解除対象を推測しない。
元メモへの標準editを認めるため、メモ全体や更新日時の一致は要求しない。
解除時は専用マーカーの行だけを除き、元メモと追記された本人回答を保持する。

成功時は `owner_question_wait` を取り除き、全待機記録と回答を
`owner_question_answer_receipts` 配列へ1回だけ移す。各要素は次の構造。

```text
domain = todo-owner-question-answer-receipt-v1
wait = 元owner_question_waitの全体
answer = 検査済みanswer-fileの全フィールド
answer_file_sha256 = answer-fileの生bytesのSHA256
applied_at = 適用時刻（RFC3339）
```

再試行はanswer-fileも同じ生bytesで行う。同じrequestの異なる回答は拒否する。
同じ回答の再試行は、後から本人が止めた場合・完了した場合・次の質問へ移った場合でも
何も解除しない。回答済みの質問fileの再試行も待機を再作成しない。

## 機械処理で担当が未設定になった旧待機の取り下げ

`question-withdraw` は通常の再開が `worker_receipt.job=null` を残した旧待機も扱う。
対象は `origin` を持たない待機だけ。全行の保存済みスナップショットとの一致に加え、
現在のschedulerと最後の履歴が同じ成功済みjobを指し、launch keyが一致し、
worker・現在行・そのjobのblock結果が同じgenerationであることを要求する。
現在のphaseはfinished、メモは結果取込が書く固定文と完全一致する必要がある。
停止・依存・別質問の既存検査は維持する。

証跡には実際のnullの担当を保存する。完了jobへの結び直しや本人回答の作成は行わない。
証跡の再読込も、旧待機と結果取込による解除の場合だけnullを認める。
新方式のorigin変更、別の担当、世代差、欠けた結果、別のメモは拒否する。
担当の作成時刻は、実際の `cc-jobs` が保存するRFC3339文字列を時差も含めて読み、
元質問の作成時刻以前であることを確認する。旧記録の数値epochも維持する。
不正な時刻や質問より後の担当を、ファイルの更新時刻から推測して認めない。

本人の返信が先に届いた場合は取り下げず、通常の `question-answer` へ戻す。
結果取込が専用マーカーを置き換えていた場合には、親Goが
`--delivery-note-file <返信参照を追記したメモ> --expected-state-file <元の全行>`
を組で渡す。Rustは取り下げと同じ完了担当・結果取込・停止条件を検証し、
固定の機械メモを保存済みの元メモへ戻したうえで返信参照を付け、回答証跡の保存と
待機解除を同じロックで行う。マーカーをeditで再作成しない。
通常のマーカーが残る場合の回答処理と、同じ回答の再試行は従来どおり。

## 出力と検査

標準出力はJSONメタデータ1件だけ。
`id`（既存CLIと同じ数値）、`request_id`、`question_receipt_sha256`、
`changed`、`answered`、`resumed` を返す。
初回待機は `changed=true, answered=false, resumed=false`、
初回回答はすべてtrue。回答再試行は `changed=false, answered=true, resumed=false`。
元メモ、質問/返信本文、私的ファイルのパスを標準出力へ出さない。

共有Cargoゲート経由で以下を実行する。全workspaceは不要。

```text
cargo nextest run --locked -p cc-todo
cargo test --locked -p cc-todo --doc
```

結合テストは実CLIと隔離したcanonical storeを使い、成功、追記後の解除、生bytesの再試行、
後からの停止・依存待ち、別世代の回答、本人/hash/出所/時刻の不一致、未知・重複schema、
壊れた保存待機とreceipt、通常ファイルの制約、共有ロック、途中成果の保存を確認する。
