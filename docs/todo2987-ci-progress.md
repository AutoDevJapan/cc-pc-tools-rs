# ToDo 2987 再開記録

対象は非公開の AutoDevJapan/GoDD-Press の既存CIのみ。公開・素材取得・追加課金・GPU利用は禁止。

- GitHub APIで所有者AutoDevJapan、private=true、main `4dc5c332d54d19325f122df1f0e4a52e9dd3a629` を確認。
- 凍結workflow3件と現在のworkflow一覧を照合。通常CIの最終成功はrun 38212791526。10ジョブ合計5308秒（個別ジョブの開始/終了差。請求分数とは区別）。
- rust-coreは1089秒、統合4分割は609/989/1107/768秒。独立ツール、書式/静的検査、Chrome受入が先行条件なしで走る。
- 新規一時取得先と未確認の作業サブディレクトリはguardで未確認/保護扱い。そこでソースは利用しない。拒否資源の複製・別経路参照は行わず、依頼で明示されたGitHub固定workflowと確認済みdocs領域で独立工程を進める。
- 元のソース・資料は保持。ローカルの重いCargoは未実行（資源割当なし）。
- 必須条件: 変更判定の実装と回帰検査、別モデルレビュー、指摘修正、最新候補CI成功、通常merge、効果の実測。請求減は設定だけで断定しない。

次: 補助ツールと本体が読む入力を確認し、軽い検査を先行させる実workflow差分を作る。GitHubへの書込は元private repoの専用branch/PRだけ。公開された運用repoへprivate sourceやworkflow本文をコミットしない。

## 2026-10-19 候補保存

- private repoの作業branch `scheduler/todo-2987-ci` へGitHub Git Data APIで4ファイルをコミット。固定候補 `9f5a18bb182ac57006744bb5d6d194e1485fc919`、PR https://github.com/AutoDevJapan/GoDD-Press/pull/176。
- 確認済み作業資源で作った候補payloadは `docs/todo2987-patch.private.json`。公開運用repoへコミットせず保持。元workflow本文・製品source・素材を公開しない。
- 変更判定6検査、実Git merge/revert/rename/delete/missing-base、rustfmt、actionlint 1.7.12構造検査は成功。
- 変更判定は独立bridge/signing以外の全pathで全件を選択。signingの本体依存を保持。
- 同じrunner/targetで本体lib/CLI/examples/doc/build・統合4分割・Chrome受入を実行。library/Clippy/docはworkspaceを含む。冷起動専用runnerとrelease launcherは維持。
- 別モデル読取reviewはToDo2991（通常scheduler受付）。初回model表記にslashを含めた登録はCLIのモデル名検証で失敗し未登録。その後claude-sonnet-5-5で1件登録済み。
- 初回CI: local-checks 39245437660、CodeQL 39245437659、Windows fixtures 39245437673、V2 HTTPS 39245437702、Windows Chrome 39245437718。受付と成功を混同しない。まだ再実行していない。
- 親2957へ候補・検証済み・未完了を追記。親依存や既存メモを削除しない。

次: ToDo2991の原応答と実モデルを照合して指摘対応。各CIを固定IDで確認し、失敗は該当ログから原因修正する。最新candidate/mainを再照合し、全必要CIと別モデルMust解消後だけ通常merge。実行回数/runner分数/請求増分は成功後に測定。

## 初回CI実行中の追補

- scope、customer-service-bridge、browser-package、browser-cold-startが成功。本体・署名・他workflowは進行中で、完了とは扱わない。
- 現在の通常workflow累計は1030実行（全期間のAPI total_count）。既存cacheは0件/0bytes。新たなtarget cacheは追加しない。
- 正式Billing RESTで対象repoの今月累計を取得し、docs/todo2987-billing-baseline.private.jsonへ保持。先の文章の課金端数には転記誤りがあり、確認済み値として使わない。累計基準と今回の削減額を区別し、金額は原API値で照合する。
- APIクエリのampersandをgh.cmdがcmd区切りとして扱うため、月指定は正規gh api --method GET -f year=2026 -f month=10で取得した。秘密を読まない。
- 最初の候補とは別に、native Chrome証跡の保存stepを統合検査後へ移し、終了時のdisk/inode/target量も記録する差分を作成・actionlint確認済み。docs/todo2987-evidence-fix.private.jsonに保持。これはまだprivate候補branchへコミットしていない。レビュー結果と合わせて必要な修正だけを一括で反映し、全CIの無駄な再起動を避ける。
- 2991は登録済みtodoで起動待ち。直接の子Codex起動や同じreviewの重複登録はしない。

