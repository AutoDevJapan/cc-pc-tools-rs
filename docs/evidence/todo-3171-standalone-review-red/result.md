# ToDo3171: 指定の追加Red検査1件を完了

作者モデルの情報がないためレビュー登録が拒否される、期待どおりの失敗を確認した。
指定した検査を1回だけ実行し、1失敗、0合格、390件は未実行だった。
実際のCargo/nextest終了コードは100。構文エラー、引数の排他エラー、資源待ちではない。

## 対象と実行

- 元ToDo3081 / [Issue277](https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/277)の有限な独立check。
- 実行時の実HEAD: `5ba9a1c274d7a882387542d15c39fd2b2858f990`。
- 指定ref: `scheduler/todo-3081-review-intake-red`。実行前に同じcommitであることを確認。
- `5a82f08`との差分はTest.runから`--auto-model`と同時に渡す`--model m`を外す4行だけ。
- 自分のブランチ: `scheduler/todo-3171-standalone-review-red`。
- 実行日時: 2026-10-21 03:40:51〜03:42:06（日本時間）。
- Nextest run ID: `f5732548-ffdc-4a74-bcf0-fd9ff1570101`。

```text
cargo nextest run --locked -p cc-jobs --test parity -E 'test(standalone_review_registers_without_guessing_authors_from_a_shared_cwd)' --no-fail-fast --status-level all
```

共有CargoゲートをCREATE_NO_WINDOWで起動した。専用の生成先と指定sccacheを使用し、
ビルド・テストの並列上限はそれぞれ8。コマンド、環境変数の必要項目、時刻、
プロセスID、実終了コードは`run-started.json`と`run-result.json`に保存した。
外側の記録スクリプトは保存成功のため0で終了したが、検査の終了コードは100である。

## 実際の停止箇所

対象名:
`capacity_regressions::standalone_review_registers_without_guessing_authors_from_a_shared_cwd`

テストは共有cwdに無関係なOpusの終了済みジョブを置き、作者を指定せずに
Opusでのレビュー登録を呼んだ。`cc-jobs/tests/parity.rs:1947:36`の
`t.run(...).unwrap()`で、次のエラーにより停止した。

```text
called `Result::unwrap()` on an `Err` value: Error { code: 1, message: "review source model metadata unavailable" }
```

製品コードの該当箇所は`cc-jobs/src/capacity.rs:253`で、
`validate_review_launch`が作者モデルを得られない場合に返すエラーである。
作者判定の処理には到達した。OpusからSolへの変更が起きたとは判定していない。

登録成功を要求する`unwrap()`には到達し、そこで失敗した。
レビューを取得する1948〜1949行と、次のassertは未到達である。

- 1950行: モデルが`anthropic/claude-opus-5-5`であること。
- 1951行: `review_source_verified`がfalseであること。
- 1952行: `review_source_jobs`が空配列であること。

## 証跡と残作業

`stdout.txt`と`stderr.txt`は完全な元ストリームで、抜粋や置換を行っていない。
外側のstdoutは0 byte。Nextestが表示したテスト内stdoutとエラーは、
コンパイル記録と共に外側のstderrへ出力されている。
読みやすい抜粋は`failure-excerpt.txt`、対象ソースは`source-excerpt.txt`に分けた。
`SHA256SUMS.txt`で証跡の内容を照合できる。
このフォルダだけの`.gitattributes`で改行変換を止め、Git内にも元のバイト列を保持する。

約1.92GBの専用ビルド生成物は、所有付き削除ツールのplanが指定した
`quarantine / size`の手順で削除希望フォルダへ移した。
`cleanup-plan.json`と`cleanup-receipt.json`に結果を保存した。
これは容量回収済みという報告ではない。

この結果は修復前の比較検査であり、現在の親の実装のGreenを示さない。
製品コード変更、追加検査、Green、他のRed6件、PR、merge、配布、
元3154/3081の変更、本人への質問・通知は行っていない。
本物のCodex子ジョブ、GPU、Civ6、APIキー、認証ファイル、原client一覧も使っていない。
共有ToDoの状態更新はスケジューラに任せる。

保存の完了条件は、この証跡のcommit/pushと最終HEADのscheduler checkpointである。
最終の保存結果は同じフォルダの`finalization.private.json`に記録する。
これはHEAD自身を含むためGit管理の対象外とし、正規schedulerにも同じHEADを保存する。
比較用のブランチをmainへmergeしない。
