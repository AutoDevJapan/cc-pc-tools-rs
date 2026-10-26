# OAuth枠不足後の独立レビューの継続（ToDo3669）

対応Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/453 。
元3080 / Issue283 / PR447の独立レビューで、利用可能性を確認して選んだ
Luna/policy/maxが通常レビューのOpusへ戻り、同じ枯渇枠で停止した。
通常レビューの既定は維持し、確認済みのquota復旧だけを保持する。

## 現在の状態（2026-10-26）

PR454は別モデルレビュー3720のMust0と修正後headのCI成功を確認してマージ済み。
Windowsのjobs/schedulerも更新・読戻し済みで、以後の並行更新はPR516の引継ぎを
正本として扱う。今回の旧候補を現用版へ再配布しない。
元3661の旧quota実行は排出済み。元19差分は受入済みの3708/3717/3722で
すべて確認され、PR459へ統合済みとして元3661を閉じた。
これは旧実行が自動でLunaへ切り替わった証明ではない。

18:16 JSTまでに、元3815の実quotaからの復旧票を1件確認した。
Grokの原失敗分類はtransientのまま保持され、正確なbalance exhaustionの情報を
PR544の分類条件で照合する。PR547の通常subwork受領の修正を経て、同じ仕様・親・
作業場所・権限・作者Sol除外を保ったAstra/policy/maxが24操作で正常終了した。
元実行と権限範囲のSHA256を通常CLIの情報から独立に計算し、復旧票と一致した。
旧processの不在、元仕様SHAの一致、OpenAI利用可・Anthropic枯渇の集約値も確認した。
元の分類・仕様SHA・世代の書き換えや、本人のモデル指定の流用はしていない。

この実例は元Lunaのテストと別のAstra実行であり、原429がcapacityだったとは扱わない。
元実行の予定世代1と未受領の元世代0は、受入済みPR547の限定照合で新受領世代1へ
結ばれている。単なる任意flagを資格にしたものではない。
PR544/547のマージとCI成功、PR547の固定headのMust0の公開受領票を確認した。
直接のnative・保存prompt・spec読取はguard未分類で未達。内容や属性・別pathを探さず、
既存3939が既に照合・保存した起動とnative、実行版の票だけを通常ToDoで依頼した。
この対応を確認するまで、今回の実動作受入は未完とする。

05:06 JSTの通常status応答では、当プロジェクトの更新後のレビュー21実行すべてに
`finite_review.spec_sha256`が保存されていた。仕様の識別情報が保存されないまま
待っている状態ではない。一方、05:04 JSTの全件応答には仕様ハッシュ付きの
quota源も自動quota復旧票もなかった。その後の実例は上の18:16の記録に分ける。
正常終了・一般のtransient・owner_requestでの起動をquota復旧へ読み替えない。
観測は返却範囲に限り、対象外の実行や読めない記録を成功・不存在と補完しない。

残る受入では、元の所有世代・仕様ハッシュ・権限・作者除外が一致する実quota源の
終了と旧process排出を確認し、通常の準備、CLI登録、最終受付、nativeの同じ起動を
順に照合する。過去の仕様SHAを後付けしたり、受入済みレビューを再実行したりしない。
実行機会がない間は同じ3669の技術再確認を保持する。時間の経過を利用枠の回復や
動作成功とは扱わず、新しいbuild・配布・モデル実行を確認の代わりに追加しない。

履歴と配布時点の証拠は[実行版の受入記録](evidence/quota-recovery-3669-runtime.json)に
保存する。下の時系列にある未実施・未確認の記述は、それぞれの検証時点の履歴。

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
- 上記62件の検証ソースは9c02e93c0a3c291af96be0197e5414338634be6e。
  開始時のmainとは区別する。その後main 3da96aee91e0c621fb4fc24a59c52bd0b303bac0を
  f65619abe6387d51f6a015fca744a8db64a29a61へ統合し、関連4クレート1,218件/1skip、
  Clippy、doctestと同headの正式CI3実行が成功した。詳細は証拠JSONの
  `integrated_candidate_validation`に保存する。
