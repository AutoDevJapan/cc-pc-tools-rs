PassShareの固定Cargo依存修復だけを、既存OAuthのClaude Sonnet 5.5で読取レビューしてください。
これは複数manifest/lock/資料の照合であり、単一ファイルレビューではありません。
実装担当はToDo2996、親2986です。子ジョブを直接起動しないでください。

対象worktree:
D:/CC/Work/unattended-operations-20261013-wt/todo-2996-passshare-ci-rust-2986

最初にD:/CC/AGENTS.md、対象worktreeのAGENTS.mdと
docs/todo2996-audit-progress.mdを読んでください。
Skill subagent-reviewを適用し、読取だけを行います。

## 固定対象

AutoDevJapan/PassShare ID1131498455/public、base c67bfb864acd51af0aafe972b2ea09e53d187ed7。
親PR720は3dec5ab5d02cf45e77c4bbbb14a734f2508be78b。
固定候補は製品Cargo.lock blob 3a573c3dd2bbf2bb3be2956874d101480aec36f4と
apps/desktop/src-tauri/Cargo.toml blob 9a882fc6c33f92c5f1a4d776227491b96f5e86e7。
対応base blobは順に2b07f61aa749594a49dfdffc0209a455d5323857と
88cc777ceb5643f42a5058038d3a463fdebbd713。
共有Gitのgit cat-fileで読めます。製品sourceをPC道具の公開commitへ追加しないでください。
同じバイト列は対象worktreeの.ci-work/root-before.lock/root-after.lockと
desktop-before.toml/desktop-after.tomlにもあります。
この2ファイル全差分、関連する元manifest、audit JSONと進捗資料を確認してください。

## 仕様と確認観点

重大cargo-audit失敗をignore/continue-on-error/監査削除なしで直す最小Cargo manifest/lock更新。
親への固定差分配送だけで、別製品PR/通常CI起動/親branch書込/workflow/署名/Prd/公開/
ライセンス/予算は変更しない。新費用/APIキー/GPU/Civ6/重Cargo/別の未知PC製品treeを使わない。
scrub391の保護判定を守り、source拒否を別経路で迂回しない。

実CIはzip4.6.1（依頼の6.0.0は誤記）を検出しました。
quinn-proto0.11.18、rustls0.23.46へ更新。
zip4はupdater2.10.1のWindows ^4依存で最新stable2.14.0も^4です。
上流merge ba98bfadc4c868c9a7e1a226c83af528e1f11b89（PR3698、2026-10-16）で^9へ
直されているため、そのrev固定でupdaterを使います。tauri^2.12等の上流要件に伴う更新です。
MSRV1.90は固定検査Rust1.95.0で満たします。製品のzip型直接使用はなく、
desktop lib.rs:951でupdater Builder::new().build()を登録しています。
同じDBで追加検出したquick-xml2件も、plist1.10.1→quick-xml0.42.0で解消しました。

全更新がCargoで解決されているか、上流制約から必要な更新だけか、機能を外す監査免除が
ないか、git rev/依存の読む側/MSRV/zip APIの互換性を確認してください。
依存解決用のRust空targetを製品検査と主張していないかも確認してください。

## 実測と限界

cargo-audit0.22.2、DB999ec89d5d3f10f20be14750ec8c2d8b44e0ac9bで、
元root14件/exit1→候補0件/exit0。ignore空。backend/nativeも変更なしで0件/exit0。
metadata --locked成功。updater Windows checkの結果は原ログを読み、進行中なら未完了と
区別してください。上流checksはaudit成功/一部integration失敗なので全successと扱いません。
製品本体の正式CIは最終統合HEADに対して親が実施します。

一次資料は進捗資料のURLと、公式RustSec/Crates.io/Tauri GitHubだけを必要に応じ参照可能。
認証ファイル/秘密/無関係な製品treeは読まないでください。対象ファイルはguard後に読みます。

## 返答

ファイル変更、結果ファイル保存、commit、ToDo更新、別review起動は不要です。
原応答は担当CLIが保存し、実装担当が受領します。
固定baseと2候補blob、読んだ全ファイル、未読範囲、実行した検査と資料確認の区別を記載。
Must / Suggestion / Nitで、場所と具体的修正を報告してください。指摘なしも明記します。
読取完了ならMustがあっても先頭を厳密に「状態: 完了」とし、レビューだけ完了と説明します。
未読や実行失敗なら残りを具体的に書き、成功と扱いません。2500字以内の日本語で回答。
