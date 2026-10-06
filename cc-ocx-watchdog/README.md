# opencodex の応答監視

Rustで、同じWindowsプロセスの `/healthz` が20秒以内に応答しない状態を監視します。3回続いたときだけ、プロセスの実行ファイル・起動時刻・PIDファイル・ポートの所有者を照合して、そのプロセスハンドルを終了します。応答を返す503、不正な応答、起動後120秒以内のプロセス、意図的に停止しているサービスは終了対象にしません。

通常は既存のサービスが5秒後に再起動します。手動起動でサービスの親が失われていた場合も復旧できるよう、終了後に `opencodex-proxy` の定期タスクを起動します。復旧待ちの間に起動が失敗した場合は60秒後に再試行します。終了後120秒は次の終了を抑制します。

## 検証・配置

```powershell
cargo test --locked --manifest-path cc-ocx-watchdog/Cargo.toml
cargo clippy --locked --manifest-path cc-ocx-watchdog/Cargo.toml --all-targets -- -D warnings
cargo build --release --locked --manifest-path cc-ocx-watchdog/Cargo.toml --bin ocx_health_watchdog
```

別モデルのレビューでMustがなくなってから、実行ファイルを `D:/CC/Tools/ocx-health-watchdog/ocx_health_watchdog.exe` に配置します。`install` はログオン時の定期タスク `CC-OcxHealthWatchdog` を登録して起動します。タスクは同一アカウントの権限で動き、異常終了時は1分後に再起動します。releaseバイナリはWindowsのGUIサブシステムを指定するため、コンソール窓を作りません。子プロセスは `CREATE_NO_WINDOW` で起動します。

`check` は1回だけ検査し、検査結果を保存します。監視が既に動いている場合は多重起動を拒否するので、状態確認には `status.json` を読みます。`watch --config <絶対パス>` では隔離検査用の設定を読み込みます。設定は `Config` の全フィールドを持つJSONです。配布する既定値は20秒・10秒間隔・起動猶予120秒・終了間隔120秒です。

## 記録

同じ配置先の `status.json` に最新の検査結果を保存します。`events.jsonl` は検査失敗・終了意図・終了結果・サービス起動・別プロセスの正常応答を保存します。成功時のメモリとCPU使用時間は1分ごとに記録します。5MiBを超えると直前1世代だけを保持します。

復旧意図は終了前にディスクへ同期して `restart-pending.json` へ保存します。監視が再起動しても、別プロセスの正常応答を照合して未処理の復旧を引き継ぎます。報告に失敗した記録は残し、10分間隔で再試行します。繰り返した復旧は1件の文章へまとめ、Discordの「後片付けの記録」へ通知・メンションなしで投稿します。本文・認証情報・アカウント情報を監視記録へ保存しません。

`ocx_watchdog_fixture` は検証専用のRust製サーバーです。指定ポートに健康応答または応答停止を作ります。10100では起動を拒否します。製品のバイナリとして配置しません。

調査の確定事項と未確定事項は [調査記録](INVESTIGATION.md) を参照してください。
