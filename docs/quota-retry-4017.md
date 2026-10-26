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
