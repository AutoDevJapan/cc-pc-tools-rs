# Codex TUIの並列担当の進捗表示

## 起動済みの公式Codexから確認する（ToDo3021）

共有ToDoの一覧を出す `/progress` と常設の一覧は、このrepositoryの独自patchです。公式CLIの `/status` はその会話の状態、`/ps` はその会話の背景端末を表示します。公式の機能で共有ToDo全体が表示されるとは扱いません。公式の説明: https://learn.chatgpt.com/docs/developer-commands?surface=cli 。

起動済みの公式TUIでは、入力欄へ `!codex progress` を入力すると、モデルを呼ばずに共有進捗を一度だけ表示します。番号を指定する場合は `!codex progress --task 3020` です。PowerShell/CMDでは先頭の `!` を外します。取得処理の実行は最大6秒、取得側の期限は4秒です。新しい常駐・更新ループを追加しません。exit1でも確認できたデータを表示し、部分取得の警告を残します。実行を確認した担当と、起動待ち・見張り・稼働未確認を分けます。元の指示・名称・パス・認証情報は出力しません。

`codex progress --check` はTUIの起動判定とcollectorのハッシュ一致を示します。公式版がmanifestの版と違う場合、従来どおり公式TUIを使います。無効な組合せを受け入れるためのmanifest変更や版の偽装はしません。`codex progress` は独立してcollectorのハッシュを検証するため、公式TUIの版が進んでも利用できます。exec・管理コマンドは公式経路を使います。

2026-10-20の調査では、公式npmは0.163.0、配備済みの進捗TUIは0.162.1でした。npm側のPowerShell/CMD shimが進捗launcherを呼ぶ変更も失われていました。2904のTUI（SHA256 `6f21e83ac9fac5ed64e5b06667a408f671da65ac9d1094e3f84b156841cdc6a1`）とcollector（SHA256 `581c813d3321c47580d21f07dd7ef7a3b654bd5d4d761cdbf7bb275cd8e7f8d1`）は実物とmanifestが一致しました。2904のproducer-first修正を未配備とは扱いません。0.163.0へのRust patch移植・再ビルドと、起動済みの公式TUIへの常設欄の反映は未実施です。

入口の復旧は `activation/install.ps1 -Launcher <このsourceからbuildしたexe>` で行います。既知の旧shim、または既に同じshimであることをハッシュ照合してから、元のlauncher/PS/CMDを `D:/CC/Tools/codex-progress-tui/backup-3021-<日時>` に退避し、同じファイルシステム上の一時ファイルから置換します。未知のshimは上書きしません。manifest、Codex本体、collector、hostは変更しません。公式npm/OpenCodexの更新後に `codex progress --check` が使えなければ、shimのハッシュを確認してこの有限導入を再実行します。未知の変更があれば内容を確認して既知ハッシュを更新し、監視や自動上書きを追加しません。

既に起動したlauncherがファイルを保持している場合、置換は失敗することがあります。その場合は配備を止めて原版を保ち、現在の会話を終了させません。`progress` はコマンドの先頭に置きます。PowerShell関数が加える `--no-daemon` だけは前にあっても認識します。`-c` の値や既存execの入力を新コマンドとして捕捉しません。

3021ではGo test/vet/build、PowerShell構文、実際のPowerShell関数とCMD経由の個別取得、公式version/exec helpを確認しました。別モデルの初回と差分確認はMust0です。隠れた新規PTYで公式0.163.0を起動し、`!codex progress --task 3020` と `!codex progress` の会話内実表示を確認しました。部分取得の警告、計測済み15%と未計測の区別、実行確認2件と待ち・見張り9件と未確認123件の区別、`/quit`の終了0を確認しました。検査用の会話だけを終了し、本人の現在の会話は操作していません。常設欄と独自slashコマンドの0.163.0対応・配備はToDo3023と3021の未完了条件です。

承認された画像の方向に合わせ、会話と入力欄の間に進捗一覧を常設します。共有ToDoの確認済みの進捗だけを使い、未計測は0%と表示しません。「テスト中」「実装中」「元の作業記録を確認中」など自然な日本語で、自分の会話の担当と別セッションの担当を表示します。

