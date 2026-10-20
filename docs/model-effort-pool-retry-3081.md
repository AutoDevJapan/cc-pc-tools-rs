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
- pool再試行の実装前受入commitはbb7cb6b。実Red/Greenはこれから確認する。
- 本実装担当のnative turn_contextはSol/maxを確認。これは新しい実行ファイルの確認ではない。
- 本担当の要求ログはrequested/served Sol、requested effort max、要求tier priority。
  response tierはdefaultでauthoritative=false、Fastのconfirmationはassumed。
  実upstream Fastとactual effortは未確認。
- 正式CI、別モデルレビュー、マージ、配布は未実施。

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
