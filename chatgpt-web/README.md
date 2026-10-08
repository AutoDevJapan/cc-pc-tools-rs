# ChatGPT ブラウザ版への質問

`chatgpt_web` は、専用プロフィールの Chrome（`D:/CC/Apps/chatgpt-web/profile`）で ChatGPT のブラウザ版を開き、新しいチャットに質問を送り、回答の本文を Markdown で標準出力に返します。API キーは使いません。設計の相談・レビューの2人目・文章の推敲・調べもののような、ファイル操作の要らない作業を Codex から移すために使います。使いどころは Skill `web-thinking-offload` に書いています。

```powershell
D:/CC/Tools/chatgpt-web/chatgpt_web.exe ask --prompt-file <UTF-8のファイル> [--model <名前>] [--timeout <秒、既定900>]
D:/CC/Tools/chatgpt-web/chatgpt_web.exe status   # 最後に確かめたログイン状態（ブラウザを起動しない）
D:/CC/Tools/chatgpt-web/chatgpt_web.exe check    # 画面でログインを確かめて記録する（毎日の定期確認）
D:/CC/Tools/chatgpt-web/chatgpt_web.exe login    # 見える窓でログイン画面を開く
D:/CC/Tools/chatgpt-web/chatgpt_web.exe stop     # 専用プロフィールの Chrome を閉じる
```

| 終了コード | 意味 |
| --- | --- |
| 0 | 回答あり（標準出力が回答の本文） |
| 1 | 失敗（送信できない・回答中のエラー表示・人間確認・画面の作りの変化） |
| 2 | 引数の誤り（質問のファイルが無い・空・UTF-8 でない、モデル名の文字） |
| 3 | ログインが必要（未ログイン・ログイン切れ） |
| 4 | アカウントが止められている表示 |
| 5 | 時間切れ |
| 6 | ほかの質問が実行中で、時間内に始められない |
| 7 | 利用上限 |

## 動き

- 同時に動くのは1件だけです。`D:/CC/Apps/chatgpt-web/ask.lock` のファイルロックで順番を待ち、待ち時間も `--timeout` に含めます。
- 専用プロフィールの Chrome が CDP 付きで動いていれば再利用し、なければ画面外（-32000,-32000）の窓で起動します。自分で起動した Chrome は終了時に閉じます。`login` で開いた窓は閉じません。
- `--model` は ChatGPT の URL の `?model=` に渡します。チャットの履歴は ChatGPT 側に残ります。
- 回答の完了は、生成中の停止ボタンが消え、回答の長さが2回続けて変わらないことで判断します。
- ログイン状態が変わったとき（ログイン確認・ログイン切れ・停止）だけ、`state.json` を更新して Discord の「後片付けの記録」へ通知なしで1件残します。同じ状態が続く間は送りません。
- Chrome は `--lang=en-US` で起動し、英語の画面を前提に判定します。画面の判定と回答の Markdown への変換は `src/page.js` にあります。

## 毎日のログイン確認

定期タスク `CC-ChatGptWebLoginCheck` が毎日 9:15 に `check-hidden.vbs`（`wscript.exe`、窓なし）から `chatgpt_web check` を実行します。ログインできるようになった日に「ログインを確認しました」と記録され、その後は `status` が 0 を返します。

## 検証

```powershell
cargo nextest run --locked -p chatgpt-web
cargo clippy --locked -p chatgpt-web --all-targets -- -D warnings
cargo build --release --locked -p chatgpt-web
```

`tests/fixtures/real-*.html` は 2026-10-07 に実際の chatgpt.com（未ログインの画面）から記録した画面です（スクリプトと見た目の属性を除去）。`chat2026*.html` はその作り（`textarea[name=prompt]`・aria-label が切り替わる送信ボタン・`li[data-message-role]`・`[data-assistant-markdown]`・`data-message-complete`）を、`chat*.html` はそれ以前の作り（`#prompt-textarea`・`data-testid`）を模しています。試験は実際の Chrome を一時プロフィールで起動し、回答の取得・ログイン切れ・停止・エラー・上限・時間切れ・人間確認・ロック待ちを確かめます。Chrome が無い環境では画面の試験を飛ばします。

画面の作りが変わったら、保守用の `chatgpt_web probe --url <URL> wait:10 "page:state()" "page:JSON.stringify(poll())" <JSのファイル>` で調べ、`src/page.js` と記録を更新します。`ask --allow-logged-out` は未ログインの画面でも送る保守用の指定で、画面の作りの確認だけに使います（ログイン状態は記録しません）。

環境変数 `CHATGPT_WEB_HOME`（保存先）、`CHATGPT_WEB_URL`（開く URL）、`CHATGPT_WEB_CHROME`（Chrome の場所）、`CHATGPT_WEB_DISCORD`（`discord_ask.exe` の場所、`off` で記録しない）で試験用に切り替えます。

画面の試験は必ず `CHATGPT_WEB_HEADLESS=1` を子プロセスへ渡し、Chrome を `--headless=new` で起動します。窓やタスクバーの表示を作らず、実際の Chrome で操作と終了を確かめます。手動で試験用に起動する場合も同じ環境変数を指定します。未指定の場合、本番の `ask`・`check`・`probe` は従来の画面外の窓、`login` は見える窓で起動します。既に動いている Chrome を再利用する動きは変わりません。

## 配置

```powershell
Copy-Item target/release/chatgpt_web.exe D:/CC/Tools/chatgpt-web/chatgpt_web.exe
Copy-Item chatgpt-web/check-hidden.vbs D:/CC/Tools/chatgpt-web/check-hidden.vbs
$a = New-ScheduledTaskAction -Execute wscript.exe -Argument '"D:\CC\Tools\chatgpt-web\check-hidden.vbs"'
$t = New-ScheduledTaskTrigger -Daily -At 9:15
$s = New-ScheduledTaskSettingsSet -StartWhenAvailable -ExecutionTimeLimit (New-TimeSpan -Minutes 30)
Register-ScheduledTask -TaskName CC-ChatGptWebLoginCheck -Action $a -Trigger $t -Settings $s
```
