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

既存の有限期限は2026-10-21 00:00 UTCの直前まで。この期限は受付だけでなく
主担当のroot/worktree分類にも適用する。従前の主担当path分類には時刻検査が
欠けていたため、既存の時刻述語を同経路へ追加し、期限外はmetadata読取前に
拒否する。子の通信不要のファイル判定と、受付APIのオンライン照合を維持する。

関連Go検査、固定差分の別モデルレビュー、正式CI、通常mergeの後、並行mainを
取り込み、最新mainから正式guardだけを原子的に更新する。正式実行物のhash、
各現在資格APIのfingerprint、各root/正規worktreeと旧worktree拒否を照合する。
登録やAPI成功を実再開とせず、通常schedulerの実jobによるコマンド・変更または
監査成果・検査結果まで2910が追跡する。製品の完成は各継承先の条件として残す。

main_windows.go、他のGo分岐、Rust、CI資源の分類、動画、課金、TUIは2910では
編集しない。主2903と元2834へ実事実を返し、別担当の配布を巻き戻さない。
