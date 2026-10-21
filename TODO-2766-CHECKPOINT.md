# GoDD Press 通常CIの再開記録（ToDo2766）
## 2026-10-21 job144040：期限の必須指摘を修正し、Linuxの受付も実行した

この節が最新。通常CIは未成功、元2766は未完了。親2642の製品PRのmerge/配布/全製品doneは担当しない。本人判断は不要。

- 実job20261021-144040-todo-2766。同branchでf1c3c265/cleanから再開し、D:/CC/AGENTS.md、最新owner-agent-conduct、元2642の最新note/spec、作業AGENTSと関連Skillを読んだ。原仕様のDiscord直接送信禁止を保ち、全終了結果は親へ返してDiscordへの配送・読み返しを依頼する。配送未確認を成功と書かない。
- review3272（job20261021-140954-todo-3272）はsucceeded/exit0、対応する実turn_context anthropic/claude-sonnet-5-5/max、最終task_completeと原last_message一致。原応答SHA3EA40E578EE9D3AF38BA622C2E95F7DCDC11F66AB906EB2941B284BE77D0A9FFをREVIEW-2766-ADMISSION-3272.mdへ保存。原bytesはprivate evidenceにも保存し、Gitの改行正規化と区別する。元68ac605の指定32fileレビューでMust1/Suggestion3/Nit2。未読secure_files*/source_materialization/normal_ci_admission本体などは未読のまま。
- MustはLinux socket40分/listener45分、Windows host/channel26分が元90/360分jobを切る不具合。7d6f73337e32a9d0d7e9f0e0b43dff374039461c/tree7b0d93f8afeaf5433b9ab5d2f745165e0c9d82b8で修正し、push/remote一致。Linuxは受理済みplanのrole+10分でsocket設定、boot時には該当workflowの閉じたrole最大+15分のlistener寿命、Press受理時にrole+15分へtimer更新。Windowsはseed.Planからrole+6分でhost context/socket/service channelを設定。元αの40/45/26分は維持。元workflowのtimeout自体は変更0。
- Suggestion1は実PR175の元3runを読取GET。全てevent=pull_request、素のworkflow path、head_sha=b2候補。全13jobもhead_sha候補/runner0。二次要約と違って現policyの期待形に一致するため、run API headをmergeへ変更しない。hook/物理Gitのmerge pinは別に保持。新runは実操作時に再照合する。
- Suggestion2はPressの返却JobNameをcompiled policy.Nameへも固定。全13roleとWindows両roleで名前改変の拒否を追加。αは元の非空ルールを保持。Suggestion3は全正負admission fixtureを13role全部へ展開し、4shardの別labelを実際に交換した拒否を追加。旧attempt違いの拒否も保持。
- Nitはsource2fileをgofmt、Linuxの観測用probeをPressのworkspace/Rust1.95.0へ選択。αは1.97.1。probeはcompiler全体のprovenanceでも受入成功でもない。

### 今回の実検査・回収

