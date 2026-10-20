# ToDo3081: モデルの推論強度と混雑時の再試行

本人の2026-10-20指示（正本: D:/CC/Policies/owner-model-effort-and-pool-retry-20261020.md）を実装する。
通常のGPT担当は gpt-6.1-sol / max / fast を明示する。本人の個別指定、単一ファイルのHaiku指定、
別モデルレビュー、読み取り権限、停止、依存待ちを維持する。未知モデルを別名へ置換しない。
Console UIとSkills導入方針の回答待ちは変更しない。

## 受入条件

1. 自動選択・直接CLI・スケジューラーの起動引数でSol/Max/Fastを確認する。
   明示したモデルとeffortは保持する。Claudeの自動代替候補にはMaxを指定する。
2. 正規protocolの混雑エラーだけを契機に、同じGPTモデルの異なる適格な既存OAuth枠で最大3回追加再試行する。
   元childの排出、PID作成時刻、job/queue/ToDoの排他と世代を維持する。
3. 試行回数、候補枠の識別、待機期限を保存し、再起動でリセットしない。
   全体のactive accountを切り替えず、他threadへ影響しない正規制御を使う。
4. GPTの有限再試行後、仕事に適するSonnet5.5かOpus5.5を既存OAuthの健全性から選ぶ。
   複雑な跨ぎ修正・原因分析・重要レビューにはOpus、狭い明確な修正にはSonnetを候補とする。
   Sonnet Maxにはadaptive thinkingを使い、between_toolsとの不適合を防ぐ。
5. 引用・tool本文・ページのcapacityを起動制御へ使わない。秘密・個人ID・メールを監査へ保存しない。
   関連検査、別モデルレビュー、CI、通常merge、既知配備先への有限CASを確認する。
6. 保存設定、起動要求、実turn_context、上流実効値を区別する。実upstream tierを取得できなければ未確認とする。

## 検査と実装の対応

- 最初のRed: cc-job-policy/tests/model_effort.rs と cc-jobs/tests/model_effort.rs。
- 既存回帰: cc-jobs/tests/parity.rs の capacity_regressions、failure-event、実process fixture。
- 利用側: cc-scheduler、cc-ask、cc-todoなど共有profileの推移的な逆依存をCI選択器で確認する。
- Cargo割当なしの担当は重いビルドを起動しない。独立した検査を--needs cargoで登録し、固定commitを検査する。

## 確認した正規の要求単位の制御

CLIのaccount useは実行中threadの次requestにも影響するため今回使わない。
OpenCodex 2.83.0の既存account namespaceは、公開識別子とnative modelを組み合わせた
要求だけにaccountを固定する。現在のnamespace mapは存在し、pickerの表示はfalse。
routerは表示設定と独立してこの経路を受け付ける。CLIのapi explainで別枠の公開識別子から
同じgpt-6.1-solへのnative Responses経路を確認した。active accountとpickerは変更しない。
認証・停止・クォータ・有料クレジット拒否は既存の認証層でも再検証される。

初回失敗の枠は、正規CLIのconversation/model限定の要求メタデータから確認する。
失敗したprotocolイベントそのものは既存の検出器を使う。要求ログは枠の帰属確認専用で、
本文やerror文字列から起動理由を新たに作らない。枠の帰属が不明なら15分の有限待機へ戻す。
追加試行は5、15、30秒後に行い、同モデルの回数・公開識別子・期限をjobへ保存する。
private IDとメールはCLI応答のメモリ内での照合にだけ使い、jobの監査へ保存しない。

### Redと実効値の確認

- Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/277
- 検査3083、固定HEAD c4eea9ad161254417cdc90e901dee2f6a940ce67。
  cc-job-policyのmodel_effortは0合格/1失敗（effort実high、期待max）。
  cc-jobsのmodel_effortは1合格/1失敗（Fastの明示引数が欠落）。実終了コードはいずれも100。
  コンパイルエラーをRedへ読み替えていない。後続assertの未到達も実行済みと数えない。
- pool再試行の実装前受入commitはbb7cb6b。検査3087で実Sonnet/期待Solによる実Redを確認した。
- 871e184の関連36件は35合格/1失敗。親と除外情報がないレビューの候補を返す失敗を修正した。
  この候補の全体Green、4crate全体検査、doctestは未確認。
- 本実装担当のnative turn_contextはSol/maxを確認。これは新しい実行ファイルの確認ではない。
- 本担当の要求ログはrequested/served Sol、requested effort max、要求tier priority。
  response tierはdefaultでauthoritative=false、Fastのconfirmationはassumed。
  実upstream Fastとactual effortは未確認。
- 正式CI、マージ、配布は未実施。別モデルレビュー3088を受領し、下の対応を進めた。

### 既存OAuthによる限定した実要求

- 要求ごとの別selectorを指定したSol/max/priorityの要求がHTTP 200で完了した。
  ログのaccountLogLabelは指定した公開selectorと一致し、served modelはgpt-6.1-solだった。
  全体のactive accountやpickerを変更する操作は発行していない。
- Sonnet5.5とOpus5.5も、それぞれmax・standardの短い要求がHTTP 200で完了した。
  Sonnetのwire modelはclaude-sonnet-5-5で、Fastはnot-requested。
  同版の公開adapterはmaxをadaptive thinkingとoutput_config.effortへ写像する。
  between_toolsをmaxと組み合わせる400は今回の要求で発生していない。