## 2026-10-19 21時台 再開・別モデルレビュー対応

- mainは引き続き4dc5c332、PR176は初回9f5a18bb。原候補の全CIを追加再起動していない。
- レビュー2991の最終実行20261019-210745-todo-2991を受領。4ファイル全文と依存manifestを確認済み。jobと該当session turn_contextの実モデルはSonnet5.5で一致。原応答SHA256 AE32C7458CCB10707CF77844D1BB58474388932C2BA343562EFF133A9ED717A8。旧API失敗3実行は未レビューとして区別。
- Must1は統合後のnative Chrome証跡の保存漏れ。保存stepを最後へ移す準備済み修正を採用。main push groupのSHA分離、終了時disk/inode/target診断、元app4852秒≒81分の記述訂正も採用。
- この修正を固定commit d9caa25386c83d1894c8fb915d87be55ce7b2df5/tree0c168558b943000e2cd28174323f8d4bccb2c7b6へGitHub APIで保存。compareで差分2ファイルだけと確認。branchはまだ9f5a18bb（初回CIを途中取消して全件を重ねないため）。修正commitを先に別モデルレビュー3003へ登録。source選択器/実行検査commandsの変更はない。
- 指摘された空差分の全件化は採らない。同一treeは新しいsource変更がないため、全falseと集約のskip確認で重複検査を避ける。base取得不能は全件、diffエラーは失敗のまま。不採用理由はPRのレビュー記録へ後で1回投稿する。
- 初回CIはCodeQL4言語/gate、Windows fixtures、V2 Linux/Windows、Windows Chrome、scope/bridge/signing/browser/coldが全成功。本体もworkspace lib/doc、CLI/examples/build、native V1/V2、統合1/4まで成功し、統合2/4が実行中。受付や一部成功を全完了とは扱わない。
- 本人のPCで一度の正式CIへ寄せる最新方針を読み直し、独立3002を--needs cargoで登録。Windows側3workflowと専用Rust helperだけを担当し、親の4ファイル/PR176を書き換えない。LinuxのPC隔離供給の実証結果も受領する。未供給labelへの切替、旧固定infraの境界拡大、保護source使用、直接子job起動、無制限runner/cache、新費用は禁止。親2957/2982/2987への循環waitも禁止。

次: review3003の原応答と実モデルを受領。初回run39245437660終了時の実所要量とdiskエラー有無を確認し、必要修正をまとめてbranchをd9caa253へ非force更新する。最新候補の必要CIとレビューを確認し通常merge。3002のPC実job/成果物hash/終了回収を別途統合し、実行量/請求増分はprivate原APIで比較する。自分の重Cargo/GPU/新予算/公開操作は引き続き無し。

## 2026-10-19 22時台 リンク失敗の修復候補

