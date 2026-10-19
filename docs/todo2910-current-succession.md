# 現在の7業務の継続と残る5製品の実作業復帰（2910）

2026-10-19の本人直接指示と元2834から、既存AO/13aと今回登録した5件を
現在の7継承先として扱う。5件の登録前に全ジョブの実live/queued/startingと旧担当を
照合し、同製品の稼働担当がいないことを確認した。MCP α、Press、AO、
13a13e1の既存担当は保持する。原名簿・原192件は復旧していない。

| 製品 | 現在のToDo | 実GitHub repository ID | 継承する有限な要件 |
| --- | --- | --- | --- |
| GoDD-AO | 2839 | 1296537193 | 既存の初回導入の未完工程。元担当と固定specを保持 |
| GoDD-13a13e1 | 2850 | 1283948432 | 既存のIssue569。元担当と固定specを保持 |
| GoDD-Agent | 2913 | 1296644614 | 元2372の既存Prdの認証・表示と配備元確認 |
| GoDD-Creators | 2914 | 1299522585 | 元2373とIssue1857の既存Prd接続 |
| GoDD-Rosetta | 2915 | 1397020302 | 元23とIssue8のCOVOL COPY |
| GoDD-Genesis | 2916 | 1199265386 | 本人決定を記したIssue1686の認可再検証 |
| GoDD-VirtualStreamer | 2917 | 1305037899 | Issue984とPR988の資格情報のbyte検証 |

すべて実所有者はAutoDevJapan。登録rootは
`C:/Users/makhmeto/Desktop/dev/ADJ/<製品名>`。既存3製品とCreatorsの
origin/common-dirはGit metadataだけで照合した。Agentの旧rootは不在で、
正規GitHubのmainから新rootへ`--no-checkout`でGit管理領域を作り登録した。
保護中の旧source/worktree/spec本文は読んでいない。旧途中成果や状態は保つ。

`repair-guard/current_windows.go`の既存有限宣言に、各実ID、新番号、
登録root、通常CLIが保存した固定specパスとSHA256だけを追加する。
AO2839と13a2850の登録root・実repo ID・固定specと元担当は保持する。
汎用allowlistや新しい許可APIを作らず、
既存の受付API、実deny、物理path、origin/common-dir、通常の停止・依存・
資源・容量・優先順位の検査を使う。受付の実GitHub照合も維持する。

原資格1626/2407/旧動画2475と原資源には、既存の2026-10-21 00:00 UTC直前までの
有限期限を維持する。期限外の原資格は原記録の読取前に拒否する。
旧192件や原資源の期限を延ばさない。

今回の5件には、主2903からこの会話で受けた2026-10-19の技術工程指示を適用する。
10/19の再開依頼と10/12の既存の継続開発許可に基づく同一業務であり、新しい
本人承認ではない。5件とも未実行jobs0を再確認したうえで、同じToDoの保存specに
継続を明記して正式CLIで更新し、新パスとSHAを再取得して宣言へ固定した。
元specと原SHAはspec_historyに残す。所有範囲・製品要件・費用・公開方向は変えない。

その後の主2903の最新工程指示により、同じ10/19に作った現在継承先のAO2839と
13a2850も同じ業務の完了または本人停止まで継続する。旧復旧期限だけで止めない。
現在7件と原資格の工程分離であり、新しい本人承認やモデル結果を作るものではない。
両既存specの実SHAは正式資格APIで宣言値と一致し、登録rootの指紋も一致した。
実行中の固定specを更新せず、現在の元担当と未完了条件を保持する。

継続は固定7宣言のコンパイル済み属性だけに指定し、任意manifestやToDoの値から
有効にしない。2026-10-19以降、同じ登録root・実repo ID・新actor・固定specと
実deny等が成立し、statusがtodo/doing/waitかつdispatchable=trueの間だけ照合する。
done/block/取消等や無効化、別root/spec/SHAは拒否する。依存待ちの資格確認は
状態を書き換えず、実起動の停止・依存・資源等は既存Rustの通常ゲートを保つ。
既存Rustの `cc-scheduler/src/current_succession.rs::verify_current_product` は
同APIのdomain/verified/元番号/現在番号/project/原名簿未復旧の各fieldと、
登録root・specパス・spec本文の3fingerprint、正式guardのpath判定を照合する。
同関数は別の絶対期限を参照せず、engineの通常停止・依存・資源ゲートは保持する。

主担当から正規登録された直接のreview/check/researchも、この同じ継続属性を
引き継ぐ。親の現在資格・停止/完了・固定spec・同repo/root/common-dir・親の実jobと
正規worktreeを毎回照合する。親停止/完了時に子を独立した継続資源へ昇格させない。
子を登録した親は実担当を持つdoing/waitを要求し、未開始のtodo親へ広げない。
子の通信不要のファイル判定と、受付APIのオンライン照合も維持する。

検査では10/21直前・同時・それ以降に現在業務と正規子の受領/path照合が成立し、
原資格と原資源が失効することを同じ時刻入力で確認する。7件の停止/完了・無効化・別root・
別spec/SHAと実denyの拒否も確認する。時刻を注入するGo検査と実CLIでの現在の受入は
区別し、OS時刻を変更したり、未来の日の本番操作を実施済みと報告したりしない。

関連Go検査、固定差分の別モデルレビュー、正式CI、通常mergeの後、並行mainを
取り込み、最新mainから正式guardだけを原子的に更新する。正式実行物のhash、
各現在資格APIのfingerprint、各root/正規worktreeと旧worktree拒否を照合する。
登録やAPI成功を実再開とせず、通常schedulerの実jobによるコマンド・変更または
監査成果・検査結果まで2910が追跡する。製品の完成は各継承先の条件として残す。

main_windows.go、他のGo分岐、Rust、CI資源の分類、動画、課金、TUIは2910では
編集しない。主2903と元2834へ実事実を返し、別担当の配布を巻き戻さない。