- 別モデルレビュー3709は上記base3da96aee..headf65619abの21ファイルを読了した。
  原応答3088bytes/SHA256 9e91a000cbb8fe65530bfee0f3a961e982e462736d65a5c06ac91f0b74869a3e。
  本人の限定モデル指定に従うLuna/maxと作者Sol/maxをjob/nativeで照合し、
  Must3件を受領した。読取完了は指摘解消・マージの完了ではない。
- 3709の修正Redは535592672c2f53df99093d906f1c2a90f94c4f02で4件が失敗した。
  完全ログSHA256: AF47FE28D07FF8FDFC5E5810A8BDAE27BE009B514B1C830E21D4E689F4F79E8E。
  後続の修正差分は別にレビューし、同じ最終headのCIを確認する。
- この検証時点ではマージ、Windows更新、実復旧は未実施だった。
  その後のマージ・配布・元3661の対応付けと、残る実行後切替の受入は上の現在状態を参照。
- 自分のブランチはscheduler/todo-3669-oauth-3080のまま。変更していない。

## 実装の境界

新しいCLIの許可フラグは作らない。schedulerが元レビューのquota分類、固定仕様、
所有世代、旧process不在、同じ権限とworktree、通常のOAuth quota更新を照合して
今回のlaunch keyに結ぶ。jobsは準備済みの行からだけ受け取り、元ジョブと新しい
OAuth集約観測を登録時とqueueの最終受付で再照合する。通常のToDo担当割当による
1世代の更新は、そのjobとworker receiptが一致するときだけ対応する。

通常のレビュー準備でも、schedulerが実際に起動本文へ埋め込んだ仕様のSHA256を
準備済みのselectionへ結ぶ。jobsは同じlaunch keyの下で、そのSHA・保存仕様・
実際にコピーするprompt内の仕様本文を照合し、元jobのfinite_reviewへSHAを保存する。
復旧時に元jobのSHAが無ければ現在のファイルから補完せず、復旧を受け付けない。
古いjobの記録を変更したり、復旧先にだけSHAを作って原本文の証明としない。

作者との独立性は既存のreview sourceから別に照合する。枯渇したproviderを
review_author_models / review_excluded_modelsへ追加しない。capacity/429の
検出・切替回数・同モデルの再試行処理は変更しない。

メタデータの元の更新時刻がある場合は既存quota samplerと同じ360秒以内を使う。
正常なOpenAIの更新応答で時刻が省略される場合は、更新応答の採取時刻を使う。
Anthropicの更新時刻欠落は未確認とし、採取時刻へ置き換えない。
5時間枠の100%は、リセット時刻が現在以前なら現在の枯渇に数えない。
週間枠が100%なら引き続き枯渇とし、短時間枠のリセットで週間枠を回復扱いしない。
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

9c02e93c検証時の観測はOpenAIがverified/available、Anthropicがunverifiedで理由は
`quota_refresh_failed`だった。最後に枯渇を観測した事実だけで新しい更新を成功とせず、
現在のこの組合せは復旧条件を満たさない。本人判断待ちではなく技術的な再確認対象。
この集約の成功を元3661の起動・native実モデル・レビュー完了の証明には使わない。

当時は、親3080が元3661の正規の待機と元19差分のレビューを保持し、レビューと正式CIの
完了後に通常マージ・Windows更新を行い、新しい実行版を読み返す手順とした。
未受入の候補を配布済みと扱わず、別モデルが使えない工程は正規の技術待ちにした。

Windows配布にはjobs/schedulerの通常の更新経路を使った。
並行変更・実source/head・実行物を照合し、別モデルレビューとCIの完了後に行った。
当時の原3661の待機や親PR447を、テスト成功だけで解除・完了とはしていない。
