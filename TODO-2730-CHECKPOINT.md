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

## 完了と残工程

GitHubから両OSの新owned CIだけへsource取得、物理treeとLinuxの実commit HEAD照合済み。
公式Rust1.95.0を新SDK rootへ導入成功。製品のCLAUDEとrepository-rulesを確認済み。
Linux正常/中止fixture、Windows新サービス正常/中止fixtureと実テストの結果は原logと照合して追記する。

残工程: regression/driver_safety→変更crate→両OS全workspace nextest各1回→doc/Clippy/fmt。
旧sourceのnative/Lua/138scenario/live結果は新sourceでの実行と書かず、変更範囲外のtree一致で継承可否を確認する。
原log/bytes/hash/exit/SDK/OS/source/実GitHEAD-treeを全保存し、回収manifestのsource/copy SHA一致を確認して資源を回収。
サービス/PID0/追加ownSID ACE解除/profile登録0/hive0を確認。親2648と元2640へnoteで配送。
元ToDo状態はschedulerに任せる。成功未確認の工程を成功としない。
