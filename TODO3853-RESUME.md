# ToDo3853 再開記録

元依頼: Discordの質問回答と一般受付の競合を明示の1件だけ正規復旧する。
2913と3852の再起動、製品実装、Chrome操作、過去全件の移管は範囲外。

- 開始base: e1a4c21d（origin/mainと一致）。branch: scheduler/todo-3853-discord。
- 実所有者: AutoDevJapan/cc-pc-tools-rs。専用worktreeの通常guardはclear。
- 作業job: 20261026-002957-todo-3853。重いCargo/GPU/Civ6の割当なし。
- 既存source-linkは元/受付/保守の同一projectを要求し、終了済み受付のjob確認がない。
  既存captureのforeign拒否は保持し、source-repairという別契約を追加する。
- 3852はsucceeded/exit0の実jobを持ち2913だけにwait。2913は現在の質問waitと
  元worker/resume_generation=5を保持。復旧直前に再照合する。
- 今回作成した一時領域の所有ID: e529a241a3f12ef9f152d81844b0d862。
  path: C:/Users/makhmeto/AppData/Local/Temp/codex-owned-e529a241a3f12ef9f152d81844b0d862。
  終了時はsafe-delete plan→返された通常削除を別call→finish。拒否時はquarantine。
- 残り: 合成Red、実装・関連テスト、別モデルread-only review、最新候補CI、PR/merge、
  hash照合付き原子更新、通常lease下で1件復旧、metadata/hashと配送readbackを保存。
- 管理chatの記録先キー: work/reply2913-newpost-1563926324979175525。
  本文・秘密・添付は記録しない。実読/実反映は配送とは別に追う。

## 2026-10-26 実装中

- Issue513。Red commit3ffede09で通常foreign拒否と新CLI終了1を確認。
- source-repair実装後、33種類の誤関連付け/未終了/資格不足の拒否、5種類の途中失敗と
  応答紛失後の再開、既存source-link回帰が成功。配備済みtodo.exeを隔離DBへ接続し、
  元worker/世代/jobを保った全文instruction登録と再呼出しの不変性も成功。
- 保護未確認の原一般受付ファイルは開かない。新契約のsource-sha256は通常受付の
  ordinaryVersion。原ファイルはstatだけ保持し、正規APIから新たにcanonicalを作る。
  原bytesのhashを確認したと主張しない。既存source-linkのrawSHA契約は変更しない。
- 共通Source資格は元製品の登録rootで通常refresh成功を確認した。作業場所のSource資格を
  コピーや例外登録で作らない。個別の非Source内部記録は一般guardの許可対象へ広げない。

## 固定版レビューと追加修正

- PR515。2fe29625に対する通常CIを開始済み。関連race 24.550sとvetが成功。
- 最初のレビュー3855はAnthropic OAuthが100%のため未起動。OpenAI OAuthを正規refreshし、
  健康な未消費4枠を確認。既存catalogの別モデルgpt-6-sol/maxで3856を通常登録した。
  3855は3856への依存wait。直接の子ジョブ起動はしていない。
- 3856はjob20261026-011308-todo-3856、read-only、実native turn_contextもgpt-6-sol/max。
  2fe29625の指定11ファイルを読了し、Must1（別jobでの再開）とSuggestion1（通常巡回の
  添付metadataだけの変化）を返した。実データ・配布・実反映を確認したレビューではない。
- 別jobの再開は新たなRedで状態公開前/後とも失敗を確認して修正。最初の関連記録は
  不変に保ち、現在の正規actorと古いactorの履歴・終了を独立に確認する。旧actorがまだ
  runningなら拒否する。追加したraceテストは成功。通常巡回のmetadata照合も追加中。
- 次はこの追加差分の読取レビュー、変更した最終候補のCI、既存手順のhash/原子更新。
  実投稿のcanonical作成と配送はまだ行っていない。管理chatの同じ指定キーへmetadataを保存済み。

## 元担当の終了を受けた配送専用モード

- fbac342dまでのレビューは3856→3858→3860でMust0を確認した。親のcheckpointも
  正規CLIでsaved=true、実HEADと所有の全照合が成功。PR515はまだ未マージ。
- 本番変更前の最新metadataで、元2913 job20261025-101415-todo-2913が00:39に
  succeededで終了したと確認。質問waitと世代5、元jobを保持。3852は2913依存wait。
- その状態で通常answerを使うと元workerを退役することを、隔離native consumerの
  新しいRedで確認。実投稿ではこの操作をしていない。