- 元host.goだけのGo overlay＋新テストは、Windows両roleの実HostSessionで「26分、必要96分」を検出してexit1。新sourceの全Windows runner race11.035s/exit0。mTLS実通信のservice channelもαとPress両roleのcontext deadlineを検査した。
- guest全module Windows race18.888s/exit0、最終13roleの期限/JobName focused race1.274s/exit0。product-ci pressci全race4.603s/exit0。3module vet exit0、guest最終vetもexit0。gofmtとgit diff --check成功。
- 最終sourceからGOOS linux/amd64のtest binaryを生成し、既存source-free SDK image89988fa...の新しい自分の隔離containerで実行。CPU1/RAM512MiB/network none/read-only/cap-drop ALL/no-new-privileges、GPU/host bind/socketなし。新11 Linux roleをserveLiveJobの実frame受理へ通し、不正な公開source fixtureで物理source処理前に止める。全11roleの実socket期限が元timeout+10分でPASS/exit0。製品のsource物理照合、VM、runner登録、元Rust、公式CIとは別の単体検査。
- container27c7a3885c2dcffe6fdd6a1c9b33137242ce68fe134d8bc9a115cb55430c3a86は所有name/labels/image、停止/PID0/exit0を照合してrm -vし、全IDの不在を確認。新image作成0、既存SDK imageは保持。新VM/runner/Chrome/Cargo/GPU/Civ6/AI APIキー/新費用/本人PC操作/Discord送信は0。
- 原log/実exit/API metadata/原reviewを47recordの全byte/base64・bytes/SHAでevidence-2766-deadlines-144040.private.jsonへ保存、全読戻し一致。bundle46,393bytes/SHA9362A928C43C4421D3FA4ABECE82175F33C038E8529743E2B2765195D1B5B6DA。旧host overlayのexit1は期待RED。最初のLinux helper compileはtimer最終修正前、最終Linux test compile/実行は最終source。どのdirty binaryもlive runnerへ使用していない。
- temp0c87b9451bed11d3e68926659183b624は全記録保存後にsafe-delete plan指定の通常削除を別呼出しで実行しfinish(source_absent)。20,933,941bytes/48entries。空き容量の前後差は未測定。旧2710/2648/2730の拒否root、2737/2783のSDK/保留物、他担当は変更/削除しない。

### 共有ToDoの容量上限と保存の修復

review3272と親2642への結果CAS保存は成功/各marker1件を読戻し確認。その後、自分2766へのcheckpoint全文のCAS保存だけが `todo_store_too_large: shared ToDo exceeds 67108864 bytes (64 MiB)` でexit1となった。直後の原台帳は67,080,256bytesで、更新後の本文・履歴が上限を超える条件だった。自動承認の拒否ではない。全原資料と本文は作業branchへcommit/push済みのまま保持し、2766のnoteを記録参照と次の手の短い日本語にして正規CLIで1回保存し直すとexit0/全文一致。その後の正規wait2766 --on3299も成功し、原台帳のstatus=wait/wait_on=[3299]、親2642のwait_on=[2765,2766]を読戻し確認。台帳の直接書換え、上限変更、他担当や既存履歴の削除、同じ失敗の反復はしていない。原stderrと各note配送receiptはこのworktreeのignored `note-*-144040.*.private.json` / `notes-144040-readback.private.json` に保持する。本文・原履歴を失わず、自分の参照だけを短くした。
### 再レビュー3299と次の一手

修正の固定f1c→7d6/tree7b0を、元3272のMust修正・付随差分だけの有限読取3299へ通常add --spec（claude-sonnet-5-5/max）で登録。TODO-2766-DEADLINE-REVIEW-SPEC.mdと保存specの全文一致/dispatchableを確認。受付時はjobs空で、再レビュー成功ではない。子Codexを直接起動していない。

1. 正規wait2766 --on3299だけを記録する。親2642や既済2737/3272を依存に入れない。再開時は3299の実job/対応turn_context/原応答/読了と未読/Mustを照合する。
2. 元product repo1386702183/owner/private/admin/nonfork/PR175 open/Draft/同repo/ref/head b2/base4dc/実merge f3aは今回もpostflight不変。製品へのmutation0、CodeQL再実行0、元3通常runの起動前budget failureを保持。b2 CodeQL成功を新headに読み替えない。
3. retained Windows SDKは公式Server2025（SDK source/保持receiptで確認）とRust1.97.1。元workflowはWindows2022、全Rust指定は1.95.0、Node24.15.0を含む。この差を確認せず同等の準備済みとはしない。実guestでOS意味とtool選択を検証し、不足を新しい使い捨てguestだけに補う。既存SDK/golden/hostの変更、別のVM基盤・Windows helper/sourcepacket実装の複製はしない。Linuxにはrustup proxyと1.97.1があるが1.95.0の実導入/選択は未確認。Windowsはportable1.97.1の直PATHと空rustup-homeの構成なのでRUSTUP_TOOLCHAIN=1.95.0だけでは選択の証拠にならない。
4. 元main側local-checksは既に別変更済み。元b2のruns-onだけの変更がPRの実mergeに競合せず成立するかは未確認。元event/4shard/全step/ゲート/timeout/provider-live-write禁止を保って公開方法を詰める。新headが生じれば必要な公式native/CodeQL結果を実取得する。手動successや元b2 CodeQLの根拠ない再実行、予算/契約増加はしない。
5. レビュー済みclean同sourceから必要なhost/Windows/Linux helperを生成し、既存共通cargo_gate/専用target/sccache/直列条件で元13roleと発火した必要jobを実検査する。完全原logのbytes/SHA、全blob/tree/commit物理Git、元head/実candidate/公式run-job-check、失敗原因と修正後再検査、取消し/PID0/登録・VM・容器不在/lock解放を親へ返す。親PRはDraftのまま、Must/9件/最終mergeは親の工程。

