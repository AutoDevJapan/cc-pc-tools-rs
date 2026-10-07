# 副業 GitHub ブックマーク

このPCの担当がRust CLIで登録したGitHubリポジトリを、Masashi（Profile 3）のChromeに追加する。既存のブックマークの移動・編集・削除は行わない。

## 一度だけ行う導入

1. Chrome右上のプロフィールが「Masashi」になっていることを確認する。
2. `chrome://extensions` を開き、「デベロッパー モード」をオンにする。
3. 「パッケージ化されていない拡張機能を読み込む」で `D:\CC\Tools\chrome-bookmark-queue\extension` を選ぶ。
4. 「副業 GitHub ブックマーク」がオンになったら、Discordに「入れた」と返信する。

Chromeを開いている間は20秒ごとに登録を試す。入れた後はChromeを閉じてもキューが残る。Chrome再起動時にも処理を始める。

拡張のservice workerがクラッシュなどで停止した場合は、Chromeの再起動か拡張画面での再読み込みで再開する。

## 担当が使うコマンド

```powershell
D:/CC/Tools/chrome-bookmark-queue/bookmark_queue.exe add --url https://github.com/dream-num/univer --name "Univer – アプリに表計算・文書編集を組み込む" --folder "開発ライブラリ・インフラ"
D:/CC/Tools/chrome-bookmark-queue/bookmark_queue.exe pending
```

名前は実際のREADMEを読んで「名前 – 何ができるか」にする。用途は22フォルダから選ぶ。同一リポジトリの大小文字・末尾スラッシュ・`.git`・クエリは同じURLにまとめる。CLIの出力は `added`（新規）、`pending`（処理待ちで重複）、`done`（完了済みで重複）。

`pending/*.json` は未登録、`done/*.json` はChromeが登録または既存URLを確認した記録。キューに入れたことと、Chromeに保存できたことを区別する。doneには元の項目とChromeのブックマークID・結果を保存する。

## 仕組みと制限

Rustサーバーは `127.0.0.1:17643` にだけ待ち受ける。自動生成したトークンをAuthorizationヘッダーで確認する。トークンはPC内の `config.json` と拡張の `config.js` だけにあり、Gitに入れない。外部への通信・トークンを含むURL・CORS許可はない。Chrome標準のブックマーク同期はChrome自身の設定に従う。

拡張の権限は `bookmarks` と `http://127.0.0.1:17643/*`。追加先は「ブックマーク バー > ビジネス > 副業」直下の用途フォルダに限定する。親がない、同名の親が複数、管理対象、用途フォルダが移動した場合は保留する。上位フォルダは新しく作らない。同じURLがChrome内のどこかにあれば追加しない。

Chrome 110以降で、20秒ごとの `runtime.getPlatformInfo()` によってservice workerの30秒の休止期限を更新する。通信中も独立して実行するため、追加のalarms権限が不要。再接続時の登録重複はURL確認で防ぐ。Chrome APIの動作は[ブックマークAPI](https://developer.chrome.com/docs/extensions/reference/api/bookmarks)、休止期限は[service workerのライフサイクル](https://developer.chrome.com/docs/extensions/develop/concepts/service-workers/lifecycle)、固定ポート指定は[match patterns](https://developer.chrome.com/docs/extensions/develop/concepts/match-patterns)に基づく。

## 配置と窓なし起動

`cargo build --release -p cc-bookmark-queue` で作った実行ファイルを `D:/CC/Tools/chrome-bookmark-queue/bookmark_queue.exe` へ配置し、`init` を実行する。`init` はトークンを保持して拡張のファイルを更新する。拡張をすでに読み込んでいる場合は更新後に拡張画面の再読み込みを押す。

`install-task` で `CC-ChromeBookmarkQueue` を登録し、すぐ起動する。ログオンと5分ごとの起動、重複実行なし、終了時間なし、失敗時再開。wscript + VBSがサーバーを窓なしで起動し終了まで待つ。すでに同名のタスクがある場合は上書きせずエラーにする。停止は `schtasks /End /TN CC-ChromeBookmarkQueue`、解除はそのタスクと自分のbookmark_queueプロセスだけを対象にする。キュー・done・認証設定は保持する。

## 検証

`cargo test -p cc-bookmark-queue`、`cargo clippy -p cc-bookmark-queue --all-targets -- -D warnings`、`node --test cc-bookmark-queue/tests/*.test.js`。Rustはキューの並行書込・結果の再送・入力検証・HTTP認証とサイズ制限、JavaScriptはChrome APIを模した登録・範囲・重複・通信切断後の再試行を確認する。実プロフィールの導入後は12件がpendingからdoneへ移ったことを確認する。

12件のREADMEは2026-10-07にMasashiのChromeで確認した。調査タブ798073964は閉鎖済み。分類と表示名は `candidates-2026-10-07.json`。