- 本人のWindows完成優先の最新方針を再読。Mac/Linuxデスクトップのための検査は増やさず、GoDD Pressの既存Linux本番向けの必要検査を維持する。3002へ正本と元の実装・供給範囲を再配送。新規自作helperの作成と、拒否された既存製品sourceの使用を混同しない。上位の実拒否を迂回する許可は与えていない。
- 初回39245437660は失敗。統合1/4・2/4までは成功したが、3/4のcompany_opsリンクでSIGBUS/exit101。root2997秒、全7job3506秒。開始時空き12GiB/inode余裕、終了時容量がないため容量枯渇は未確定。未完走値を旧main5308秒との削減成功に扱わない。全ログ/秒数はprivate原本に保持。
- 再レビュー3003は固定d9caa253の2ファイルを読んでMust0、元のnative証跡Must解消。実job20261019-222237-todo-3003とsession turn_contextはSonnet5.5一致。原応答SHA256 48D57AA5DC5D19D89A80712079A2E895B3249468638BDE0FED282F96A4AFD73C。実行検査なし/固定commit自体と前のselector等は未読であることも記録。
- 追加修正commit3ec5c2fbad5463f83f9a5008645b5d5c473f5c36/tree501a36eadb915807cb2af75ab47e2e86e72d9e87を保存し、branch/PR176を非forceで進めて読戻し一致。d9比の差分はworkflow/docs/adapterのJSON出力指定/新規Rust回収処理の4件。全検査範囲・assertion・profile・release launcher・Chrome受入は維持。
- 各分割成功後だけCargoのcompiler-artifact/profile.test/kind=test/executable記録をjqでNUL name/path組にし、Rustで選択分割・exactfilename・ordinary file・target/debug/deps直下・symlink/越境/重複を全件確認後、検査済み実行ファイルだけ回収。共有lib/通常bin/後続Chromeの3fixtureを保持。3→1→2→4の順で既失敗分割を先に確認し、回収byte/容量/inodeと最終診断を残す。
- 新Rust回収処理はWindowsで3検査、実filesystem保持/越境拒否時無作用/不正manifest拒否、実jq投影（bin/未tested/非JSON除外、空白pathとNUL保持）を確認。rustfmt/actionlint成功。最新の正式Linux scopeも選択器6/回収4検査（symlink含む）が全成功。重いローカルCargo/GPUは未実行。
- 追加差分の別モデルレビュー3013を通常登録。3013へ固定4ファイル全文をspec内に渡し、API制限で同じ読取を繰り返さないようにした。source実行や直接子job起動は禁止。
- 新候補の必要CIは1回開始: local39261974120、CodeQL39261974119、Windows fixture39261973834、V2両OS39261973826、Windows Chrome39261973845。scope/bridge/coldは成功、他は進行中。新候補全体の成功・通常mergeはまだ。
- 月累計の課金基準と現取得値はprivate Billing原本へ保存。差は増加であり、対象期間の他実行を分離できていないため、この修正の費用削減としない。金額や私有workflow本文は運用repoへ追加掲載しない。

次: 3013の原応答/実modelを受領しMustを解消。新候補の固定CIが通るまでmergeしない。特に統合3/4のリンク、回収byte、各回収後と最後の容量、後続Chrome、全4分割を確認する。失敗時はログと該当範囲から直し、同一候補の全CIを重ねてrerunしない。3002は3005レビュー後に残る正式PC供給/実job/hash/回収を進める。Linux server検査をWindows製品完成と同一視しない。

## 2026-10-19 22時台 失敗伝搬のMust対応

