# 配送記録の読取上限

既存の `recovery-messages.json` は送信側の
`cc-ask/src/recovery_messages.rs::LEDGER_BYTES` と同じ32MiBまでを、
unattended-runnerでも読む。
受信状態など別の記録に使う `maxState` の8MiB上限は変更しない。

対象はunattended-runnerの6読取のみで、同じ配送記録を使う発見処理、質問の文脈確認、進捗の出所確認、
送信応答を失った場合の照合、製品別の結果配送である。
保存場所、domain・本人・bot・版・対象の照合、状態の保存方法は変えない。
32MiBを超える入力、壊れたJSON、異なるdomainを空の台帳として扱わない。
この変更で台帳を編集、切り詰め、退避、新設することもない。

発見の初期のbot・forum照合と配送記録の読取に失敗した場合は、既存の固定stage/status型を使い、
従来の失敗表示とともに段階とHTTP状態だけを返す。
本文、認証値、保存先のパス、入力中の任意文字列は診断へ出さない。

`health.errors` には従来の `discord_discovery_failed` に加え、
`discovery_<stage>_<code>` と、200以外のHTTP応答の場合の
`discovery_http_status_<数字>` が入る。
botとforumのstageは `bot_identity` / `request_forum`、codeは取得失敗の
`source_unavailable` または照合不一致の `invalid_or_unavailable` である。
配送記録のstageは `receipt_registry_missing` / `receipt_registry_limit` /
`receipt_registry_malformed` / `receipt_registry_domain` / `receipt_registry_unavailable`、
codeは既存の `invalid_or_unavailable` を維持する。
ファイル不存在、上限超過、JSON不正（型の不一致を含む）、domain不一致、その他の読取不能を区別し、
失敗した記録を空と見なして処理を進めない。保存段階の失敗にはこの診断を付けない。

回帰検査は架空JSONの文書サイズを8MiB超・32MiBちょうど・32MiB超へ変え、
読取成功、上限超過の拒否、元バイト不変と登録なしを確認する。
サイズ用の空白はJSONの文書容量だけを検査し、送信側の業務的な承認を作るものではない。
一般状態の8MiB制限と、発見の前提が失敗した後に追加HTTPや状態更新を行わないことも検査する。

実環境の台帳サイズと障害原因は、この架空データの成功から推定しない。
反映後の通常経路での取得と、その出所・版・元作業への対応は別の受入条件として残す。
