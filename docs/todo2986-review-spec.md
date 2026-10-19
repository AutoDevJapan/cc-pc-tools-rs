# PassShare CI の固定候補レビュー（親2986）

別モデルの読み取り専用レビューです。変更・commit・push・ToDo更新・成果ファイル保存は求めません。
最終返信で、実際に読んだ全ファイル、Must/Suggestion/Nitの具体的根拠、未確認条件を返してください。
GPU/Civ6/重Cargo/ブラウザ/秘密情報/新費用/APIキーなし。子Codex直接起動なし。

起動時 D:/CC/AGENTS.md と作業先AGENTS.md、
D:/CC/Policies/actions-cost-control-20261019.md、
D:/CC/Policies/owner-local-build-windows-first-20261019.md を読みます。
対象は実所有をAPI照合した public AutoDevJapan/PassShare（ID 1131498455）の二workflowだけです。
基点c67bfb864acd51af0aafe972b2ea09e53d187ed7、候補3dec5ab5d02cf45e77c4bbbb14a734f2508be78b、
branch scheduler/todo-2986-passshare-ci。APIのimmutable contents/compareを通常の方法で読めます。
`.github/workflows/quality-gate.yml`と`.github/workflows/mobile-build.yml`以外の差分がないことも確認。
原本を読むために未知のPC上の製品treeへ入らない。scrub391の保護判定と拒否非迂回を守ります。

補助コードはこのrepoの固定commit eeac5ce58d2957be8cab1fac04e38a96c98e3da0 にある
tools/passshare-ci/{main.go,main_test.go,README.md,testdata/before/quality-gate.yml,testdata/before/mobile-build.yml}。
親作業先 D:/CC/Work/unattended-operations-20261013-wt/todo-2986-passshare-ci のgit objectから
読み取ってもよい。生成後のYAMLは同作業先.ci-work/candidate/に保持。
親のdocs/todo2986-ci-progress.mdとWindows工程specも背景として読む。

確認したい実害：
- 同じmobile試験を1回にする一方、元QGの全Rust試験/Clippy/React受入/JS/build/APIの設定・env・依存を落としていないか。
- Desktopの追加固有native_lock/tsc/Go検証器を保持しているか。Desktop Buildの削除/Windows正式runnerは別2994の所有であり、このPRでは未実装。
- workflow_callの呼出しは同候補SHAを使うか。required workflow入口がPendingにならないか。
- PRの実merge checkout、push全before..after、削除/移動元/移動先、未知比較元を正しく扱うか。
- shared/lock/toolchain/workflow/packages/crates/backend/documents/scripts/未知の読取依存を誤ってskipしないか。
- preflight失敗や関連job失敗、Go SDK matrix失敗が集約へ伝播し、影響なしと判定したclientだけskip成功を許すか。
- 無関係なLinux本番/Web/backend/DB/SDK/e2eジョブのコマンド・env・設定を変えていないか。
- 新しいconcurrency/timeoutが成功偽装や無限待ちを作らないか。security-audit.ymlは完全無変更で重大失敗を免除していない。
- ソース/秘密/Prd/公開/ライセンス境界、対象外の変更がないか。

検査済み：Go test（実Git/Bashの15ケース×正常比較/初回push/未知比較元）、Go vet、actionlint。
移動元が漏れる初回失敗を--no-renamesで修復済み。構造比較で対象外全jobsはneeds追加以外を同一確認。
集約厳格化後の構造検査も成功。候補の正式GitHub CIはこれから実行し、成功を主張していません。
runner登録0件であり、このPRに未供給self-hosted labelの追加はありません。
監査の直近失敗/実runner分数/課金効果/子2994の実Windows受入は未完として保持しています。

推測の機能追加や対象外のアプリ改変を要求せず、具体的な実害と該当箇所を示してください。
Mustがあっても読取レビュー自体は完了です。最終返信だけ返し、親2986や2957を待たないでください。
