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
