主担当が正規job.jsonから受領した別モデルの原報告。読むだけの担当の制約を変更せず、元2628の実装担当として記録した。モデル: anthropic/claude-sonnet-5-5 / source job: 20261015-003819-todo-2633 / finished: 10/15/2026 00:39:26 / reviewed HEAD: 080bf6cc6628ecc87487747733363e868d01f973 / original message SHA256: 9da18c3a8cf7a61a4f04834efdb45f4218c8420080ac9803a9f3d4d26b16fb78
現在のsource HEAD: d06d7f40f741cc5b3c8bba46116a1c04aa82cc29
原報告のMust0は静的レビューだけ。Git/shellのDLL起動失敗とreadonly書込み拒否、実検査未実行を保持する。完了はレビュー受領だけであり、元2628の実装/検査/配備ではない。

状態: 継続中

レビューは終わっていて、固定HEAD 080bf6cc の Must は 0 件です。結果ファイル `todo-2628-review-result.md` は今回も保存できませんでした。このジョブではファイル書き込みが read-only のサンドボックスで拒否され、シェルも起動しないためです。

- **書き込み**: apply_patch が "writing is blocked by read-only sandbox" で拒否されました。
- **シェル**: `git status` を含む全コマンドが STATUS_DLL_INIT_FAILED（-1073741502）で起動しません。作業フォルダの状態を読めず、前回から引き継げる途中コミットや未保存の変更も確認できませんでした。
- **共有ToDo**: 結果ファイルが無いので、レビュー項目は完了にしていません。書き込める枠で再実行すれば、下の本文をそのまま保存できます。

## レビュー結果（claude-sonnet-5-5）

- 対象: AutoDevJapan/cc-pc-tools-rs
- 基準HEAD: 0b703782a81e5c262f771caa849e766872591b2e
- 固定HEAD: 080bf6cc6628ecc87487747733363e868d01f973
- 件数: **Must 0 / Suggestion 7 / Nit 5**
- S1〜S3 は、実CLIの exit 0 / ok:true が実データで何を意味するかに関わります。マージ前か、直後の追補PRで直すことを推奨します。

## 実施範囲と未確認

- **取得方法**: ローカルのシェルが使えないため、GitHub読取APIで固定コミットを取得しました。compare は ahead_by=1 で、変更は指定の6ファイルと一致します。
- **差分の外も読んだ**: 意味を判断するため、同じrepoの cc-todo・cc-job-policy・cc-scheduler・docs の関係箇所を読みました。原台帳・registry・秘密・他担当の成果は読んでいません。
- **未確認のもの**:
  - 新規7テストの実行成功。担当報告の nextest 6/6 と、最大ページのテストを含む `-p localai-web-rs` の全件結果は、私は見ていません。
  - CI、配布、稼働CLIの exit 0、サーバー復帰、稼働台帳の bytes 保持の実確認。
  - 稼働台帳に `agent`・`wait_ready`・`recovery_unverified` が実在するか。S1・S2 はソースコード上の根拠に基づきます。

## Must 0 件の根拠

- **元台帳の不変**:
  - 読取は `cc_todo::read_store` だけで、書込・backup・保護解除・委譲有効化の経路に入りません。
  - agent.rs:4988 のテストが、17MiB履歴の bytes が GET の前後で同一であること、`on()` が作られないこと、委譲の外部呼出しが空であることを、privacy あり/なしの両方で確認しています。
- **保護不明時の挙動**:
  - `project()`（todo_listing.rs:73）は `json!` で許可項目だけを組み立て、本文・履歴・パス・製品名をコピーしません。
  - privacy が欠けても 200 を返し、常に `masked_metadata` を付けます。
  - CLIも許可リストで二重に絞っています（cc-console.rs:352-368）。
- **ページ漏れ**:
  - `limit+1` 件を取り、`nextAfter` を最後に返した行のidにしています。漏れや重複の経路は見つかりませんでした。
  - 101件・2ページ・600リンクのテストがあります。
- **取得失敗**:
  - HTTP失敗、200 でも `ok:false`、途中切断、不正JSON、保護モードや型の不正は、すべて exit 1 または 2 です。
  - サーバー本文は出力しません。
- **応答上限**:
  - 最大ページは概算で約0.6MBです。
  - サーバーとCLIの両方に1MiB上限があり、旧コマンドの16MiB上限も維持されています。
  - Content-Length と chunked の境界テストがあります。
- **エラー分類**: 409 / 404 / 400 / 503 が code と httpStatus に写り、未知の code は `listing_failed` になります。
- **旧API互換**:
  - GET `todo`、`display/*`、`jobs`、`handoff`、POST `todo/*` は未変更です。
  - agent.rs の display の変更は整形のみで、動作は同じです。

## Suggestion

**S1（高）担当ハンドル `agent` が実台帳に存在しない可能性が高い。**
- 対象: todo_listing.rs:128、cc-console.rs:333・358、テスト agent.rs:4994・todo_listing.rs:158。
- 根拠: cc-todo、cc-scheduler（common.rs・engine.rs）、cc-job-policy/todo_wait.rs のどこにも `item["agent"]` の読み書きがありません（検索0件）。この欄はテストの fixture にしか出てきません。実際の担当は `scheduler.job`・`scheduler.phase`・`worker_receipt.job` に保存されます。
- 実害: `agent` が常に null でも、テストは自作の欄で通ります。そのまま ok:true を返すと、担当を表示できているように見えてしまいます。
- 修正: `activeJob`（`scheduler.job` のハンドル）と `schedulerPhase`（許可リスト）を別の欄で返し、fixture を cc-todo が実際に書く形にします。

