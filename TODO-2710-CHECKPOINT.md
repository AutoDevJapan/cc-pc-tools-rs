# ToDo2710 固定3ソースの実検査

2026-10-16。共有スケジューラのcargo割当を受けた有限の独立検査。主担当2648のsource・PR174・配布済みruntimeは変更しない。製品sourceはGitHub APIから新規隔離containerにのみ取得し、このCCブランチへcommitしない。

## 再開情報

- branch: scheduler/todo-2710-3-rust-lua-ci
- job: 20261016-154928-todo-2710
- 所有root: C:/Users/makhmeto/AppData/Local/Temp/codex-owned-06d9fa30f2a60e4dbf33ddcefa715618
- 所有ID: 06d9fa30f2a60e4dbf33ddcefa715618
- helper source: tools/ci-check-2710（Go。既存product-ciのidentity/archive処理を独立して再利用）
- SDK image: sha256:89988fa8449f89b0d8f12fd2369aa5b167bfe3389189df877808a15055869011
- sccache SDK: upstream v0.18.0 asset563130201。配布APIと実bytes SHA256を照合。SDK原imageは変更しない。

## 確認したこと

- 3repoのprivate/admin/許可owner/nonfork/同head+base repo/current branch SHAをGitHub APIで照合。
- repo1392152644のarchiveを新規containerだけへ取得し、git write-treeがGitHubの固定commit treeと一致。
- 専用containerはinit/nonroot/capdropALL/no-new-privileges/read-only image/noGPU/host source・profile・authfiles・Docker socket無し。
- Windows共通cargo_gate.exeを使用。repo別target識別子、Windowsbyte lock、共有CPU/RAM admissionと実行中の並列上限を保持。
- Linux supervisorは正常終了とEOF中止を実行し、kernel group不在と直接の子のreapを確認。中止ではgateをkillせず、stdin EOFで回収完了を待つ。
- 最初の中止fixtureはCRLFのshell planで失敗。transfer時に改行を正規化し、再実行でexit125と回収を確認した。元logを保持。
- 原workflowのRust1.95.0を新規scratchへ導入し、repo1392152644の元alltargets、全nextest、doctest、release bins、scenario、native/live検査を実行中。Windows受入とは区別する。

## 残ること

1. repo1392152644の実結果を回収し、失敗を原全文logから特定。Windows検査はまだ実行していない。
2. repo1154066087のHTTP2/3関連crate回帰、原workflow/直列条件、全件/native/npm/Windows受入。
3. repo1386702183の固定修正版publication_schedules、旧親版へ追加回帰だけを適用した別sourceのRED、全件/doc/Clippy/原workflow/Windows受入。
4. Linux sccacheは専用scratchで開始。3source間の再利用と共有cache条件を未検証。PC共通Windows wrapperはLinuxへ転送しない。
5. 実行前後identity、原logの非表示保存/byte数/SHA256、normal+cancel cleanup、親2648/元2640/2641/2642へのnote配送。
6. 実行不能条件をskip/successに変えず残す。製品のPR変更/merge/手動success status/新下請け・monitor/Discord投稿を行わない。
7. 完了した自分のcontainer/volumeだけ回収。原log/manifestを所有付きopaque保管先へhash照合して保存後、safe-delete plan→別呼出し通常削除→finish。未完sourceは保持。

この記録は検査成功・CI全件成功・製品完了を意味しない。

## 2026-10-16 16時台の更新

- Linux repo1392152644: 関連fixture成功、元workflow alltargets成功、全workspace nextestは1850 PASS/0 FAIL。workspace doctest・Clippy・release全binsと138scenarioは初回で成功。native fmt/test/Clippy/4 Lua checkも成功。live-chainの元workflowはWindows専用であり、補助Linux実行のtest/Clippy失敗をWindows成功へ置換しない。
- Linux repo1154066087: HTTP関連nextest、native fmt/Clippy/全workspace nextest/doc/原test、report-contract、diagnostics-service成功。後続7crateのfmt/Clippy等も実行。common-entryのdescriptor回帰は子がenv_clearして/tmpを使い、UID1001所有1777のancestorをUnsafePathで拒否した。所有する/tmpを0700にして関連/全crate再検査が成功。release-stage testは明示RELEASE_STAGE_TEST_NODEを要求しており、元契約どおり新規Node22 SDKの絶対パスを渡して再実行。初回の失敗を保存。bin-onlyの2crateへ追加した--docはno library targetsで実行不能であり、合格やskipにしない。元workflowの受入とは区別する。
- Linux repo1386702183: 固定親版の別source/treeと別targetへ回帰testファイルだけを適用済み。旧版RED→fixedのpublication_schedules全件を実行中。original fixed sourceは変更しない。
- Linux依存cache: upstream sccache0.18.0の正式名sccacheを使用。閉じた先行containerのcacheだけをtar pipeで次の専用containerへ引き渡し、製品source/profile/authfilesは混ぜない。PCのWindows compiler cacheはLinux compilerに転送しない。後続sourceで命中率を実測する。
- Windows: 新規の有限サービスCC-CI2710-Finiteを専用virtual accountで作成。既存runner27/28・runtime・accountは触っていない。original pc-ci sourceのtoken制限/Win32 Job回収を独立copyし、再起動監視ループを外した。normal/cancel両fixtureで実子PIDの稼働、サービス終了とJob回収を確認。新アカウントのprofile登録/hiveは作成前なし。本人profile読み取りや認証値転送は行わない。
- Windows製品テスト前のGit initはexit1で停止し、製品テスト0。raw stderrは私的ログだけへ保存。専用accountから同じ場所での実mkdirとWin32 canonicalizeは成功。sourceを専用account所有へ直してもGit失敗が残るため、profile/canonicalizeが原因とは断定しない。System Tempへのsafe-delete createは非0で失敗し、新rootは作れていない。同じ作成の再試行や設定変更で回避しない。この失敗は自動承認レビューの拒否とは報告しない。
- WindowsSDKのread-only共有とcompiler cacheだけに、今回新設したSIDの一時ACLを追加。所有記録はowned root/windows/.ci-work/service-ownership.private.json。完了後は自分のサービス停止/削除と、新SIDの追加ACEだけの回収を行う。元SDK/runtime設定を戻す一括上書きはしない。
- 追加Go sourceは実検査を行うための有限の補助であり、独立PR/merge/配布はしない。全件成功のstatusを書かない。元2640/2641/2642をdoneにしない。

