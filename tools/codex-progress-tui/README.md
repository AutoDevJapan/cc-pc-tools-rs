# Codex TUIの並列担当の進捗表示

承認された画像の方向に合わせ、会話と入力欄の間に進捗一覧を常設します。共有ToDoの確認済みの進捗だけを使い、未計測は0%と表示しません。「テスト中」「実装中」「元の作業記録を確認中」など自然な日本語で、自分の会話の担当と別セッションの担当を表示します。

幅と高さに合わせて一覧が縮み、入力欄・カーソル・承認画面を優先します。`/progress` は表示時点の全体一覧を開く読み取り専用コマンドです。取得失敗時は前回の世代と古さを示し、部分取得なら確認できた新しい状態を更新します。

## 構成

`../agent-progress` はGoの単発の取得処理です。元データは変更しません。`patches/codex-0.162.1-progress.patch` は公式CodexのRust TUIへの変更です。`launcher` はGoの起動処理です。公式の版と更新TUI・取得処理・公式code-mode-hostのSHAが一致した場合だけ対話TUIを更新版へ回し、exec・app-server・ログイン・更新・管理コマンドは公式版を使います。既存OpenCodexのOAuth・proxy起動・引数・作業場所を保ち、新しい支払いを発生させません。

## 再現

`source.json` のGit commit・patch SHA・toolchainを照合します。公式tagのCargo.lockが内部workspaceの版を0.0.0で保持していたため、0.162.1へ合わせた161項目だけを含めます。外部の依存版とchecksumは変更しません。

公式 https://github.com/openai/codex の `rust-v0.162.1`（commit `092d3acd6bec3e3a14bdc7e7a2810ab628ab759d`）を新しい自分用ディレクトリへ取得し、HEADを照合した後に `git apply --check` と `git apply` を使います。無関係の作業ツリーをreset・cleanしません。

その `codex-rs` で順に実行します。

```powershell
# The public upstream .bazelrc uses 8 MiB for Windows async test fixtures.
$env:RUST_MIN_STACK='8388608'
cargo nextest run --locked -p codex-tui -E 'test(parallel_progress)'
cargo clippy --locked -p codex-tui --lib -- -D warnings
$env:CARGO_PROFILE_RELEASE_LTO='false'
$env:CARGO_PROFILE_RELEASE_DEBUG='0'
$env:CARGO_PROFILE_RELEASE_STRIP='symbols'
$env:CARGO_PROFILE_RELEASE_CODEGEN_UNITS='16'
cargo build --locked --release -p codex-cli --bin codex
```

Goは各ディレクトリで `go test ./...`、`go vet ./...`、`go build` を実行します。元の実行版と設定を退避し、Dドライブへ更新版と同じ公式版の `codex-code-mode-host.exe` を配置します。manifestは実ファイルのSHAを記録します。公式版は削除しません。

## 運用

cacheは `D:/CC/Work/parallel-agent-progress-runtime`、実行版は `D:/CC/Tools/agent-progress` と `D:/CC/Tools/codex-progress-tui` です。2秒の画面更新と4秒の共有cacheを使い、取得はプロセス内とOSロックで重複させません。子処理は窓を出さず、取得4秒・終了6秒の期限を持ちます。

旧TUIへ外からコードを読み込む仕組みはありません。正常な起動・再開へ更新版を適用し、今の会話を強制終了しません。自分の隠しConPTYで実起動と画面幅の変更を確認します。確認していない現画面の切替を完了と報告しません。

本人のaと日本語の指定は元ToDo2543へ出所付きで保存しました。古い作業の消失は通常の起動待ちと区別します。顧客名・私的なアカウント・元の指示文・認証情報は表示データに入れません。公式LICENSEとNOTICEを `LICENSE.upstream`、`NOTICE.upstream` に保持します。

## 検証の範囲

追加の5検査とTUIのClippyは合格しました。最終ソース3133424からビルドした実行版SHA256 `7eda7d645c4dcd7cae038451d60b7ab5e89e28e6868876060d2f5a4af5b4079b` をコピーして、新しい隠れたConPTYで起動・`/progress`・幅120→60→148の変更・未送信入力の保持・`/quit`と終了0を確認しました。配置済みGoの起動処理を経由した同じ検査も成功しました。可視ウィンドウは0件で、所有したPTY・パイプ・一時物を回収しました。本人の実行中の会話は再開・終了していません。

全体の検査で、公開ソースのsnapshotに残っていた版0.0.0と、この公開tagの版0.162.1の不一致が見つかりました。大きいasync検査の一部も、Windowsのdebug実行で公式指定の8MiBを超えていました。製品の版やassertを偽装せず、正しい版の期待値と専用の検査スレッドで修復しました。最終TUI検査は5,650合格、8skip、doctestは失敗0・2ignore、全targetのClippyも成功しました。これは製品のメモリーリーク修復の証明ではありません。

全件の検査では一時フォルダーをホームの`.git`の外に置き、プロセスだけの`NO_COLOR`・`TERM`・`CODEX_*`を外しました。ownerの`.git`や設定は読み書きしません。Windowsでパイプの閉じ待ち344件が検出され、1件は内蔵app-serverが起動したgitの子であることを観測しました。ほかの内訳は未確認ですが、検査終了後にこのcloneの担当プロセスが0件であることを確認しました。

取得元の世代が実際に変化する受入と、モデルを呼ぶネイティブ担当の状態遷移の実測は、このConPTY検査に含めません。取得失敗時の保持・状態表現・表示の境界は単体検査で確認しています。通常のPowerShell/CMD起動へ適用済みで、双方のversion終了0を確認しました。起動済みのTUIへ外から読み込ませる機能はないため、今のTerminalへ既に表示されたとは報告しません。
