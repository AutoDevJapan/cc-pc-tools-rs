# ToDo2628の共有ToDo有限取得を別モデルで確認する

元2628の独立レビュー。実装担当はCodex。レビューは既存OAuthサブスクのClaude Sonnet 5.5で行う。子Codexジョブを直接起動しない。GPU、APIキー、従量課金、Cargo、Civ6、本人PC操作は使わない。

対象repoは実GitHub所有者AutoDevJapan/cc-pc-tools-rs。
作業先 D:/CC/Work/unattended-operations-20261013-wt/todo-2628-todo-cli。
基準HEAD 0b703782a81e5c262f771caa849e766872591b2e。
レビューする固定HEAD 080bf6cc6628ecc87487747733363e868d01f973。
`git diff <基準HEAD>..<固定HEAD>` のlocalai-web-rs/src/agent.rs、src/bin/cc-console.rs、src/lib.rs、src/todo_listing.rs、docs/todo-listing-2628.md、todo-2628-checkpoint.mdだけを読む。
対象sourceのAGENTS.mdと最新D:/CC/AGENTS.mdを起動時に読む。

元2626実測: 2026-10-15 00:10 JST、稼働cc-console todo listがexit2/cli_error/response too large。旧board()は全原台帳を複製して返す。16MiB上限撤廃、原記録の消去・圧縮・状態変更は禁止。親2488のPR144はマージ・5本配布済みで再配布しない。
仕様はdocs/todo-listing-2628.md。本文・履歴を一覧へ複製せず、有限ページ/番号指定で必要な状態・依存・担当を取得する。原privacy欠落でもfree textを返さず、新APIは常に数値/許可済状態/不透明担当ハンドルだけ。原保護/委譲/依存停止は維持する。互換性・サイズ・完了と受付の区別を確認する。

確認済みの事実: 新しい関連nextest6/6実成功、run caf4bd34-25b8-4358-8d94-f5cb9b7a9bb5。原17MiB履歴/混在参照、privacy有無、GET原bytes不変、実HTTP形、HTTP失敗、途中切断、Content-Lengthとchunkedの16MiB/1MiB上限を検査。固定HEADに追加した最大ページ予算テストを含むp localai-web-rs全件検査を実装担当が実行中。最終workspace/doc/Clippyは正規Build speed CIに任せる。
未確認: 配布、稼働CLI成功、server復帰、CI実成功。稼働CLIは正式guardでclassification_unverified/client_resource=trueであり、保護判定を変更しない。原registry/秘密/他担当の成果を読まない。

読み取り専用でレビューする。実装変更、checkout/reset、merge、deploy、状態変更、別担当の起動をしない。参照はこのsourceの取得CLI/API/関係する検査/運用文書に限る。Must/Suggestion/Nitで仕分け、実害・根拠・対象file:line・具体修正を書く。誤検知と思われても根拠を記載し、未確認を検証済みとしない。
新規テストの強さ、実CLIexit0/ok:trueへの意味、ページ漏れ/取得失敗/依存担当の表示、保護不明時の挙動、HTTP/app error分類、応答上限、元台帳不変、旧CLI/APIとの互換性を重点確認する。
結果はこのworktreeのtodo-2628-review-result.mdへ日本語で保存する。冒頭にモデル・固定HEAD・Must件数を記録する。Must0でも根拠を明示する。結果を保存して共有ToDoのレビューだけを完了し、元2628や製品全体をdoneにしない。
