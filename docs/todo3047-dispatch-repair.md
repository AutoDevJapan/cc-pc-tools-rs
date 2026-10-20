# 通常の担当実行を修復する（ToDo3047）

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/257

2026-10-20の本人依頼「また誰も作業してないじゃん。勝手に止まらないでほしいな。」に対応する。
元ToDo3047が実装・検査・配備を担当する。別のToDoへ付け替えない。

## 入力と受入条件

1. 保存された明示モデルがsonnet/haikuなら、既存OAuthの正式なClaude 5.5 IDでcatalogを確認する。
   元の入力値はToDoとselection_inputに保持し、未知の明示モデルは拒否する。
2. 起動CLIが応答する前に登録したsubworkは、修復巡回でも通常の結果処理に渡す。
   key・todo_id・subwork・spec・needs・cwd・model・effortが保存値と一致し、
   projectとworktreeの既存保護判定が通ることを条件とする。モデルが未保存なら採用しない。
3. live候補が1件なら完了候補より優先し、2件以上なら変更しない。
   結果受領済み、本人停止、質問、保留中の復旧、依存待ちも変更しない。
   書き込み時にToDo行全体が読み取り時と一致しない場合は何もしない。
4. 修復対象は既存PC修復の3projectに限る。登録の採用自体は起動・取消・通知を行わない。
5. 既存scheduler.exeの正確な1ファイルだけを、既存のphysical・legacyMatch・sourceVerifiedで分類する。
   ディレクトリ、設定、履歴、未知の兄弟、リンクによる別実体は含めない。
6. 別モデルのMust解消、正式CI、通常mergeを経て、既存配備物のSHAを比較しbackupを保持して更新する。
   通常の定期実行から担当が開始し、工程と成果が進んだことを実記録で確認する。

## 検査の対応

- 条件1: `subscription_model_aliases_keep_the_requested_model_and_launch_canonical_id`。
  未知モデルの拒否は既存の明示モデル検査も維持する。
- 条件2〜4: `repair_tick_adopts_an_exact_registered_subwork_after_lost_launch_ack` と
  `repair_tick_prefers_one_live_receipt_and_leaves_ambiguous_live_receipts_untouched`。
  結果のdoneへの反映、元行の不変性、選ばれたlive IDを確認する。
- 条件5: runtimeExecutableの既存検査をschedulerのファイル名でも実行し、junctionの物理パス不一致を確認する。
- 条件6: CIと配備記録、既存巡回の結果、同じToDoの実ジョブと工程を対応付ける。

保存済みWIPにはRedの実行証拠が無いため、Red実行済みとは扱わない。
今回、変更前main425f90aのengineに追加した3検査を適用し、3件とも期待する動作の欠落で失敗した。
境界条件の追加後はモデル未保存の検査を先に失敗させてから修正する。
保存済みの196件・doctest・Clippyの成功とSonnet5.5の4ファイルレビューは保持し、新差分の影響範囲を検査する。

## 調査2939の実行条件

2939は元specで許可した匿名モデルの保存、固定GET、小さい所有Dockerでの工具版照合と、
researchのread-only/network-offの実行枠が一致していなかった。
既存の拒否を別経路で迂回せず、元の保護と審査を維持した正規経路を確認する。
2939は3047の完了待ちであり、3047から2939への待ちは循環するため登録しない。
起動を登録しただけでは復旧完了とせず、実操作の成功と元の成果を確認する。

通常の自動審査への提出口を、同じ2939の次の1試行に限って用意する。
read-onlyとnetwork=falseは維持し、approvalだけをon-request / auto_reviewへ切り替える。
これは操作の自動許可ではない。元specで列挙した固定GET・匿名出力保存・所有Dockerの
工具版照合だけを通常の審査に提出し、拒否は守る。一般research/reviewと固定レビュー・
要約には適用しない。原specの実SHA、ToDo・親・種類・owner・作業場所・キー・needs、
モデルとeffort、試行番号、期限を登録時と実行直前に照合する。
今回の明示依頼と原specを資格の根拠とし、後の任意specや別のToDoへは広げない。
有効期限は2026-10-22 00:00 UTC、対象は元174回に続く175回目だけとする。
匿名出力の保存以外の編集・配備、未知GET、拒否経路の変更、sandbox/networkの変更は認めない。
スケジューラーから呼ぶ既存codex_jobs.exeへの修復反映が必要なため、配備時の資格も
その既存1ファイルに限って追加する。既存の物理パス・sourceVerified・legacyMatchを維持する。
この追加の実行物の資格も2026-10-22 00:00 UTCで失効する。

1試行とは、試行175の同一ジョブの通常継続を含む。新しい176回目や別キーには適用しない。
原promptのSHAは登録から起動までの不変性を確認するもので、文意の正しさの証明ではない。
承認された操作はsandboxの外で実行される。操作の範囲は、保持したREAD_GUIDANCEに
原specの例外だけを追記したプロンプトと、通常のauto_reviewで制限する。
別の仕組みで操作一覧を機械的に強制するものとは扱わない。
配備は実SHAを直前に再比較し、既存ロックと稼働中プロセスを確認して、旧版backup付きで原子置換する。
specが読めない・原promptが変わった・期限切れ等では資格を落として起動を拒否し、
失敗を保持する。資格が成立しないまま同じ調査を繰り返す通常readerへ戻さない。

## 配備後のCodeQL受付と古いpush

通常配備後、CodeQLのmain pushがrunner待ちの間に次のマージが入ると、
既存bridgeは古いHEADをcheckout前に403で拒否する。実行39377282842では4件、
39380094549ではRust成功後の3件でこの条件を観測した。
最新mainだけを許可する保護は維持する。

mainへの通常pushは同じworkflow内で同じconcurrency groupを使い、新しいpushが
古い実行を取り消す。PRの初回は従来どおりPR番号ごとに取消可能とし、
PR再実行と定期実行はrun IDごとの独立したgroupを保つ。
workflowの権限、対象、checkout、runner、解析言語と必須検査は変えない。
変更前の失敗は実CIで確認し、変更後はYAML解析とconcurrency以外の構造一致、
別モデルレビュー、正式PR CI、マージ後の通常pushで確認する。
取消とhook検査が競合する場合の拒否は保持し、過去の403を成功へ書き換えない。