最終記録とspecをこのbranchへcommit/pushし、実job/branch/実HEADのscheduler checkpointをHidden/Wait/PassThruで保存し原台帳と照合する。親2642へこの区切りの結果・残作業・次の手・本人判断不要を1回CAS記録し、最新方針による親経由の既存Pressスレッドへの結果配送/読み返しを依頼する。Discordへの直接送信は禁止のまま。


## 2026-10-21 job113146: 受付を接続し、実RAM source照合と追加レビューへ進んだ

この節が最新。元2766の通常CIは未成功で、親2642のmerge/配布/製品全体doneは本担当では行わない。本人判断は不要。

- 今回は実job20261021-113146-todo-2766、同branch scheduler/todo-2766-godd-press-ci-2642。再開時は1784c0d/clean。起動入口の運用指示・AGENTS・元2642のnote/spec・最新owner-agent-conductを読み直した。新方針は全終了結果を親が永続記録してDiscordへ記録する義務を含む。この担当の原仕様はDiscord直接送信禁止なので、親への結果と配送依頼を残す。
- 3246の実reviewを受領: job20261021-110245-todo-3246/succeeded/exit0、対応turn_context anthropic/claude-sonnet-5-5/max。原応答SHA B2F518173E58730DA45EA16D40FD9DAD95FE56BD7922232C8EE42A1BD9026B69、task_completeとlast_message一致。旧8aの指定12全文でMust0/Suggestion3/Nit3。原応答をREVIEW-2766-PREPARATION-3246.mdへbyte保存、元3246のnoteへ照合と処置を通常CAS editで返した。未読secure_files*/source_materialization/normal_ci_admission本体は未読のまま。新差分をレビュー済みとはしない。
- 新実装68ac605c6520262d60b7ff26d0467f0717c0fd70/tree72cc7c66b29c1ff22a3905a90bc01e6e690cdd38をcommit/pushし、remote ref一致。32file、1649追加/82削除。sourcepacket/pressci/既存Linux・Windows controller/既存Windows backendを接続し、VM/SCM/SDKを複製していない。旧MCP αの空profileと役割・label・timeout・回収を保持。
- Pressの候補と実mergeを別のpinへ固定し、元13roleのmatrix/run/attempt/runner/OS/timeoutをAPIとhookで照合。候補treeは元b2の3 route blob以外不変を要求。PrepareRoutesは元workflowのruns-on scalar以外をbyte不変にする。local-checksは元dispatch、残る2workflowは元PR event。events/checkout/permission/step/gateは変更しない。nonce変数とrole/run/attempt labelを要求。API mutation/route公開/dispatchの実装や実操作はこの差分に無い。
- Windowsのworker権限/SYSTEM private proof/stock token・hookを保ち、checkout owner path1箇所だけPressへ選択。新Windows2roleは元90分、Linuxも元の各timeoutを保持し、外側の有限時間だけ必要な余裕を持たせた。共有cargo_gate/専用target/上限/intent/lock・VM・登録回収は共用。

### 実検査と保持する失敗

