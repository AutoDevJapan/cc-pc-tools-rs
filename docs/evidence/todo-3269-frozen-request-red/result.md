# ToDo3269 / Issue277 の固定ソース比較結果

指定の1コマンド1回の検査を完了。製品のRedを1件確認し、会話再開の対照1件は合格した。親ToDo3081の実装全体の完了ではない。

- source: `519cd0cb17e6a7b9a7c4830ae98ac40208f8771a`、ref `scheduler/todo-3081-frozen-request-red`。
- 製品は `072c776898621d552bf1cf831014171ed105961c`。差分は `cc-todo/tests/product_review.rs` と `localai-web-rs/src/model_resume_tests.rs` だけ。担当によるソース・型・fixtureの変更は0。
- 実ジョブ: `20261021-124551-todo-3269`。実行時ブランチ: `scheduler/todo-3269-frozen-request-red`。
- 実時刻: 2026-10-21 12:49:55.6879102〜12:50:51.8002680 JST。Cargoの実終了コードは100。コンパイル53.51秒、選択された2件は0.520秒。1合格・1失敗、253件はフィルターで省略。
- cargo割当で共有ゲートを使い、専用target・既存sccache・ビルド上限10/テスト上限8で実行。`CreateNoWindow=true`。GPU/Civ6/実AI呼出しなし。

## 製品のRedと到達範囲

`watchdog_review_recovery_passes_scoped_routing_and_provenance_through_tick` は、最初の `current=true` の復旧で `model_resume_tests.rs:166:9` のassertに到達し失敗した。

```text
assertion `left == right` failed
  left: None
 right: Some("gpt-6.1-sol")
```

本番の `cc_todo::recovery::delegation_request` が旧policyのSolを入れた本文を生成したことを先行assertで確認し、API受付・Console tick・起動1件のassertは通過した。その起動に `--model-recovery-from` が付いていない。依頼作成・fixture・compileの失敗ではなく、指定された製品のRedに数える。

ここで停止するため、起動の `--model` がAstraかを調べる後続assert、作者/除外/出所/Max/read-onlyと実DB保持のassert、2周目の `current=false` は未到達。起動モデルがSolだったことや旧repair陰性が実行で確認できたとは主張しない。

`explicit_console_resume_excludes_retired_threads_and_keeps_compatible_threads` は合格した。退役していない旧threadの再開、退役した旧threadの除外、旧threadが退役済みでも互換の新threadを再開する3条件を通過した。

## 記録と制限

実行引数・source/tree・時刻・PID・資源はrun.json、終了後のソース不変確認はsource-after.jsonに保存。生のstdout.logは0 byte、stderr.logは46,187 byte。コンパイル出力、テストの捕捉stdout/stderr、最終Summaryと末尾の `error: test run failed` を含む。SHA256はlog-hashes.jsonとSHA256SUMS.txtに残す。

記録補助は、子の終了コード100と終了時刻を保存した後、PowerShellの `Task.WhenAll` の多義的なoverloadで終了コード1になった。生ログは書き換えず、終了後に両テスト結果と末尾まで確認した。copy taskの成功/EOF状態を明示保存できていない点はobserver-error.jsonに残す。補助の失敗と製品のRedを分け、Cargoを再実行していない。

Green、古い検査、全workspace、Clippy、release、配布、実AI受入は実行していない。同じモデルの独立checkであり、別モデルレビューではない。PR作成・mainへのmerge・親/他担当フォルダーの編集・親ToDoの状態変更・Discord直接送信はしていない。

後片付けはcleanup-plan.json/cleanup-result.jsonに保存し、退避・保留があれば実測容量と次の手を親へ返す。親が結果を永続記録し、元の話題のDiscordへまとめる。本人判断は不要。

## 生成物の退避と次の手

自分の専用targetの生成物は1,436,299,440 byte（約1.34GiB）、2,182項目。safe-deleteのplanが容量を理由に通常削除でなく退避を指定したため、その指定どおりquarantineを1回実行した。削除の拒否や再試行はない。

退避先は `C:/Users/makhmeto/Downloads/削除希望/20261021-cc-pc-tools-rs-todo-3269-frozen-request-red-d111d047`。所有IDは `d111d047a5d0d972c63580ef437f7392`。元の一時パスは消えており、退避先と所有記録のquarantinedを確認した。回収した空き容量は0 byteで、生成物は退避先に残っている。退避先全体の実測値にはsafe-deleteが追加した理由.txtを含み、cleanup-verified.jsonに記録する。fixtureの自動後片付け後のconsole-tests配下は空である。

親はこの退避先・所有ID・容量を結果記録に含め、既存の削除希望清掃へ引き継ぐ。本人の判断は不要。製品ソースと小さい検査証拠はGitで保持する。この担当は他担当の生成物・共有sccacheを片付けていない。
