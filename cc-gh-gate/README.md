# gh 公開ゲート（gh_gate.exe）

`gh` で公開する本文・題名から秘密の値を `[REDACTED]` に置き換えてから、本物の `gh.exe` を実行します。終了コードは本物の gh のものを返します。検出の規則は `cc-redact` と共通です（ESA・Basic・Bearer の Authorization、`serviceSecret`・`licenseKey` などの代入、既知の API キー・トークン、URL のパスワード、秘密鍵、資格情報らしい名前の環境変数の値）。

## 置き換える範囲

1. `pr create/edit/comment/review/merge/close/reopen`、`issue create/edit/comment/close/reopen`、`release create/edit` の `--title`・`--body`・`--subject`・`--comment`・`--notes`。`-b text`・`-btext`・`-b=text`・`--body=text`・まとめた短い旗（`-cb text`）と、`-R/--repo` の後に続く形も扱います。
2. `--body-file`・`-F`・`--notes-file` のファイルと標準入力（`-`）。呼び出し元のファイルは書き換えず、置き換えた写しを一時フォルダーに作って渡し、終了後に消します。
3. `gh api` の `-f/--raw-field`、`-F/--field`（`@file`・`@-` を含む）、`--input`（ファイル・標準入力）。JSON は解析してから置き換えるので、GraphQL の変数を含めて JSON として正しいまま渡します。フィールド名やJSONのキーが資格情報らしい名前（`serviceSecret`・`licenseKey`・`variables[token]` など）なら、値の形や型に関係なく値全体（`@file`・`@-` の中身も）を置き換えます。
4. `pr create --fill`・`--fill-first`・`--fill-verbose` は、公開されるコミットメッセージ（`<base>..<head>`）を `git log` で読み、秘密があれば実行しません。

## 実行しないもの（終了コード 2）

置き換えられない入力は、そのまま公開せずに止めます。

1. `--editor`・`--recover`・`--notes-from-tag`、表にない旗、2回目の標準入力、UTF-8 でない入力。
2. エイリアスと拡張機能（`gh co` など）。展開後の内容を確かめられないためです。
3. `gist create/edit`、`workflow run --json`、`--body` を使わない `variable set`、それ以外のコマンドでのファイル入力（`--body-file`・`=@file` など）。
4. 秘密を含む引数を持つ、その他のコマンド。秘密を含む release の添付ファイル。
5. 本物の gh として、このゲート自身・`.cmd` などのスクリプト・存在しないファイルを指定した場合と、ゲートが3段以上入れ子になった場合。

止めた理由は標準エラーに出します。この文も秘密を置き換えてから出し、呼び出し元が渡したファイル名や、gh の名前の形でない旗・コマンド名は `<hidden>` として書きません。

読み取りだけのコマンドは、引数も標準入力もそのまま本物の gh へ渡します。`auth` と `secret` は資格情報の保管先へ値を渡す操作なので、置き換えずに渡します。

`api --allow-escape-sequences` は gh の Bool フラグとして渡します（`=true`・`=false` も保持）。ANSI 制御文字を含むログを保存するときは、呼び出し元で標準出力を非公開ファイルへリダイレクトしてください。このフラグを付けても、API へ送る本文の秘密の置換と未知のフラグの拒否は続けます。

## このゲートで守れないもの

1. `git push` で公開されるコミットメッセージと差分。ゲートは `gh` の呼び出しだけを見ます。コミット前に `cc-redact` 相当の確認が別に必要です。
2. GitHub 側で既存の内容から作られる本文（`pr merge` の既定のコミットメッセージ、`release create --generate-notes`）。元の PR・Issue はこのゲートを通して作られている前提です。
3. ゲートを通さずに本物の `gh.exe` を直接呼ぶ場合。

## このPCへの配置

本物の gh は既定で `C:\Program Files\GitHub CLI\gh.exe` を使います。別の場所なら環境変数 `CC_GH_GATE_REAL_GH` に `gh.exe` のフルパスを設定します。

1. `cargo build --release -p cc-gh-gate` で `gh_gate.exe` を作ります。
2. `gh_gate.exe` を `D:/CC/Tools/codex-jobs/gh_gate.exe` に置きます（`cargo_gate.exe` と同じ場所）。
3. `cc-gh-gate/bin/gh.cmd` を `D:/CC/Tools/codex-jobs/bin/gh.cmd` に置きます。中身は `"%~dp0..\gh_gate.exe" %*` だけで、終了コードを引き継ぎます。
4. `D:/CC/Tools/codex-jobs/bin` が PATH で `C:\Program Files\GitHub CLI` より前にあることを `where gh` で確かめます。

`.cmd` を経由すると、cmd.exe の都合で改行を含む引数は渡せず、`%` や `^` が変わることがあります。本文は `--body-file` で渡してください。

子プロセスは `CREATE_NO_WINDOW` で起動し、空の窓を出しません。
