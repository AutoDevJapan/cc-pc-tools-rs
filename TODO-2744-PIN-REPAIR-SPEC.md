# 正式product-ciの固定SHAだけを更新して配布する

元ToDo2744から切り出す、修復source専用の実装工程。元2639の製品ソースを編集しない。製品PR1002のdraft解除・merge・Prd変更は行わない。元1636/2016/2274、他3製品、元2648/2710/2730/2381の状態・停止・依存・未完を変更しない。

元2744は有限な独立checkであり「独立したPRの作成やマージを担当しない」と指定されている。この工程が修復sourceの通常PR・別モデルレビュー・必要CI・通常merge・既存tool正式配布だけを担当し、元2744は配布後の製品CIを担当する。同じ作業を重複登録しない。

## 確認済みの現物（2026-10-17 01:22 JST）

- 修復source repository ID1406818713。GitHub APIで実owner AutoDevJapan/private/admin/nonforkを確認した。最新main=a83aa2606e7354088cd23da111709729e4c82e35。着手時に再確認し、正規最新mainを基準にする。
- tools/product-ci/main.goの候補1285649124/PR1002だけが旧b93e126b172adbb29ad2081af4732f4b42f8bc95。main.go blob462ad8966919c52267ca3fa05d5eeb01aa3d5684。
- PR187は通常merge済み。正式配布元a83aa2606e7354088cd23da111709729e4c82e35、module tree b223ef91a223e15a480500aca55bef2f1144835f。
- D:/CC/Tools/product-ci/product-ci.exeの現SHA256 E5D2C61AC5B0001A565A68C37561B1CBDAA6931EE13B8109D83BCFCBD6A2BE45、11718656bytes。配布receiptと一致。Linux正式helper product-ci-linux-edd3af3 SHA2568FA00AC4B89FAF934C00C3E547FD1D21054BB5A62474B4BE60481D97E4F35FFF、11579380bytes。
- 正式tool inspectは完了までHidden起動で待ち、実exit1。候補1285649124はfixed same-repository PR head rejected。他3候補はfixed-head-verified。新headに旧toolを使用しない。
- 製品repoID1285649124/PR1002の指定head20c441df545a88f4a839d7e906b5aeb4f1708ae3、tree007b84082008ea9effd5052f63856273aabbfb17、唯一の親b93e126b172adbb29ad2081af4732f4b42f8bc95。実ownerAutoDevJapan/private/admin/nonfork/同headRepo・baseRepo/branchHead一致/open/drafttrueをAPI再確認済み。名前はAPI経路を組み立てるメモリ内だけに保持し、source/rawlog/名前/名前入りURLをstdout・会話・報告へ出さない。

## 変更と完了条件

1. 最新mainのtools/product-ci/main.goで当該候補の固定SHAをb93から20cへ更新する。変更は必要なpin1行とその意味のある回帰検査だけ。他3候補ID/PR/SHAはそのまま。未知repo/fork/owner/workflow bytesの拒否条件、復旧・排他・guard・registry・queue設定を変えない。
2. tools/product-ciの既存unit/vetと必要buildを実行。原PR173/174/183/185/187を再実行せず、新規通常source commit/PRで保存する。既存CCルールに従い別モデルのMust0、必要CI成功、通常mergeを順に実施する。単一ファイル読取レビューはHaiku5.5を共有ToDoに--spec付き登録。複数ファイルにまたがるレビューは適切な別モデルに共有ToDo経由で登録。子Codexジョブ直接起動禁止。
3. mergeした正式sourceから既存product-ci.exeと対応Linux helperを正式配布し、旧版backupと新source/module tree/binary bytes/SHA256/配置後inspectの実結果を保存する。即席実行機・旧toolの固定拒否迂回・mock successは禁止。
4. 元2744へsource/head/tree/PR/CI/レビュー/配布receipt/新Linux helper絶対パスとhashを返す。成果は指定済み D:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016 の明確に自分の所有ID配下へ保存し、元資料を保持する。scope判定が限定される場合も指定された操作証跡以外へ広げない。

この工程では製品CIのjob実行・新dispatch/rerunは行わない。元2744が、既存queued CodeQL run38929313859（4job）と通常secret run38929321895/job117068929592を同候補で直列処理する。受付/配布/inspectを製品CI成功に読み替えない。

## 運用条件

起動時にD:/CC/AGENTS.md、作業先AGENTS.md/CLAUDE.mdを読む。既存OAuthサブスク枠のみ、APIキーAI/新費用/Hosted予算増額/GPU/Civ6/本人PC操作/Terminal/UACは禁止。CLIは非表示起動。guard/除外一覧/原製品folder/owner-work登録は変更しない。拒否操作の迂回禁止。新tempはsafe-delete create→plan→別呼出し通常削除→finish、拒否は正規quarantine。既存他担当のtempは不操作。

自分のworktree branchに意味のある区切りでcommit。branchを変えた場合、終了・待機前にscheduler checkpoint-workspace --request-fileで自分のToDo/job/expected_branch/branch/実HEADを保存。クラッシュ再開情報を作業フォルダへ残す。共有ToDoの状態はschedulerが更新する。依存待ちのみならtodo wait --onを記録し、owner blockにしない。日本語報告はjapanese-report-writingで確認。

元2744の読取証跡は D:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016/57716f4a99527a8f0a4f73b742aa6a18/ に保存予定。親2639の所有root d84c967c6eb3b9abf993ad85a20fb9b9は作業中なので削除しない。