- 3013の固定3ec5c2fbレビュー4ファイル全文を受領。実job20261019-224040-todo-3013とthread01a15468-b473-76f1-9ce6-e7a8cb05dd18のturn_contextはSonnet5.5一致。原応答SHA256 B27EBDE1D0ECC4FBD74CEB055F7BB0BC05BF1B97CEA032EF70FCBFA93E284775。source reviewのみ、追加実testなし。
- 新MustはパイプでCargo失敗が隠れること。指定なしrunのBashは-eのみ、明示bashは-eo pipefailという公式仕様を確認し、前提の誤りを修正。個別4ステップだけの継ぎ足しではなく、workflow共通defaults.run.shell:bashへ設計をそろえた。scopeの安価なnegative/positive probeで将来も失敗後に回収へ進まないことを確認する。
- 旧3ec5c2fbのlocal39261974120とCodeQL39261974119へ取消を依頼。CodeQLの取消完了を確認。localの終端はAPI照合を継続中（TLS handshake timeoutがあり、受付と終了を区別）。旧候補の緑を最終成功として使わず、重い新CIは今回のMust修正レビューが済んでから開始する。
- 修正75edee9b7204bc3724f31579d2aa40c01648822f/tree9585531f950743878115aa17b563cb3d75521f5dをGitHub API保存し、3ec比4ファイル差分をcompare確認。PR176 branchはまだ3ecのまま。候補本文はdocs/todo2987-pipe-patch.private.json。
- Windowsの実Git BashでRed/Green/正常対照を実行。旧-eはexit0で後続afterを作る。修正-eo pipefailはexit23でafterを作らない。正常対照はexit0でafterを作る。3ケースPASS、CREATE_NO_WINDOWを明示し窓なし。
- jqにobjectsを足し、数値/boolean/配列/null/文字列の行でもprojectionが落ちないことを実fixtureで確認。Cargoのハイフンtarget名とunderscore実行名を正規化して比較し回帰assertを追加、Rust3検査/fmt成功。診断はjson-render-diagnosticsへ変更してartifact JSONを維持。actionlint構造/式も成功。
- NULのname/path組をsort -zuする提案は、組を壊すため採らず、予期しない重複は全削除前に失敗させる。全4分割と後続fixture/元source/ライブラリ/通常binの保護は維持。
- 固定新候補の4ファイル全文を限定review3016へ渡して通常受付。直接子job/新費用/ローカル重Cargo/GPU/保護source使用なし。3002のPC実job/hash/終了回収は別の未完了のまま。

次: 3016を受領してMust解消を確認後、PR branchを75edee9へ非force更新して最新必要CIを1回開始する。取消途中の旧jobは固定run IDで終端を確認する。shared checkpointとprivate runtimeを更新。CI通過後だけ通常mergeし、同一最終sourceの結果を再利用する。初回SIGBUSの原因と容量・回収量、実行回数/課金増分の検証は未完了。
- 追補: 旧local runのjobsとPRのcheck rollupを別々に正規取得し、rust-core completed/cancelled、local-gates completed/failure、他5job successを確認。重い旧実行の終了を確認済み。取消された未適合候補をmergeしない。個別run GETのTLS失敗だけを全API不通や権限拒否とは扱わない。

## 2026-10-19 23時台 最終候補の正式CIを開始

- 再開指示のitem_1/item_8/item_5は保存eventsで完了を照合。既済のbranch更新・CI開始・レビュー受領を再送していない。
- review3016は固定75edee9の4ファイル全文を読んでMust0。実job20261019-225948-todo-3016と該当threadのturn_contextはSonnet5.5一致。原応答SHA256 BFFDE6A3C686803C622243DFC2F448CA43CA84ED2CE1AC73129CBEC9AF1F026B。実testなし、selector未読は元2991で確認済みかつ以後不変として区別。
- 3016のSuggestion1/2とhyphen fixture Nitを提案どおり適用。probeは共通shellを継承しpipefailの実効値を確認。空manifestは削除前に拒否。ハイフンのmanifest→underscore実行物→削除の経路も実fixtureで確認。Windows局所Rust3検査/fmt/actionlint成功。実Git Bashでdefault無しはexit1/default有りはexit0となり、negative後続無し・positive後続有りも確認。
- これら提案の適用をcommit a1dcdfe3348ae0ebf85ea7e47932529ed86f4286/tree108a6987dbe0ed8af9a35f19d8e00930cf582001に保存し、PR176のbranchを非force更新。refとPR実HEADの読戻し一致。mainは4dc5c332のまま。最新版payloadはdocs/todo2987-reviewed-patch.private.json、candidate receiptはdocs/todo2987-reviewed-candidate.private.json。
- 最新必要CIはbranch更新により1回だけ開始。local39267691574/CodeQL39267691152/Windows fixture39267695963/V2両OS39267691182/Windows Chrome39267691220。手動rerunしていない。local scopeの選択6/回収4とBash probeは正式成功。直近観測は本体Clippy実行中、signing test実行中、bridge/browser/coldは成功。全件成功・merge・料金減は未確定。
- PC担当3002の待ち先3005は、原応答が固定60ab2b5cの9資料を読了・Must0で実job succeededのまま、完了行へ括弧を付けたため再開が11回以上記録されていた。親が実job20261019-231444-todo-3005とturn_contextのSonnet5.5一致・原応答を照合し、元review3005のみdoneで受領。原応答SHA256 1686060E85D7BEFFD676E6FF7F87136B31DA951632835C01365DEB77BA15A82D。PC正式受入の完了には拡大しない。
- 3002のwaitが解除されtodoへ戻ったことを実確認。review結果と元の実装許可・書込分離をspec/ノートへ配送。追加の同じ読取は作らず、残る供給/実job/成果物hash/終了回収へ戻す。境界/保護/本人のWindows優先/新費用禁止は保持。競合する最後の再読job232040も既にsucceededで、無関係なprocessを止めていない。

