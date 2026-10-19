# PassShare PR720の修正差分を確認する（2986）

読み取り専用の別モデル確認。最終返信だけを返し、ファイル編集/保存/commit/ToDo更新を求めない。
GPU/Civ6/重Cargo/新費用/APIキー/秘密/ブラウザなし。子Codexを直接起動しない。
起動時D:/CC/AGENTS.md、作業AGENTS.md、次の最新方針を読む。
D:/CC/Policies/owner-windows-completion-before-other-os-20261019.md
D:/CC/Policies/actions-cost-control-20261019.md

対象は実API確認public AutoDevJapan/PassShare ID1131498455、PR720。
元main c67bfb864acd51af0aafe972b2ea09e53d187ed7、前回レビュー候補3dec5ab5d02cf45e77c4bbbb14a734f2508be78b、
今回固定HEAD 2decea3d655e3a244f2b3ddb8019c516dbe5516c。
最終diffは二workflow、Cargo2、JS manifest/lock4、.cargo/audit.tomlの9ファイルだけ。
parent補助固定commit e8a38bf7a23b3ce55d464b96bab9ba1f680f73b9 の
tools/passshare-ci/{main.go,main_test.go,preserve.go}と docs/todo2986-ci-progress.md も読む。
親worktree D:/CC/Work/unattended-operations-20261013-wt/todo-2986-passshare-ci のgit objectから読める。
製品はGitHub APIの固定contents/compareで読む。未知のPC製品treeや保護sourceを読まない。
scrub391の保護判定を守り拒否を別経路で迂回しない。

前回レビュー2998の実Sonnet5.5を元job/thread/cwd/turn_contextで照合済み。
原応答は親.ci-work/review2998-response.private.md、SHA256
33f72f7ecec40af3860651a3e4b39cdc31cdcb066baac162c26188f46499afc5。
前回の読了範囲を全面的に調べ直さず、今回の修正と必要な読む側に絞る。

修正事項：
1. 前回Must1（frontend/cloud-assets.jsonとfrontend/public/cloud-crypto/*の読取依存）を
   両client対象の分岐へ追加。17実Git/Bashケースで検査。main pushは先行runが取消されても
   最新runで取りこぼさないよう常に両clientを対象とし、PRはbaseからmerge checkoutの全差分で判定。
2. DB E2Eの同時migrationで実40P01失敗があった。元Compose configの全build/install/test commandを
   保持してnode --testの全出現へ--test-concurrency=1だけを挿入。未知形式/$式は成功偽装せず拒否。
   追加test file/複数node invocation/未知形式/ドル式を実Bash+jq fixtureで検査。
   元の7 test filesと外部DBの必要受入は消さない。正式PostgreSQLの修復合格はまだ未確認。
3. YAMLはNodeでコメント/元キー順/action版注記を保持し、往復で実構造一致を検査する。
   古い監査非blockingコメントは現行のblockingに訂正。gate needs全job網羅も検査。
4. cargo修復2996を統合。子の固定Cargo2blobを同じバイトと照合済み。
   Cargo.lock=3a573c3dd2bbf2bb3be2956874d101480aec36f4、desktop Cargo.toml=9a882fc6c33f92c5f1a4d776227491b96f5e86e7。
   子の初回/補足レビュー3001/3006は実Sonnet5.5/各18,11steps/exit0/Must0、未読補足も完了。
   この2blobの全件レビューを重ねず、最終HEADで同一blobかとJS側整合/監査設定の変更を見る。
   子の配送docs/todo2996-parent-handoff.md（77d299bee612276831f8bbd159b6f0eff4ac8286）が必要なら参照可。
5. 必須JS整合をpnpm10.11.0で通常解決。両clientの@tauri-apps/api2.12.4/cli2.12.3、
   desktop updater2.14.0。Rust tauri2.12.4/updater2.14.0に整合する。
   API/CLIのminor範囲~2.12、plugin同版2.14.0に制限。JS source/製品機能の追加変更なし。
   公式条件 https://v2.tauri.app/develop/updating-dependencies/#sync-npm-packages-and-cargo-crates-versions
6. 旧quick-xml免除2件と理由を削除しignore空。security-audit.ymlは無変更で重大失敗を免除しない。
   upstream registry2.14.0はzip^4で脆弱性を残すため子が公式merge ba98bfadc4c868c9a7e1a226c83af528e1f11b89へ
   git rev固定した。zip>=9採用の新stable公開・同等監査/関連検査成功時だけregistryへ戻す条件を追跡する。

確認済み：子2996のroot明示audit14→0/native0（ignore空）とWindowsの関連check/独立5受入。
このgraphは製品本体の正式CIとは区別する。親のGo test/vet/actionlint、実Git17ケース、
集約6ケース、DB command4ケース、YAML往復が成功。最終製品CIは今回初実行中で、成功を主張しない。
新HEADの同じ検査をPCで重ねていない。Mac/Linux配布は3010/PR721の専有範囲なので触らず、待たない。
Windowsの本人PC正式jobの供給/hash/回収は2994/元2957の別工程であり今回完了としない。

実害あるMust/Suggestion/Nitを該当箇所・具体例・修正案とともに日本語で返す。
既知の保留事項を新Mustと誤認しない。読んだ固定base/headと全ファイル、未読と未実行を区別。
Mustが残っていても読取自体が完了なら先頭は「状態: 完了」、実装は親2986が追う。