- これは既存OAuth経路の限定接続確認で、自然な混雑・Codexの会話再開・同一実課題の品質比較の実測ではない。
  providerメタデータにactual effortは無い。SolのFastはconfirmation=assumedのまま未確認。
- 追加のspec/検査対応: 関連検査3087、別モデル読取3088。両方の固定候補は871e184。
  その後の待機中の世代照合と観察時刻、文書・exampleの修正は追加差分として検査・レビューする。
- account selectorが非表示でも元モデルの能力と文脈長を保つため、job専用catalogへnative metadataを渡す。
  global catalogとpickerは変えない。追加検査3091ではRed/Greenともfixtureの構文エラーで
  コンパイルが止まった。テスト開始0件であり、metadataのRed/Greenは未確認。
  原因はjson!の配列値直後の.mapで、Rust式として括弧で囲む修正を行った。
  exampleのコンパイルだけは成功した。4crate全体とdoctestは未実行だった。

### 別モデルレビュー3088への対応

比較はc8d71fb..871e184、実turn_contextはanthropic/claude-opus-5-5 / max。
旧候補の読取完了は、現在の候補のMust解消やCI成功を意味しない。

| 指摘 | 対応と確認範囲 |
| --- | --- |
| Must: 自動レビューがSolの作者と同じモデルになる | 通常レビューをOpus/maxにし、元ToDoのjobs、subworkのorigin、固定レビューの元jobから作者履歴を取得する。終了した作者も除外する。scheduler、ask、直接CLI、cc-liveのreview呼出しへ引き継ぐ。本人の明示model/effortは保持し、同モデルなら起動を拒否する。実検査は未受領。 |
| Must: Claudeのservice_tier=defaultがnative CLIで拒否される可能性 | 実Codex CLI 0.163.0のmcp list --jsonで設定を解析させ、default単独とOpus指定/fast_mode=false併用が双方exit0・stderrなしだった。出力本文は表示・保存していない。モデル推論を起動した検査ではない。バイナリ文字列からの推測だけでは拒否を再現できなかったため設定は維持する。 |
| Must: 作者不明のレビューが同モデルの別枠再試行まで止まる | 作者不明の拒否をモデル変更段階へ移し、同じGPTの別枠は最大3回試せるようにした。既知の作者と同じモデルである場合の拒否は維持する。実検査は未受領。 |

追加指摘も対応した。Claude同士の候補を残し、通常429の再試行を混雑用queue gateで止めない。
古い子の終了後にscopeが変わった場合は新scopeを保持して終端を保存し、新workerは終わらせない。
ToDo排他の取得失敗は有限待機へ保存する。公開namespaceとログlabelをCLIのmasked metadataで照合し、
同じ枠の別名や名称変更を重複試行にしない。監査には公開labelとselectorだけを残す。
未知GPT/Claudeはtierやmodelの自動変更の対象にせず、モデル修復の説明も実操作に一致させた。

固定レビューは起動時の作者履歴を保存する。その後に親が別の仕事でモデルを変更しても、
固定済みの旧ソースの作者として追加しない。新しいソースのレビューは新しい履歴で資格を確認する。
元jobの取得失敗、停止、未完依存、世代不一致は自動で権限を広げる根拠にしない。

直接CLIの--todo-idは、元ToDoへ担当として正規に結び付けられている場合だけ再開できる。
番号だけを渡した未所属jobへ再試行の権限を与えない。通常schedulerの所有確認後に再評価する。
OAuth acceptance exampleはprotocol混雑と枠帰属を注入する限定harnessで、自然な混雑の実測ではない。
今回は子Codexを直接起動しない指示に従いexampleを実行していない。

要求本文、応答本文、秘密情報を受入記録へ残さず、モデル・公開selector・HTTP終端・effort/tier要求だけを保持した。
公開adapterの根拠: [Anthropic adapter](https://github.com/lidge-jun/opencodex/blob/v2.83.0/src/adapters/anthropic.ts)。

実装の中間記録をコミットし、Cargoを割り当てた独立検査へ固定HEADを渡す。
通常の起動直前にproviderのペース配分だけでClaudeへ置換する旧経路も除く。
Console UI、モデルカタログの表示設定、Skillsの判断待ちは変更しない。

公開された同版の根拠:
[account namespace](https://github.com/lidge-jun/opencodex/blob/v2.83.0/src/codex/account-namespaces.ts)、
[要求のrouter](https://github.com/lidge-jun/opencodex/blob/v2.83.0/src/router.ts)、
[既存のaccount retry](https://github.com/lidge-jun/opencodex/blob/v2.83.0/src/server/responses/core-codex-account.ts)。

一次資料: [Sol](https://developers.openai.com/api/docs/models/gpt-6.1-sol)、
[Fast](https://developers.openai.com/api/docs/guides/fast-mode)、
[Sonnetのthinking制約](https://platform.claude.com/docs/en/build-with-claude/thinking-troubleshooting)、
[Opus](https://platform.claude.com/docs/en/models/opus-5-5/overview)。
これは適合判断であり、同一実課題での比較ベンチマーク結果ではない。
