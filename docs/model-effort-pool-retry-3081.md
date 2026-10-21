# ToDo3081: モデルの推論強度と混雑時の再試行

本人の2026-10-20指示（正本: D:/CC/Policies/owner-model-effort-and-pool-retry-20261020.md）を実装する。
通常のGPT担当は gpt-6.1-sol / max / fast を明示する。本人の個別指定、単一ファイルのHaiku指定、
別モデルレビュー、読み取り権限、停止、依存待ちを維持する。未知モデルを別名へ置換しない。
Console UIの見た目とSkillsの導入方針は本作業では変更しない。

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

修正後4d0822cのBuild speed（run 39477909091）は、選択した2331件がすべて合格し、
6件は選択外だった。対象ライブラリのdoctestもすべて成功した（各0件）。
Clippyはcc-jobs/src/capacity.rsの入れ子if 1件を指摘し、実行全体は失敗である。
Rust 2024のcc-jobsでは指摘どおり条件をまとめ、Rust 2021のschedulerとは構文を分けた。
全ログのSHA256は712f095ca4d0973a92b4ed517ce8348c2532681e835ce07c5665cb6a6bfa1bee。
この形式修正後は正式CIの結果と、別モデルによる追加差分の確認を受ける。

3171は比較用5ba9a1cの指定1件を1回だけ実行し、作者モデル情報がないため登録拒否となる
実Redを確認した（exit100）。モデル等の後続assertには未到達である。
完全証跡は比較検査の90c492f8de94caf18f81ca4376060bda048f93e2へ保存済み。
この補助refをmainへ統合せず、修正後のGreenは上の正式CIの対象検査で確認する。

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
未知モデルの名前を自動変更しない。Claude系は未知の型でもGPT向けFastを継承せず、default tierを使う。
モデル修復の説明も実操作に一致させた。
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

## 2026-10-21追加レビュー後の会話移行

3162は固定660043fまでの読取を158操作で完了し、原応答SHA256は
8F55D29CB0D78B812BFCE39D31B0E0F7F6A79D24F41419852D9952C8D4E20F8F。
3179は660043f..08b038cの17ファイルを138操作で読了し、原応答SHA256は
5B5AB7D3B98DB883C69958372A9414E3411306643D90DC2893070BC160A3EEF7。
双方の実turn_contextがOpus5.5/maxであることを対応threadの型付き記録で確認した。
3162のMust（完了ヘッダーの衝突）は解消済みだったが、3179のMust2件は未解消として受領した。

08bの正式Build speed run39479243284も2331/2331 pass・6skip、8ライブラリのdoctest成功。
Clippyはcc-todo/lib.rs:460で停止した。全ログのSHA256は
89EB5858E1AB9E7EA19343243BCD23DDC2F7E303A8F2DA608CE131C94D220B4F。
入れ子のifをRust2024に適合する条件へまとめた。これを最終CI成功と数えていない。

会話移行を共通のlaunch_contextへ整理した。通常のCLI登録・再admission、Discordの共通起動入口、
回答後の選択で元の実行記録と比較する。model/effortが変わる旧会話は外し、元の仕様・cwd・権限・
履歴・途中成果と有限予算を保つ。退役したIDを保存し、requeueが旧thread IDを再設定しても
起動引数へ戻さない。既に旧版で正規化されたjobもlegacy hintから一度だけ移行し、新しい互換な
会話では継続できる。追加回答で移行済みcacheが再選択されても、元workerとの比較を失わない。

| 追加指摘 | 対応 |
| --- | --- |
| 再開経路の抜け | 共通launch境界とCLI/admissionで同じ整合処理を使う。個別owner指定と同モデル混雑再試行の会話・予算は保つ。 |
| 親を作者の証明にする誤り | 親は現在モデルの経路除外だけ。明示した作者・固定sourceの履歴と分ける。AI親のplain flagsはpolicy、個別ownerは元指示とoriginを必要とする。 |
| Haikuの未確認Max | 保存値へ新たなMaxを強制しない。Sonnet/Opusと区別し、モデルの名前は保持する。 |
| 厳格なsource資格との衝突 | original_product_retryと固定レビューの既存許可tupleを一般方針だけで変更しない。通常の再資格確認が必要。 |
| 起動中に仕様を変更した根のレビュー | 完了を新仕様へ流用せず、同じ元job/key/世代・停止/待機をCASで確認して旧結果を記録し、通常の再開へ戻す。新仕様には新会話を使う。子作業の既存の保守的な境界は保つ。 |
| モデル置換後のorigin | 自動の別モデル選択はpolicy。引き継いだowner指定は明示指定として扱い、提供確認より先にAIの旧GPTヒントを移行する。 |
| 一時ロック待機の終了 | 共通の最大3回という技術待機上限は延長しない。使い切った終了理由を固定文で残し、active pendingを解除する。 |
| 準備エラーの過去分合算 | 正常な起動受付後、過去の失敗は履歴へ移し、現在の連続失敗回数から外す。 |
| 古い起動文書 | READMEとjob-launch-modelsを最新方針へ合わせた。Consoleの表示や取得対象は変えない。 |

