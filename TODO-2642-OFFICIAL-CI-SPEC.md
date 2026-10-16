# GoDD Pressの通常CIを既存の専用検査基盤で通す

親2642の独立した有限検査・実装工程。本人2026-10-15の指示による原GitHubの未完工程を通常PRまで進めるため、予算上限で未起動の通常CIを、既存の専用guest基盤で成立させる。原29/2191の停止/依存/未完を保持する。親の製品PRのmerge/配布/製品全体doneを行わない。

## 最初の一手と依存

GoDD MCP αの通常CI基盤は既存ToDo2737で開発中。Windows2746、Linux2748、RAM sourcepacket2754、peer2753がその元工程である。同じVM/Windows helper/sourcepacket実装や担当を複製しない。2737が未完なら自分を `todo.exe wait <自分の番号> --on 2737` で待機し、同じ基盤準備を再実行しない。親2642を待たない。2737完了後に、そのレビュー・実guest・source固定・通常CIの成果を読み、必要なPress固有の有限admission/workflowだけを追加する。元2648のadvancedCodeQLを再作成しない。

## 固定対象と許可

実repo ID1386702183 / AutoDevJapan/GoDD-Press / PR175、branch fix/todo2642-schedule-predecessor-overflow。開始時の候補 `b2b6d47b7f9e17617d16a9079a70de272bc3c04c` / tree `dac21168dfdb55b007da5c06f36c05e66de0a9f7`、base `4dc5c332d54d19325f122df1f0e4a52e9dd3a629`。owner/private/admin/nonfork/同head+base repo/open/refをAPIで操作前後に確認する。別所有者の環境を変更しない。原GitHubのAGENTS.md/CLAUDE.mdとworkflowの全工程を読む。

原製品folder/名簿/保護一覧/guard/owner-work登録/登録manifestは読まない・変更しない。ホストへ製品checkoutを作らない。拒否されたscope登録の代替を作らない。製品ソースは原GitHubからRAM/所有する隔離guestへ送り、全blob/tree/commitを物理Gitで照合する。既存profile/auth/hostsource/socket/GPUへ到達させない。runner tokenはRAMだけ、no checkout auth、公式ephemeral runnerと単一job・固定repo/run/attempt/SHA受付、期限、取消し、PID0/登録不在/容器またはVM不在/lock解放を維持する。

原CIのruns-onを専用資源へ向ける必要がある場合だけ、同じPR/branchへ通常expected旧SHA/forcefalseで最小変更する。原言語/queries/config/権限/分岐/ゲート/timeout/検査手順を保持し、Hosted予算・契約・権限scopeを増やさない。原CodeQLの成功済み元runを根拠なく再実行しない。最新headを更新したら旧native/CodeQL結果を新headでの成功へ読み替えず、新headの必要な公式結果を実取得する。単にlabelを変えるだけで受入成功にしない。

PC運用helperの新実装はGo、製品の変更はRust。必要なコードは確認済み修復worktreeへ保存する。API要求に製品ソースが含まれる場合はprivateとしてコミットへ混ぜない。稼働版更新は他担当の並行変更を照合し、別モデルMust解消・必要CI成功・通常merge後の正規配布だけを行う。子Codexを直接起動せず、必要な有限reviewは共有ToDoへ--specで登録する。自分のreviewを親2642待ちにして循環させない。

## 既済の実結果と保持すべき失敗

元2739のSonnet固定候補レビューはMust0、原応答は親worktree REVIEW-2642-ORIGINAL.mdに親が保存。CI/alert判定の完了とは別。元2710のApp SHA a39a2b1では旧版RED panic、回帰48PASS、Linux/Windows全workspace/doc/Clippy/fmtと元関連受入が実成功。candidateは.github以外不変だが、その結果をb2で再実行したとは書かない。原ログ完全性・cleanup拒否を保持する。原2710の証拠rootとindexは親の最新noteを読む。未認証70recordの別candidate結果を合格へ混ぜない。

現在candidateの通常CI run38893666794、V2 HTTPS38893667129、Windows38893666823は起動前failure。check116948917330(rust-core)と116948917352(junction-fixtures)の原annotationがActions budget preventing further use。費用増額・同じ未起動runの盲目rerun・手動success statusは不可。

CodeQL official run38893812340はcandidateSHAの4言語+gate全5job success。check116959200210 CodeQL gate、check116955636194 CodeQLはsuccess。旧PRイベントのrun38893666832はcancelledだが、後発の公式dispatch成功と区別する。元2648は各ephemeral回収/PID0/登録不在/lock解放を実確認。これを根拠なく不在として同じCodeQLを繰り返さない。CodeQLの9件は別の有限check担当が判定するので、本担当がalert修正やdismissを行わない。

## 受入と返すもの

元local-checks/workflow全工程、4 integration shard、V2 HTTPS両OS、Windows hidden-fixtures等の発火した原必要jobを省かず公式runで成功させる。元workflow内のprovider/live writesは禁止のまま。稼働非Prd環境を作らず、使い捨て検証guestだけを使う。外部Docker parityやnative受入は元の意味を保ち、shim/mock/手動statusで代用しない。共通cargo_gate/専用target/sccacheと直列条件、割当cargoの範囲を維持する。追加の重い並行buildを独自に起動しない。

必要なguestのCPU/RAMやOSの意味が満たせなければ、不足を記録して実装を進め、未実行を成功としない。結果は原head、実candidate、公式run/job/check、原log全回収のbytes/SHA、失敗原因と修正後再検査、source固定証拠、回収まで区別して親へ返す。通常CI成功まで無制限に新担当/新質問を作らない。親の製品PRはDraftのまま、Must/原9件/最終merge判断を親へ返す。

起動入口でD:/CC/AGENTS.mdと親2642の最新note/specを再読。GPU/AI APIkey/新費用/予算増額/本人PC操作/Terminal/UAC/Discord送信は禁止。管理者操作が必要なら既存brokerを使い窓を出さない。自分の一時資源だけsafe-delete手順で回収。元2710/2648/2730の拒否済みrootを再削除しない。自身のbranchに成果をcommit/pushし、scheduler checkpointをHidden/Wait/PassThruで保存して実終了と原本を確認する。自分や子の完了を待つ親を依存に入れない。
