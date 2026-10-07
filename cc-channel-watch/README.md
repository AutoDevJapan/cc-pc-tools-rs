# channel-watch

YouTubeの更新を毎日確認し、新しい公開動画があるチャンネルだけに、解析を自動実行できる共有ToDoを1件登録するRustツール。巡回で実際に再生数を取得したときは、測定日時と件数・合計・一覧の先頭3本の題名/URL/値を専用topicへ報告する。数値がない一覧を0と扱わない。

## 配置と実行

- ソース: `D:/CC/Tools/rs/cc-channel-watch`
- 実行ファイル・設定・台帳・ログ: `D:/CC/Tools/channel-watch`
- 設定: `channels.json`。各チャンネルのURL、知識Skill、報告フォルダー、固定topic、ToDoプロジェクト、初期台帳の資料を指定する。
- 定期タスク: `CC-ChannelWatch`。毎日01:00、最大4時間のランダム遅延（JSTで01〜05時）。ログイン済みユーザーの権限で `wscript.exe //B //Nologo run-hidden.vbs` を実行する。VBSの表示指定は0、Rustの子プロセスにも `CREATE_NO_WINDOW` を設定する。同時実行を禁止し、実行時間の上限は20分。PCが起動していなかった回は起動後に実行する。
- 登録済みタスクの定義: `D:/CC/Tools/channel-watch/task.xml`。

```powershell
cargo test --manifest-path D:/CC/Tools/rs/cc-channel-watch/Cargo.toml
cargo clippy --manifest-path D:/CC/Tools/rs/cc-channel-watch/Cargo.toml --all-targets -- -D warnings
cargo build --release --manifest-path D:/CC/Tools/rs/cc-channel-watch/Cargo.toml
# このPCのCargo設定ではビルド出力はD:/CC/BuildCache/cargo-shared。
Copy-Item D:/CC/BuildCache/cargo-shared/release/channel-watch.exe D:/CC/Tools/channel-watch/channel-watch.exe
& D:/CC/Tools/channel-watch/channel-watch.exe seed
& D:/CC/Tools/channel-watch/channel-watch.exe run
```

`seed` は台帳がないチャンネルだけ、既存のJSON動画一覧またはMarkdownの動画リンクを取り込む。資料がない、壊れている、動画IDが0件の場合はそのチャンネルをエラーにし、全過去動画の再依頼を防ぐ。現在の公開一覧を初期台帳の代わりにしない。台帳があれば資料の変更で上書きしない。設定のチャンネルURLと台帳のURLが一致することも確認する。

`run` は設定ファイルの親フォルダーで動く。別の設定は `--config <絶対パス>`。一覧は導入済みyt-dlpの `--ignore-config --flat-playlist --dump-single-json --skip-download` で取得する。チャンネル直下のVideos・Shorts・Liveの入れ子を展開し、11文字の動画IDで重複を除く。取得の再試行は1回、通信待ちは20秒、1チャンネルの実行上限は240秒。失敗した一覧は台帳へ反映せず、他チャンネルは確認する。次の日に再試行する。設定・取得・登録エラーがあれば終了コード1。

## 台帳と登録

`ledgers/<チャンネル>.json` の `seen` は既存資料にある動画、または解析を依頼した動画を表す。解析完了の証拠ではない。登録済み動画にはToDo番号を残す。会員限定等は `skipped` にID・題名・理由・時刻を記録する。会員限定だけが増えてもToDoを作らない。会員限定が一般公開へ変わったときは解析対象となる。配信予定・ライブ中・処理中の配信は解析せず、後日再確認する。

字幕の言語や公開状態はflat一覧で欠けることがあるため、解析ジョブが対象動画だけを改めて確認する。APIキー・課金・資金移動・採掘・ステーキング・取引・報酬目的のPC提供を禁止する。公式資料との照合、D:/CC/SkillsでのSkill作成、既存知識Skillと索引の更新、チャンネル名を固定topicにしたlogでの通知なしの本文投稿を仕様に含める。

依頼文は `specs/` に保存し、`todo.exe add <project> <日本語題名> --spec <file> --model gpt-6.1-sol --effort high` へ渡す。これは自動実行の許可を含む。ToDoに登録済みとなるまで `seen` を進めない。登録前に `pending` と一意の識別子を保存し、再開時は共有ToDoの同じ識別子を照合する。登録直後の中断でも二重登録しない。前回の登録を復旧した回は新しい依頼を増やさず、次回へ残す。WindowsのOSファイルロックと一時ファイルからの置換で、同時実行と台帳の途中書込みを防ぐ。

`runs.jsonl` は実行ごとに1行。時刻・所要時間・チャンネル別の一覧/新規/除外/延期件数・ToDo番号・エラーをJSONで記録する。最新のflat一覧は `evidence/<チャンネル>-latest.json` に保存する。新動画がない日はToDo・解析を増やさない。再生数を測った日は専用topicへ測定を報告する。Discordへの未送信の測定はledgers/<チャンネル>-reports.jsonへ最新1件を保存し、次回の測定へまとめる。送信はJSTで1日1回までにする。Discordの失敗で新動画の登録を止めない。

初期資料: XB 472本、Motocrab公開166本＋会員限定18本、健康ナビ38本。Motocrabの指定報告フォルダーは既存 `xb-bittensor/motocrab-love` へのジャンクションであり、資料を移動していない。健康ナビの元解析は知識Skillのreferences、メタデータと制作素材は `D:/CC/Work/adj-health-20261004` に残す。

## 検証

Rustの偽yt-dlp JSONを使い、新動画検出、3タブの重複、台帳の再読込、更新がない日、会員限定の除外と記録、一般公開への変更、登録直後の中断復旧、初期資料の不足・破損を検証する。実際のDiscordやAPIキーを使うテストはない。

yt-dlpのオプションとflat一覧の制約は [公式README](https://github.com/yt-dlp/yt-dlp#usage-and-options) を参照。確認日: 2026-10-04。

報告の固定topicはchannels.jsonのname。report_threadは旧設定の互換用で、送信には使わない。解析完了、失敗、数値測定のたびに題名・URL・更新Skill・測定日時などを本文に記載する。Skills化依頼へは活動を送らない。
