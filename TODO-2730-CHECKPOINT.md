# ToDo2730 固定修正の独立検査

対象はrepo1392152644のPR148だけ。製品sourceの編集・独立PR・merge・配布・Discord投稿はしない。
固定headは75a4051305b6f47c70ddb59a8d2bf9015666f12c、treeはcecb9cd535d135a1eb4407882db532c325f42860。
親74f023c0ee4b460f05f87dc3db5ceaf75e999a8aからrunner.rsだけが変わり、提案blobと一致する。
GitHubの実owner/private/admin/nonfork、head/base repo、実branchref、固定head/parent/tree/変更file/blobを実照合済み。

## 所有資源

- 制御/一時原ログ: C:/Users/makhmeto/AppData/Local/Temp/codex-owned-e4959f397147b0eaa9f9db9e0f43411f
- SDK: C:/Users/makhmeto/AppData/Local/Temp/codex-owned-84690dee173be63f1dd576aba39a63a7
- 新Windows source/target: このworktreeのcodex-owned-b0a6f0387481904db8c15d432cc4a7fb
- 永続証拠: このworktreeのcodex-owned-ac8143dbbc50a8cf65b2987dfb99ee96（消さない）
- Linux状態: 制御rootのrepo1392152644.private.json。新containerだけ。candidate lockはこの資源の終了確認後に解放する。
- 新Windows有限サービス名: CC-CI2730-Finite。既存runner/runtime/accountは変更しない。

helperはf7acf6dのtools/ci-check-2710から独立copy。旧親pinを明示された75aだけに更新し、候補を本対象だけに制限。
API条件・Git物理tree・commit再構成・共通Cargo gate・tar末尾drainと全file照合を保持。
新rootはsafe-delete create済み。終了時はplan→別呼出し通常delete→finish、拒否ならquarantine。
旧2710の未回収4rootへは触らず、削除の再試行もしない。

## 最新の実結果（2026-10-16 20時台）

新sourceの回帰テスト1件とdriver_safety全48件は両OSで成功。
Linuxは変更crate802件、workspace1835件が成功。default/all-targetsのJSON一覧を照合し、
追加対象がexample/mod_selection_rollbackの15件だけであることを確認して、その15件だけ成功。
計1850件を網羅。初回のコマンドに--all-targetsが無かったことを隠さず、一覧と追加実行の原ログを保存。
全workspaceを重複実行していない。保存したlinux-original-executed-plan.shは231abaeの実行版。

Windowsは変更crateの全対象とworkspace1854件を実行。
初回は1853成功、migrationのGit index検査1件がdubious ownershipで失敗。
新規sourceだけをGIT_CONFIG_COUNTのsafe.directoryへ指定して、その失敗1件だけ再実行し成功。
ソース変更による修正ではない。全workspaceの失敗exit100と原ログを保持し、全件を成功へ書き換えていない。
初期のリンカー配置失敗exit101も保存。新source内のrust-lld.exeを同SHAコピーし、実パス指定で解消。
Windows実生成先はv3以降、共有gateが排他する専用TargetをCARGO_TARGET_DIRへ明示。
両OSのdoctest、Clippy全対象の-D warnings、fmtは成功。doctestは各crate0件であることを保持。

HaikuのToDo2729は供給されたcreate_dirのsliceだけを静的レビューしMustなし。
レビュアーはdisk/SHAを独立照合していない。その制限を保持し、この担当がsliceの実runner内完全一致と
runner SHA256 80F2B5118C0E128BDAAC65E1F8C8A534B68F3BDF70ACB2E94374289FEACA6303を追加照合。
GitHub全treeの非treeエントリーはrunner以外1119件が一致。旧native/Lua/138scenario/liveは親SHAの証拠として保持。
それらを新sourceで再実行したとは書かない。製品全体完了、正式CI/status成功、merge許可は主張しない。

## 回収の実結果と未完了

原ログ/manifestを全fileのsource/copy SHA・bytes一致で永続証拠rootへ保存。
Linuxのtarは末尾paddingまでdrainし、manifest生成後にだけsource/container/volumeを回収。
最終の実Git HEAD/tree、物理source、GitHubの実identityを照合済み。
Linux新containerとanonymous volumeは回収済み、停止PID0確認済み。候補lockは解放済み。
Windows新サービスは停止PID0確認後に削除し、新SIDへ足した10か所の明示ACEを除去。
新SIDの実プロセス0を管理者側で照合。normal実child観測とnormal/cancelのJob排出ログを保存。
cancel fixtureの一時child PID JSONは後続実行で上書きされており、PID個別の保存証拠とはしない。

未完了1: 管理者側で新サービス専用のprofile登録とhive残留を検出。
通常ユーザーのTest-Path=falseを不在証明に使わない。管理者の実結果は両方true。
新SIDのprocess0、サービス無し、実profileパスが新サービスだけと確認してから、
CC-AdminBroker経由でそのSID_Classesだけをreg unloadしたが、WindowsがAccess is deniedを返した。
profileの削除は試していない。同じunloadの再試行や別API/強制経路での回避をしない。
これは自動承認レビューの拒否ではない。詳細はremaining-cleanup.private.jsonとbindingの診断記録。
本人のPC操作を依頼しない。元のrunner/account/profileには触れない。

未完了2: 新Windows source/target root b0a6...のplan/quarantineはいずれもPROTECTED_PATH。
新SDK root8469...のplan/quarantineはいずれもENTRY_LIMIT_EXCEEDED。
両方を保持し、同じ削除・退避を再試行しない。別経路や上限/保護の変更をしない。
旧2710の4rootにも触れていない。

制御root e495...はplanの通常Remove-Itemを別呼出しで実行し、finish=source_absentを確認済み。
永続証拠root ac814...は残す。execution-index.private.jsonに全OS/各run/原log/bytes/hash/exitを保持。
次回はRust全件・RED・同じ回収拒否を繰り返さず、未完の新profileと2rootだけを扱う。
親2648と元2640へ実結果/未完条件をnoteで配送する。本人判断は必要としていない。

## 以下は準備時の記録（最新の成功/失敗は上記）

GitHubから両OSの新owned CIだけへsource取得、物理treeとLinuxの実commit HEAD照合済み。
公式Rust1.95.0を新SDK rootへ導入成功。製品のCLAUDEとrepository-rulesを確認済み。
Linux正常/中止fixture、Windows新サービス正常/中止fixtureと実テストの結果は原logと照合して追記する。

残工程: regression/driver_safety→変更crate→両OS全workspace nextest各1回→doc/Clippy/fmt。
旧sourceのnative/Lua/138scenario/live結果は新sourceでの実行と書かず、変更範囲外のtree一致で継承可否を確認する。
原log/bytes/hash/exit/SDK/OS/source/実GitHEAD-treeを全保存し、回収manifestのsource/copy SHA一致を確認して資源を回収。
サービス/PID0/追加ownSID ACE解除/profile登録0/hive0を確認。親2648と元2640へnoteで配送。
元ToDo状態はschedulerに任せる。成功未確認の工程を成功としない。
