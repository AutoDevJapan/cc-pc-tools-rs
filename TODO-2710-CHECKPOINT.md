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