除外リストはaliasも含めて同一モデルとして扱う。作者履歴に残る複数の観察表記は、
除外判定とは区別して保持する。本人個別指定を付けたConsoleの合成検査は出所の保存を検査し、
実UIがowner_requestを発行すると主張しない。UI変更はこの修復の対象外である。

追加Redは製品08bのまま既存公開APIのテストだけを加えた6d7358aで、Cargo付き3191に固定した。
旧会話の残存、HaikuへのMax強制、Discord共通起動入口と個別指定/同モデル予算の対照を限定検査する。
親自身はCargoを起動していない。実結果を受けるまではRed/Greenの件数を推測しない。

CodeQL集約は、未完の3言語構成に加えてcapacity_oauth_acceptance.rs:179のログ警告を返した。
検査用exampleの戻り値を純粋なboolにし、catalog由来の構造体やselector、詳細なErrorを出力せず、
固定の成功・失敗文だけにした。関数名の変更や警告抑制ではない。正式解析で解消を確認する。
example自体は本担当で実行せず、実モデルの追加確認は配布後の通常schedulerで行う。

fab9505のBookmark queue Windowsでは、resume 84件中83合格・1失敗だった。
追加回答の検査は投稿失敗を注入する位置が早すぎ、選択を保存する前に停止していたため、
これは再選択の製品Redではない。既存のcrash_before_launchを使って起動直前に中断し、
保存済み選択を確認してから次の回答を渡す形へ修正した。完全ログSHA256は
FAEF3925D497B1F2CE8825DFE922AFD1CDFBA390021D8C99C9E74DE763A64751。
旧CLIのLuna/Astraレビューも、AIの選択であれば共通の別モデル選択へ移す。

移行前の旧会話と、新規起動で正しく設定された最初の会話を区別するため、実際に移行した
起動にはpolicy版を保存する。これにより、新規起動に残ったlegacy hintだけを理由に
最初の互換な会話を捨てない。退役ID・policy版も再試行の世代照合へ含める。
レビューの自動モデル選択で設定を変えた場合も、登録時の元設定との比較を行う。
配布資格3177の5ファイルを通常mergeし、統合04a1e3cの関連Go検査76件とvetは成功した。
これらは新候補未結合の実装確認であり、更新資格・Rustの合格・配布完了ではない。

fab9505の全関連検査は2357件中2340合格・17失敗・6skipだった（完全ログSHA256
B9ABD52D3FBFF7645E4B1E10F78F9B8E22F2904E0C1A6044561191F278C0455F）。
移行でnative resume IDを外しても、元の論理的な会話の実行予約は外してはいけない。
共通gateで元IDと有効IDの双方、およびsidecarに保存した元IDを照合し、二重作業を防ぐ。
また、モデル利用不能の復旧で選択器が除外したモデルへ一般方針で戻さないよう、
既存のrouting除外を通常起動にも渡して尊重する。tool本文やページの語句は根拠にしない。

検査の前提も確認した。根レビューのfixtureには正規todo_idを付け、CLI親のfixtureは
実行中として出生時刻を与える。既知作者の拒否とfallbackには作者IDを明示し、親だけでは
作者としないことを別検査にする。会話の引継ぎ・回答配送だけを検査する合成workerは
現在のSol/Max設定に合わせ、旧High移行は専用のpolicy/owner対照で確認する。
これらの修正後のGreenはまだ受領していない。

## 2026-10-21 追加レビュー3200の会話引き継ぎ修正

固定04e02baの追加差分30ファイルをOpus5.5/maxが137操作で読み、Must1件を受領した。原応答SHA256は8A2A77015C2E29DA10387CD5FBB6714AF677B6764E584A9E8F87E17A95D5FF08。対応native記録は2026-10-20T21:22:40.777ZのOpus5.5/maxで、上流の実effort/tierは未確認。レビュー完了を製品合格とはしない。

