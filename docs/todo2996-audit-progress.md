# PassShare 監査修復（ToDo2996、親2986）

## 対象と固定候補

GitHub APIで AutoDevJapan/PassShare、ID 1131498455、publicを確認した。
製品baseは `c67bfb864acd51af0aafe972b2ea09e53d187ed7`、親PR720候補は
`3dec5ab5d02cf45e77c4bbbb14a734f2508be78b`。この子はworkflowを変更せず、
製品PR・通常CI・親branchへのpushも行わない。正式CIと統合・mergeは親の担当。

取得した製品ファイルは固定baseのCargo manifestとlockfile。依存解決用に各crateへ
空のRust targetを置いている。これは製品ソースや製品テストではなく、製品の成功とは
数えない。製品側updaterの読む側はAPIで確認し、desktop lib.rs:951の
`Builder::new().build()`の登録だけで、zipの型を直接扱う箇所はない。

変更は次の2ファイルだけ。Git blobはこのworktreeと親が共有するGit object storeへ
`git hash-object -w --no-filters`で保存した。製品raw sourceをPC道具の公開commitへ
含めていない。blobだけではGitHubへpushされないため、親は同じ共有Gitから取り出す。

| 製品内path | base blob | 候補blob |
| --- | --- | --- |
| Cargo.lock | 2b07f61aa749594a49dfdffc0209a455d5323857 | 3a573c3dd2bbf2bb3be2956874d101480aec36f4 |
| apps/desktop/src-tauri/Cargo.toml | 88cc777ceb5643f42a5058038d3a463fdebbd713 | 9a882fc6c33f92c5f1a4d776227491b96f5e86e7 |

## 修復方法と一次資料

実CIのzipは6.0.0ではなく4.6.1。quinn-proto 0.11.18、rustls 0.23.46へ
Cargoの通常updateで更新した。zipはtauri-plugin-updater 2.10.1のWindows依存^4から入る。
公開済み最新stable 2.14.0も^4のままであり、zipだけのメジャー更新は解決できない。

上流PR3698は2026-10-16にzip ^9へ変更してmergeされた。desktopの依存をその固定merge
`ba98bfadc4c868c9a7e1a226c83af528e1f11b89`へ変更し、Cargoがzip 9.0.5を解決した。
この上流はtauri ^2.12を要求するため、tauri 2.12.4と関連ライブラリも更新される。
MSRVはupdater/tauriが1.90、zipが1.88。検査PCはRust/Cargo 1.95.0で、
親のWindows工程の固定SDKも1.95.0。共有coreのrust-version 1.86は変更していない。

監査DBの同じ固定版で、元lockにquick-xml 0.39.4の重大2件も実検出した。
その読む側plist 1.9.0を1.10.1へ更新し、quick-xml 0.42.0へ解決した。
これは監査成功に必要な追加修復で、元の12件へ混同せず14→0と記録する。
unmaintained/unsoundなどの情報warningは重大脆弱性エラーと区別する。

- https://github.com/tauri-apps/plugins-workspace/pull/3698
- https://github.com/tauri-apps/plugins-workspace/blob/ba98bfadc4c868c9a7e1a226c83af528e1f11b89/plugins/updater/Cargo.toml
- https://crates.io/api/v1/crates/tauri-plugin-updater/2.14.0/dependencies
- https://crates.io/api/v1/crates/plist/1.10.1/dependencies
- https://rustsec.org/advisories/RUSTSEC-2026-0369.html
- https://rustsec.org/advisories/RUSTSEC-2026-0194.html
- https://rustsec.org/advisories/RUSTSEC-2026-0195.html

上流mergeのchecksはSecurity auditがsuccess、一部integrationとmatrixはfailureであり、
上流CI全成功とは主張しない。このPCでupdaterのWindows依存をコンパイル確認中。

## 実測済みと残条件

- cargo-audit 0.22.2、DB 999ec89d5d3f10f20be14750ec8c2d8b44e0ac9b（1338 advisories）。
- 元rootはexit1/14件。候補rootはexit0/0件。ignoreは空。
- backend/nativeの独立lockは変更せずexit0/0件を確認。
- 候補はcargo metadata --locked成功。逆依存をcargo tree --locked --target allで確認。
- `cargo check --locked -p tauri-plugin-updater`はWindowsで実行中。
- 別モデルの固定差分読取レビュー、関連検査の終端確認、親への最終配送が残る。
- 製品本体の全テスト・正式CI・mergeはこの工程では未実施。

証拠は.gitignore対象の`.ci-work/`へ保存。root-before.lock/root-after.lock、
desktop-before.toml/desktop-after.toml、audit-*-before/final.private.json、
metadata-final.private.json、update-*.log、check-updater-windows.log、handoff.private.json。
所有付き一時rootは本worktree直下、safe-delete ID `799bea062b6fe7ba91eed988f3f50215`。
scrub391で作成済みrootと本worktreeは両方falseを確認。未知Tempは空のまま通常plan削除し
finish済み。元資料は保持し、拒否された製品sourceを別経路で取得していない。

## 再開

1. この記録と親2986のnoteを読み、上記の固定候補blobを照合する。
2. 実行中のCargoは重ねず終端を回収し、失敗なら原因を直して対象だけ再検査する。
3. レビュー担当の実モデルと固定差分を照合し、Mustがあれば修正する。
4. 親2986の既存noteを保持して、候補blob、audit、関連検査、残条件を追記する。
5. 証拠保存後、所有rootをplan→別呼出の通常削除→finishで回収する。
6. 支援記録だけをscheduler/todo-2996-passshare-ci-rust-2986へcommit/pushし、
   終了前にscheduler checkpoint-workspaceへ実branch/HEADを保存する。