- source全module Windows race初回72.908sで、新しい公開fixtureのGit object書換えだけReadOnlyにより失敗。fixtureだけchmodしfocused race4.176s/exit0。残りの初回有効検査は保持。修正版のmerge/signature focused race4.172s/exit0。元verify.goだけのGo overlayでは新merge親pinの回帰が実FAIL/exit1。commit改変negative controlは旧版でも拒否できたので、それをREDとはしない。
- real RAM Fetch初回はcommit段階failure。旧記録はAPI/payloadを細分しておらず、初回の単独原因を捏造しない。別途、実APIの署名付きpayloadでsignature末尾LF保持だけが実OIDに一致し、元の末尾LF除去方式は不一致を確認。timezoneは+0000で旧対応範囲だった。公開の同形式fixtureは旧originalPayload関数だけのoverlayで実FAIL/exit1、修正版GREEN。新診断はcommit_api固定分類/identity/payloadを区別する。
- 追加のAPI診断1回はTLS timeout後に補助がnullから値を作りかけたため、元receiptを無効として保存。ErrorActionPreference=Stop/実exit/同identity確認後、有限の読取1回で実SHA一致を確認した。無効なfalse値を判定に使わない。overlay作成の初回補助も結果が無かったため実検査とはせず、分離した作成と実Go起動でREDを回収した。
- 修正後の実RAM Fetch/検証はexit0。物理実行候補merge f3a704132f286d58eee2c16b819bc85cd4565c37/tree a0c5944f8fc69b78db78afd525d2eab104fe3319、PR候補b2、main2da7d4af1dd9911eaff970fdfe08b60531732eb6を区別。archive49,306,964bytes/SHA0326e36cc480c5fc6f29ac32f6c386a52eecbf249b03f8879ce7702de78a8a73、2812 entries/2315 blobs。host checkout無し。初版の広いArchiveGitObjectsVerified名を実際のArchiveBlobIDsVerified/CommitIDVerifiedへ訂正。物理tree/guest/runner/公式successは全て未確認/false。
- product-ci全module raceはmain1.320s/normalci1.037s/pressci1.435s exit0。新13role、全非route byte不変、元契約・candidate/merge・owner/ref/nonce/workflow/run/attempt/runner/partial拒否を合成APIで検査。最新版pressci focused exit0。
- 利用側ci-guest全module race14.528s/exit0、新role focused1.038s/exit0、timeout focused exit0。Windows runner全module race8.771s/exit0、新2role/ACL差分1箇所 focused1.029s/exit0。source/product-ci/guest/runner全module vet exit0、最終product vetもexit0。
- Linux helperはGOOS=linux/amd64 compile-only成功。新差分の実Linux/guest実行やRust検査ではない。dirty版のcompile確認物はliveに使わず回収した。git diff --check成功。
- 75原recordを同worktreeの無視対象evidence-2766-admission-20261021.private.jsonへ全byte/base64とbytes/SHAで保存し、全読戻し一致。bundle SHA F9343613CE2BF5DDA91AC41B7904A19698F7BE0A1FF11D42279ECC9FCD0C2476。無効な診断値/REDでなかったcontrol/未確認treeの解釈も同bundleに明記。

### 追加レビュー3272と再開の一手

固定base1784c0d→head68ac605/tree72cc7c6の32全文/局所を3272へ通常add --spec、claude-sonnet-5-5/maxで登録。保存spec全文一致、dispatchableを確認。指示はTODO-2766-ADMISSION-REVIEW-SPEC.md。登録時はtodo/jobs空であり、レビュー成功ではない。検査時間の初期転記2.998sを原stdoutの4.176sへ訂正し、未起動の保存specも更新/全文一致を確認。子Codexは直接起動していない。