旧jobがqueued中にMaxへ移行した後、最初の新しいnative会話が始まる前に取消されると、schedulerがlast_job.thread_idから退役IDを戻せた。共通launch_contextでresume_sessionとthread_idの両方を退役印と照合し、schedulerの成功継続・失敗復旧とConsoleの明示resumeを同じ判定へ接続する。元jobのthread_idは履歴として保持し、新しい互換threadは再利用できる。Consoleの見た目は変更しない。

通常のDiscord再開が過去capacity履歴を複製して例外扱いされる指摘も修正する。新しい論理起動の選択用コピーからcapacity実行状態を外す。元jobの予算・履歴と、cc-jobsが同じjobで行う実capacity再試行は変えない。仕様差替えのレビュー再準備は失敗予算を増やさず、以前の失敗回数を理由に1時間待たせない。レビューへ自動選択し直したeffortの出所はpolicyへ合わせる。

受入: cancelled prestartの実scheduler準備で旧IDが渡らないこと、成功継続でも同じIDを除外すること、新しい互換threadは保持すること、通常のDiscord再開で過去混雑履歴を元に旧会話を復元しないこと、元の未コミット成果・仕様・権限・capacity予算を保持すること。比較source565ec9bf6d0145721884978eb73a05b03cf3b623は製品04eのまま既存APIの検査2ファイルのみ追加し、資源付き検査3209へ登録した。まだRedを受領していない。

04eの正式CIは関連2362件全合格/6skip、対象8ライブラリdoc成功。残ったClippy失敗はcc-jobs/tests/parity.rsの同一if分岐1件で、冗長な分岐を除く。Bookmark/ChannelのWindows・Linux、GoのUnattended readiness、CodeQLの4言語と集約は成功。完全失敗ログSHA256は759D081255A4DBFFDEEAB639C24408489F158BB9C6DAB2F5EB8AECE2C4EBE056。新しい修正のGreen・別モデル確認・merge・配布は未実施。
3200の追加指摘への対応として、モデル利用不能の復旧情報を作者の恒久除外から分けた。repaired_jobが今回の遷移にだけmodel_repair_pending/model_recovery_fromを付け、共通入口はCLIのmodel-recovery-fromへ渡す。新jobはpendingを保存しないので、その後の通常再開はこの除外を継承しない。次の失敗では復旧元モデルを置き換え、失敗したモデルがすべて永久除外される行き止まりを避ける。本人の指定・既存作者の除外・未知modelは変更しない。既存の失敗分類と復旧待機は維持し、この指摘をcapacity検出条件の拡大には使わない。

AIの親が渡すowner_requestは、親または対応ToDoの同じ値・出所と照合し、自己申告だけならpolicyへ戻す。独立したCLI/ToDo登録が本人IDを認証する機能とは主張しない。登録側は正規に取得した元指示・仕様を保持する。親が作者であるとは扱わない。回帰検査は連続2モデル失敗、現在の復旧hintだけの継承、通常再開での解除、CLI登録、偽のowner_requestと既存の正しい個別指定の対照を追加した。これらは構文/差分の確認のみで、実Greenは正式CI待ち。
## 2026-10-21 08時台の結果受領とレビュー復旧の修正

比較3209はsource565ec9bf6d0145721884978eb73a05b03cf3b623を各command1回、実Red2件・陽性対照4件・fixture失敗1件・compile失敗0件。原応答SHA256=9A79B12849A5EB11011BA5706444D7876DC3B4C978DA756C79E79EE088E181F8、証拠commit56f7100f67e4a9af26e922dd558c59b2d9a4c66b。46操作、対応nativeは2026-10-20T22:31:18.793ZのSol/max。取消直後のケースはテスト時刻がRFC3339でないためblockとなり、本来のassertへ未到達だった。製品Redへ数えず、今回その時刻を正した。

レビュー3211は固定04e..027の17ファイルを118操作で読了し、Must1・Suggestion3・Nit3を受領した。原応答SHA256=1C81C7B2CD20C6B7AD20ECE226F3D87E6E3B8649F5B0BDEC2CC971962CD33A21、対応nativeは2026-10-20T22:53:32.099ZのOpus5.5/max。製品や配布の合格とはしない。

MustはOpus作者のためSolで動くレビューがmodel unavailableになった場合、Astraへ復旧してもCLIのreview_profileが失敗したSolを再選択する経路。model_recovery_fromを今回の選択のrouting conflictとprofile除外にだけ加え、review_excluded_models/review_author_modelsへは混入させない。既存作者の除外と本人指定の照合を維持する。この回帰検査だけを製品027のまま追加した比較4b2cf1da5321600fe5443c272191de9e7372edeaを補助refへpushし、資源付き検査3227へ正規登録した。登録時点の実Redは未受領。

