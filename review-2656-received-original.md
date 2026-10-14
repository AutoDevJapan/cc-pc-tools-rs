実装担当が正規job.jsonの原応答を受領。source job: 20261015-023022-todo-2656 / model: anthropic/claude-sonnet-5-5 / fixedSHA: e4421c378ea0bd65e982bb21406c8049eaaf0c75 / chars: 3837 / messageSHA256: ede3feee435bd4db7a0144762d69d0a742b6f61b523f91c1b148534f22f57507

状態: 完了

**レビュー報告(元2628追加差分)**

- 実モデル: anthropic/claude-sonnet-5-5(静的レビューのみ)
- 固定SHA: e4421c378ea0bd65e982bb21406c8049eaaf0c75
- 対象: 080bf6cc..e4421c37 の追加diffだけ。範囲は todo_listing.rs の project/metadata/validate、cc-console の応答検証、cc-todo の job_refs と pending_on の公開、agent.rs の試験、docs/todo-listing-2628.md。main 5010498 のmerge差分は対象外。
- **Must: 0件**
- 根拠はdiffと提示されたsource本文だけです。コンパイル、nextest、CIは実行していません。「関連nextest 8/8」と「共通25/25」は依頼文の報告として引用しており、私は検証していません。

**確認できた点(S1〜S7)**

- **S1(担当):** `scheduler.job` → `worker_receipt.job` → `job` の順で、正規idだけを `activeJob` にします。`agent` は同じ値の互換欄です。`schedulerPhase` は固定リストか `unknown` に丸めます。
- **S2(待ち):** 未完依存は共通 `pending_on` を再利用しています。`wait_on` は i64 の正数だけを数え、それ以外は `waitUnverified` に入ります。`wait_ready` は条件と数値の `notBefore` だけを返し、`message` は出しません。
- **S3(参照):** idの抽出と未確認数は `job_refs` を共有しています。object形や不正文字列は `jobsUnverified` に入り、原本は変更しません。
- **S4(complete):** 応答の `complete` は、サーバーとCLIが同じ条件で計算します。その条件は、`nextAfter` がnull、`link_offset` が0、id指定または `after=0`、全 nextOffset がnullの4つです。`complete` が偽装されると不一致で拒否されます。
- **S4(要求照合):** CLIは `after`、`requestedId`、`limit`、`linkOffset`、`revision`、件数、`nextAfter` を要求と照合します。各リンク配列は `total` と offset から期待件数と次offsetを再計算して検査します。
- **S5(revision):** revisionは各行のmetadata投影だけから作ります。history と note を変えてもrevisionは変わらず、status を変えると変わります。この2点は試験で確認されています。
- **S6(試験):** 上限は2点を検査しています。応答1MiBちょうどは通り、超過は拒否されます。また最大構成(100行、jobs/wait_on各64件)は約760KBと見積もれ、`listing_too_large` は実質発生しません。HTTP 404/409/400/503 の分類も試験されています。
- **S7(種類分離):** jobハンドルは `todo-list/job/v1:` を前置したSHA256で、revisionは `todo-list/metadata/v2` で区別されます。

**Suggestion(実害は限定的、対応推奨)**

1. **試験が空振りになっています。** agent.rs と todo_listing.rs の試験で、fixtureから生の `agent` フィールドを外しました。しかし `private-actor` と `secret-agent` の漏えい検査は残っています。そのため、旧 `agent` 名がハッシュにも本文にも出ないことを何も検証していません。fixtureに `"agent":"private-actor"` を残し、scheduler とは別に置けば、「無視されて漏れない」ことを実際に証明できます。
2. **旧 `agent` だけを持つ記録の扱いが変わりました。** この記録は `activeJob=null`、`agent=null` になります。docsに「互換欄agentはjobハンドル」とあるので意図どおりですが、復旧台帳の `doing` 行が担当なしに見えることを、docsに1行足してください。
3. **正規の `scheduler.job` が不正でも、次の候補へ落ちます。** `scheduler.job` が `"bad secret"` などで、`worker_receipt.job` が正規なら、後者が `activeJob` になります。S1の「正規scheduler担当」から外れ、不整合な記録を正規の担当に見せかけます。`scheduler.job` が存在して不正なら、`activeJob` はnullにして `jobsUnverified` に数えるのが安全です。
4. **空文字の扱いが不一致です。** `jobs:[""]` は未確認に数えますが、`scheduler.job:""` は黙って捨てます。同じ規則に揃えてください。
5. **`activeJob` の名前が稼働を連想させます。** `schedulerPhase` が `finished` か `cancelled` の行や、`recovery_unverified` の行でも値を返します。docsは「稼働成功を主張しない」としていますが、消費側が誤読しやすいです。終端phaseではnullにするか、docsに「phaseと必ず組で読む」と明記してください。
6. **revisionが台帳全体に及ぶため、全件走査が進みにくくなります。** 範囲外の行の `progress` や `schedulerPhase` が変わっただけでも、`listing_changed` になります。常駐schedulerが動く31MB台帳では、3回の再試行を使い切って不完全になる可能性があります。対策は次のどちらかです。
   - 揮発しやすい `progress` と `schedulerPhase` を revision から外す。
   - 「先頭〜現在ページ」までの接頭revisionにして、後続行の変化で既取得ページを無効にしない。
7. **CLIが照合する要求を `response.url().query()` から取っています。** reqwestはリダイレクトを追従するので、最終URLのqueryが照合基準になり、CLI自身が組んだqueryとずれる余地があります。送信前にqueryを保持し、それと照合してください。
8. **dangling依存の意味が未記載です。** 台帳に存在しない依存先(例: 2467)も共通 `pending_on` では未完扱いです。スケジューラと同じなので正しい動作ですが、docsに明記するか `waitMissing` 件数を足すと誤読を防げます。
9. **性能の最悪ケースがあります。** `pending_on` は依存ごとに全行を走査し、全行分を毎回計算します。通常規模は問題ありませんが、最悪は行数の2乗に依存数を掛けた量になります。1リクエストごとにdone集合を一度作る形も検討してください。

**Nit**

- `validate_listing` は `#[cfg(test)]` の薄いラッパーだけになりました。試験側で直接 `todo_listing::validate` を呼べば不要です。
- `cc_todo::job_refs` を `pub` にしたことで、公開APIの固定が増えます。docsは「保存・起動の動作は変更しない」としており、diff上もその通りです。
- 追加を勧める試験は次のとおりです。
  - 依存先が `done` に変わったときに revision が変わる。
  - `--id` と `link_offset>0` を `validate` に通す。
  - `wait_on` が非配列。
  - `scheduler.job` が不正で receipt が正規。

**成功主張の範囲**

docsの「実CLI終了0/ok=trueと原台帳bytes保持の実確認まで配布完了と報告しない」は、diffの内容と整合しています。新HEADの全CI、配布、31MB台帳への実CLIでの全ページ取得は未確認のままです。
新CLI+旧サーバーの混在は取得失敗になるとdocsに明記されていて、検証で偽装される経路は見当たりません。

以上、静的に確認できる範囲では Must はありません。