1. 正規wait2766 --on3272でこのレビューだけを待つ。親2642や既済2737/3246を依存に入れない。実job/対応turn_context/固定版/全読了と未読/原応答を照合し、実Mustを直す。
2. その後に原head/ref/base/実merge/全workflowを再読して、route公開方法を確定する。現在mainはlocal-checksを変更済み（blob b6e9216...）。元10job構成との3-way mergeが競合せず成立することは未確認。元event/4shard/ゲートを勝手に削らず、旧sourceの成功を実mergeへ読み替えない。製品headはまだb2で不変。
3. 実guestでSDKのOS/CPU/RAM、Chrome/Node、RustupとRust1.95の選択が原workflowの意味を満たすかを確認し、不足を新しい使い捨てguestだけへ追加する。原SDKはRust1.97.1/MSVC等の実測済み資産だが、それだけでPress用toolchain/Chromeの準備済みとはしない。既存SDK/golden/ホスト/他担当を変更しない。
4. レビュー済み同clean sourceからhost/Windows agent/Linux helperを必要分だけ生成し、VCS/clean/OS/実SHAを揃える。今回のdirty compile物は回収済みで再利用しない。元API/head/tree/原ガイダンス/全workflow/必須job/元timeout、共有Cargo gate・専用target・sccache・直列条件を維持。
5. 既存の同PR/branchだけへexpected旧SHA/forcefalseの必要最小変更を行う。新製品headが生じたら必要なCodeQL/native公式結果を新headで実取得する。b2の成功済みCodeQL38893812340を根拠なく再実行せず、元3通常runのbudget failureを保持。手動success/予算増額/新費用/alert修正dismissは行わない。元9件は2765の範囲。
6. 原公式全jobと完全logのbytes/SHA、source固定、失敗と修正後確認、期限/取消し/PID0/VM・登録不在/lock解放まで実測して親2642へ返す。通常CI成功、親製品merge/配布、原29/2191/製品全体の完了はまだ無い。

### 後片付け・親への報告・再開保存

自分の新temp47fb3a1f0120bb045917e6c250e7694fは原記録退避/全hash一致後、safe-delete plan指定の通常削除を別呼出しで実行しfinish(source_absent)。9,968,357bytes/79entries。空き容量の前後差は未測定。原2710/2648/2730/2737の保留物・SDK・旧失敗・他担当は不触。新VM/runner/container/ブラウザ/実Cargo/GPU/Civ6/AI APIキー/費用/本人PC操作/Discord送信は無い。

postflight-2766-113146.private.jsonで実repo1386702183/AutoDevJapan/private/admin/nonfork、PR175 open/Draft/同repo/ref、headb2/base4dc/mergef3aが不変と確認。parent2642へこの区切りの結果・未完・次の手・本人判断不要と、最新方針によるDiscord結果記録の配送依頼を通常noteに1回記録する。直接Discord禁止を迂回せず、配送を未確認のまま成功とはしない。

自分のbranchへこの記録とspecをcommit/pushし、実job/branch/最終HEADのcheckpointをHidden/Wait/PassThru保存・原台帳読戻しする。結果はcheckpoint-2766-113146-*.private.json。状態は正規wait以外schedulerへ任せ、推測progressを付けない。

## 2026-10-21 再開: 2737完了を受領し、Pressの準備差分を保存

この節が最新。通常CIはまだ未実行・未成功で、元2766は未完了。本人判断は不要。

- 今回のjobは20261021-094646-todo-2766。元ToDo2766はdoing、jobsとscheduler.jobに同jobが正規に紐付いている。前回のprepared/jobs空というcheckpoint拒否条件は今回の原記録では解消。台帳を直接変更していない。
- 2737はdone。元の最終7b8dbb6/checkpointと履歴noteを読み、実Windows13工程、最終Gate、同sourceの公式run39201344542 attempt9 success、レビュー3230のMust0、sourceproof/VM/runner/route回収を受領。これはPressのCI成功ではない。
- 2737の既存5 moduleとその履歴を自分のbranchへmergeして再利用した。新しいVM/SDK/sourcepacket実装や実行担当を複製していない。merge commit fe19223。READMEの競合マーカーを取り除いたf01265dの後に自分の差分を追加。
- 準備差分8a686793f397b1fa0d473e4b673332990228621d/tree55f79020fd1a22dcd9188df9a46d7e18894f06e9をcommit/push、remote ref一致を確認した。Product=press-2766と3 workflowの明示pinだけを既存sourcepacketへ追加。空profileのMCP αを保ち、元repo/owner/private/admin/nonfork/Draft PR175/元ref/baseを検査する。物理Git照合は共用。guest側のPress admissionはまだ接続していない。
- `press-ci-outline`は読取専用。元3workflowの完全Git blob/hashと、元PR/head/base/ref/treeを前後照合し、local-checks10 + V2 HTTPS2 + hidden1の13 jobを返す。4 integration shardと両OSを保持する。source命令やworkflow bytesをホストへ保存しない。出力のRunnerAdmission/OfficialCISuccessはfalse。
- 実APIで製品repo1386702183/AutoDevJapan/private/admin/nonfork、PR175 open/Draft/同head+base repo/ref、元b2 head/4dc baseを再照合した。原AGENTS全文と全workflowをRAM読取し、CLAUDE.mdは404。修復repo1406818713は現在public/admin/nonfork。製品sourceやAPI書込要求を修復repoへ混ぜていない。

