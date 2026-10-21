# ToDo3126 再開記録

- 最終結果: この限定修復は完了。PR307のマージ、正規release CLIでの実移行と
  読戻し、原記録の保存・一時フォルダーの整理を完了した。3050/3070は3084の完了待ち。
- 2026-10-21 10:30以降の再開: 独立修復3189の実結果を元3126の正規メモで確認した。
  3189はPR328/334を通常マージし、必要CIと別モデルmaxレビュー、担当記録の実回収、
  scheduler配布と無変更読戻しを完了した。終了済み担当をrunningへ戻していない。
  3189の完了後、通常の依存解除で今回の実担当20261021-103042-todo-3126が連結された。
  自分の真のCC_JOB_IDと元branch、実HEAD94ed755によるcheckpoint-workspaceは
  saved=true / launched=0 / source_files_changed=falseを返した。
  同じ移行・質問・成功済み検査・ビルドを再実行せず、現在のwait_on=[3084]を正規APIで確認した。
  この文書の最終コミット後の実HEADも同じ正規手順で保存し、原応答を私的な最終記録へ保持する。
- 元Consoleの機能修復・UI・配布と質問監査の残作業は3084/3110が保持する。
  本項目からそれらを完了扱いにせず、追加の本人判断やPC操作は求めない。

- 作業: 回答済みのConsole旧本人待ち3050/3070を、正規CLIで既存3084依存待ちへ移す。
- Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/305
- 作業ブランチ: scheduler/todo-3126-console。基準main: 6d5c46e62100a212dbf223e29dc8b0f979bdf7aa。
- 原証拠: 依頼が指定したtodo-2903-qualified-release/.ci-workの3110各資料を保持。
  本人ID・元質問・原回答は照合済み。再質問せず、旧UI候補を配布しない。
- 仕様: docs/answered-wait-3126.md。正常CLIのATDDを追加。
- Red: `cargo nextest run --locked -p cc-todo --test answered_wait -E 'test(answered_wait_moves_only_verified_manual_hold_to_existing_dependency)'`。
  1件が `unrecognized subcommand 'answered-wait'` で失敗した。既存コードは未変更。
- 2026-10-21 00:20台の観測: 3050/3070はblockとwait_on3084を保持。
  3084は3103/3121の技術依存待ち。本人待ちへ戻さず、他担当の状態を変更しない。
- 実装: `answered-wait` を追加。実台帳は提示済み原資料SHAと今回依頼原文を固定し、
  3050/3070だけを、保存済み依存を変更せずblockからwaitへ移す。通常Wait/Startは不変。
  対象行SHAの比較と正規ロック、原質問世代・原回答・元履歴・正式guard・実担当終了を照合する。
  正常Startの起動鍵なし割当と、終わった担当の古いrunning phaseを、実担当記録から検証して保持する。
- Green: cc-todo nextest 222件成功。新規は9件（CLI6、内部3）で、CRLF原資料、
  状態競合・未知保護・質問世代・二重キー・依存循環・担当停止/稼働/退役・再実行を含む。
  Clippyの等価な論理式整理1件を修正し再確認する。
- 実物の無変更検査: 3050/3070の両方が成功し、status=block / wait_on=[3084]を読戻した。
  `--apply` は未実施。API原文の受領を機能の実反映と記録していない。
- 前回の検査後: 現head新規9件と利用側169件の計178件成功、対象4ライブラリのdoctest正常終了、Clippy成功。
- レビュー3142: 実ジョブ20261021-011248-todo-3142、通常CLIと対応sessionで
  anthropic/claude-opus-5-5/maxを確認。固定base6d5c46e/headc00289a、原応答は
  review-3142-received-original.md。Must2件を受領し、レビューだけが完了。製品の完了ではない。
- 対応: 本番の現在メモSHAと対象行SHAを固定。既存owner_instructionへ原回答/今回指示/原資料を追記。
  原履歴欠落の無変更検査、起動予約なし、PID再利用/出生不明、期限付きguardも確認する。
  指示配送欠落と予約キー許容は追加の正常CLI検査2件でRed、修正後13件Green。
  最新の実物3050/3070の無変更検査は、固定した以前と同じ状態SHAで両方成功。
- 修正後: cc-todo全226件成功。cc-schedulerの実prompt作成までを含む関連4件成功。
  対象2クレートのClippy成功、doctest正常終了。guard呼出しも実物で各約1.3〜1.5秒、無変更検査成功。
- 追加レビュー3157: 実ジョブ20261021-023950-todo-3157、対応session
  01a15a68-330e-7eb3-b8ee-ff5ffe93f692、anthropic/claude-opus-5-5/max、exit0。
  basec00289a/head639d9b3でMust0を確認。原応答review-3157-received-original.md、
  UTF-8本文SHA77779a1390c93727521b323cb84fb0b3311d9795e2e9e6e659355af4a31ea304。
- 3157の改善提案も反映: 原質問本文と判断対象、3050から3070への限定引継ぎを指示に明記。
  updatedは既存書式を保持し、領収時刻だけRFC3339。key/phase/last_jobの拒否を各々検査。
  変更後の機能13件成功。既存の226件・利用側の有効結果は影響がない範囲を再利用。
- 2026-10-21の実施結果: 最終head7e0be3eの必要CIは成功。
  Build speed39472463638、CodeQL PC39472463775を確認した。
  package-toolsは対象条件によるskip。利用上限で実行されなかったBugbotをレビュー成功とは数えない。
  PR307を固定head指定の通常mergeでmainへ取り込み、merge commitはcb26d3112d537e2f602fbed07108673517746898。
  配布用の生成元7e0be3eがmainの祖先であり、関連RustソースとCargo入力に差がないことを確認した。
- 実行物: この作業場所のtarget/release/todo.exe。
  SHA256=a4be80b1ddca717d27afe023ae5e90b0e4f1770df5b8bcdc88bdc3eabf4ece48。
  同じ生成物を、今回の有限移行用の正規CLIとして実運用した。既設の共有実行物は置換していない。
  ソース・SHA・実行結果はTODO-3126-RELEASE.private.jsonとTODO-3126-ACCEPTANCE.private.jsonに保持する。
- 実3050/3070へ、凍結した元状態SHAを指定してanswered-wait --applyを各1回実行した。
  両方がchanged=true、status=wait、wait_on=[3084]を返した。
  同じ根拠による再読取は両方changed=false。通常listでも待機を確認した。
  元メモ・担当履歴の表示・依存はstatus以外の変更がないことを照合し、原資料3点のSHAも再確認した。
  3084の状態と依存を保持した。同時刻の別担当による追記だけを別に記録し、書き戻していない。
- この修復の実移行は完了。Consoleの機能・画面の実反映と配布は引き続き3084側の受入で追う。
  元の質問の監査・正規配送・closeは親3110が保持する。同じ移行や質問を再実行しない。
- 画像sender/履歴readのPR296は編集しない。raw台帳編集・guard緩和・applied/jobの捏造はしない。
- 一時所有ID7a11b876847b64f5e8444bfa0f54070eのフォルダーは、
  必要な原検査と実操作の記録67件を保存・バイト照合した後、safe-deleteのplan/通常削除/finishで整理済み。
  原記録の私的archive/manifest、ソースと今回実使用した正常release CLIを保持する。
- 自分の共有ToDo状態はschedulerが更新する。レビュー等の別ToDoだけ待つときは正規wait --onを保存する。
