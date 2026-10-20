# 質問へ説明を補う送信入口の独立検査

## 結果

通常の `post` から、検証済み質問への説明だけを送る経路はまだ存在しない。
修復済み・配備済み・配送済みとは判定しない。この検査では製品コード、
送信記録、実行物を変更せず、実投稿も行っていない。

対象ソースは `1cdf23da03e84483cb244c18e9a699ff6ffd99c5`。
検査開始時に取得した `origin/main` と一致する。

## 原因と必要な修復

`cc-ask/src/conversation_bridge.rs` の `try_post` は `send_log` を呼ぶ。
Go側の `conversationLog` は固定入力と送信先を保持し、`recovery-log` を呼ぶ。
`cc-ask/src/recovery_messages.rs` の `Kind::Log` は `known_progress` を使い、
検証済み `log:0` または `progress` だけを許可する。`question:0` は対象外である。

停止位置は送信意図の保存とbot照合の後、`ensure` による `Pending` の保存と
POSTの前である。「要求を予約する前」と「POSTを予約する前」を混同しない。
外側の `conversation unavailable` は処理エラーをまとめた表示であり、
この表示だけでは原因を区別できない。

修復には、質問を通常の進捗送信先へ広げる変更ではなく、説明の補足を明示する
限定した入口が必要である。元ToDo、元質問の検証記録、bot、本人の説明要求、
固定本文と無通知を結び、既存の送信意図を保ったまま再開できる必要がある。
通常の進捗、新しい質問、未知の送信先をこの入口へ流してはいけない。

## 実行した検査

同じソースで、`cargo nextest run --locked -p cc-ask --lib` により次の7件を選択し、
すべて成功した。既存の架空APIを使う検査であり、実Discordへの配送ではない。

- `recovery_messages_explicit_progress_log_splits_and_deduplicates_same_hash`
- `recovery_messages_existing_progress_cannot_notify_and_legacy_thread_cannot_be_targeted`
- `recovery_messages_same_request_id_cannot_change_body_or_todo_scope`
- `recovery_messages_exact_decision_dedup_keeps_original_wording_and_scope`
- `recovery_messages_notify_prefix_owner_and_silent_defaults_are_get_verified`
- `recovery_messages_unknown_outcome_remains_pending_never_reported_zero_or_completed`
- `recovery_messages_replies_require_exact_new_thread_starter_and_todo`

Go側では次の4件を `go test -count=1 -v -run '<対象名>' .` で実行し、すべて成功した。
これらも架空の送信処理を用いている。

- `TestConversationLogsReuseTopicAndOriginalRetryDestination`
- `TestConversationCLIHasFixedErrorsAndReleasesLock`
- `TestConversationCLIDispatchesContractAndOnlyParentJSON`
- `TestConversationCLIRejectsInvalidArgumentsBeforeLoading`

最初のRust実行は誤って `--bin discord_ask` を選び、対象0件で終了した。
テストはライブラリ内にあるため `--lib` に訂正し、上記7件の実行結果を確認した。
製品のビルド失敗ではなく、検査側の対象指定の誤りだった。

## 検査の限界と残る受入条件

既存の検査成功は、質問への説明を送れることを証明しない。修復後は、同じ固定要求で
説明だけが届くこと、別本人・別質問・改変本文・通知・再質問・通常進捗を拒否すること、
再実行で重複しないことを入口から確認する必要がある。本人からの説明要求の有無も
架空APIの成功例と拒否例で確認する。

送信側の配備版と原記録の追加検査には保護判定上の制限が残った。
正常な再開手順が使えるという結論は出していない。実行版の更新、別モデルの確認、
必要な自動検査、元の投稿先への配送とAPIでの読み返しは未完了である。
実案件の識別情報・本文・費用・送信記録はこの文書に含めていない。
