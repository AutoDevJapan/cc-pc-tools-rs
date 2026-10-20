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

## 2026-10-21の追加受入と設計の見直し

検査3096の固定170bf906は58件すべて合格し、exampleのコンパイルも成功した。
比較用8b70dd71のRedは、自動レビューのモデルと別枠catalogのassertで実際に失敗した。
この結果は、以下の追加修正後のGreenを意味しない。

別モデルレビュー3097は871e184..170bf906の30変更ファイルを確認し、Must3件を返した。
原応答のSHA256はDC62D3ECBE2F6C8BBF44E5530181BDAA12ECB4AD5AB1CEBAF85094A519663849。
実native turn_contextはOpus5.5/max（2026-10-20T15:53:06.126Z）。
検査3096もSol/max（2026-10-20T12:01:53.858Z）を実記録で確認した。
要求記録だけからactual effortや上流Fastが確認できたとは扱わない。

| 入力と問題 | 修正後に求める結果 |
| --- | --- |
| 作者情報のない通常レビュー、またはDiscordの共有作業場所からのレビュー | 登録できる。通常のOpus/maxを使い、作者のメタデータは未確認と記録する。共有cwdにいただけの別ジョブを作者と推定しない。 |
| 作者ジョブを明示し、起動した親ジョブが別にいる | 明示した作者だけからモデルを取得する。単なる起動元を作者に足さない。作者が分かる場合の同モデル拒否と、自動選択時の別モデル化は保持する。 |
| 作者不明の既存GPTレビューにprotocol混雑が起きる | 同じGPTの別枠を最大3回先に試せる。作者未確認のまま別モデルへ移さない。ルーティング除外リストを作者の証明と呼ばない。 |
| 元ToDoの担当・世代・停止・依存・完了が変わる | 古い実行の再試行を終了し、activeな順番待ちを残さない。ToDoと途中成果は保持し、通常のスケジューラーが現時点の条件で次の担当を扱う。 |
| cooldown後の排他取得が一時的に失敗する | 同じqueued世代を再照合して、保存した15分待機を最大3回使う。別担当の状態を上書きしない。 |
| 旧AIのHighが管理されたジョブへ残る | 新規起動・再開の直前にMaxへ合わせ、元の値はlegacy_effort_hintに残す。実行中の要求はこの設定だけで終了させない。 |
| 本人が同じ対象に個別のモデル・effortを指定している | 正規に受領した指示とともにmodel_origin / effort_originをowner_requestで渡す。保存値だけを本人指定の証拠にしない。通常AIの選択はpolicyとする。 |
| aliasのopus・大文字表記・provider付きモデル | 同一の作者モデルとして比較する。未知のモデルの名前を別物へ変更しない。Claude系は未知の型もGPT用Fastを継承しない。 |
| reviewのモデル照合が同じ入力で失敗する | 準備は3回で止める。新しい指定・仕様、または欠落していた作者記録の復旧で再確認できる。5分ごとの無限再準備をしない。 |
| native model catalogを使う会話の再開 | config引数をresumeより前に渡す。取得できない場合は子を起動せず失敗を保持する。欠けた能力・文脈長を推測しない。 |
| 有限レビューが終わり、本文に親の未完了もある | 本物のscheduler予約と仕様へ結び付いたread-onlyレビューだけを完了とする。一般実装の完了parserは緩めない。親のToDoは変更しない。 |

有限レビューの結果はsource job、仕様、記録された本文のハッシュ、source jobの終了時刻、受領時刻を分けて保存する。
受領した時刻を、新しい推論・モデル消費・実観測の時刻へ読み替えない。native記録の有無も別に保持する。
3067の既に受領済みの22実読取・Must0の結果は再実行しない。今回、原結果と有限な役割の扱いを検査する。
rootの役割を明示する場合はtask_typeまたはrole=review-onlyを保ち、モデル・effortが指定済みでも実装役へ落とさない。

Must3のヘルプ不一致は、元のPython比較資料を保持し、追加オプションと変更した既定説明だけを厳密に許可する。
パーサーの終了コードや、その他の本文の違いを広く無視する方法にはしない。

追加Redの固定版は52bc2dd900b68bfee86e3f5acf9d7378d0c760dbで、検査3154へ登録した。
有限レビューの比較用5a82f08a57d0774c4e01fb0e8760a57a750a13c2は、同じ旧実装に受入テストだけを加えた版。
これらの補助refをmainへmergeしない。正式CIは修正後の変更範囲と逆依存、doctest、Clippyを対象にする。
別モデルの追加差分レビューでMustを解消してから通常mergeし、既知配備先を照合して反映する。

### 正式CIで見つけた不一致

PR313の7573026に対するBuild speed（run 39467020619）は、2327件中2314合格・13失敗・6選択外だった。
関連するBookmark queueの660043f（run 39465334063）も、回答後再開の旧High期待で失敗した。
成功したことや「変更と無関係」として扱っていない。完全ログは所有一時データに保持した。

- 3件の新規自動レビュー受付は、Test.runが固定`--model m`と`--auto-model`を同時に渡したため未到達。
  製品の作者判定のRedと数えず、自動選択時だけ補助の固定値を外した。
- 3154の他の6件は比較版で期待どおり失敗した。受付1件だけは補助コードを直した比較用5ba9a1c274d7a882387542d15c39fd2b2858f990を3171で検査する。
- 保存済み作者の除外情報が再試行のたびに重複していた。作者とrouting除外を統合する1処理で、aliasを含めて重複を除いた。
- 有限レビューでも、独立した完了・継続中ヘッダーが衝突すれば不明として扱う。親の未完了を説明する通常の文章とは区別する。
- 準備の失敗回数の検査は、公開APIが返すエラーを捕捉して、3回後の停止と新しい根拠での復旧まで確認する。
- 旧い値だけのHigh/自動GPTヒントは最新方針へ合わせる。一方、個別指定を確認した検査にはその出所を明示し、同じモデル/effortに付いた実際のowner指定だけを継承する。

ToDo登録にもmodel-origin / effort-originを追加した。値と元仕様を1回の登録で保存でき、
本人指定を残すために共有JSONを手で書き換える必要はない。localai-web-rsの変更は、
新しいoptional引数をNoneで渡す構築コードの互換修正だけで、UI描画とConsole配布は変更しない。

5f214deの正式CIではBookmark queueのWindows検査が成功した。一方、Build speed
（run 39476108879）とChannel reportsのWindows検査は、schedulerに追加したlet chainが
Rust 2021で使えずコンパイル時に停止した。テスト失敗やGreenとして数えない。
同じ条件を入れ子のifへ直し、schedulerとaskの変更部分を各クレートのRust 2021で構文解析した。
次の固定ソースに対する正式CIで、関連テスト・doctest・Clippyの結果を確認する。

モデルと推論の設定を変更した通常の再開は、旧設定を復元しないよう新しい会話で始める。
cwd、権限、元履歴と途中成果は保持する。混雑時の同モデル・別枠の再試行の会話IDは引き続き保持する。
実CLIのrefresh付きOpenAI metadataは1.165秒で返り、5行すべてcodex/health有り/Healthy/creditsAfterLimit=falseだった。
週quotaが確認できたのは4行で、全5枠のモデル利用可否や実upstream tierを保証する値ではない。

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
終了直前の照合にはadd_dirs、network、MCP、審査設定、固定レビュー、再試行回数と枠履歴も含める。
これらが変わった場合も新scopeを上書きせず、子の作成時刻が不明なら新workerの終端を推測しない。

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