次: 固定a1dcdfe3の5runを照合。特に既失敗の統合3/4、その後の各回収byte/空き容量、残る3分割、後続Chrome/証跡保存を確認する。実害のある失敗を直し、全必要checkと最新main/source一致を確認して通常merge。merge前のPRレビュー記録へ採用/非採用理由を1回まとめる。PC3002と効果測定は未完了で、元2957と継続記録を保つ。自己起動の重Cargo/GPU/新費用/公開/原資料削除は無し。

## 2026-10-20 00時台 最終CIの統合4分割を確認

- 最新の再開item_2はskill読取の完了/exit0で、再実行していない。共有2987はdoing、3002は3018/3019待ち。3019はdone、3018は起動準備失敗の有限再試行todoで、親から重複の担当や重いビルドを起動していない。
- PR176のhead a1dcdfe3348ae0ebf85ea7e47932529ed86f4286/base4dc5c332を読戻し確認。CIで検査する合成merge6f08809cc0fc04f2cfac197cdaa7b2f02bdaab15のtreeは、候補tree108a6987dbe0ed8af9a35f19d8e00930cf582001とAPIで一致。mainは変わっていない。
- 正式local39267691574/root118111717229は、以前SIGBUSだった統合3/4と、その後の1/4・2/4・4/4および各回収stepがすべて成功。workspaceのClippy/lib/doc、bins/examples/build、native V1/V2、release launcherも成功。直近00:24のAPIではauthored buyer Chromeが実行中で、残るcontact/customer/V3 Chrome・証跡upload・最終容量診断・集約は未完了。全CI成功とはしない。完了後のログで回収bytes/各df/最終容量と全受入を照合する。
- CodeQL4言語とgate、Windows fixtures、V2両OS、Windows Chrome2job、local scope/signing/bridge/browser/coldはすべて成功。Cursor Bugbotは利用上限のためNEUTRAL/未実施で、成功に含めず課金枠を変更していない。
- 採用と非採用理由をPRコメント6270816263へ1回投稿。marker todo2987-self-review、既存3botコメントにmarker無しを確認してから送信。再送しない。固定75edee9の3016 Must0と、その提案のa1適用/局所検査/正式scope成功を区別し、全必要CIの終了後に通常mergeすることを明記。
- 最新月次Billingの対象repo項目だけをdocs/todo2987-billing-observation-0024.private.jsonへ保存。Linux43854分/Windows6106分。金額の合算はdecimalで扱い、対象期間中の他実行と未完了CIを含むため削減額とはしない。公式timing APIの0も無料の証拠にしない。最終成功runのjob秒合計を元5308秒と比較してから、実測の範囲で記録する。

次: 15分後に同じroot/runだけ再確認。失敗は終了ログを取得し該当処理を修正する。成功なら全5workflow/集約・最新main・実HEADを照合し、通常merge後にAPI読戻しと必要なmain確認を行う。Windows正式PC供給/4job/成果物hash/終了回収は3002の未完了。元2957への記録と有限再開を維持し、親を3002待ちへ戻すのは独立CI/merge工程が済んでから。