- cc-todoの既存delivery-onlyへpreserve-question-waitを追加中。正確な元wait/ownerの
  CASを要求し、待機・job・世代・依存を保ったまま全文instructionだけ登録する。
  新契約のGo配送と毎分処理もこの経路を使い、途中失敗後に普通のanswerへ落とさない。
- Cargoは親に割当なし。Rustのテスト/配布ビルドは--needs cargoの独立checkへ渡す。
  追加native差分の別モデルレビュー、最新CI、todo.exeとrunnerのhash更新が必要。
  fbac候補の実行物は本番反映していない。実投稿復旧・実読・実反映も未確認。

## 2026-10-26 再開後

- 3865の固定de419413レビューを受領。実native gpt-6-sol/max/read-only。
  Must2（適用phaseの後退、保持票の元wait/recipient未照合）を合成Redで再現し、
  2e8a1410で修正。関連native Go raceは29.762s、vet成功。
- 元3853の保存済み管理指示を再読し、配送専用は製品の通常再開を永久に止める
  指定ではないと確認。canonicalと全文配送を先に保存した後、既存question-answer
  による同じ回答版の正常適用を別工程で確認する。元workerの直接起動はしない。
- その際の全文の二重追加を隔離した実ToDo CLIでRed確認。通常answerが現在の
  同じ保持配送のnote/instruction hashを検証して再利用する差分と受入を追加中。
  元待機を保持した配送、通常回答適用、実セッションの受領、実反映、製品完了は別。
- 旧de419413のRust CIは1636件成功、scope受入も成功。追加差分の成功には流用しない。
  本番変更はまだ0。新しいCargo確認、追加差分の別モデルレビュー、最終候補CIが残る。

- d11e646cは3868でRust296件と新しいnative consumerのGo47件/サブテスト120件が成功。
  実行物2f8fc5feをhash照合して親の所有領域へ受領。子のCargo生成先4,390,431,118Bは
  正規planのサイズ規則で削除希望へ退避済み。容量回収は未確認、既存清掃が次の手。
- 別モデル3869はMust1（保持配送後の別投稿で全回答が詰まる）を返した。
  実native CLIで「保持配送→2通目note公開→回答失敗→同版再試行」のRedを確認。
  現在のpending hashに合う保持票と、旧note prefixのhashを独立に確認し、新しい
  reply blockだけを結合する修正を追加。後の正常回答で同じwaitが消費されても、
  先の投稿は配送履歴として保ち、先の回答版の実反映やsession読取を推定しない。
  この追加差分のnative確認・別モデル差分review・最新CI成功後に配布する。

- 3873の固定55733837レビューはMust1（GoのTrimRightが旧note末尾改行を消し、
  Rustのprefix SHA照合を止める）を返した。元noteの全bytesを保って追記するよう
  Goだけを修正し、native受入のfixtureが末尾改行を持つことも明示確認する。
  Rust runtime sourceは55733837から変わらないため、3872の新実行物を受領し、
  このGo差分の関連nativeテストだけをやり直す。新しいCargo releaseは重ねない。

## 2026-10-26 本番配布と有限入口の原票照合修正

- PR515は最新72b38aedの必要CI成功・別モデルMust0後に2a609eaaへ正常merge。
  native84e79278とrunner8e1bfa89を通常runner byte lease下でatomic更新し、
  hash/DACL/旧receiver終了を確認した。全定期タスクの再起動は0。
- 原2913/3852のbaselineをhashで保存。実source-repairは通常lease busy後に有限再試行し、
  canonical作成前にconversation unavailableで拒否。原データ・担当・世代は未変更。
- 正規APIの返信body SHA、question SHA、owner/thread/requestは一致した。
  失敗原因はfixed decision原票のhashを、正規化済みidentity hashへ比較していたこと。
  original_decisionのhashは原票と一致し、identityは質問indexと一致することを確認。
  本文・秘密・添付を報告/別Sourceへコピーせず、metadataだけ管理chatへ保存した。
- この不一致を別の合成原票とidentityでRed再現。repairQuestionSourceで既存APIの
  original_decisionを原票へ、identityを元identity hashへ別々に結び直す。
  欠落/null/非object/別原票/別identityは拒否。原質問やindexを手で書き換えない。
- Rust source不変。配布済み最新nativeを隔離DBへ接続する会話全回帰を実行中。
  scheduler/todo-3853-decision-sourceへ分離し、最新mainへffした。変更はGoと合成テスト、
  この注記だけ。追補PRの別モデルreviewと最新候補CI後にrunnerだけ更新し、1件復旧を再開する。