幅と高さに合わせて一覧が縮み、入力欄・カーソル・承認画面を優先します。`/progress` は表示時点で処理中と確認できた担当の一覧を開く読み取り専用コマンドです。待機・終了・状態不明・報告だけの担当を除外し、取得が完了して0件なら「作業中なし」と表示します。取得失敗や12秒を超えたsnapshotの行を、現在の稼働として表示しません。部分取得なら確認できた新しい処理だけを表示し、取得不備も示します。0件でも取得が不完全なら「稼働の確認待ち」と表示します。

外部担当は取得処理の `--active-only` を使い、TUIでも状態・処理イベントの時刻・待機理由を確認します。ネイティブ担当はlive接続のActive状態で、承認/本人入力待ちがなく、30秒以内に思考やツールの実行イベントが届いたものだけを表示します。文だけの報告、終了、待機への遷移で稼働の根拠を消します。イベントのない長時間の処理は、確認できない間は一覧から外れます。担当自体を停止する変更ではありません。

cacheの `snapshot-active-v1.json` と `acquisition-active.lock` は旧版と分けます。Goの取得処理は旧TUIの `snapshot-v1.json` にも処理中だけを出力します。ネイティブ担当の絞り込みと空一覧の表示変更は、新しいRust版の通常起動から適用します。画面の配置、入力、OAuth、execの起動経路は維持します。

## 構成

`../agent-progress` はGoの単発の取得処理です。元データは変更しません。`patches/codex-0.162.1-progress.patch` は公式CodexのRust TUIへの変更です。`launcher` はGoの起動処理です。公式の版と更新TUI・取得処理・公式code-mode-hostのSHAが一致した場合だけ対話TUIを更新版へ回し、exec・app-server・ログイン・更新・管理コマンドは公式版を使います。既存OpenCodexのOAuth・proxy起動・引数・作業場所を保ち、新しい支払いを発生させません。

## 再現

`source.json` のGit commit・patch SHA・toolchainを照合します。公式tagのCargo.lockが内部workspaceの版を0.0.0で保持していたため、0.162.1へ合わせた161項目だけを含めます。外部の依存版とchecksumは変更しません。

公式 https://github.com/openai/codex の `rust-v0.162.1`（commit `092d3acd6bec3e3a14bdc7e7a2810ab628ab759d`）を新しい自分用ディレクトリへ取得し、HEADを照合した後に `git apply --check` と `git apply` を使います。無関係の作業ツリーをreset・cleanしません。

その `codex-rs` で順に実行します。

```powershell
# The public upstream .bazelrc uses 8 MiB for Windows async test fixtures.
$env:RUST_MIN_STACK='8388608'
$env:CARGO_PROFILE_RELEASE_LTO='false'
$env:CARGO_PROFILE_RELEASE_DEBUG='0'
$env:CARGO_PROFILE_RELEASE_STRIP='symbols'
$env:CARGO_PROFILE_RELEASE_CODEGEN_UNITS='16'
$env:CODEX_JOBS_REAL_CARGO=Join-Path $env:USERPROFILE '.cargo/bin/cargo.exe'
$env:CODEX_JOBS_CARGO_LOCK_DIR='D:/CC/Tools/codex-jobs'
$env:CARGO_TARGET_DIR=Join-Path (Get-Location) 'target'
$progressCargoGate='D:/CC/Tools/codex-jobs/cargo_gate.exe'
& $progressCargoGate nextest run --locked --release -p codex-tui -E 'test(parallel_progress)'
& $progressCargoGate clippy --locked --release -p codex-tui --lib -- -D warnings
& $progressCargoGate test --locked --release -p codex-tui --doc
& $progressCargoGate build --locked --release -p codex-cli --bin codex
```

Goは各ディレクトリで `go test ./...`、`go vet ./...`、`go build` を実行します。元の実行版と設定を退避し、Dドライブへ更新版と同じ公式版の `codex-code-mode-host.exe` を配置します。manifestは実ファイルのSHAを記録します。公式版は削除しません。

