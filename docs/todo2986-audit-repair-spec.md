# PassShare PR720 の監査失敗を修復する（親2986）

親のCI改善候補で実際にcargo-auditが失敗したため、監査免除をせず原因を直す有限工程です。
対象は実API確認 public AutoDevJapan/PassShare（ID 1131498455）だけ。
起動時 D:/CC/AGENTS.md、作業先AGENTS.md、actions-cost-control-20261019 と
owner-local-build-windows-first-20261019 の方針を読みます。OAuthサブスクのみ、新費用/APIキー/GPU/Civ6なし。
重Cargoはこの工程の割当内だけ。子Codex直接起動は禁止。必要な独立レビューは共有ToDo --spec。

所有と対象を確認したGitHub上の固定sourceから、該当するCargo依存manifest/lockfileだけを通常の方法で取得します。
未知のPC上の既存製品treeは使わず、scrub391の保護判定を守り、拒否されたsourceを別経路で迂回しません。
基点main c67bfb864acd51af0aafe972b2ea09e53d187ed7、親のworkflow候補
3dec5ab5d02cf45e77c4bbbb14a734f2508be78b / PR720。
親はquality-gate.yml/mobile-build.yml、2994はdesktop-build.yml/Windows runnerの書込所有。
この子はworkflow/署名/Prd/公開/ライセンス/予算を変更しません。

実失敗：Security Audit run39247094667、job118041538621、2026-10-19。
pnpm auditは6ディレクトリ全成功。cargo auditは12 vulnerabilitiesで失敗。
- quinn-proto 0.11.15: RUSTSEC-2026-0341/0339/0342/0340/0338、修正版>=0.11.18。
- rustls 0.23.45: RUSTSEC-2026-0347/0348/0349/0346、修正版>=0.23.46。
- zip 6.0.0: RUSTSEC-2026-0369/0367/0368、修正版>=9.0.0（実ログの版も再確認）。
warningのunmaintained等と脆弱性エラーを混同しません。ルートcargo auditの後続には
backend/native/Cargo.lockの独立auditがあるので、こちらも実結果を確認します。
原全文は親作業先 .ci-work/cargo-audit.private.log に保存されています。

要求：依存の読む側/上流release/互換性を一次資料と最小の関連検査で確認し、
上記重大監査失敗を解消する最小Cargo manifest/lock更新を作ります。
zipのメジャー更新が必要なら利用上流tauri等の対応関係を調べ、誤ったlock手編集や
`cargo audit --ignore`/continue-on-error/監査削除で成功にしません。
新しい機能・製品全面改修・無関係な依存更新はしません。
別モデルに固定差分を確認させ、関連検査とaudit成功を実測します。

製品の別PRや通常CIを勝手に起動せず、親PR720へ一度統合できる固定変更を返します。
親branchへ直接pushしません。固定baseと変更したファイルのGit blobまたはGitHub commit SHA、
関連検査結果、監査結果、残条件を親へ配送します。親が最終候補HEADの正式CI/通常PR/mergeを担当。
既存のraw sourceをcc-pc-tools-rsの公開commitへ混入しません。補助コード/仕様/進捗だけ専用scheduler branchへ保存。
秘密/認証情報は読まず表示せず、登録token等は扱いません。

新一時rootはsafe-delete create、検査証拠保存後plan→別呼出通常削除→finish。拒否はquarantine。
子は原2957/2955/親2986/2994へのwaitを作らず、元2986に実施・未達をnote追記します。
終了前は自身のbranch/HEAD再開情報をscheduler checkpoint-workspaceへ保存。
通常完了メンション・通知を送りません。fixtureや登録を製品CI成功と報告しません。
