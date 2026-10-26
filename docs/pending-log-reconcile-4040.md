# 予約済みの通常報告1件を回復する

ToDo4040。対象は run `automation:2026-10-26T18:34:26.422Z` の
`conversation-log-1ff3c6481a24325fcd88363de3a7593c`、`log:0` のみ。
通常senderのPendingは結果不明を意味し、GETで不在でも再POSTしない。
この既存動作を保持し、明示的な資格入力を用いる専用コマンドを追加する。

## 受入条件

- 元Intent/Pending、本文、同じrequest/tag/slotを保持する。質問、画像、他runを扱わない。
- 正規conversation登録・凍結本文・sender journalを照合し、既存runner.lockと
  recovery-messages.lockを同時保持する。生存sender/runnerのlockは奪わない。
- check-onlyはGETのみで、journal/registryを書かない。適用は資格入力・journal・
  登録のSHA256とRelayのOSプロセスidentityに結ぶCASを要求する。
- 全履歴をcursor付きGETで終端まで確認し、対象thread starterを含む完全性を確認する。
  exact投稿が1件ならGET検証して採用し、複数・曖昧なtag候補・不完全GETでは停止する。
- 不在時の明示回復は、資格と単一attemptを正規journalへ順に永続保存した後だけ行う。
  再実行・同時実行・応答喪失後もこのslotの追加attemptを許さない。
- 保存失敗/CAS競合/unknown lease/active sendは成功扱いにしない。
  POST後のGET/保存失敗も再POST資格を復活させない。
- 投稿前後の本番操作は既存Relayだけが実行する。ソフト担当はPOSTしない。
- 変更部テスト、Clippy、doctest、Windows受入、別実モデルレビュー、必要CIを確認し、
  通常マージと最新実行版を保持する配布を行う。

## 調査時点の制約

2026-10-27 06:15 JST。専用worktree/rootは現guard clear。
指定再現フォルダ、正規sender journal、正規conversation登録は現guardが
classification_unverified/access_allowed=falseを返したため、本担当は未読。
別pathやコピーでは回避せず、ソースに基づく独立修正を進める。
本番precheck・CAS・Relay実GET/受領は未実施であり、モック成功を本番回復へ読み替えない。

一次資料: [Discord Message API](https://docs.discord.com/developers/resources/message)。
履歴APIはREAD_MESSAGE_HISTORYとcursorの確認が必要。nonceの短期間の重複抑止だけを
長時間経過したPendingの不在証明に使わない。

## CLIと資格入力

`discord_ask recovery-reconcile-pending --qualification-file <JSON> --check-only`
は、同じRelayプロセスを親とするWindows CLIから実行する。
戻り値の`command_id`を、同じ入力の
`--expected-precheck <command_id>`へ渡すと限定適用する。
`--check-only`と`--expected-precheck`は併用できない。
引数は追加の本文・thread・notify・media・新request IDを受け付けない。

資格JSONの必須フィールドは次のとおり。未知のフィールドは拒否する。

| フィールド | 照合内容 |
|---|---|
| domain / purpose | `discord-pending-log-reconcile-v1` / `reconcile_exact_pending_text_log` |
| run / outer_request_id / request_id / fingerprint / phase | 元依頼の固定値。phaseは`log:0` |
| body_sha256 / body_utf8_bytes | 凍結本文のSHA256 / 2292 |
| guild_id / thread_id / bot_id | 元依頼の固定送り先とauthor |
| notify / media_count | false / 0 |
| journal_sha256 / registry_sha256 | 正規2ファイルの現在の生バイトSHA256 |
| transport_input_sha256 | `logs[outer_request_id].input_sha256`。本文SHAと別のhash |
| relay_session | `01a16dad-9ec4-76a1-9586-1f819c9330b1` |
| lease.kind | `existing-runner-and-sender-locks-v1` |
| lease.holder_pid / holder_created_filetime | CLIの直接の親であるRelayプロセスのPID / Windows作成FILETIME |
| lease.expires_at | RFC3339。実行時から300秒以内の将来時刻 |

OSの既存2ロックを資格の実体として使う。時刻だけでロックを奪わず、登録に未知のlease項目が
現れた場合も拒否する。親PIDが再利用された場合は作成時刻の照合で拒否する。
Relayのsession IDは固定した申告値と照合するもので、OSプロセスがその会話を所有することを
暗号学的に証明しない。元Relayが資格を作成・実行する運用を維持する。
expires_atは、全GET後・永続保存前の受付期限。受付後は生存する本CLIが既存2ロックを保持し、
同じ送信資格のもとでqualificationとattemptを保存する。attempt後に追加のlease判定・GET・
本文生成を行わず、準備済みの同じHTTP要求を送る。lockedまたは状態不明のthreadでは
attemptを保存しない。既存exact投稿のGET採用はlockedのままでも可能。
資格を作る前に正規pathの保護判定と元Relayの所有を確認する。
`journal_sha256`や`registry_sha256`が変われば資格を新しく照合する。
GET範囲が変わった場合も新precheckを行い、古いtokenを強制適用しない。

check-onlyは候補数・hash・lease確認・attempt有無を返すが投稿・保存・完了扱いは行わない。
適用で`delivery=verified`を得た場合だけmessage ID、timestamp、quiet flags、owner_mentionsを
読戻す。保存済みのattemptがあり投稿が見つからない場合はPendingを保ち、追加送信はしない。
元Go registryは書き換えない。senderで一件が検証できた後、元Relayが同じ外側要求を通常経路で
照合し、外側receiptも回収する。この通常再開は新POSTを許可するものではない。

この追記event形式を読めない旧senderへ実行版を戻さない。元Pending/Intent/eventを旧版に
合わせて削除・改変することもない。
適用は、レビューとCIを通した本機能を既存Windowsの正規CLIへ配布した後だけ行う。
作業用のdebug/release候補から本番journalへ追記しない。現用source・配布receipt・並行writerの
照合が終わるまで、候補の作成やcheck-onlyを配布・適用済みとは扱わない。

出力の`qualification_input_sha256`は資格JSONの生バイトSHA256、attemptの
`qualification_receipt_sha256`は保存したqualification receiptのSHA256。
保存済みreceiptの検証は当時のPendingのdigestに結び、現在の本文分割や表示形式で
過去のeffectを再生成しない。履歴のCASはID・本文・author・quiet/media等の検証対象に結び、
リアクションだけの変化は不一致の理由にしない。
