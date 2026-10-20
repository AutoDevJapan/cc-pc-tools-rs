# ToDo3171: 作者が不明なレビュー登録の比較検査1件

元ToDo3081 / Issue277の追加Red検査だけを扱う。製品の修復や完成判定は担当しない。

- 指定ソース: `5ba9a1c274d7a882387542d15c39fd2b2858f990`
- 比較元: `5a82f08`。差分は`cc-jobs/tests/parity.rs`のTest.run補助4行だけ。
- 自分のブランチ: `scheduler/todo-3171-standalone-review-red`
- 元の自分のブランチ: `scheduler/todo-3171-1-3081`
- 自分のジョブ: `20261021-033543-todo-3171`
- 対象: `capacity_regressions::standalone_review_registers_without_guessing_authors_from_a_shared_cwd`
- `Test::new`はFake実行系と専用の架空データを使用する。本物のCodex子ジョブは起動しない。

実行前にD:/CC/AGENTS.md、プロジェクトのAGENTS.md、関連する検査規則を確認した。
CLAUDE.mdはプロジェクト内に存在しなかった。元範囲と自分のworktreeのguardはfalse。
GitHub APIで実際の所有者AutoDevJapan、repository cc-pc-tools-rsを確認した。
ブランチ切替直後の正規scheduler checkpointはsaved=trueを確認した。

## 再開するとき

`run-started.json`があれば、同じ検査を再実行しない。`run-result.json`と
完全な`stdout.txt`/`stderr.txt`を照合し、残る保存工程だけを進める。
run-resultが無い場合も、既存の実行とログを確認してから処理する。

実施済み: 指定の検査を1回だけ実行した。終了コード100、1失敗、0合格、390件未実行。
1947行の登録結果のunwrapで作者モデル情報不足のエラーになり、1950〜1952行のassertは未到達。
完全ログ・実HEAD・終了コード・停止箇所を保存した。詳しくはresult.mdとrun-result.jsonを参照。
専用のビルド生成先は所有付きツールのplan指定どおりquarantine / sizeで退避した。
保存を終える手順: この証跡をcommit/pushし、実HEADで正規scheduler checkpointを行う。
最終結果は`finalization.private.json`、その要求と応答は`checkpoint-final.private.json`に
保存する。HEAD自身を記録するこれらのファイルはGit管理の対象外である。
再開した場合はその記録と実HEAD・remoteのtipを照合し、未完了の保存工程だけを行う。
検査を繰り返さない。

製品コードの修正、追加検査、Green、他のRed6件、全workspace、Clippy、
doctest、release、元3154/3081の変更、PR、merge、配布、本人への質問・通知は行わない。
共有ToDoの状態はスケジューラに任せる。