### 検査と初回失敗

- sourcepacket全moduleのWindows raceは57.438sでパッケージok。追加3workflowの公開合成Gitを実Materialize/VerifyCheckoutし、正確なhead/tree/all blobs、改変blob拒否、別workflow pin拒否を確認。直接起動した最初のGo processは終了コード取得がnullだった事実を保持し、exit0を作らない。完全stdoutのokと実終了を確認済み。source vetは別プロセスでexit0。
- product-ci全module raceはmain1.622s/normalci1.066s/pressci1.066s、wrapperの実exit0。最初のvetはmerge後の型aliasに古いunkeyed literal9箇所が残り失敗。main/main_test/merged_jobの値を変えずkeyedへ修正しvet実exit0。型記法だけの変更で同じ成功検査を重複しない。
- 利用側ci-guest race14.360s/exit0、Windows runner race7.483s/exit0。どれも元製品Rustやguestの実受入ではない。
- 実APIのpress-ci-outlineはexit0、3元Git blobと13 job、dispatch true/false/false、SourceRevalidated=true、RunnerAdmission=false、OfficialCISuccess=false。git diff --check成功。
- 完全な原stdout/stderr/実exit/元workflow digest/merge観測/pushを、同worktreeの無視対象evidence-2766-20261021.private.jsonへ28recordとしてbyte/base64保存し、全bytes/SHAを読み返し一致。bundle SHA256 B648C581FEE067D56F98927C87A27CC2AF9153414811E4F3ACA2C5246EBCAAE5。元原ログ/旧cleanup拒否は変更していない。

### レビュー3246と次の手順

固定base f01265d40618dc45a0e9747a60c5ef4ecc62b61d → head8a686793の12file全文/局所を、別モデルの読取レビュー3246へ通常add --specで登録した。指示はTODO-2766-PREPARATION-REVIEW-SPEC.md。model claude-sonnet-5-5/effort max、dispatchable true、保存spec全文一致を確認。確認時はtodo/jobs空で、まだMust0の受領ではない。最初の`anthropic/`を含む--modelはCLIのモデル名文字規則でValueError・未登録だった。slash無しの正規aliasはschedulerがanthropic/へ正規化する実装を確認して登録した。承認審査の拒否や迂回ではない。