**S2（高）依存停止と状態の表現が、スケジューラーの判定と合っていない。**
- 対象: todo_listing.rs:103・119-124、cc-console.rs:332。
- 根拠:
  - 実際の停止判定は `todo_wait::pending_on` で、done でない待ち先だけを数えます。新APIは `wait_on` の生の番号を返すだけで、どれが未完了か分かりません。
  - 依存番号なしで status=wait になる `wait_ready`（chatgpt-plus / retry-time）が見えません。
  - cc-todo/src/lib.rs:1038 の状態 `recovery_unverified`（元2467の復旧に直結）は許可リストに無く、"unknown" に潰れます。
- 修正:
  - `pending_on` を使った `waitPending`（数値、64件ずつ、total と nextOffset 付き）を追加します。
  - `waitReady` は condition の許可リストと `notBefore`（数値）だけにします。
  - 状態の許可リストに `recovery_unverified` を足します（サーバーとCLIの両方）。

**S3（中）ジョブ参照の数え方が todo.exe と違い、取りこぼしが表示されない。**
- 対象: todo_listing.rs:106-117、テスト agent.rs:4995 付近と todo_listing.rs:174 の `jobsTotal==1`。
- 根拠: todo.exe は `[A-Za-z0-9_-]+` だけを有効なジョブidとし、残りを「未確認のジョブ参照 N件」と数えます。新実装は任意の非空文字列をジョブとして数え、オブジェクト形（復旧された証跡）を黙って捨てます。テストはその取りこぼしを仕様として固定しています。
- 修正: 有効なものだけをハンドル化し、残りを `jobsUnverified`（数値）で返します。テストは `jobsTotal==1 && jobsUnverified==1` に直します。

**S4（中）exit 0 / ok:true が「1ページ検証できた」だけだと、出力から読めない。**
- 対象: cc-console.rs:285、docs/todo-listing-2628.md。
- 根拠: 旧 `todo list` は全件を返しましたが、新コマンドは既定で番号昇順の先頭100件（多くは古い done）を ok:true で返します。出力形も変わります（title・note・project が無くなる）。
- 実害: 旧手順が ok:true を全件取得と読むと、先頭100件だけで判断します。repo内で `todo list` を呼ぶコードは検索で見つかりませんでした。repo外のSkillは読んでいません。
- 修正: CLIが検証後に派生値 `complete`（`nextAfter` が null かつ全行の nextOffset が null）を付けます。docs の冒頭に出力形の変更と完走手順（`--after`・`--link-offset`）を書きます。

**S5（中）`revision` が台帳全体のハッシュなので、複数ページの照合が完走しにくい。**
- 対象: todo_listing.rs:75。
- 根拠: 17MiB超の履歴を含む全体を毎ページ再直列化してSHA-256を計算します。どの項目の更新でも値が変わります。
- 実害: 稼働台帳は頻繁に書かれるため、`--revision` 付きの走査はほぼ毎回 409 になり得ます。
- 修正: revision を、返す項目の projection（id・状態・進捗・依存・ジョブのハンドル）のハッシュにします。または、409 の再試行上限と扱いを docs に書きます。

**S6（中）テストの穴。**
- (a) todo_listing.rs:161-186 は、秘匿検査が `second`（id=2）だけです。title・spec・note が "private" の id=1 は `first` に入り、検査されません。回帰で title を返しても通ります。`first` も同じ検査にかけます。
- (b) サーバーからCLIまでの結合テストがありません。`reply()` の redact を通った応答をCLIが受理することは未証明です。
- (c) エラー分類（cc-console.rs:274-281）の code と httpStatus を直接検査するテストがありません。
- (d) 上限は「+1で拒否」だけで、「ちょうど1MiBは通る」境界がありません。
- (e) `todo.json` 以外（queue・state・lock・backup）の不変は検査していません。

**S7（低）不透明ハンドルが無塩のSHA-256。**
- 対象: todo_listing.rs:59。
- 根拠: 担当名やジョブidが推測できる文字列なら、辞書で復元できます。
- 修正: 種類（job / agent）を前置したドメイン分離を最低限入れ、「秘匿ではなく同一性の照合用」と docs に書きます。

## Nit

1. 動作に無関係な rustfmt 差分（use の順序、`assert!` の折返し、lib.rs の mod 並び替え）が入っています。一方、新規行は未整形です（agent.rs:2937-2941・4536、todo_listing.rs:74）。CIに `cargo fmt --check` があれば落ちます。CIの有無は未確認です。
2. `invalid_store`・`invalid_query`・`listing_too_large` がすべて 503 `todo_store_invalid_json` に集約されます（agent.rs:2940）。上限超過は現実には到達しません。1件でも数値でないidがあると全ページが失敗しますが、todo.exe の list も同じ挙動です。
3. サーバーの1MiBは `data` だけで、CLIは封筒込みです。差は約30Bで、到達しません。
4. `validate_listing` は、`nextAfter` が最後のidか、`total` が件数以上か、`linkOffset` が要求値か、`--id` で1行かを検査しません。
5. 新CLI + 旧サーバーでは `listing_failed` で exit 1 になります。docs に「サーバー、次にCLI」の配布順を書いてください。

## 残る作業

書き込みとシェルが使える枠で、次を行います。

1. 上の本文を `todo-2628-review-result.md` に保存し、冒頭にモデル・固定HEAD・Must件数を記録する。
2. 共有ToDoのレビュー項目だけを完了にする（元2628は完了にしない）。

担当Codexには、S1〜S3 の修正（または追補PRへの分離）と、CIの実成功の確認が残っています。