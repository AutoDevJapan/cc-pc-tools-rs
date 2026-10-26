# GPTの利用枠待ちで研究が再開されない問題（ToDo4017）

対象: AutoDevJapan/cc-pc-tools-rs、repository ID 1406818713。
対応Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/570 。
元3080の読み取り専用研究3719を同じ仕様・権限・担当関係で再開する。
原Issue283、598件の研究完了、他担当の原資料は本修復の完了対象ではない。

## 仕様と受入条件

- quotaで終了・排出したGPTの有限作業は、同じGPT/max/Fastと新しく健康を確認した
  別の既存OAuth枠を先に使う。匿名namespaceを実際に選択する枠の健康情報へ結ぶ。
  同じ口座の別namespaceは別枠と数えない。未知・古い・消耗済みの枠は使わない。
- 最初の失敗後の同GPT追加再試行は既存分を含め最大3回。元ジョブの回数・履歴・
  待機予算を新ジョブへ引き継ぎ、仕様・scope・sourceの変更で予算を作り直さない。
  使える同GPT枠が無い場合か予算終了後だけ、健康な別providerへmaxで進む。
- 本人指定モデル、作者と同じレビュー、本人停止、質問・依存・明示resetは保持する。
  元の仕様本文SHA、世代、親、worktree、権限、実processの排出を正常受付で照合する。
  旧job・reader・ACKを補完せず、元3719の既存4f6仕様を保持する。
- 過去の同じquotaエラーから毎回now+1時間を作らない。過去の終了時刻へ期限を固定し、
  正当な明示reset/cooldownは保持する。新しい完全な回復証拠だけが自動準備待ちを再評価する。
- 登録時と最終queue受付で、新しい健康情報・実選択枠・元scope・履歴を再照合する。
  元sourceの検査や研究本文の利用を、新しいreaderの結果へ付け替えない。

## 再現と進行記録

開始branch: scheduler/todo-4017-skills3080。
開始HEAD: 59c40352b8bea4366a58ec85de2e5f44f7404583。
実CC_JOB_ID: 20261027-043055-todo-4017。割当資源: cargo。
remote/APIで実所有者・公開・固定repository IDを照合し、worktreeのguardはclear。

親が保存した限定metadataでは旧研究jobは490 steps、quota、同GPT再試行1回、
有限仕様SHAは4f6fe32f62db56bed2acb4d378147ff952ad06230243fb45f7674f3158313fc3。
通常ToDo APIで3719のpreparing/todo、履歴6件、依存無しを確認した。
共有台帳全体・旧job/specの直接guardはunverifiedのため直接参照を打ち切った。
通常の公開project CLI/APIと親の指定集約資料だけを使用し、保護設定は変更しない。

Red: 変更前の公開本体に新しいscheduler回帰4件を実行し、3件が意図したassertで失敗、
拒否境界1件は成功した。初回のテストコンパイルの借用エラーは直し、Redの根拠にしない。
コマンド: `cargo nextest run --locked -p cc-scheduler -E 'test(gpt_quota_) | test(historic_quota_)'`。
完全stderr SHA256: 1ce6c6462cce89a21c67b9e68df4c7c19b28cf43da76a2885c53651dc16db188。

初期Greenではpolicy/schedulerの関連21件、jobsの登録・最終受付9件が成功した。
選択枠だけ不明になり別の正常枠が残る場合、登録後の履歴・回数・sourceの変更も拒否した。
その後、provider変更後の失敗でも予算が戻らない回帰と、通常CLIによる一件限定の
`status --job-id ID --json`を追加した。このモードはbody・path・error・口座を返さず、
現在の仕事の固定metadataだけを読む。全jobの読み込みやrefreshは行わない。
最終候補の関連5クレートは共有ゲートで順番を待っている。成功前に全件成功と扱わない。

追加の実証: 通常の候補選択がnot_beforeを先に除外していたため、prepareだけでは
自動待機の再評価が届かなかった。Sourceに結ぶ候補判定を共有し、保存期限を変更せず
通常の優先度・資源・最終受付へ通す。模擬時刻と作成時刻の間隔も揃えたRedでは
allowance1/資源空き/queue0により未起動を再現し、修正後は同じ正常dispatchが
新しいjobを一件だけ起動した。質問終了待ち・本人停止・正当reset・別資源は保持する。
関係するquota/targeted dispatch/準備の66件は成功。最初の模擬時刻の不一致による
launch spacingの失敗を、不具合の根拠には使わない。

