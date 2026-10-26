# 元3942の再開情報

担当job: 20261026-174601-todo-3942（依存世代2）。branch: scheduler/todo-3942-work。
初期HEAD: f30f9e763e25884fa48d1cce0cac22da1cebfb69。
現証拠フォルダー: C:/Users/makhmeto/AppData/Local/Temp/codex-owned-5249eb2a5e0382b871074f454633ec84
所有ID: 5249eb2a5e0382b871074f454633ec84。旧jobの空フォルダーID d1f4219e57edeeee0818d75ac8bd9e54も保持。

最新共通方針・元依頼・AGENTS・清掃/ビルド規則を読了。元3757のdoneと配布/実受入記録を確認し、
旧jobで完了済み依存へwaitを再登録した誤りは管理側が正規cancel/再割当で復旧済み。
current job/Rootの世代2・正常リンクと3757doneを読返した。wait再登録なし。状態更新はschedulerに任せる。
作業場所の現guardはclear。originはAutoDevJapan/cc-pc-tools-rs。未コミットの先行変更なし。

Issue545、初回実装0d90458a。レビュー3949は実gpt-6-sol/max/62steps/終了0でMust3件。
原応答と照合はdocs/completed-build-cleanup-3942.md。Cargo生成元確認・全ルート不在・変化時残量未測定で修正。
cc-jobsに既存キューのfresh complete active IDを無変更で読むAPIを追加し、毎回の全履歴走査を廃止。
修正後Windows38件/0skip、active API1件/0skip、Clippy成功。cursor破損の保持/走査継続をさらに追加。
原logは現証拠フォルダー。初回実機dry-runは8秒/512行/削除0、job全項目を上限に数えた問題を検出し修正。
初期release SHA99e0.../source0d904...とbuild-manifest.private.jsonは履歴。現修正の配布用ではない。
Go更新補助はcleanupのみの新世代を追加したがpinは初期候補で未確定、実配布禁止ではなく資格未成立で保持。
残り: 修正差分の追加レビュー、修正後release/build manifest/pinの確定、同head CI/PR/merge、
cleanupだけCAS/ACL保持配布、通常1時間タスク実発火、実測と後片付け。実回収/現用更新は0。

19時台の区切り: 3954のMust3件をさらに修正。通常metadata全件を有限に読み、生存supervisor PIDも保護する。
fingerprint package/markerをディレクトリごとに固定、incremental/session/OUT_DIRの形式を絞った。
profile名は保持済み通常ファイルの同一性で確認。相対参照・破損cursor回復位置も追加。
Cargoの内部hardlinkを実測（release名2、library名1）。同じ計画内に全リンクがある場合のみ
既存handle削除で処理し、bytesは最終リンクで1回。外部リンクは保持。実Windowsの回帰成功。
Go配布票に元Rust出力とsingle-linkコピー一致を追加。まだpinは初期候補であり配布未成立。
現在のtarget/release/cleanup-8h.exeはe7f0b893 sourceの未配布v3。初期v1/v2は証拠フォルダーへ保存。
次は全関連テスト/Clippyの結果を確認してcommit、修正後releaseと149入力manifestの作成、
Go pin確定と追加レビュー/CI/PR/merge/実配布/通常task実発火/実回収、最後に自分のdebug生成物を回収する。

20時前の追記: PR548 https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/548 。
3957のMust2を修正中。root/exampleにkind markerを要求し、以前の終了jobも相対参照を確認。
CI663834fcは7fixtureで短縮Temp名比較が失敗。fixtureのみcanonicalizeで修正。
元3570の欠けたjob.json/公開claimは触らず、normal claim+元ToDo履歴で保持範囲を比較するAPIを追加。
最新版の49関連テストとClippyを確認後commit/build/pin更新して限定追加レビューとCIを行う。
c550be8f/22951fb0のsingle-link deliveryと149入力manifestは履歴（配布0）。
現用は依然7fbc49...。新sourceでbyteを生成してから新しいmanifestとGo pinへ確定する。

最新確定: Rust source a64372a6f1f1c969cb3b83c0cb39be22b433b924、clean通常release終了0。
single-link delivery SHAa2658a6e60544a598a809ef85686e7ac87416ec57d042958865cdecdbddccb58。
149入力manifestは証拠dir/build-manifest-a64372a6f1f1c969cb3b83c0cb39be22b433b924.private.json、
SHA86a858825aea27f929b8dcb45e6fab3d4d1c66ec97e2ba23964033fce93b5be8。Go pinは0da6b8acで確定済み。
最新49関連テスト/Clippy、Go race/vet成功。PR548新head CIと追加差分レビューを進める。
CI旧6638の7失敗はfixtureの短縮名比較を修正済み。現在のcodeで正常Windows CI成功まで完了にしない。
Root3570 rawjobは保持、normal published work claimと同じToDo job履歴で保持scopeだけ照合。
3960は通常main sourceを1回Cargo checkし、正常done/元jobで実機回収を受入する独立builder。
次: 最新追加review Must0/CI成功→通常merge→正規Go updater --completed-build-check/applyをAdminBrokerで
実施→現用SHA/ACL/元backup一致確認→既存1h task実発火→3960等の実回収と不在/空き前後→自分のdebug回収。
未知Rootや他担当のraw recordは修復目的へ拡大しない。現用更新/本件実回収はまだ0。
safe-delete/3757、3939、元3628/3796/3815/3816のソース/状態/原scopeは変更しない。
既削除8件を再回収・二重計上しない。本人回答とPC操作は不要。
