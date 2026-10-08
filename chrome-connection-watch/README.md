# Chrome接続の見張り

Goで5分ごとに実接続を検査し、`D:/CC/Tools/owner-chrome/connection-status.json`へ結果を書きます。終了0はChrome拡張の接続確認、1は未接続、2は記録失敗です。

普段選ばれているプロフィールの拡張、Windowsのネイティブ接続登録、実行ファイル、Codexが記録した画面操作の接続口を確認します。実接続は既存の`owner_chrome inventory`で確認し、ファイルやChrome本体が存在するだけでは成功にしません。ページの本文・URL・アカウント名・認証情報を記録しません。

Chromeの再起動、タブの作成・終了、登録や設定の変更、通知は行いません。終了済みアプリが作った接続先や、別のプロフィールにしか拡張がない状態を識別できます。`desktop_native_reference_stale`は画面操作の設定についての記録で、Chrome拡張の実接続と区別します。

ビルドは`go test ./...`、`go vet ./...`、`go build -ldflags '-H=windowsgui' -o chrome_connection_watch.exe .`。Windows GUI形式でコンソール窓を出さず、検査用の子プロセスも窓なしで起動します。
