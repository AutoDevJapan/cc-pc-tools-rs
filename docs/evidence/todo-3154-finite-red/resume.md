# ToDo3154: Issue277の有限なRed検査

元ToDo3081から独立した検査。製品ソース・テストは修正しない。

- 開始時branch: scheduler/todo-3154-3081
- 開始時HEAD: b995d2e119f31dca3af8aeed77598c8713ea421f
- 証跡branch: scheduler/todo-3154-3081-red-evidence
- job: 20261021-023539-todo-3154
- 実行場所: この専用worktreeだけ。
- 所有付き一時フォルダー: codex-owned-b4e5f2d379be26397ee60a13f2b53458
- 所有ID: b4e5f2d379be26397ee60a13f2b53458

52bc2dd900b68bfee86e3f5acf9d7378d0c760dbへ固定済み。
指定のmodel_effort 2コマンドとparity 1コマンドは実行済み。
全てコンパイル成功後に実exit100。選択6件は0合格・6失敗。原ログと実行metadataを保存済み。
standalone reviewは引数の相互排他エラーでunwrapが失敗し、作者判定のassertへ未到達。
その後、5a82f08a57d0774c4e01fb0e8760a57a750a13c2へ固定してfinite_reviewを実行する。
全コマンドで完全stdout/stderr・実HEAD・実exit・実失敗assertを保存する。
ループ後続の未到達assertは確認済みと数えない。
APIキー・追加AI・GPU・Civ6・本物のCodex子ジョブを使わない。

実行中に再開した場合は、一時フォルダーの各jsonとpid/作成時刻、ログを照合し、
同じ検査を二重起動しない。製品の修正、全workspace、Green、Clippy、doctest、release、
PR、merge、配布は対象外。元ToDoと自分の状態はスケジューラが管理する。

残作業: 4本目、到達assertの照合、原ログを証跡へコピー、commit/push、
生成物の所有確認付き片付け、最終HEADのcheckpoint-workspace（saved=true）確認。
オーナーへの質問・通知は行わず、元担当へ最終返信で返す。