1. 正規wait2766 --on3246でレビューだけを待つ。親2642やdone2737を依存に入れない。3246の実job/対応turn_context/固定base/head/tree/全読了/未読を照合し、原応答を保存してMustを直す。子Codexを直接起動しない。
2. その後、既存guest controller/Windows backendへPress固有の有限admissionと元workflowの最小routeだけを接続する。現13 jobを一つも省かない。元2workflowはPR専用でdispatchが無い。原event/checkoutを保つなら、公式PR merge refの実sourceを候補と区別して物理固定する必要がある。元dispatch専用MCP bindingやlabel変更だけで受付成功にしない。
3. 今回の追加観測: PR API baseは元4dcのままだが、実heads/mainは2da7d4af1dd9911eaff970fdfe08b60531732eb6。refs/pull/175/mergeとPR merge_commit_shaはf3a704132f286d58eee2c16b819bc85cd4565c37、parentsは2da7d4aとb2、tree a0c5944f8fc69b78db78afd525d2eab104fe3319で元candidate treeと異なる。元4dc→mainはahead6/file5。これを同sourceへ読み替えない。outlineは元固定候補の読取結果であり、最新mainとの統合検査を保証しない。次の実行前に元head、現在base ref、実merge commit/treeとworkflowの全bytesを再照合し、実run/job/attempt/SHAの受入条件を追加する。無断でfeature sourceや親branchを更新しない。
4. 既存SDKがPressのChrome/Node/Rust1.95と必要なOS/CPU/RAM/元timeoutを満たすか、source-freeの既存仕様を確認する。足りないものは新しい使い捨てguestだけへ有限に追加し、原SDK/host/GPU/profile/auth/socketに広げない。既存cargo_gate/専用target/sccache/直列条件を保つ。
5. route/head更新が必要なら元PR/branchへexpected旧SHA/forcefalseの最小変更だけを行う。その新headで必要な公式CodeQL/native結果を実取得する。元b2 CodeQL5job成功はそのまま保持し、head不変の間に根拠なく再実行しない。元9件のalert判定は2765の範囲。
6. 通常CIの全必要公式job、原ログbytes/SHA、source固定、失敗原因と修正後再検査、取消し/回収/lock解放を区別して親へ返す。親PRのmerge/配布/全製品doneは本工程では行わない。

### 回収と再開保存

新safe-delete temp85bf6d016d4f513bbdda56ebeb6fc35bは、終了した自分のGo検査だけに使用。原記録を上記bundleへ保存/照合後、planの通常削除を別呼出しで実行しfinish(source_absent)を確認。6305bytes/29entries。空き容量の前後差は測っていない。新しいVM/runner/container/ブラウザ/GPU/Civ6/Cargo/Discord/本人PC操作/新費用は今回なし。旧2710/2648/2730/2737や他担当の未回収は触っていない。

この再開文書とレビュー指示を自分のbranchへcommit/pushし、実job/branch/実HEADでcheckpointをHidden/Wait/PassThru保存する。要求・実exit・原todo.scheduler.workspace_checkpointの読戻しはcheckpoint-2766-20261021-*.private.jsonに保持する。共有ToDoの状態は正規waitを除きschedulerに任せ、noteだけ通常editでこの再開情報を残す。確認できない進捗率を作らない。

2026-10-17 04:51 JST。親2642の独立した有限check。通常CIは未実行・未成功。

## 今回確認したこと

- 起動入口の `D:/CC/AGENTS.md`、本worktreeの `AGENTS.md`、共有ToDoと日本語報告のSkillを読んだ。本worktree直下にCLAUDE.mdはない。
- 親2642の最新ToDo本文、`TODO-2642-CHECKPOINT.md`、`TODO-2642-OFFICIAL-CI-SPEC.md` を読んだ。原資料は変更していない。
- 共有ToDoの原記録で2737はwait、未完依存は2746。親の最新記録では2748/2753/2754は既済だが、2737自身の基盤統合・公式CI完了を意味しない。
- 本仕様の最初の条件は「2737が未完なら2766を2737の完了待ちにする」。同じVM、Windows補助、sourcepacket実装、レビュー担当を追加しない。
- 親2642は2766の結果を待っている。2766の依存先に2642を入れない。

## 今回の区切り

この記録だけを自分の作業ブランチ `scheduler/todo-2766-godd-press-ci-2642` へcommit/pushする。共有ToDo2766には正規 `todo.exe wait 2766 --on 2737` を記録する。進捗は推測して付けない。scheduler checkpointは実job・実branch・実HEADで保存し、終了コードと原記録の一致を確認する。実行結果は同worktreeの無視対象private JSONへ保存する。

製品ソース・workflow・PR・runnerへ今回の変更はない。Cargo/GPU/Civ6/VM/container/ブラウザ/Discord送信/本人PC操作は行っていない。新しい一時フォルダを作っておらず、回収する一時資源はない。古い回収拒否資源も触っていない。

