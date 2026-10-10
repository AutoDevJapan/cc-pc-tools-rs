# ChatGPT ブラウザ版への質問

`chatgpt_web` は、ログイン済みのオーナーの Chrome（既存の拡張機能経由）で ChatGPT のブラウザ版を開き、新しいチャットに質問を送り、回答の本文を Markdown で標準出力に返します。API キーは使いません。設計の相談・レビューの2人目・文章の推敲・調べもののような、ファイル操作の要らない作業を Codex から移すために使います。使いどころは Skill `web-thinking-offload` に書いています。

```powershell
D:/CC/Tools/chatgpt-web/chatgpt_web.exe ask --prompt-file <UTF-8のファイル> [--model <名前>] [--timeout <秒、既定900>]
D:/CC/Tools/chatgpt-web/chatgpt_web.exe status   # 最後に確かめたログイン状態（ブラウザを起動しない）
D:/CC/Tools/chatgpt-web/chatgpt_web.exe check    # 画面でログインを確かめて記録する（毎日の定期確認）
D:/CC/Tools/chatgpt-web/chatgpt_web.exe login    # オーナーの Chrome で状態を再確認する
D:/CC/Tools/chatgpt-web/chatgpt_web.exe stop     # この道具が作ったタブだけを片付ける
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
- 本番では既存の Chrome 拡張機能に接続します。Chrome を起動したり、プロフィール・保護設定・起動引数を変えたりしません。作成したタブだけを記録し、途中で失敗した場合も閉じます。閉じられなかった記録は次の check または stop で再試行します。後片付けには1タブにつき最大3秒の猶予があります。
- `--model` は ChatGPT の URL の `?model=` に渡します。チャットの履歴は ChatGPT 側に残ります。
- 回答の完了は、生成中の停止ボタンが消え、回答の長さが2回続けて変わらないことで判断します。
- 状態は state.json に保存します。道具は Discord に直接投稿しません。確認に失敗した場合、26時間を超えた場合、旧プロフィールの記録の場合は準備済みとして扱いません。会話本文以外のアカウント欄で Plus を確認できるまで送信を止めます。
- オーナーの Chrome の言語設定は変えません。画面の判定と回答の変換は既存の src/page.js を使います。

## 毎日のログイン確認

定期タスク `CC-ChatGptWebLoginCheck` が毎日 9:15 に `check-hidden.vbs`（`wscript.exe`、窓なし）から `chatgpt_web check` を実行します。目的の Plus が画面で確認できた日に状態を保存し、その後は status が 0 を返します。未確認・別プランの場合は送信せず、元ToDo 1974に実送受信の受入を残します。

## 検証

```powershell
cargo nextest run --locked -p chatgpt-web
cargo clippy --locked -p chatgpt-web --all-targets -- -D warnings
cargo build --release --locked -p chatgpt-web
```

`tests/fixtures/real-*.html` は 2026-10-07 に実際の chatgpt.com（未ログインの画面）から記録した画面です（スクリプトと見た目の属性を除去）。`chat2026*.html` はその作り（`textarea[name=prompt]`・aria-label が切り替わる送信ボタン・`li[data-message-role]`・`[data-assistant-markdown]`・`data-message-complete`）を、`chat*.html` はそれ以前の作り（`#prompt-textarea`・`data-testid`）を模しています。試験は実際の Chrome を一時プロフィールで起動し、回答の取得・ログイン切れ・停止・エラー・上限・時間切れ・人間確認・ロック待ちを確かめます。Chrome が無い環境では画面の試験を飛ばします。

画面の作りが変わったら、保守用の `chatgpt_web probe --url <URL> wait:10 "page:state()" "page:JSON.stringify(poll())" <JSのファイル>` で調べ、`src/page.js` と記録を更新します。`ask --allow-logged-out` は未ログインの画面でも送る保守用の指定で、画面の作りの確認だけに使います（ログイン状態は記録しません）。

環境変数 `CHATGPT_WEB_HOME`（保存先）、`CHATGPT_WEB_URL`（開く URL）、`CHATGPT_WEB_CHROME`（Chrome の場所）、`CHATGPT_WEB_DISCORD`（`discord_ask.exe` の場所、`off` で記録しない）で試験用に切り替えます。

HTML試験の file:/// URL、または CHATGPT_WEB_FIXTURE=1 と数値ポートの 127.0.0.1 の HTTP URL に、CHATGPT_WEB_HEADLESS=1 を指定した場合だけ、隔離した Chrome を使います。本番の https:// URL ではこの設定があっても既存の Chrome 拡張機能へ接続します。未ログイン送信の保守用指定は HTML 試験に限定し、本番では拒否します。実際の Plus での送受信は未確認です。
## 配置

```powershell
Copy-Item target/release/chatgpt_web.exe D:/CC/Tools/chatgpt-web/chatgpt_web.exe
Copy-Item chatgpt-web/check-hidden.vbs D:/CC/Tools/chatgpt-web/check-hidden.vbs
$a = New-ScheduledTaskAction -Execute wscript.exe -Argument '"D:\CC\Tools\chatgpt-web\check-hidden.vbs"'
$t = New-ScheduledTaskTrigger -Daily -At 9:15
$s = New-ScheduledTaskSettingsSet -StartWhenAvailable -ExecutionTimeLimit (New-TimeSpan -Minutes 30)
Register-ScheduledTask -TaskName CC-ChatGptWebLoginCheck -Action $a -Trigger $t -Settings $s
```
