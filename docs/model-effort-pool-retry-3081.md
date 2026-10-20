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

## 調査中

CLIのaccount useは実行中threadの次requestにも影響するため今回使わない。
正規per-request/per-thread/failed-account制御の調査は継続中。プールの件数は利用可能性の保証ではない。
実Red/Green、実起動、配布は結果が得られてから記録する。

一次資料: [Sol](https://developers.openai.com/api/docs/models/gpt-6.1-sol)、
[Fast](https://developers.openai.com/api/docs/guides/fast-mode)、
[Sonnetのthinking制約](https://platform.claude.com/docs/en/build-with-claude/thinking-troubleshooting)、
[Opus](https://platform.claude.com/docs/en/models/opus-5-5/overview)。
これは適合判断であり、同一実課題での比較ベンチマーク結果ではない。
