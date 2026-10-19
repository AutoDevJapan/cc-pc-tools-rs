# PassShare の Windows 正式検査（親2986の独立工程）

親2986は PassShare の Quality Gate / Mobile Build のみを書き込みます。この工程は
PassShare の `.github/workflows/desktop-build.yml` と、新規の専用 Go helper
`tools/passshare-ci-windows` だけの書込所有を持ちます。ほかの製品・既存runnerは変更しません。
元の費用是正2957や公開2982を待つ循環を作りません。

起動時 D:/CC/AGENTS.md、作業先 AGENTS.md、
D:/CC/Policies/actions-cost-control-20261019.md と
D:/CC/Policies/owner-local-build-windows-first-20261019.md を読みます。
GitHub APIで実所有 AutoDevJapan/PassShare（ID 1131498455）/ public / main を再照合します。
監査基点は c67bfb864acd51af0aafe972b2ea09e53d187ed7 ですが、凍結資料を最新と見なしません。
未知の既存製品treeは使わず、明示GitHubの固定ソース・固定workflow・確認済み資源のみ通常の取得方法で使います。
scrub391 --guard-pathで保護対象を除外し、拒否を別経路で迂回しません。

本人の2026-10-19指示により、WindowsアプリはこのPCの正式self-hosted jobで一度ビルド・受入し、
同じ最終ソース・条件のクラウドWindowsビルドを重ねません。常時macOS配布matrixを除きます。
Linux本番・鍵保管・セキュリティ・OS別受入を消しません。
親はQuality Gateへ既存Desktop smokeの追加固有検査（Go候補検証器、native_lock::tests、tsc --noEmit）を移し、
既存desktop/mobileのLinux検査は維持します。Quality Gateのジョブ名Desktop smokeは保持予定です。
この工程は最終mainの当該ステップ存在と成功をAPI/固定YAMLで照合してから重複smokeを削除してください。
親の候補保存・マージ状況は親作業先docs/todo2986-ci-progress.mdと共有noteに配送されます。
親のファイルを編集せず、親ToDo待ちを登録せず、先に独立したrunner実装と供給実証を進めます。

現APIではPassShareのself-hosted runnerは0件です。未供給labelへ先にworkflowを切り替えません。
固定した信頼済みsourceとworkflow/run/attempt/jobを照合する有限ephemeral runnerを用意し、
外部fork/未確認PRを本人PCへ流しません。既存pc-ci・civ6-ci-windowsの確認済み設計を参考にできますが、
既存担当のrunner・共有gate・認証範囲を包括的に広げません。
公式runner tokenは既存OAuthからRAM/stdinのみ。秘密値・認証ファイルを読んだり表示したりしません。
専用account/Job Object/終了排出・本人profile不読・共有SDK RX・Cargo gate・専用target・sccacheを維持。
CREATE_NO_WINDOW、管理者はCC-AdminBrokerのみ。GPU/Civ6起動不可。Cargo割当を受けて重い工程を実行。

Desktop Buildは必須入口を消してPendingにしない構成にします。main/関連変更または必要な手動候補で
Windows配布物を一度作り、同じSHAのQuality Gate成功を参照します。変更判定はlockfile/toolchain/
workflow/shared/crates/packages/読まれるdocuments/scripts/configも含め、失敗や不明な比較をskip成功にしません。
PR一般はGitHub隔離runnerのQuality Gateで検査し、本人PCは信頼確認済み固定候補だけ。
配布物の実バイトSHA256・ソースSHA・正式job結果を記録。署名・公開・Prd・ライセンスは変更しません。
runner供給・正式実job・成果物hash・終了回収を実測してから切替を完了とします。
未供給や正式検査失敗を登録成功/fixtureで成功と扱わず、実エラーと次の処置を保存してください。

新helperはGo。変更YAMLを構造として比較し、対象外差分なし。新一時rootはsafe-delete create、
plan→別呼出通常削除→finish。拒否はquarantine。自作タブがあればowner-chromeで工程終了時に閉じます。
実装・関連検査・別モデルレビューは通常共有ToDo --spec経由（子Codex直接起動禁止）。
実害あるMustを解消し、同HEADの正式CI成功を確認し通常PRでマージ。無目的rerunなし。
同じsourceの候補ビルドを親と重ねません。新費用・予算増額・AI APIキー・無制限runnerなし。
途中成果を専用schedulerブランチへcommit/push。分岐変更はcheckpoint-workspaceを保存します。
元2986へ実コミット/PR/実行/ハッシュ/回収/未達条件をnote追記し、通常完了通知やメンションは送りません。
親はこの工程の完了後に全体効果を統合するので、親2986へのwait登録は禁止です。
