# ToDo3406: 回答照会の必須修正を独立してテストする

親はToDo3392、実ジョブ `20261023-102927-todo-3392`。
この担当はToDo3406、実ジョブ `20261023-103917-todo-3406`。
対象はAutoDevJapan/cc-pc-tools-rs、Issue392 / PR393。

## 確認済みの途中結果

- Red `babed84edc0048ac9100cc7489402de4bf3acf53`: 指定2件がコンパイル後に失敗。
  架空名 `ExampleClient3392` の伏せ字漏れと、未確定配送が `not_sent` になる
  assertionを確認した。Cargo gateの実終了コードは100。
- Green `490ffd47e78aa0c5f7a4c5d2f054bddd05746314`: 指定された関連テスト66件が成功。
  lookup 9件と旧identity/bridgeの公開利用側を同じ生成先で確認した。実終了コード0。

全stdout/stderr、実コマンド、開始・終了時刻、実HEAD、生成先、実終了コードは
このworktreeの `.ci-work/todo-3406-3392-check/` に保持する。
次は同じ固定Greenでdoctest、Clippy、配布候補のreleaseビルドを順に実行する。

`release_eligible=false`。最新の別モデルレビューと同じソースのCIはこの子では
確認していない。候補生成と現用への配布を区別し、現用への配布、親ソースの変更、
PRのマージ、共有設定・台帳・ToDo状態の変更はこの子では行わない。
元3396のf804候補と既存141件の成功履歴は保持し、修正後の成功に流用しない。

## 再開

作業ブランチは `scheduler/todo-3406-3392-check`。
固定ソースでテストするときは一時的にdetached HEADを使い、終了前に同ブランチへ戻す。
`.ci-work/todo-3406-3392-check/resume.json` と各 `*.result.json` の実終了を読み、
成功済みのテストを重複実行しない。親3392全体の完了とは別の有限工程である。
