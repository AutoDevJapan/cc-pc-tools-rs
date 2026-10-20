# ToDo3190: 方向回答の仕様記録と元質問の終了

関連Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/325

## 今回の区切り

実装は未着手です。共有コード、稼働版、質問、元作業の仕様・停止・依存は変更していません。
3181のレビュー終了を確認しましたが、結果にはMustが1件あります。PR315の通常マージは
まだ確認できないため、依頼の順序に従い3190自身の有限な技術再確認待ちへ移ります。
3153全体のdoneは、方向記録の実装に着手する条件にしません。

## 確認した事実

- 自分のjobは `20261021-052447-todo-3190`、ブランチは
  `scheduler/todo-3190-3110`。着手時HEADは
  `cb26d3112d537e2f602fbed07108673517746898`。着手時の未コミット差分はありませんでした。
- Git remoteと正常なGitHub照会で、実所有者を `AutoDevJapan/cc-pc-tools-rs` と確認しました。
  自分のworktreeのguardは `ok=true`、`client_resource=false`、
  `classification_unverified=false` でした。原保護一覧全体の復旧を意味しません。
- 通常の `todo list --all` で3181はdone、3153と3110はdoingでした。
  正常な `codex_jobs.exe status --json` では3181のjob
  `20261021-043746-todo-3181` は succeeded、exit 0、終了時刻
  `2026-10-21T05:00:13+09:00`、モデル `anthropic/claude-opus-5-5`、effort `max` でした。
  同じ照会で自分のモデルは `gpt-6.1-sol`、effort `max` でした。
  これは正常statusの値です。今回、実turn_contextの独立照合はしていません。
- 3181の正常なtailで、最終回答の「レビューだけが完了」「Must 1件、Suggestion 2件、Nit 0件」と、
  base `9883b5eda6e0db27b5bba70dcd5131e7a4aca8a1`、
  head `577b17baed706fc8b783dca0f671f8a8d65206b1` を確認しました。
  Mustは `conversation_delivery.go` のimported経路のproject差し替えに関する指摘です。
  tailには最終回答全文が表示されないため、指摘の全条件・解消の判定は3153担当の原結果照合に委ねます。
- PR315のheadは同じ `577b17baed706fc8b783dca0f671f8a8d65206b1`、baseはmain、
  stateはOPEN、mergeCommitとmergedAtはnullでした。最後に取得したrollupは全件COMPLETED、
  SUCCESS 15件、SKIPPED 1件、NEUTRAL 1件でした。先行照会の実行中表示から変化しています。
  CodeQLのrun `39480808907` は同headでsuccess、終了更新時刻は
  `2026-10-20T20:29:34Z` でした。CI終了はMust解消やマージ・配布を意味しません。
- Issue325の本文、元3153の正常note history、現worktreeのAGENTS.md、起動時の
  `D:/CC/AGENTS.md` を読みました。方向回答の私的原資料、元ToDoのspec、
  canonical appliedや旧台帳は開いていません。本人の原文を新たに引用・認証した記録ではありません。

## 再開条件と次の操作

1. 元番号3190のまま、期限到来後に正常なPR315照会を1回行います。未マージなら
   3153の正常記録で理由・実head・レビュー修正状況を確認し、同じ条件の短時間再試行や
   並行修正はしません。本人判断block、3153全体のdone待ちへ置き換えません。
2. PR315が通常マージ済みなら、実merge commitを検証し、最新mainをこの独立worktreeへ
   統合します。元の途中成果を残し、別担当の作業場所は変更しません。
3. 当該ソースのAGENTS/CLAUDE、実所有者・guard、正規CLIと関連コードを読み直してから、
   Issue325の仕様・観測可能な異常系と成功の受入検査を作り、Redから修正します。
   共有の `conversation_delivery.go`、`delivery.go`、`cc-todo/lib` はそれまで変更しません。
4. 本人の現在回答を正規のnormal conversation repliesとfresh APIで再照合します。
   元質問世代、設定済み本人、本文・編集版、source SHAを確定してから、実在する自分の
   ToDo/jobと同project・実対象scopeで方向だけの仕様記録を行います。
5. 元仕様への実追記、同一lock内のguard対象snapshotを使うCAS、正常readback、
   方向専用の適用票、既存の別silent進捗、質問終了GETを個別に確認します。
   actor/origin/destination/観測者を分け、今回の引継ぎ文をDiscord本人の原発言へ昇格しません。
6. 変更クレートと利用側のnextest/doc、関連Go race/vetを選び、同sourceの成功検査を再利用します。
   必要な別モデルmaxレビューは共有ToDoへ `--spec` で登録し、子jobを直接起動しません。
   Must解消、必要CI成功、通常マージ後に最新稼働source/hashと並行修復を再確認して配布します。
   古いsourceや未マージexeで上書きしません。

## 固定する対象と境界

- 対象は元3050の方向質問2件だけです。元3084の機能修復と未完コード/Prdは残します。
- 質問 `1561946066696278066`、本人返信 `1561946270082404463`、request
  `normal-ask-60c451a37242cbedd20473a68992b74e2345b4cb2b3e96c7168f0fe9052aa4b0`。
  引継ぎ上の意味は「製品ごとの責任担当」で、29常駐の承認ではありません。
- 質問 `1561951095847260160`、本人返信 `1561951289468911707`、request
  `normal-ask-9b2610177e9b54b979ba874d149dd8fd67378cd1aacbeffb50ee08ff9edab0af`。
  引継ぎ上の意味は現在のOperations画面構成の維持です。画像案の実装承認ではありません。
- 設定済みowner `730822776147148835`、guild `1552944707464597524` を正常経路で照合します。
  原資料の所在と対象2件は元3110の割当指示を保持し、正規の用途付き読取/配送を使います。
  未知の私的領域、旧台帳、appliedをraw読取・コピーして迂回しません。
- 第3返信 `1562010560239771659` は限定した来歴照合の許可です。この方向記録により
  機能実施済み、新UI承認、guard解除と扱いません。
- 旧3050/3070の待機修復は3126、実装修復は3084に保持します。元ToDo、3084、他の停止・依存を
  doneや解除へ変更しません。「仕様へ記録、機能は未完了」を進捗に明記します。
- 通常の機能appliedが元担当jobを求める条件は維持します。方向専用kindを通常の機能反映へ
  昇格できず、元担当偽装、foreign job/project、未知scope、同noteのproject変更、新block/依存、
  古い/編集後reply、sourceSHA相違、未記録仕様、配送だけ、画像承認への流用を拒否します。
- 本人のPC操作、新しいOwner質問/メンション、UI実装、追加費用、AI APIキー、GPU、
  新常駐、配備方針変更は行いません。通常の完了通知も送りません。

## 検証と保存

今回は依存の事実確認と文書保存だけです。製品コードの検査・ビルド・配布・仕様記録・
質問closeは未実施で、進み具合の推測値も付けません。文書の差分とリンク、秘密情報や
無関係な変更がないことを確認してcommit/pushします。実HEADで
`scheduler checkpoint-workspace` を保存してから元3190に有限wait-retryを登録し、
正常な一覧で待機を読み返します。未完了の実装はこの担当の元ToDoに残します。
