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
