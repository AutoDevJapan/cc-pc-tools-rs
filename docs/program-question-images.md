# Goで描画した画像を同じ本人質問へ添付する

imagegenの証拠形式とは別に、元の担当がGoで新規作成したPNGを検証する。
一般の保護分類、失われた原保護一覧、UIの本人承認は変更しない。
画像ごとの固定許可、画像のコピー、原監査の編集は使わない。

`unattended-runner.exe --check-program-question-evidence --evidence <private JSON>`
は送信も登録もせず、元ToDo/job/worktree、現在の正規本人依頼、元Codex監査の
ソース新規作成・成功したGo実行・元PNGの表示結果を照合する。
ソースは図形と文字を描く単独Goファイルに限定し、外部画像、ネットワーク、
任意ファイル読取を許可しない。Windowsのフォント読取だけを区別する。
生成元のコードは実行せず、原監査で表示されたPNGの全バイトと原画像を照合する。

同じ `recovery-ask` に `--program-evidence <private JSON>` を追加すると、
同じrequest-id、本文/decisionのSHA、本人/guild/forum、元スレッド、各原PNGの
SHAとsizeを送信前に再確認する。資格はこの質問だけに使い、logへ流用しない。
全画像の確認後に既存の重複防止・添付送信・API読戻しを使う。

親が依存wait中ならcheckは成立し得るが送信資格は成立しない。
状態・担当・依存は解除しない。親の通常再開後に同じ原入力で再確認して送る。
送信後の質問世代登録と毎分の本人回答配送は元担当が既存conversation経路で行う。

受入条件は、合成データの成功、改変画像/コード/監査、コピー、別job/ToDo、
別質問/decision/本人/送信先/元スレッド、未知scope、停止/完了/依存waitでの送信拒否。
check、実送信、回答反映、元の動画制作の完了はそれぞれ別の結果として記録する。

証拠JSONのdomainは`owner-program-question-evidence-v1`。
`scope`は既存質問と同じtodo_id・question_sha256・decision_sha256・owner_id・guild_id・forum_id。
本文SHAはrecovery-askと同じBOM除去後の全バイト、decisionは同じcanonical JSONのSHAとする。
`request_id`、`origin_thread_id`、`job_id`、`worktree`、`session`、`rollout_path`、
`session_record_sha256`、`source_message_id`、`source_version_sha256`を指定する。
元scopeは現在の正規受付と元ToDoのintake-owner-source、実jobのToDo/cwd/sessionと照合する。
任意パスの本人原文や呼出し側の「承認済み」という文言は資格に使わない。

`program_path`/`program_sha256`、`body_path`/`decision_path`と、順序付き`images`の
path/sha256/sizeを指定する。`creation_events`、`execution_events`、`preview_events`は
それぞれ原監査の実call/resultのline/sha256/kind/name/call_idの2件。
許可するwrapperはソース追加の`text(await tools.apply_patch("…"));`、
Go実行の`const r=await tools.exec_command({...});text(r);`、
順番を保つ`Promise.allSettled`でview_imageの実戻り値をimageへ渡す形式だけ。
生成コマンドは単独Goファイルの`go run <name>.go .`、必要なら直前の`go mod tidy`だけ。
新規ファイルの排他作成、PNG保存とcloseの成功後の出力名、原表示バイトを照合する。
7日を過ぎた生成、参照画像、任意JavaScript、別のtool応答は受け入れない。

成功時の`validated=true`はこの用途の読取検証で、登録や送信を意味しない。
`send_allowed=false`は依存wait等を保持した結果。`--diagnostic`は失敗した段階だけを返す。
原監査と既存の本人ユーザー環境が信頼境界であり、環境侵害や画像の意味の自動判定は保証しない。

## 閉じた依存による再描画の確認

最初に `--register-program-question-replay --evidence <同じJSON>` を実行する。
登録前にも元ToDo、現在の本人依頼、原監査、原画像、同じ質問を通常checkと同じ順で照合する。
元の画像・コード・質問を編集しない。原PNGのコピーや、コピー先を原画像とする登録もしない。

原監査の追加ファイルは当該単独Goファイルと、その隣のgo.modの2件だけに限定する。
登録器は確認済みの同じコード/モジュール宣言のバイト列を所有付きの別の作業領域へ渡す。
GOWORK/GOENV/GOAUTHをoff、GOTOOLCHAINをlocal、CGOを無効にし、GOPATH・GOMODCACHE・
GOCACHEをその新しい作業領域へ分ける。継承したGOFLAGS、認証、ローカル置換は使わない。
既存の履歴にあった実行環境を復元した、とは主張しない。

nativeの宣言にある固定版のgolang.org/x/imageから依存を解決する。対応する公開Goモジュール
（image/textとそのtools/mod/syncの依存グラフ）だけをproxy.golang.orgと公開checksum databaseで取得し、
`go mod verify`で確認する。解決した版と各h1、go.sumのSHAを記録する。
この閉じた依存で再描画したPNGが原画像のSHA/sizeと全件一致したときだけ、
別のprogram-question用ストアへDPAPIで保護した鍵によるMAC付き記録を新規公開する。
一般guard、旧保護一覧、imagegen証拠形式は変更しない。

通常checkは原監査/原画像/質問と最新の本人指示を読み直し、当該入力・フォント・Go実行物に
結び付く正規の再描画記録を照合する。手書きの成功宣言や改変した記録は資格にならない。
画像作成後の本人指示、未知の取得範囲、署名鍵・記録の欠落は未成立として扱う。
登録が返す所有IDとpathは、成功/失敗ともsafe-deleteの通常手順で別途片付ける。

Go実行物のSHAはそのファイルの変化を照合する値であり、コンパイラ/リンカー、標準ライブラリ、
DLL、OS全体をハッシュ固定した意味ではない。絶対パスの既存Goツールチェーンとホストを信頼する。
外からの依存と作業領域は上の閉じた環境で分ける。

送信側は通常todoの依存待ち・停止更新と同じtodo.lockを資格確認から配送まで保持する。
その間に古いsend_allowedを使って待機状態をまたぐ送信は行わない。
新しい本人指示は、正規取得済みの同じToDo/元スレッドの記録と、元スレッドの現在のAPI取得を照合する。
画像作成後の指示や取得上限のため確認できない範囲があれば、元の画像は送らない。

一次資料: [Goのworkspaces・モジュール認証・verify](https://go.dev/ref/mod)。
