# ToDo2628の再開記録

## 2026-10-15の再開と指摘修正

元worktreeとsessionから再開した。開始HEADはd06d7f40f741cc5b3c8bba46116a1c04aa82cc29。
最新main5010498531449fa81640e3f2570be5e2c5c623ccをe37ad30の通常mergeで統合し、SourceCore2488の変更を保持した。製品CI2648のsource/runtimeは変更していない。

受領した原レビューはreview-2633-received-original.md。正規source job20261015-003819-todo-2633のlast_messageをメモリ内で照合し、5774文字、anthropic/claude-sonnet-5-5、SHA256 9da18c3a8cf7a61a4f04834efdb45f4218c8420080ac9803a9f3d4d26b16fb78一致を確認した。
そのMust0は080bf6ccの静的レビューだけであり、修正後HEADのレビューと偽装しない。readonly担当に保存やtodo更新を要求しない。

- S1: 実担当をscheduler.job/worker_receipt.job/単一jobから取得し、activeJob/phaseと互換agentを揃えた。
- S2: 共通pending_onの未完依存、wait_readyの固定condition/数値期限、recovery_unverifiedを保持するmetadataを追加。
- S3: todo.exeの同じjob_refs関数を公開して再利用し、object形/不正文字列をjobsUnverifiedに数えた。cc-todoの変更は読取関数の公開とpending_onの再公開だけで、保存・起動動作は変更していない。
- S4/S5: completeと要求照合を追加。revisionは出力metadataだけを対象にして履歴を再直列化しない。最大3回の有限再試行を文書化。
- S6/S7: 実reply/redact応答のCLI共通validator受入、state/mark/backup不変、秘匿検査両ページ、HTTP分類、ちょうど1MiB、job種類を前置したハンドルを検査した。

最新の関連nextestはe1e8645f-f5c7-49c8-a705-d6425c6b0ed0で8/8成功。
共通job_references/wait/readyは25/25成功。完全ログはtodo-2628-followup-final-related.logとtodo-2628-shared-rules-corrected.log。
初回の共通検査は存在しないtodo_waitというtest target指定でcompile前に失敗。正しいwaitへ直した。元失敗ログを保持した。

旧PR171 CI38589625129の失敗は既存dependency fixture取消のinspect/OpenProcessアクセス拒否。main501のCI38601682945は別のnative recovery取消で同じアクセス拒否を記録している。未知を終了扱いすることで緩めず、新固定HEADの正規CIを確認し、再現した場合は既存core担当と原因を照合する。
CLI正式guardは現在もclient_resource=true/classification_unverified=true。原guard/拒否済みscopeは変更しない。配布と実CLI取得は未確認。

次工程: 追加diffだけを本文付き静的レビューへ登録して原応答を実装側で保存する。最新固定HEADの正規CI成功/Must解消→merge→対象確認済みの既存経路で配布→実CLI/番号/全ページ/API取得と原bytes保持。元31MB台帳の全件取得は未確認であり、テスト成功で代用しない。

## 完了した調査

- 起動時のD:/CC/AGENTS.md、専用sourceのAGENTS.mdを読み直した。CLAUDE.mdはsource内にない。
- remoteとGitHub APIでAutoDevJapan/cc-pc-tools-rsを確認した。
- 最新origin/mainとこのworktreeの開始HEADは0b703782a81e5c262f771caa849e766872591b2e。
- 原sourceはtracked変更なし。他担当の未追跡成果は保持した。
- 修復sourceと元登録sourceはguard=false。稼働CLIはclassification_unverified=true/client_resource=true。保護設定は変更しない。
- 旧board()が履歴を含むitemsとgroupsを複製することを確認した。

## 実装中

localai-web-rsの新規有限取得API、CLI、意味のある回帰検査とdocs/todo-listing-2628.mdを追加。
他クレート、原記録、モデル切替、共有キューは編集していない。
最初の関連検査はAPIクエリの挿入位置誤りでcompile失敗。正しいHTTP入口へ修正した。
関連nextest実成功: run caf4bd34-25b8-4358-8d94-f5cb9b7a9bb5、6/6成功。
追加の最大ページ予算検査とCLI失敗分類を含むlocalai-web-rs全件nextest実成功:
run 713da660-754d-4f2a-b47b-8baecd3357ca、335件成功・3件skip、13.913秒。
完全な検査出力はtodo-2628-related.logとtodo-2628-localai.logへ保存した（Git除外）。

実装を保存した固定HEAD: 080bf6cc6628ecc87487747733363e868d01f973。
PR: https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/171。
別モデルレビュー: 共有ToDo2633、claude-sonnet-5-5。spec保存・doingと担当記録を確認した。結果はまだ未取得。
CLIのmodel引数はslashを受け入れないため、正規モデル名claude-sonnet-5-5で登録した。
スケジューラーはanthropic/を補完する既存仕様をsourceで確認した。失敗したaddは項目を作っていない。
Build speed CI: https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38589347209。
CodeQL CI: https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38589347200。
両者の固定HEADは上記実装HEAD、現在queued。開始・成功・merge・配布は未確認。

検査生成先の所有記録: f49c14cb8bc76f0d4e97563ee358396e。
path: C:/Users/makhmeto/AppData/Local/Temp/codex-owned-f49c14cb8bc76f0d4e97563ee358396e。
生成先はこの配下target。safe-deleteのplan/別呼出し削除/finishを使う。

## 残る工程

1. 関連nextestと実HTTP形・原bytes保持検査は成功した。新しいMustがあれば関連検査を追加・再確認する。
2. 固定HEADの別モデルレビューToDo2633のtodo-2628-review-result.mdを読み、Mustを直す。
3. PRを作成して既存Build speed CIのworkspace/doc/Clippy実成功を確認。固定HEADでmerge。
4. 稼働対象の正規guard確認、並行source確認、bytes/hash照合、CLIと必要なサーバー更新。
5. 実CLI終了0/ok=trueと原台帳不変を確認してreceiptを保存。元2626へ正確な成果を渡す。
6. 自分の検証生成物を回収。ブラウザとGPU/Civ6は使用していない。

自分の共有ToDo状態はスケジューラが管理する。別レビューToDoだけを待つときはtodo wait 2628 --on <レビュー番号>を記録する。
