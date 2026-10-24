# PR149の固定main合流と未実行枠の移行（ToDo3576）

元2962が製品PR149、実台帳、配布、登録、実行の唯一の担当。この工程は
cc-pc-tools-rs（repo ID 1406818713）のcontract / route / queueと関連資料だけを変更する。
製品のpublish / rerun / 登録 / サービス / 配布 / 台帳実操作は行わない。

## 固定仕様

- OriginalHead / Tree / Base、Proposeと旧replace-expiredは保持する。
- 追加世代は旧tip `2bd3a366f8974ba54df83927102aa5bdad3983e5` を第一親、
  main `392e7f9bbcea6ee5867b78da37de1efd9f9fec48` を第二親とする。
  main treeは `b5680a158cfcad929e49e97702eec210cc82135d`。
  変更はmainとの差を3workflowだけに限定し、親集合・順序もAPI照合する。
- mainのAPI原sourceをtestdata/main392へ保持する。イベント、権限、job名、
  Rust/Lua/シナリオ/all-targets/doc/release/fmt/Clippy、checkout版を保持。
  旧concurrencyを再適用し、Windows限定、固有label、同repo head checkout、hash記録だけを加える。
  mainのdoctestを重複追加しない。forkはhostedへ送る。
- route prepareはGETと候補保存だけ。追加世代のpublishは元の準備とは分離し、
  固定old tip・main・tree・親・3file、exclusive intent、非force PATCH、再読を要求する。
  mainが進んだ場合は追従しない。
- queueの明示移行は旧initial3keyだけから同workflow/jobの新世代attempt1へ一対一。
  元予算配列、期限、延期、結果、cleanup、supervisor履歴を保持する。
  旧expired_replacementsとは別のproof配列を使う。通常tickで移行・rerun・成功生成をしない。
- 旧取消・未割当・stepsなし、元source、元資源closed/root不在/同SCM/登録不在/
  privileged marker/queryable account process 0を移行前後に照合する。
  未作成は厳密なneverCreated。foreign fenceを消さず、再差替えを拒否する。
- proofは同期後に排他的公開。中断時は原At/Censusを保持し再検証する。
  最終台帳更新は全体CASとatomic保存。Windows1、Cargo gate、24h/6延期を保持する。

## 実測と受入記録

2026-10-24、正規GitHub APIで両repoの実owner/ID/publicを確認。
PR149はopen/head2bd3/base1100/dirty、実main392e7を確認。
main原workflowのblob / SHA256:

| file | blob | SHA256 |
|---|---|---|
| rust.yml | ac17a1126f7a8b19f16e52b6d92a17e630cc2801 | 72858e501824f1949b3caa4a298e6610212b27f1382e68b98bd2b7b1a11d077f |
| native-driver.yml | 4ef3cf54b291f313647c77126374d5b41fe62c40 | c2eb4b8e7710e52810e190183c30df40e114a88670357ddb83fc96c772c0414c |
| live-chain.yml | c72c23c71e67d131b841289fb2e90dd20fb63b5b | d1f68680e1be885240d205a4981011a46209dbc27c4fc34a7c54f51ec891f989 |

Red: `go -C tools/civ6-ci-contract test -run TestFixedMainMergeAdmission -count=1` は
旧コードで `initial routing commit must retain sole original parent` により失敗。
`go -C tools/civ6-ci-queue test -run TestExpiredReplacementRejectsUnsafeOrUnboundedRecovery -count=1`
は成功し、別head/attempt等を拒否する既存境界を確認した。
追加世代実装後のcontract / routeの通常Goテストは成功。
残るGreen・CLI・証拠schema・レビュー・CI・CC側merge・親への返却は実施後に追記する。
製品全体のPR149完了とは区別する。旧PC実行0/過去CodeQL2success、
新source Windows3+CodeQL2、merge後Windows3+CodeQL2を分け、計12実行上限を保つ。

## 再開

branch `scheduler/todo-3576-pr149-2962`、初期HEAD `d234d3144c26973bd35d44c518507852ceaee55a`。
新しい所有領域 `codex-owned-c11fcf9d7457219759180aaa506cce2c` はguard clear。
最初に作ったTemp ID `092593cc32622e6903dc831cf4ca6d68` はguard unverifiedで空のまま使用しない。
親の旧Temp c297...は参照していない。原sourceは正規APIから再取得した。