## 2026-10-16 後半の保存点

- 追加所有root（Windows source・target）: D:/CC/Work/unattended-operations-20261013-wt/todo-2710-3-rust-lua-ci/codex-owned-f63ee9e9c1833708c3220fe91bbf7736。所有ID f63ee9e9c1833708c3220fe91bbf7736。このrootは.gitignoreへ追加。製品sourceをcommitしない。
- 個人profile配下を通らない上記rootへWindows sourceをGitHubから再取得。SDKの取得/必要Git index作成は制御側だけで行い、実行account側は実ファイルのGit Merkle treeをGoで独立照合する。合成Git fixtureとの一致・改変拒否・gitlinkの単体検査成功。原headはAPIで固定。
- Linux repo1154066087: Node22.21.0と元workflowのrelease helper3件を正式位置へbuild後、docs-contract/typecheck/全Nodeテスト成功。元CIのRELEASE_STAGE_TEST_NODEとprofileを保持。原GitHub commitをbyte再構成し、commit SHAが元c53...に一致した場合だけ新CIのHEADへ配置。shallowであり全履歴とは主張しない。
- repo1392152644 Windows: fmt/doctest/Clippy/release全bins、native test/Clippy、138scenario/4native Lua check成功。live-chainはpwsh7/Windows PowerShell5のSDKパス、COMSPEC/PATHEXTを補って関連→全件成功。全workspace nextestはno-fail-fastで未実行分も含め全件実行。残る失敗はdriver_safetyの既存出力拒否テストがOSの局所化された183エラーへ英語existsを要求する1件。固定sourceを変更せず、安定したAlreadyExists文脈を加える私的patchを保存。patchの適用可能性は確認済み、修正後のRust実行は未検証。
- Windows補助はCiRoot内のCLI作業用HOME/APPDATA/TEMPを渡すだけで、OS user profileをロード/登録しない。source作成にWindows Tempを使おうとした失敗は再試行せず、safe-deleteが認める現在worktreeの所有rootを使用した。
- Windows repo1154066087: source tree/gitlink6件を照合。Rust1.97.1 SDK、元CI profile writerの最適化/直列条件を保持。profile writerのbare rust-lld解決は専用accountで失敗した。同じSDKリンカーをDのCI領域へSHA一致コピーし、絶対パス解決したところwriter build/実行とHTTP関連回帰が成功。残り元Windows native/all-features/credential/helper crate検査を実行中。共有SDK/旧runnerの書換えではない。
- Linux repo1386702183: 親版+回帰のみのREDはoverflow panic/対象回帰FAILを実測。固定版publication_schedules48件成功。全workspace nextest/doc/Clippy、統合4shard、署名/TLS helper、frontend各shard等を実行。packaging2失敗は原HEADを欠いたarchive metadataが原因で、元GitHub commit bytesのSHA確認後に補い、失敗2件の再検査成功。
- Chrome SDKはLinuxの新CI内の/workへだけ配布。公式配布のChromeとDebian公開SDK libsを取得し、base image89988faは変更しない。CPU専用Go SDK launcherが実ChromeのSHAを検査し、GPU/ソフトGPUの使用を禁止し、追加SDK libsだけを設定して実Chromeへexecする。DOM実描画/owned Chrome退役/ブラウザーcold start成功。GPU device要求なし。
- 元ブラウザーfixtureはLinux CIの互換フラグGITHUB_ACTIONS=trueと、worktree内target・厳密なevidence名を要求する。これは検査用入力だけで、GitHub Actionsの実run/status成功ではない。Root3の既存targetを同じowned volume内のsource/ci-target-2710へ移し、/work/targetから内部aliasを作り、絶対ビルド参照を維持。tracked source/原headは不変。TLS helperもそのtarget配下へbuildして関連virtual HTTPSを再検査中。
- Primary C owned root内のci2710-v*.exeは制御側、windows/.ci-runtime/service/ci2710.exeとwindows/bin/Runner.Listener.exeは今回専用の有限サービスだけ。既存runnerとは無関係。実行中のexeを上書きせず、制御側は世代別出力を使う。
- 一時SDK read ACEは初期7パスに加え、C:/Program Files/PowerShell/7 と C:/Program Files/Google/Chrome/Application に今回SIDだけ追加済み。終了時にこのSIDのACEだけ除去する。永久設定/他SIDを上書きしない。

残り: Windows2結果と失敗対応、Windows3のroot/native/hidden/browser、Linux3の追加browser受入、patchと原log/manifest SHAの配送、元ToDoごとのnote保存、今回のservice/container/volume/SDK rootだけの回収。原2640/2641/2642/親2648はdoneにしない。新規PR/merge/配布/Discord投稿/新担当は追加しない。
