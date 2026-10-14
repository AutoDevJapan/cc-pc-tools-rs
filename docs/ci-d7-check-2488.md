# d7固定CIの確認記録（親2488・独立check2613）

対象はPR144にpushされた固定HEAD `d7d2b30ba0be788f2ad84f560016dd62082ff885`、tree `179e84815ef01e158a022e2fa5e181a49e4d2fc8`。
正規GitHub APIで本人所有のPRIVATEリポジトリ、PR HEAD、commit treeを照合した。

必要な実行はBuild 38478465876、CodeQL 38478465845、Readiness 38478465881、AgentProgress 38478465892、RecordRecovery 38478465833、Channel 38478465840、Browser 38478465853、Bookmark 38478465870。すべてrun_attempt=1、jobのHEAD/attemptも一致。旧sourceの実結果を流用しない。

作業の原証拠はこのworktreeの `.ci-work/d7-2488/` に保持する。APIレスポンス、各呼出のexit/path/bytes/SHA256、取得できた原全文、取得不能のstderrを保持する。秘密値を含み得る原ログ・PRIVATE JSONはGitへ追加しない。

途中状況（2026-10-14 07:37 JST）：8workflowとも未終端。20job中4jobが先行完了。全workspace、doctest、全targets Clippyはまだ未確認。GoのCodeQLのみ同SHAのuploadと解析error空/results_count=0を確認。ただし原ログにGo未処理ファイルの診断があるため、全Goソースを解析済みとは主張しない。

取得制限：3先行jobの通常ログ取得が端末制御文字でexit1。許可オプションは正規ghゲートのunknown flag拒否でexit2。制限を迂回せず、原全文は未確認として保持する。cmd wrapperの最初の3APIは構文エラーだった。wrapperが呼ぶ同一gh_gate.exeをCREATE_NO_WINDOWで起動し、通常APIのexit0を確認した（ゲートの無効化や直gh.exeは使っていない）。

再開時は `.ci-work/d7-2488/check-state.json` と最新pollのAPI、呼出receiptを読み、同じHEAD/run/attemptだけを確認する。有限checkの上限は2026-10-14 08:04:30 JST。未終端が残れば同ToDo2613のwait-retryへ保存し、別監視担当を作らない。修復・Cargo・再実行・merge・配布・Discord送信・GPUはこのcheckの範囲外。自分の状態はスケジューラーが更新する。親2488の全修復完了を主張しない。

2026-10-14 07:47 JSTの区切り：CodeQL、RecordRecovery、Channel、Browserの4workflowがsuccessで終端。20job中13job終端。CodeQLは同SHA・refs/pull/144/head・同workflowの新解析4件を正規APIで確認した。actions 1947445510（17 rules）、go 1947411189（34 rules）、javascript-typescript 1947449569（87 rules）、rust 1947470713（26 rules）。すべてerror空・results_count=0、対応する4SARIFのprocessing_status=complete。Go/Actions/JavaScriptの原全文は取得できた。Rustの原全文は通常取得の制御文字エラーにより未確認。Goの未処理ファイル診断は保持し、全ファイル解析の証明へ広げない。

詳細manifest/本文は `.ci-work/d7-2488/ci-d7-result-2488.private.json` と同名 `.private.md`。全原取得のpath/bytes/SHA256と、取得不能のstdout/stderr/receiptもmanifestのfile_inventoryに残す。状態だけで全workspace・doctest・Clippyの実コマンド成功を主張しない。

2026-10-14 08:04 JST：当初の約30分の有限checkを終了。最新poll024の8run/20jobは固定d7・attempt1・所属run一致・重複なし・ページ欠落なし。CodeQL/RecordRecovery/Channel/Browserはsuccessで終端。Buildはin_progress、Readiness/AgentProgress/Bookmarkはqueuedで未終端。20job中13job終端、7job未終端。CIのfailure/cancelled/timed_outはこの時点で検出していないが、全件成功を主張しない。

原全文はCodeQL Go/Actions/JavaScriptの3jobを取得し、receiptのbytes/SHA256と現物を照合。その他の終端10jobの原全文は通常ghの端末制御文字制限で未確認。allow-escape-sequencesの正規ghゲート拒否を保持し、別経路・直gh.exe・ゲート改変で迂回していない。全workspaceのpass/fail/skip実数、doctest、全targets Clippyの実コマンドexitはnull/未確認。APIの先行4検査successも原全文の件数やexitへ代用しない。GoのOS別step状態を保存し、nonverbose/cachedの個別件数を推測しない。poll015のBookmark API TLS timeoutは保存し、poll016以降の通常取得成功も別receiptとして保持。

