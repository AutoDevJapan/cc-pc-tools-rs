# ToDo2804の再開記録

- 元依頼は本人ID730822776147148835の正規API投稿1560922714573766750。
  作成日時は2026-10-17T07:49:35.696000+00:00。
- 元投稿の保存原資料を保持。受領時のSHA256は
  206abfa991bb7c684b5cd587ede584956cb9d82b3f3193c3d9e43acefaf259bc。
- 依頼は全作業のテストを影響範囲へ絞り、全件は必要な場合に限ること。
  独立ビルドを大幅な変更後へまとめて効率化できるかも検討すること。
- 開始branchはscheduler/todo-2804-discord、開始HEADは29ba93b。
  標準jobは20261017-171054-todo-2804。資源割当なし。GPU/Cargoビルドは実行しない。
- 対象repositoryの実所有者AutoDevJapanとremoteをAPI/ローカルで照合済み。
- 原資料のguardは未確定。今回明示された本人投稿だけを内部で照合し、
  案件資源への閲覧・実行権限や保護設定を広げていない。
- 追加指示の保存先は開始時に未作成。外部操作と作業の区切りに自分の番号を再確認する。

完了: 元投稿照合、現行の無条件全件ルールとCIの重複実行箇所を確認、
Cargo/nextestの公式資料を確認、方針文書とGo選択器を作成してコミット。
Goの関連unit/vetは成功。実metadataと固定差分を使った選択も成功し、選択器自身の
変更のため全18クレートを選ぶことを確認した。Cargoのコンパイルは実行していない。

共通指示とCargo用Skillの該当規則を2026-10-17方針へ更新し、読戻しhashを照合した。
原文backupと各段階は.ci-work/todo2804-policy-deployment.private.jsonに保存。
共通指示のSHA256は1ca28d9bce67e51cb7b5ba97693befb5f0c0921d22b56b8b471fd98fe09799ae、
Skillはfea54bbc669036a8a0ba000e8a516517adc1299805111365efd98210c6ae1282。
旧参照資料は保持し、新しい本文の優先を明記した。既存全担当の読了は未確認。

2026-10-17の再開時、同じ標準jobのexecution-promptに新方針が入ることを確認した。
共通ファイルのSHA256も上記と一致。全稼働担当の読了を示すものではない。

選択器はpushを直前tipと比較し、PRをmerge-baseと比較するよう追加修正した。
実Git履歴を使ったrollbackの検査と、関連Go unit/vetが成功した。
7b249c36a4f95e1158a4c4c8ff2bd665bf8d65b3での実metadata・固定push差分もexit0。
全18クレート・doctest対象16ライブラリを選び、理由はtest_selection_changed。
原JSONは.ci-work/todo2804-scope-plan.private.jsonに保持する。

独立レビューは元2804のsubworkとして2807へ登録した。モデルはgpt-6-lunaで、
元担当gpt-6.1-solと異なる。固定したbase/head、全11差分/全文を読ませる。
複数ファイルなのでHaikuへ振らない。登録時点ではまだ未起動で、読了やMust0と
数えない。最初のSonnet指定はtodo CLIがmodel内のslashを拒否して登録0件。
同じ失敗を繰り返さず、CLIが受理する上記の別モデルで1件だけ登録した。

pushはGitHubからOAuthのworkflow scope不足で拒否された。ghのscopeは
repo/read:org/gistで、workflowはない。別の認証・接続への迂回はしていない。
GitHubの実アカウントmashfrombandと本人Chromeの同一ログインだけを照合した。
権限追加・認証コード発行・PR作成・CI実行・mergeは未実施。

必要な追加権限はアカウント単位のため、Codex Computer Useの確認規則
「Creates or materially expands security-sensitive access」のその場の確認を行う。
質問は独立recovery-askで1回だけ送信し、正規GETの配送確認も成功した。
- request_id: todo2804-workflow-scope-20261017
- question: https://discord.com/channels/1552944707464597524/1560934008383209512
- silent progress: https://discord.com/channels/1552944707464597524/1560934014490255390
- fingerprint: c74620c60b65fc2688b97c1cd499e9fc918e357b8393f59bc8b4a8b863ecc35c
- aはGitHub CLIへworkflow scopeを追加し、このrepoのCI確認とmergeまで進める。
- bは追加せず、共通ルールを適用したままCI変更を保留する。

