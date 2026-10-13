# ToDo2493の再開記録

2026-10-13 11:27 JST、標準ジョブ `20261013-112211-todo-2493` が作成。

## 元の依頼と範囲

正規Discord APIで保存された本人投稿に対応する。元投稿URLは https://discord.com/channels/1552944707464597524/1559229635412230294/1559371702490767391 。本人IDと実対象を保持し、a/bを質問原文なしで解釈しない。元の未完了を消さない。結果の配送は独立受付が追うため、同じ進捗を手動でDiscordへ重ねて送らない。

元登録範囲は `D:/CC/Work/unattended-operations-20261013`。この専用worktreeのブランチは `scheduler/todo-2493-discord`、開始時のHEADは `602eb63eb6ff6e25b91938eb8f7c853230fb7bf9`。remoteはAutoDevJapan/cc-pc-tools-rs。開始時のworktreeは変更なし。

## 完了した確認

- D:/CC/AGENTS.md、対象AGENTS.md、shared-todo、japanese-report-writing、discord-owner-ask、work-scheduler、durable-task-handoff、git-github-workflowを確認した。CLAUDE.mdは対象worktreeの根に存在しない。
- 元登録範囲と専用worktreeはscrub391で非保護・分類確認済みと判定された。
- 元2493が現在の標準ジョブでdoingになっていることを共有ToDo CLIで確認した。自分のstart/done/progressは変更していない。
- 独立確認をToDo2494として `--spec --subwork-of 2493 --parent-job-id 20261013-112211-todo-2493 --subwork-kind check` 付きで登録した。CLIの受付結果は `added 2494`。元依頼を複製したのではなく、保護判定と受付の不一致だけを確認する。受付は作業成功・原文照合成功ではない。
- 受付の作成元ソースを読んだ。設定済み本人IDと正規APIの照合を持つが、保存済みのこの投稿への適用結果は本文を読む前の未確認条件である。
- 原設定/旧台帳の元2467と無人運用の親2488は未完了で保持されている。親worktreeには他担当の変更があり、この担当は編集していない。

## 未解消の条件

明示された投稿ファイルに対するguardは `client_resource:true,classification_unverified:true,original_registry_recovered:false`。本文・質問文脈を読まず、本人の具体的な依頼を推測していない。これは上位の自動審査による拒否ではなく、このPCの原保護設定が欠落した状態の分類結果である。

投稿のJSONとMarkdownは原位置に保持する。全許可・空原本・コピーによる判定回避を作らない。追加指示のsteeringディレクトリは11:27 JST時点で不存在。本人へPC操作や技術的な許可を頼んでいない。新しい質問は登録していない。

## 残作業と次の一手

1. 登録済みToDo2494の有限な技術確認の結果を受け取る。同じ確認を再登録しない。仕様は `docs/todo-2493-receipt-check.md` にあり、共有ToDo CLIによる保存と受付が成功した。起動と実処理は別に確認する。
2. 正規な閲覧経路が確認できたら、保存原文・取得元・本人ID・日時・元質問・実対象と範囲を照合して元の作業を続行する。対象資源のguardは別に実行する。
3. 新しい返信を実際に反映した時点でだけ、`unattended/applied/<message_id>.json` に元todo_id、message_id、実job_id、実revision、verificationを保存する。現時点では受領・反映済みを保存していない。
4. 依頼に必要な実装、関連検査、別モデルMust解消、CI、マージ、既存実行版への反映、後片付けを完了まで追う。重いCargoが必要なら資源付きの独立作業を共有ToDoへ登録する。

原文を確認できていないため、達成率は推測していない。技術確認だけを待って担当を終了する際は必ず元2493へ `todo wait 2493 --on 2494` を登録する。オーナー判断用blockを使わない。

## 終了前に保存した実結果

- 再開手順と有限な確認仕様をコミット `a211010ec288529af2c4664162daf36444222b78` に保存し、originの同じ作業ブランチへpushした。`git ls-remote` のHEAD一致を確認した。GitHub APIの実所有者はAutoDevJapan。
- `todo wait 2493 --on 2494` は成功し、読み返しで元2493がwait、依存先2494がtodoであることを確認した。登録済み仕様を持つ依存先が完了すると、元2493が標準スケジューラーで再開する。まだ独立確認の起動・成功を主張しない。
- 外部push前にもsteeringディレクトリの不存在を確認した。本人の投稿原文を未確認のままapplied記録を作っていない。PR作成、CI、マージ、実行版の変更は未実行。
- 作成したのはこのworktreeの再開文書と共有ToDoへ保存された仕様だけ。検証用のプロセス、ブラウザタブ、Docker資源、ビルド生成先、一時フォルダは作っていない。親worktreeの変更と原資料は保持した。
