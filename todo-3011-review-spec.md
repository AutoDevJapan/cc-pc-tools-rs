# ToDo3011のrepair-guard変更を別モデルで確認する

元3011の独立レビュー。レビューは既存OAuthサブスクのClaude Sonnet 5.5で行う。子Codexジョブを直接起動しない。GPU、APIキー、従量課金、Cargo、Civ6、本人PC操作は使わない。

対象repoは実GitHub所有者AutoDevJapan/cc-pc-tools-rs、PR252。作業先 D:/CC/Work/unattended-operations-20261013-wt/todo-3011-ci。
基準 origin/main 3423a38。レビューする固定HEAD d60a2c801321bac76479a4fcb7aeac8fd54b7f69。
`git diff 3423a38..d60a2c8` のrepair-guard/discordask_windows.go、discordask_windows_test.go、main_windows.go、runtime_windows.go、docs/todo3011-runner-guard.md、docs/todo3011-checkpoint.mdだけを読む。対象sourceのAGENTS.mdと最新D:/CC/AGENTS.mdを起動時に読む。

目的: 消失した原保護名簿の復旧ではなく、Discord無人runnerの3資源（D:/CC/Tools/discord-ask/unattended-runner.exe、discord-ask-tick-hidden.vbs、unattended/config.json）と、更新用の2つの名前だけを、検証済み修復repoのソースが名指しする運用資源として正規scrub391 --guard-pathに分類させる。
確認する点: 正確な葉以外（兄弟ファイル、会話記録、通知認証ファイル、別ディレクトリ、大小文字違い、ADS、リンク/junction/reparse、ディレクトリ）が通らないか。既存のlegacyMatch/保護判定とsourceVerified(base)を迂回しないか。product-ciのruntimeLeaf抽出で挙動が変わっていないか。分類時に本文を開かず、設定値や認証ファイルを読まないか。無制限allowlistや空の除外一覧になっていないか。テストの強さ（偽陽性・偽陰性）。

読み取り専用でレビューする。対象の3資源や、秘密設定、通知認証ファイルを開かない。実装変更、checkout/reset、merge、deploy、状態変更、別担当の起動をしない。Must/Suggestion/Nitで仕分け、実害・根拠・対象file:line・具体修正を書く。誤検知と思われても根拠を記載し、未確認を検証済みとしない。
結果はこのworktreeのtodo-3011-review-result.mdへ日本語で保存する。冒頭にモデル・固定HEAD・Must件数を記録する。Must0でも根拠を明示する。結果を保存して共有ToDoのレビューだけを完了し、元3011や親2962をdoneにしない。