元質問の範囲と選択肢を変えず、recovery-repliesで本人返信を照合して進める。
原入力と受付は.ci-work/todo2804-pending.private.jsonおよび独立台帳に保持する。
回答待ちの間も独立レビューは残す。新しい質問や同じ依頼の担当を重複登録しない。

残り: 2807の読了とMust修正、権限追加の回答・必要な正規認証、push/PR/CI成功、
通常merge、appliedへの実反映記録。原依頼全体は未完了であり、未完了を消さない。

一時検証フォルダの所有IDは71e81b42d79d4f92d7ecc103f00fb1be。
同じDドライブ上の置換用stageの所有IDは032da64b3792155260f94cc58da1a262。
stageの2ファイルは正常な原子的置換で消費済み。元指示のbackupは.ci-workに保持。
認証確認用の自作2タブは終了済み。初回はCLI closeのclosed1で確認した。
CUA用のタブは再開前の自動回収でclosedになり、inventoryで不在を確認した。
その後のCLI closeは所有記録が既にないため失敗した。閉じ直しや本人タブの操作はない。
所有一時フォルダの回収結果と最新checkpointは.ci-workに保存する。
原資料・他担当のソース・サービス・ブラウザは変更していない。

## 2026-10-17の標準ジョブによる続行

標準job `20261017-184106-todo-2804` で元の作業ブランチを継続した。
元の5コミットと作業範囲を保持し、通常のマージ許可は再質問していない。
現行の共通指示とCargo用Skillは、上記の反映後SHA256と一致している。
`7b249c3` から固定head `7e57881f52cc0fc2a8c3e29b16845087b4822d8b`
までの変更はこの再開文書だけであり、選択器のGo検査を重複実行していない。
今回もGPU、Cargoのコンパイル、子ジョブの直接起動は行っていない。

元2807の2実行の `job.last_message` 実原文を正規の
`codex_jobs.exe status --json` から受領した。
`20261017-174119-todo-2807` は17:42:42、
`20261017-184210-todo-2807` は18:43:01にexit0で終了しているが、
どちらも保護判定により差分と11ファイルを読んでいない。
Must・Suggestion・Nitは未判定であり、レビュー完了やMust0にはしない。
原文と実モデル・終了結果、原文のUTF-8 SHA256は
`.ci-work/todo2804-review-receipts.private.json` に保存した。
これは `job.last_message` のハッシュであり、未読の結果ファイルのハッシュではない。

実装担当は元の実装先の正規化した絶対パスと元2807の作業先を再判定し、
両方で `client_resource=false / classification_unverified=false` を得た。
同じ元sourceの固定base/head、11ファイル、tree、判定実行物のSHA256と日時を
`.ci-work/todo2804-source-qualification.private.json` に保存した。
正規GitHub APIで実所有者AutoDevJapan、対象repositoryとprivate=trueも照合した。
未pushのsourceをpublicと推測したり、所有者だけで物理folderの閲覧を許可したりしない。
原registry・guard・manifestは変更せず、拒否された内容を別pathへ移していない。
元2807の仕様・固定範囲を保ったまま、通常の `todo edit --note-file` で
この再判定の根拠を追記した。次の担当自身の正式判定が通る場合だけレビューする。
判定が再び異なればその相違を保持し、拒否されたfolderを別routeで読まない。

稼働GitHub CLIのOAuthには引き続きworkflow権限がない。
既存の権限追加質問を `recovery-replies --read-only` で照合した時点では
本人返信は0件だった。元質問の対象と意味を保ち、再投稿・追加メンション・
権限追加・別認証経路への切替は行っていない。
これは通常のマージ許可の待機とは区別する。
push先mainは `29ba93b4d1298f8012406dca5a77fa3d6a656154`、
この作業ブランチはまだ未pushであり、PR・CI成功・マージは未確認である。

追加指示・原記録・appliedの直接ファイルは保護判定が未確定だったため、
未読・未更新のまま保持した。停止前のイベントを再実行したり、未読の記録を
適用済みとしたりしない。原資料の複製でこの判定を迂回しない。
元2804には通常の60秒後の技術再試行を登録し、同じ担当と作業場所で続行する。
次は元2807の原応答、元質問の正規返信、同じsourceの権限状態を有限に照合し、
レビューと同headのCI成功を実確認してから通常マージを行う。