本人指定の継承は、稼働を確認した同じToDoの親、検証済みsubworkの親、番号の無い同じ作業場所の親に限定する。別ToDoの本人指定を流用しない。対応ToDoに保存された指定と、同じlaunch key/phase preparedのscheduler選択は保持する。終了済みの環境親を理由に正規のroot CLI指定を格下げしない。値の不一致・別ToDo・ToDo本人指定・有効/終了済みCC_JOB_ID・prepared/旧世代を実CLIで確認する7対照を追加した。

Consoleの実表示は変えず、既存の停止復旧にある同じpending request/source/modelとの一致からだけ一時復旧hintを引き継ぐ。元レビューの作者source IDs/除外と、確認できた本人指定の出所も伝え、旧AI hintはpolicyとして共通CLIへ渡す。一般のdelegate入力から復旧hintや作者情報を捏造しない。旧会話の明示resume・退役ID・互換な新threadを実Consoleの起動引数で確認する検査と、現pendingのレビュー復旧情報の対照を追加した。型・実行結果はまだ確認していない。

退役IDの比較は大文字小文字の扱いを共通化した。直接CLIに出所のないUUIDを渡す入口は、互換性の確認を呼出し側の契約として明記する。全過去jobの退役印を走査する新機構は追加しない。混雑後にClaudeへ移った工程はClaude/Maxで継続するという現在の挙動も明記した。新しい通常GPT担当はSol/Max/Fastとし、本人の新指定を優先する。

027の正式CIは2370件中2368件合格・2件失敗・6skip。取消fixtureの時刻不備と、明示本人指定の出所を親に保存していない古いauthor-conflict fixtureが原因だった。後者も同じ作業場所/owner_requestを親に記録し、異なるモデルでのレビューを要求する期待自体は維持した。完全ログSHA256=F60F0426C3B4503B0D8A1AC9D14ACFB97600AD4D9579DB39AE20FCDCF08C5F08。その他Bookmark/Channel Windows+Linux、Unattended readiness Go、CodeQL4言語/集約は成功。現修正はcrate editionに合うRustfmt構文確認とdiff検査だけで、最新Green・Must0・merge・配布は未確認。
## 2026-10-21 10時台の限定修正

3227の固定4b2cf1d比較は1command1回、実Red1・陽性対照1・compile/fixture失敗0、後続assert5件未到達。原応答SHA256=4A3A16DA797DF8364F7F7B7A9D380F519EF9C6BA1ACCC56B3FD469837112723D、証拠3387162315f40156f507633b620b913b3c956d7b、43操作。対応nativeは2026-10-21T00:43:30.339ZのSol/max。

3229は027..75fの10ファイルを100操作で読み、Must1（新しい本人指定テストが既存の同ToDo二重起動防止へ先に当たる）とSuggestion/Nitを返した。原応答SHA256=085ED9FB9E6CC401E084E1114E20E8BC9E3AF30ECF8D070C4B4D502E56EFB3C4、対応nativeは2026-10-21T00:00:07.638ZのOpus/max。稼働中の親と同じToDoで重複登録を許す修正はせず、番号無し/同cwdの親で値の一致・不一致と有効な環境親を検査する。終了済み環境親は異なるToDo・異なる指定にして、修正前の誤った照合との差を検出する。ToDo本人指定とprepared世代の対照も保持する。

75fの正式CIは2374件中2372合格・2失敗・6skip。上記fixtureに加え、Consoleの実delegate/tickの退役会話テストが旧IDを起動引数に渡して失敗した。原因はConsole::jobsのruntime射影がpolicy_resume_resetを落とすことだった。起動時に必要な退役印・model/effort出所・レビュー元/除外の5項目を射影へ加える。条件式だけの読取確認と、実runtime情報の到達は違うため、このCI失敗を製品の実Redとして記録する。完全ログSHA256=C611CF526C4625B687CDBD69CFA5727EF74C80F4370BC08DC453AA234FC636D1。その他75fのWindows/Linux Bookmark/Channel、Unattended Go、CodeQL4言語/集約は成功。

Consoleの実tick検査では、現pendingによる復旧hint、元作者のsource IDs/除外、policyの出所、read-only/Maxを引数として確認し、過去のrepair印だけならhintを送らない対照も追加する。ToDoのpolicy既定が失敗modelのままでも、同じrequest/source/modelの現修復だけが優先する。後の別モデル指定と本人指定、固定source資格は保持する。モデル再選択は現在のreview_profileの順により、この例ではSonnet/Maxになることも受入期待へ明記した。新しいテスト名は実在するSol名へ揃えた。現修正は構文/差分確認だけで、最新Green・Must0・merge・配布は未確認。