全原取得のstdout/stderr/receiptの絶対path・bytes・SHA256、各job/step状態、同d7のCodeQL4新解析(error空/results0・4SARIF complete)、取得可能な原ログ根拠行は `.ci-work/d7-2488/ci-d7-result-2488.private.json/md`。再開手順は同フォルダの `resume.private.md`。派生manifest以外の原証拠を上書きせず、次回は既存の最大poll番号+1から新しい有限30分窓で確認する。同ToDo2613へ600秒wait-retryを記録し、元spec/同worktree/同source/同runを保持してスケジューラーに再開を任せる。親2488や他ToDoの状態は変更しない。新監視担当・Cargo・GPU・CI再実行・source修復・PR・merge・配布・Discord送信は行っていない。

2026-10-14 08:18 JSTに同ToDo2613・同worktreeから再開。gitは865483dでclean、本人所有PRIVATEとPR144 HEAD d7・固定treeを新しい通常gh APIで再照合した。原報告をcheckpoint-20261013-231835-result.private.json/mdへコピーし、原API/原ログ/拒否receiptを保持。今回はpoll025から保存し、確認期限は08:48:09 JST。新担当・Cargo・GPU・CI再実行は作らない。

再開直後は4workflow成功/4workflow未終端、13/20job終端のまま。Buildのcheck-run115574911494も同d7でin_progress・annotations_count=0（通常APIの新しい原JSONを保存）。注記0を検査成功や実コマンドexitへ代用しない。原全文の許可flag拒否は再試行せず、既存の未確認を保持する。

2026-10-14 08:48 JST：再開後の約30分確認を終了。今回poll025〜poll050の新しい通常APIは8run/20jobの同d7・attempt1・run所属・ページ欠落なしを維持し、最新API取得欠落0。CodeQL/RecordRecovery/Channel/Browserはsuccess、Buildはin_progress、Readiness/AgentProgress/Bookmarkはqueuedのまま。終端13job/未終端7jobで、新たなfailure/cancelled/timed_outを検出しなかった。全件成功とは判定しない。

固定sourceのCodeQL4新解析は今回末尾のpoll050-codeql-analyses-final.jsonで再照合した。前source解析を流用していない。取得済み原全文3jobのbytes/SHA256と現物を再照合。原全文未確認10終端job、全workspaceの実pass/fail/skip・doctest・全targets Clippyのexit未確認を保持。拒否flagの再試行・別ログ経路・ゲート改変・source修復・Cargo・GPU・CI再実行・PR・merge・配布・Discord送信・新担当作成なし。

今回の詳細は同 `.ci-work/d7-2488/ci-d7-result-2488.private.json/md` へ保存。前回末尾のmanifest/mdはcheckpoint-20261013-231835-result.private.json/mdに保持。原API/ログ/取得エラー/各receiptの絶対path・bytes・SHA256・根拠行は現manifestのfile_inventory/evidenceに保存。親2488の全修復完了は宣言しない。同ToDo2613の600秒wait-retryで元spec/同worktree/同固定runを保持して自動再開する。

2026-10-14 09:01 JSTに同2613・同worktreeから再開。git dc2cd77/clean、本人所有PRIVATE、PR144 HEAD d7、固定treeを新しい通常gh APIで再照合。前回末尾manifest/mdはresume-20261014-000110-checkpoint.private.json/mdに保持。今回poll051から原APIを追記し、有限確認期限は09:31:14 JST。状態は4workflow成功/4未終端、13/20job終端のまま。拒否flag・別経路ログ・source修復・Cargo・GPU・CI再実行・PR・merge・配布・Discord・子jobの追加なし。

2026-10-14 09:09 JST：Build run38478465876/job115574911494/attempt1がcancelledで終端。通常ghのcheck-run/annotations原APIで「The job has exceeded the maximum execution time of 1h30m0s」を確認した。全workspace nextest工程がcancelled、doctest/全targets Clippyはskipped、後続package-toolsもskipped。これらを成功と扱わない。原全文の通常gh取得は端末制御文字でexit1/0bytes、拒否flagは再試行せず迂回しない。最小確定原因は90分上限超過で、そこに至った個別test/function・原ログ行・実exit・実pass/fail/skipは未確認。annotation start_line1412はAPIの参照であり、原ログ1412行を読んだという主張ではない。

新しい原注記/チェックAPIのpath/bytes/SHA256と取消工程をbuild-cancelled-evidence.private.json、および主manifest/mdへ保存。修復は親2488へ返し、自分はsourceやCIを変更しない。今回のAPIに後続package-toolsのskipped jobが出たので全job数は20から21へ更新し、各HEAD/attempt/run所属を照合する。その他必要workflowの終端確認は当初の09:31 JST期限内で続ける。
