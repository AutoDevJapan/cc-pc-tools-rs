# 残る5製品の現在の継承（2910）

2026-10-19の本人直接指示と元2834から、次の5件を現在の継承先として
通常ToDoへ登録した。登録前に全ジョブの実live/queued/startingと旧担当を
照合し、同製品の稼働担当がいないことを確認した。MCP α、Press、AO、
13a13e1の既存担当は保持する。原名簿・原192件は復旧していない。

| 製品 | 現在のToDo | 実GitHub repository ID | 継承する有限な要件 |
| --- | --- | --- | --- |
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
AO2839と13a2850の宣言は変えない。汎用allowlistや新しい許可APIを作らず、
既存の受付API、実deny、物理path、origin/common-dir、通常の停止・依存・
資源・容量・優先順位の検査を使う。受付の実GitHub照合も維持する。

旧の原記録・原資源と旧宣言には、既存の2026-10-21 00:00 UTC直前までの
有限期限を維持する。AO2839/13a2850も今回継続扱いへ変更しない。期限外の
これらのpath分類は資格読取前に拒否する。旧192件や原資源の期限を延ばさない。

今回の5件には、主2903からこの会話で受けた2026-10-19の技術工程指示を適用する。
10/19の再開依頼と10/12の既存の継続開発許可に基づく同一業務であり、新しい
本人承認ではない。5件とも未実行jobs0を再確認したうえで、同じToDoの保存specに
継続を明記して正式CLIで更新し、新パスとSHAを再取得して宣言へ固定した。
元specと原SHAはspec_historyに残す。所有範囲・製品要件・費用・公開方向は変えない。

継続は新しい5宣言のコンパイル済み属性だけに指定し、任意manifestやToDoの値から
有効にしない。2026-10-19以降、同じ登録root・実repo ID・新actor・固定specと
実deny等が成立し、statusがtodo/doing/waitかつdispatchable=trueの間だけ照合する。
done/block/取消等や無効化、別root/spec/SHAは拒否する。依存待ちの資格確認は
状態を書き換えず、実起動の停止・依存・資源等は既存Rustの通常ゲートを保つ。
既存Rustの現在資格の受け取り処理に別の絶対期限はなく、同APIとfingerprintを照合する。

主担当から正規登録された直接のreview/check/researchも、この同じ継続属性を
引き継ぐ。親の現在資格・停止/完了・固定spec・同repo/root/common-dir・親の実jobと
正規worktreeを毎回照合する。親停止/完了時に子を独立した継続資源へ昇格させない。
子の通信不要のファイル判定と、受付APIのオンライン照合も維持する。

検査では10/21直前・同時・それ以降に現在業務と正規子の受領/path照合が成立し、
旧宣言と原資源が失効することを同じ時刻入力で確認する。停止/完了・無効化・別root・
別spec/SHAと実denyの拒否も確認する。時刻を注入するGo検査と実CLIでの現在の受入は
区別し、OS時刻を変更したり、未来の日の本番操作を実施済みと報告したりしない。

関連Go検査、固定差分の別モデルレビュー、正式CI、通常mergeの後、並行mainを
取り込み、最新mainから正式guardだけを原子的に更新する。正式実行物のhash、
各現在資格APIのfingerprint、各root/正規worktreeと旧worktree拒否を照合する。
登録やAPI成功を実再開とせず、通常schedulerの実jobによるコマンド・変更または
監査成果・検査結果まで2910が追跡する。製品の完成は各継承先の条件として残す。

main_windows.go、他のGo分岐、Rust、CI資源の分類、動画、課金、TUIは2910では
編集しない。主2903と元2834へ実事実を返し、別担当の配布を巻き戻さない。
