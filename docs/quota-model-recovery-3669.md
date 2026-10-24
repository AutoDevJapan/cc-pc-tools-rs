# OAuth枠不足後の独立レビューの継続（ToDo3669）

対応Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/453 。
元3080 / Issue283 / PR447の独立レビューで、利用可能性を確認して選んだ
Luna/policy/maxが通常レビューのOpusへ戻り、同じ枯渇枠で停止した。
通常レビューの既定は維持し、確認済みのquota復旧だけを保持する。

## 受入と変更範囲

1. 共有ToDoの通常受付、schedulerの準備、jobsのCLI登録と最終起動を通し、
   同じレビューの旧実行がquotaで終了・排出済みで、Anthropic枯渇とOpenAIの
   利用可能な既存OAuth枠を確認できる場合にLuna/policy/max/Fast要求を保つ。
   本人の個別指定と復旧のpolicy指定を区別する。
2. 元job、所有世代、仕様、worktree、権限、作者モデルを照合する。
   古い記録、偽の記録、未排出、本人停止、未完依存、作者と同モデル、
   未確認の利用枠、APIキーだけの枠、追加支出が有効な枠では特例を認めない。
   queueの排他・二重起動防止とcapacity/429の分類・有限予算は保持する。
3. 一時的なprovider枯渇を作者の恒久除外へ入れない。通常レビューは
   review_profileを使う。全体のモデル既定、口座、guard、UI、中央Skills、
   queue設定、資源上限は変更しない。
4. 生のquota返信、アカウントID、メール、秘密値を新記録へ残さない。
   要求、登録、起動、nativeの観測、上流実効値、完了を区別する。
5. 影響クレートと利用側のテスト、doctest、Clippyを共有Cargoゲート経由で行う。
   Red/Green、別モデルレビュー、同じheadのCI、通常マージ、Windows実行版の
   正規更新・読戻し、元3661の限定復旧を別々に記録する。

## 再開記録

- 開始ソース: 76bc178702511ddea00f8136730eec31056252c5（確認時のorigin/main）。
- 実GitHub owner/publicをAPI確認した。親b249caaf/PR447は変更しない。
- 原todo.jsonと原3661/job.jsonはguardがunverifiedとして読取りを許可しなかった。
  別パスやファイル属性による探索はしない。許可された親の集約証拠では、
  原3661はcancelled・旧process不在・レビュー未完と記録されている。
- Red: c4aadd85でschedulerのprepareがLunaをOpusへ戻すassertに失敗した。
  完全ログSHA256: C16B9E3A7812FBDFD4244AE844EFF77D05F20B09BE7E139021A85D71CA5AF551。
- 最初のGreen: `cargo nextest run --locked -p cc-job-policy -p cc-scheduler -p cc-jobs
  -E 'test(quota_)'` は11件成功。保存したToDo行からのprepare、CLI登録、通常の
  ToDo担当割当を通すqueue admissionをテスト用runtimeで確認した。
  生のモデル実行・native観測・上流Fastの実効値はこのテストの対象外。
  完全ログSHA256: 5D63E4F239272BCD624A3CEA1C847482E57CAC6EAFA951437A01F7442D0727DD。
- 166054deの関連する4クレートは、先に成功したquotaテストを除く1,194件が成功
  （285.080秒、12skip）。完全ログSHA256:
  B254FE1D742A939CE2CA4F5B0D9160D71AF886651343F77759EC6ADF23C0A212。
- その後の実メタデータ対応と最終受付の修正は、影響するquota経路と通常の
  schedulerモデル・レビュー・準備の62件で確認し、全件成功（15.815秒）。
  仕様がメタデータ取得中に変わる場合と未来の終了時刻も拒否する。
  最終完全ログSHA256:
  38851A68725CE51FD1D34D05C5C1B384DDD9AFC6C8CE3EDF2B18691AA31F61BA。
- 対象4ライブラリのdoctestは各0件、Clippyは4クレートのall-targetsで警告0。
  Goは変更していない。workspace全件やreleaseビルドは追加していない。
- 別モデルレビュー、正式CI、マージ、Windows更新、実復旧は未実施。
- 自分のブランチはscheduler/todo-3669-oauth-3080のまま。変更していない。

## 実装の境界

新しいCLIの許可フラグは作らない。schedulerが元レビューのquota分類、固定仕様、
所有世代、旧process不在、同じ権限とworktree、通常のOAuth quota更新を照合して
今回のlaunch keyに結ぶ。jobsは準備済みの行からだけ受け取り、元ジョブと新しい
OAuth集約観測を登録時とqueueの最終受付で再照合する。通常のToDo担当割当による
1世代の更新は、そのjobとworker receiptが一致するときだけ対応する。

作者との独立性は既存のreview sourceから別に照合する。枯渇したproviderを
review_author_models / review_excluded_modelsへ追加しない。capacity/429の
検出・切替回数・同モデルの再試行処理は変更しない。

メタデータの元の更新時刻がある場合は既存quota samplerと同じ360秒以内を使う。
正常なOpenAIの更新応答で時刻が省略される場合は、更新応答の採取時刻を使う。
起動受付へ渡す観測の期限は別に60秒とし、登録と実行前に新たに観測する。
利用可能性は確認済みの1枠で足りる。全枠の枯渇には未確認の枠を含めない。
queueの再確認は15分間隔・最大3回で、通常の429/capacity予算とは別に記録する。
再確認失敗の終了を、新たなprovider quota/capacityの原エラーとは記録しない。

通常レビューが別providerへ選び直された場合も、選択後のproviderの保存済み枯渇を
確認してから起動する。自動選択で利用枠の事前確認をすり抜けない。

## 実際のOAuthメタデータの確認と限界

`cargo run --locked -p cc-jobs --example quota_inventory`で、モデルを実行せず、
通常のOAuth quota更新を新しい集約処理へ通した。新しい記録には固定の時刻・真偽・
理由分類だけを出力する。raw返信、口座ID、メール、秘密値は保存していない。

最後の観測はOpenAIがverified/available、Anthropicがunverifiedで理由は
`quota_refresh_failed`だった。最後に枯渇を観測した事実だけで新しい更新を成功とせず、
現在のこの組合せは復旧条件を満たさない。本人判断待ちではなく技術的な再確認対象。
この集約の成功を元3661の起動・native実モデル・レビュー完了の証明には使わない。

親3080は、元3661の正規の待機と元19差分のレビューを保持する。レビューと正式CIの
完了後に通常マージ・Windows更新を行い、新しい実行版を読み返してから限定復旧する。
未受入の候補を配布済みと扱わず、別モデルが使えない工程は正規の技術待ちにする。

最終的なWindows配布はjobs/schedulerの通常の更新経路を使う。
並行変更・実source/head・実行物を照合し、別モデルレビューとCIの完了後に行う。
原3661の現在の待機や親PR447を、今回のテスト成功だけで解除・完了にはしない。