実機の一件限定metadataで、旧3719の490steps/quota/同GPT再試行1回、4f6仕様、
native GPT6.1-Sol/maxと、別モデル4024のnative Opus5.5/maxを照合した。
この読取は旧job/spec本文を返さず、インストール済みの正本EXEも変更していない。
レビュー対象a5a4ffe6の関連5クレート2189件、後続変更の影響40件、5ライブラリの
doctest各0件とClippy all-targetsは成功した。正式CIとレビューは別途確認する。

最終Green、別モデルレビュー、CI、マージ、Windows配布、元3719の
実再開・tool処理、親3080へのCAS受領は未完了。登録だけで完了にはしない。
一時領域所有ID: df33b3e0d7170e637ab7900564257515。

## 別モデルレビュー4024の修正

固定a5a4ffe6は実native Opus5.5/maxで読了し、Must5件を受領した。
回復ジョブの初回受付と、受付後の通常capacity再試行の状態を分けて設計し直した。
元capsuleは保存したまま、初回予算と履歴をseedにし、その先頭を保持する通常capacityの
追加履歴・単調な回数・現在の実選択枠を照合する。namespaceの再作成も匿名の安定した
log labelで照合する。登録済みv1は元のv1証拠で検証し、実際の後続履歴を消さず予算を結ぶ。

- 同じモデルの枠が未指定だった最初のquotaでも、新しく健康な枠を確認して再開する。
- 別providerからGPTへ戻る場合も、未使用の正常枠を選び、同GPTと切替の回数を戻さない。
- 早期の再確認を300秒ごとに制限し、同じ失敗のnot_beforeを再延期しない。
  不正な履歴・有限予算終了は理由を残す技術終了とし、新しい通常jobで予算を作り直さない。
- queued/starting/runningのretry_not_beforeだけではresource待ちに分類・取消ししない。
- 初回の準備予約から通常linkまでの世代受渡しと、初回受付後のcapacity所有確認を分け、
  元仕様・scope・本人停止・依存・質問は最終受付で引き続き照合する。

これらの回帰7件を修正前fd3f03d1のruntimeで実行し、7件とも想定したassertで失敗した。
修正後は、再混雑後の同GPT・provider切替・15分待機・登録済みv1の再受付、停止・依存・
世代・scopeの負例を含む49件が成功した。新しい全件確認を無条件に追加せず、関係する
残り3クレートの検査を別に進める。同じ49件は重複して回さない。

fd3f03d1の正式CIでは2872件中、ヘルプの旧互換oracleだけが失敗した。
元oracleのバイト列を保持し、今回の`--job-id`の追加箇所だけを明示的に取り除いて比較する
既存の拡張方式へ合わせた。同テストは上の49件に含めて成功した。
修正後の別モデル確認・同一headの正式CI・Windows配布と実研究再開は、引き続き未完了。

## 再確認4034の境界修正

4034は固定725fb9c5を実Opus5.5/maxで読了した。元Must2・3・5の解消と、1・4の
主要経路の改善を確認し、残る境界をMust A〜Cで返した。
Anthropic内の異なるモデルへの切替も有限switchとして数え、同一モデルやGPT間の
切替とは区別する。成功した厳密なsource/spec/scope/health照合でmetadataの連続失敗
回数だけを解除し、nativeの同GPT回数・切替回数・待機予算・履歴は解除しない。
metadata失敗の累計は別に保持する。非GPT sourceがswitch3回を使い切れば、同GPT回数が
3未満でも技術終了になり、在庫照会を繰り返さない。

GPT→Sonnet→Opus、成功を挟むmetadata失敗1回＋2回、非GPTのswitch終了を追加した。
旧runtimeでAnthropic内切替と非GPT終端のRedを確認し、metadataと旧v1再受付の別2回帰も
それぞれRedを確認した。修正後の関連52件は成功した。
v1 Claudeの再受付は、初回の厳密な証拠を維持し、実行後は現在のClaudeの健康情報で
照合する。旧GPTが回復したことだけで、そのClaudeを拒否しない。
登録時の実モデル・effortも準備と直接比べ、異なるworkerを作らない。

配布はjobsを先に、schedulerを後に同じ候補から更新する。二つの既存deployment.lockを
同じ順序で確保し、旧SHA・元ACL・候補SHAを確認して旧版を保全する。新jobsはv1/v2を
読み、新schedulerがv2を作る前に新jobsを配置する。古いschedulerの準備済みv1や、
稼働中の健康な担当は書き換え・一斉停止せず、通常のSource/世代/停止/依存照合を保つ。
実際の配布・旧SHA・新SHA・通常CLI・新3719の実操作は別の証拠で確認する。