## 2026-10-20 00:47 正式CI成功・PR176を通常マージ

- a1dcdfe3の全必要check（local-gates含む）がSUCCESSで終了。最終rootログ2,055,525bytesを保存し、統合4分割・後続authored/contact/customer/V3 Chrome・両証跡upload・終了容量を実確認。job APIはcompleted/successなのに一部stepがpendingの古い表示を返したため、実終了ログとCheckRunの全体成功を別々に照合した。CursorのNEUTRALは未実施のまま区別。
- PR176はhead一致条件を付け00:46:59JSTに通常merge。main2da7d4af1dd9911eaff970fdfe08b60531732eb6とtree108a6987dbe0ed8af9a35f19d8e00930cf582001を読戻し確認。検査済み候補・合成PR mergeとtree全体が同一。private=true/実所有AutoDevJapanとactive workflow一覧も再確認。Prd機能や配備設定を変えておらず、アプリの配備buildはこのCI変更へ追加しない。
- 正式localは7job合計4651秒（77.52分）、元の成功10job5308秒（88.47分）より657秒/12.38%減。丸めの参考値は81分対93分だが確定課金ではない。rootは69分17秒と90分枠内。runner時間合計の比較で、並列の壁時計待ち時間が同率で短縮したとは言わない。jobs-listの2回のHTTP502後は無限再試行せず、正規CheckRunの開始・終了時刻で測定。原本docs/todo2987-duration-comparison.private.json。
- 回収は3→1→2→4で44/44/43/43件、合計174実行物/15,776,367,080bytes（14.69GiB）。各回収後空き8.3/8.2/8.0/7.8G、最終7.9G、inode12%、target3.4G。検査範囲/プロファイルを縮めていない。初回SIGBUSは当時の終了dfが無くディスク枯渇の確定診断はできず、この成功を過去原因の証明へ拡大しない。
- authored/nativeの数値証跡はartifact12198750738/12199395052。ログのSHA256とAPI digestが一致し、559/599bytesで保存成功。顧客資料や素材の取得・公開は広げていない。
- 対象repoの月次課金net313.667225391USD、開始基準310.987223784との差+2.680001607USD。期間内の他run/試作/Windowsも含むので、この差を修正の削減額へ読み替えない。基準の43行をdecimalで再合算し一致。取得原本はprivateに保持。
- mergeの自動pushで同じソースのlocal39280571145/V239280571286が起動したため、同tree/同toolchain・lock・workflow・試験設定を照合し取消を各1回要求。受付は成功だがAPIはqueuedを返すため終端未確認。取消を成功checkに偽装しない。PRの実成功を参照し重い同内容を重ねない。main CodeQL39280570988はdefault-branch解析公開という文脈が異なるため維持し、定期監査も保持。
- 3002へ正規noteでmergeと新main/source、正式PC4job/hash/回収の未完了を配送。3019done。3018はcb4e505のDACL構造実装/局所検査後もSCM/token/IPC/fence/回収/Cargo接続が未完了。元jobはsucceededの継続報告、元worktreeは正しいbranch/clean。00:15/00:44の再開準備tool_failedは元schedulerが再試行中。直接子job起動や新しい重複担当、依存解除、別worktreeへの置換はしていない。
- 自分の所有temp f97a987aeae3a2f8545c8ed51a360a32は不要な未利用cloneとvalidator。safe-delete planがPROTECTED_PATH、指定どおりquarantine --reason policyもPROTECTED_PATH。通常のhelper拒否であり自動承認審査の拒否とは扱わない。原データを保持し、別shell/手動移動・削除/guard変更で回避していない。実未完了の後片付けとしてprivate結果に保持。

次: main CodeQLと取消受付済み2runの終端を有限に確認する。PC3002/既存3018の正規再開を維持し、必要な残工程が他ToDoだけになったら2987をwait --on 3002で登録。原tempのcanonical保護判定による未回収も保持し、削除できたとしない。親2957と3002へmerge/実測/未完了を配送済み。PRコメント6270816263は再送不要。
