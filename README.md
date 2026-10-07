# PCのRust補助ツール

このPCで使っているDiscordの質問・作業一覧ツールと、そのビルドに必要なRustモジュールのソースを保存する非公開リポジトリです。実行記録・認証情報・実行ファイルは保存しません。

- `cc-todo`: 共有ToDoと担当が記録する進み具合。
- `cc-scheduler`: ToDoから作業を起動する既存の共通処理。
- `cc-ask`: `discord_ask.exe`。質問の受付・返信の反映・定期処理・作業状況の一覧。
- `cc-chrome`: オーナーのChromeで、担当が開いたタブを記録する補助。
- `cc-job-policy`: ジョブの実行範囲と権限の共通設定。
- `cc-task-title`: オーナー向けの表示名と案件名の除去。
- `cc-live`: 実機検査とプロセス管理の共通処理。
- `cc-ocx-watchdog`: opencodexの応答停止を検出し、対象プロセスを終了してサービスの復旧を確認するRust監視。
- `chatgpt-web`: ChatGPTのブラウザ版（専用プロフィールのChrome）へ質問を送り、回答の本文を返すCLIと毎日のログイン確認。

稼働中のソースは `D:/CC/Tools/rs/` にあります。Windows上で `cargo test --manifest-path cc-ask/Cargo.toml`、`cargo build --release --manifest-path cc-ask/Cargo.toml` を実行します。ビルドと配置の前に別モデルでレビューし、Mustを解消します。

作業一覧の仕様と状態ファイルは [BOARD.md](cc-ask/BOARD.md) を参照してください。初回の検証は2026-10-06にWindowsで全430件成功、配布物を指定する必要がある既存の1件は対象の配布物がないため未実行です。一覧の11件はすべて成功しました。別モデルgpt-6-lunaの再レビューでMustは0件でした。

新しい有料の検証環境は設けず、このPCで検査します。機能テストのGitHub Actions実行設定はありません。GitHubの標準CodeQL検査は別に確認します。
