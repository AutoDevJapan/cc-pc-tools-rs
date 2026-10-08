# Gitを含む公開配布物の所有フォルダーを退避する

共有ToDo #2027 の復旧用モジュール。通常の `safe-delete.exe` のGit保護は変更しない。このRustモジュールは、保存済み仕様で片付けを許可されたToDo #2001の所有IDと2つの取得物だけを扱う。一般的な `.git` 許可、既存フォルダーの所有登録、削除機能はない。

対象の所有ID、プロジェクト、用途、Temp直下のパス、Go形式のチェックサム、Windowsのvolume/file IDを確認する。公開取得元とcommitは仕様の記録に固定している。追跡ファイルの変更、未送信commit、stash、追加のGit管理領域、worktree・submodule・外部object参照、リンク・外部hardlink、保護対象OpenMausBotや認証用ディレクトリー、クライアント資源は拒否する。試験専用の追跡外ファイルも含めて全内容を退避先に保持する。

```powershell
# 実行ファイルを既存のsafe-delete.exeと同じ場所に置く。
D:/CC/Tools/safe-delete/safe-delete-recovery.exe plan --id dee85bb67fa66449ba0330f0fbe049f1
# 返されたMove-Item命令を別の通常実行ツール呼び出しに提出する。
# 成功後に:
D:/CC/Tools/safe-delete/safe-delete-recovery.exe finish --id dee85bb67fa66449ba0330f0fbe049f1
```

`plan` は全検証が通ってから固定の同じドライブの「削除希望」に新しいフォルダーを作り、未完了の理由と元ツリーのメタデータ一覧を保存する。通常の実行ツールを通す `Move-Item -LiteralPath` だけを返す。元の `plan` / `quarantine` を拒否された後に再試行する仕組みではなく、Git管理領域を含む全内容を保持する復旧手順を明示的に実装したもの。

`finish` は元パス不在、移動先の同一volume/file ID、一覧の一致、Gitの状態を再確認してから理由を完了にし、既存所有記録を原子的に `quarantined` へ更新する。移動後に担当が止まった場合も `finish` から再開できる。移動先を差し替えた場合は拒否する。失敗のまま完了記録は作らない。

検査後の同一ユーザーによるデータ変更を防ぐセキュリティ境界ではない。所有サーバー停止済み、他の担当が使っていないという元仕様の前提を維持し、計画の直後に実行する。メタデータだけで試験認証値を開かず、Git検査の出力は成功時にだけ内部で比較し、報告には出さない。

```powershell
# safe-delete createで作成した専用フォルダーを指定。
$env:CARGO_TARGET_DIR='<所有フォルダー>/target'
$env:SAFE_DELETE_TEST_ROOT='<所有フォルダー>'
cargo nextest run --locked --workspace --test-threads 10
cargo test --locked --workspace --doc
cargo clippy --locked --all-targets -- -D warnings
cargo build --locked --release
```

テストは隔離した所有フォルダーの中に試験用receiptとGitリポジトリーを作る。実物の所有記録を偽造しない。試験用コードだけにあるDrop処理が、作成したcaseの直下境界と識別番号を確認して試験データを片付ける。リンク先には入らず、読み取り専用属性の解除も架空の試験データだけで行う。この処理は本番の実行ファイルには含まれない。残ったビルド生成物の所有フォルダーは通常のsafe-delete手順で片付ける。

今回の旧版の試験が残したcaseは、同じ所有フォルダーだけに限定した無視指定の検査 `retire_prior_generated_fixtures` で一度だけ片付ける。拒否された動画試行データはこの検査の対象にならない。

移動前に担当が止まった場合も、移動票と内容が完全一致する場合だけ `plan` から再開できる。CIの判定用プログラムは試験factoryだけが環境変数で指定し、本番のクライアント判定は固定のscrub391.exeを使う。
