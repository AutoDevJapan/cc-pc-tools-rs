# 進み具合の記録

共有ToDoの `progress` は担当が確認した0〜100の整数です。稼働時間やジョブの状態から数値を推定しません。古い項目と数字が不明な項目は未記録として扱います。

`todo.exe progress <id> <値>`、`todo.exe add/start/block ... --progress <値>` で記録します。`todo.exe progress <id> --clear` は数字だけを未記録に戻します。`done` は100を記録し、完了済み項目を数字指定なしで再開・停止すると古い100を除きます。

一覧は製品ごとに、短いタイトルの次の行へバーを表示します。数字なしは「進み具合：未記録」、停止理由は横に一言です。

担当向けに次の1行を既存のshared-todo Skillと各エージェントのルールへ追加しました。

- 共有ToDoを更新するときは、確認できた完了条件の達成率（0〜100）を start/block の --progress または todo.exe progress <id> <数値> で記録する。推測で数字を作らず、不明なら未記録のままにする。done は100を記録する。

更新先はD:/CC/Skills/shared-todo/SKILL.md、C:/Users/makhmeto/.claude/CLAUDE.md、.codex/AGENTS.md、.config/opencode/AGENTS.md、.gemini/GEMINI.mdです。Skillの実体とジャンクションは変更していません。
## ToDo2632: PR169のCI確認（2026-10-15T00:26:57.9719860+09:00）

固定source cbf0a7468182c466d5e5286793346c1f2b999c9dの3workflowを照合した。全runと全14登録jobのSHA・attempt1が一致。Buildは全workspace検査中、Readiness9jobとCodeQL4jobは未終端。開始・待機・skipを成功としていない。

原メタデータ、通常gh run view --logの取得終了値1、個別jobのログ取得終了値1と原stdout bytes・SHA256を、このworktreeの .ci-work/todo-2632-cbf/ に非公開保存した。原ログ本文はまだ取得できていない。旧b920/e380の結果を流用していない。

残りは同3runの終端、全実jobの原ログとBuild件数/doc/Clippy、Readiness全9job、CodeQL4言語の同SHA解析/upload/results/errors確認。再開は同ToDo・同固定runで行う。Cargo・CI再実行・dispatch・取消・merge・配布・Discord・本体編集は行わない。親2488と原2512の完了を変更しない。

### 2026-10-15 00:35の保存確認

固定cbfのBuildが成功。原ログでは全workspace2,164成功・5skip・422.761秒、doc16群の実0件、Clippy成功。Readinessは4job成功・5job取消。取消は同PRの後続要求優先と公式注記で確認した。CodeQLは3言語取消・rust未終端で、全件成功ではない。

6実jobの原ログをbytes・SHA256・取得exit・根拠行とともに非公開保存した。PRのheadは進んでいるが、この依頼は元の同3runを保つ。残るCodeQL終端と原結果の確認は、元2632の正規wait-retryから自動再開する。本体編集・再検査・再実行・マージ・配布は行っていない。

### ToDo2632: 903版のCI確認（2026-10-15T00:51:42.7842171+09:00）

親の仕様更新に従い、固定source 903fa283f1970cc629416bff7aa87af63239704aと新3runを別フォルダ .ci-work/todo-2632-903/ に保存した。旧cbf/e380/b920の記録は保持した。

新Buildは原ログ3035行で2,164成功・5skip・417.281秒。docは16群で実0件、Clippy成功。ReadinessはWindows側5jobがAPI上で成功、Linux側4jobとCodeQL4jobは待機中。通常ghの原byteログと取得exitを保存し、未終端を成功にしていない。残る同runの終端・全実job原全文・CodeQL同SHA解析を有限確認する。

### 2026-10-15 00:56の有限確認完了（903版）

指定903の3runは全件終端。Build成功、ReadinessはWindows5job成功・Linux4job取消、CodeQL4言語は全取消。取消8件はstep0で、同PRの後続要求優先を公式注記で確認した。必要CI全成功は成立していない。

実行6jobの原全文・bytes・SHA256・取得exit・根拠行を非公開保存して再照合した。取消8jobの取得exit1/not foundとpackage-tools1skipを分けた。最終結果は .ci-work/todo-2632-903/result.final.private.json などに保持。旧原資料は維持し、親へ不足条件を返した。指定3runは終端済みのため、この版の新wait-retryは不要。親の全作業・新sourceのCI成功・マージ・配布は完了扱いにしない。