Windowsのrelease検査で、単独TUIのasync型の展開が再帰上限128を超える場合があります。TUIライブラリと同じ上限256を単独TUIと配布CLIの入口にも設定しています。処理や検査を省略する変更ではありません。

## 運用

cacheは `D:/CC/Work/parallel-agent-progress-runtime`、実行版は `D:/CC/Tools/agent-progress` と `D:/CC/Tools/codex-progress-tui` です。2秒の画面更新と4秒の共有cacheを使い、取得はプロセス内とOSロックで重複させません。子処理は窓を出さず、取得4秒・終了6秒の期限を持ちます。

旧TUIへ外からコードを読み込む仕組みはありません。正常な起動・再開へ更新版を適用し、今の会話を強制終了しません。自分の隠しConPTYで実起動と画面幅の変更を確認します。確認していない現画面の切替を完了と報告しません。

本人のaと日本語の指定は元ToDo2543へ出所付きで保存しました。古い作業の消失は通常の起動待ちと区別します。顧客名・私的なアカウント・元の指示文・認証情報は表示データに入れません。公式LICENSEとNOTICEを `LICENSE.upstream`、`NOTICE.upstream` に保持します。

## 検証の範囲

今回の絞り込み修正は、ソース69a95baで関連nextest 8件、TUIのClippy、doctest（失敗0・既存ignore 2）、配布CLIのrelease buildが成功しました。独立したtargetと既存gate・sccacheを使い、全workspaceの検査を重複実行していません。Goの全件検査・vetと、起動処理の検査も成功しました。

同ソースから作った候補実行版SHA256 `6f21e83ac9fac5ed64e5b06667a408f671da65ac9d1094e3f84b156841cdc6a1` をコピーし、新しい隠しConPTYで起動・実データの`/progress`・幅120→60→148の変更・未送信入力の保持・`/quit`と終了0を確認しました。取得処理SHA256 `581c813d3321c47580d21f07dd7ef7a3b654bd5d4d761cdbf7bb275cd8e7f8d1` は旧TUI本体と配置済み起動処理を通した受入にも合格し、待機・確認中は0件でした。可視ウィンドウは0件で、所有したPTY・パイプ・一時物を回収しました。本人の実行中の会話は再開・終了していません。

検査用セッションだけに `-c check_for_update_on_startup=false` を指定します。更新案内が出た場合は入力せず検査を止めます。起動後の更新案内を入力の不具合と誤判定しないためです。

前回のソース3133424の全体検査で、公開ソースのsnapshotに残っていた版0.0.0と、この公開tagの版0.162.1の不一致が見つかりました。大きいasync検査の一部も、Windowsのdebug実行で公式指定の8MiBを超えていました。製品の版やassertを偽装せず、正しい版の期待値と専用の検査スレッドで修復しました。最終TUI検査は5,650合格、8skip、doctestは失敗0・2ignore、全targetのClippyも成功しました。これは製品のメモリーリーク修復の証明ではありません。

全件の検査では一時フォルダーをホームの`.git`の外に置き、プロセスだけの`NO_COLOR`・`TERM`・`CODEX_*`を外しました。ownerの`.git`や設定は読み書きしません。Windowsでパイプの閉じ待ち344件が検出され、1件は内蔵app-serverが起動したgitの子であることを観測しました。ほかの内訳は未確認ですが、検査終了後にこのcloneの担当プロセスが0件であることを確認しました。

旧TUIと同じ引数で取得元の世代が実際に変化し、処理中以外の行が0件であることも確認しました。モデルを呼ぶネイティブ担当の状態遷移の実測は、このConPTY検査に含めません。待機・報告・終了・取得失敗・空一覧の表示境界は関連単体検査で確認しています。旧TUIの外部一覧はGoの通常更新から変わります。ネイティブ担当の絞り込みと空一覧の表示変更は、新しいRust版の通常起動から適用します。