## 保存結果と再開情報の照合不足

最初の記録をcommit `a2b6d908b19b82028e894fc069b2ba2871ba6686` としてpushし、リモートrefと実HEADの一致を確認した。修復リポジトリは実APIでAutoDevJapan/private/admin/nonfork、ID1406818713を確認した。独立PRの作成やmergeはしていない。

2026-10-17 04:52 JST、実job `20261017-044530-todo-2766` と同じ実branch/HEADの要求で、指定のHidden/Wait/PassThruによりcheckpointを1回実行した。実プロセスは終了済み、exit1、`workspace checkpoint failed InputRejected`。原ToDoのcheckpointはnullであり、保存成功とはしない。これはschedulerの入力・所有照合による拒否で、自動承認レビューの拒否ではない。

有限の読取調査で、原job.jsonはrunning/todo_id2766/cwd一致だが、原ToDo2766はtodo、scheduler.phase=prepared、scheduler.job=null、jobs=[]だった。保存branchと実branchは一致する。`cc-scheduler/src/workspace_checkpoint.rs` のsource選択は当該jobが原ToDoのjobsにあることを要求するため、この欠落で拒否される。詳細な要求・exit・原記録抜粋は本worktreeの `checkpoint-2766-*.private.json` に保存した。

ジョブ紐付けの偽装、原台帳の直接書換え、startによる自己紐付け、ブランチ切替、同一失敗の再試行はしない。変更したのは自分の再開文書だけで、branch自体は切り替えていない。正規waitで2737依存を保持し、次の正規再開時にジョブ紐付けとworkspace checkpointを再照合する。本人判断は不要。元ToDoのnoteにもこの不足と次の処置を残す。

## 2737完了後の再開

1. 起動入口の運用指示、親2642の最新note/spec、2737の完了記録・レビュー・実guest・source固定・通常CI結果を読む。原GitHubのAGENTS.md/CLAUDE.mdとworkflow全工程は製品操作前に読む（今回は未取得）。
2. 実GitHub APIでrepo ID1386702183 / AutoDevJapan / private / admin / nonfork、PR175のopen/draft、同head+base repo、refを照合する。保存候補が進んでいれば、旧結果を新候補へ流用せず範囲を照合する。
3. 保存対象はbranch `fix/todo2642-schedule-predecessor-overflow`、head `b2b6d47b7f9e17617d16a9079a70de272bc3c04c`、tree `dac21168dfdb55b007da5c06f36c05e66de0a9f7`、base `4dc5c332d54d19325f122df1f0e4a52e9dd3a629`。これらは親からの引継ぎ値であり、本担当が今回API再取得した値ではない。
4. 必要なPress固有の有限admission/workflowだけを既存基盤へ追加する。原工程・4 integration shard・V2 HTTPS両OS・Windows hidden-fixturesを省かず、固定sourceと単一jobの公式結果、原ログbytes/SHA、取消しと回収まで親へ返す。
5. 元CodeQL公式run38893812340の5job成功と、通常CI runs38893666794/38893667129/38893666823のActions予算による起動前失敗を区別する。成功済みCodeQLの再実行や失敗runの盲目再試行、手動successはしない。headを更新した場合の必要な新結果は別途実取得する。
6. 原2710のa39a2b1 native成功をb2候補での再実行へ読み替えない。9件の実害判定は別担当2765の範囲。親の最新結果を受領し、alert修正/dismissへ範囲を広げない。

製品checkoutはホストへ作らず、製品sourceはRAM/所有する隔離guestだけで扱う。原製品folder/名簿/保護一覧/guard/登録manifestを読まない・変更しない。既存profile/auth/hostsource/socket/GPUへの到達を許可しない。APIキーAI・追加費用・Hosted予算/契約/scope増加は禁止のまま。親PRのDraft解除・merge・配布・製品全体done、原29/2191の停止/依存/未完の変更は本担当の範囲に含まれない。
