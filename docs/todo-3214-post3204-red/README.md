# ToDo3214: 修正前の追加7検査の測定結果

固定ソースで指定の7件を実行し、**意図したassertで5件失敗、対照の2件成功**を確認した。
2コマンドともコンパイルとテスト起動に成功し、終了コードは100（テスト失敗）。
実装・テストは変更していない。この有限測定の完了は、親3103の修正・マージ・配布の完了を意味しない。

## 対象と比較前提

- 親3103 / Issue289 / PR302、実リポジトリ `AutoDevJapan/cc-pc-tools-rs`、ID `1406818713`。
- remoteとOAuth経由のGitHub APIで実所有者AutoDevJapan、認証ユーザーmashfrombandを確認した。
- PR302の観測headは `18d4f5efc32629092250469356696cbf0bc4b0db`、状態OPEN。
- 測定ソースは共通Gitオブジェクトの **`684bfba418f1b293b5ed2672038654ede6323fd6`** のみ。
  treeは `eddc0bbdc0ff62a5b8b797d6d21c8f1ec69b8308`。親の未コミットsource・後続commitは使っていない。
- 18d4f5eとの差分にあるconsole_ui.rsの5行削除は `#[cfg(test)] fn linked` 内である。
  今回は追加された直接表示検査が実際の表示関数を通る。削除したテスト補助関数を製品実装の修正と数えない。
- 以前のCI、Go検査、親の新しいGreenは今回の結果に含めず、再実行していない。
  別モデルレビュー・修正案の実装・親PR/配布は親3103の範囲である。

## 実行したコマンド

通常のcargo.cmdが呼ぶ `D:/CC/Tools/codex-jobs/cargo_gate.exe` を、同じ引数で直接起動した。
両方ともCreateNoWindow。共有ゲート、sccache、同じ所有付き専用target、最大10並列を使用した。
実引数・生成先・PID・時刻・出力SHA256は `command-N.result.json` にある。
各コマンドは1回だけ実行し、再実行していない。

```text
cargo nextest run --locked -p cc-job-policy --test product_identity -E 'test(mcp_live_requester_identity_drives_formal_dependency_priority) | test(mcp_live_requester_dependency_exclusion_controls) | test(mcp_history_only_product_job_does_not_borrow_code_priority)' --no-fail-fast
cargo nextest run --locked -p localai-web-rs --lib -E 'test(mcp_live_requesters_supply_product_priority_before_dependency_propagation) | test(mcp_duplicate_task_ids_preserve_every_private_job_edge) | test(mcp_missing_policy_has_no_runtime_binding_override) | test(mcp_related_private_ids_mask_both_display_lists)' --no-fail-fast
```

| コマンド | 実行日時（JST、2026-10-21） | コンパイル | 選択/実行 | 個別結果 | 選択外 | exit |
|---|---|---|---|---|---|---|
| 1 / cc-job-policy | 08:04:52.068–08:05:01.216 | 成功、ログ上8.00秒 | 3/3 | 成功1、失敗2 | 10 | 100 |
| 2 / localai-web-rs | 08:05:23.595–08:05:57.623 | 成功、ログ上33.27秒 | 4/4 | 成功1、失敗3 | 96 | 100 |

選択外106件はフィルターで実行されていない。過去の正式CIのskip件数とは別である。
開始から終了までは各9.148秒・34.029秒で、ビルド速度の比較測定ではない。

## 個別結果と到達範囲

| 検査名 | 結果 | 実際に到達した比較 | 到達していない範囲 |
|---|---|---|---|
| `mcp_live_requester_identity_drives_formal_dependency_priority` | Red | `alpha=false/edge=wait_on/chain=false`。依頼元10のassertは通過し、必要工程が期待10/実際30で失敗。product_identity.rs:189 | 残る7組。特にalpha=true、work_wait_on、連鎖ありはRed/Green未測定 |
| `mcp_live_requester_dependency_exclusion_controls` | PASS | parent_done、parent_block、wait_ready、child_done、no_product_jobの全5条件 | なし（この検査の定義範囲） |
| `mcp_history_only_product_job_does_not_borrow_code_priority` | Red | 履歴だけで関連するαのジョブが期待60/実際30。product_identity.rs:245 | なし（単一assert） |
| `mcp_live_requesters_supply_product_priority_before_dependency_propagation` | Red | `masked=false/alpha=false/edge=wait_on/chain=false`。依頼元行10のassertは通過し、必要工程行が期待10/実際30で失敗。operations_product_identity_tests.rs:656 | 同じ組のproductId確認と残る15組。masked=true、alpha=true、work_wait_on、連鎖ありは未測定 |
| `mcp_duplicate_task_ids_preserve_every_private_job_edge` | Red | 同じid=6の先頭だけにprivate jobへの辺がある。タスク詳細は期待Err(404)/実際Okで、noteに合成マーカー本文を返した。job詳細は実際Err(404)。同:688 | collect自体は呼ばれたが、AND条件の先頭で失敗するため後続のマーカー不在条件は未評価。タイトル・一覧・report全部の漏れを実測済みとはしない |
| `mcp_missing_policy_has_no_runtime_binding_override` | Red | masked=false、設定ファイルなしで期待false/実際Null。同:703 | masked=trueは未到達 |
| `mcp_related_private_ids_mask_both_display_lists` | PASS | display_todos/display_jobsの直接呼出し。表示件数・note/reportの空文字・合成マーカー不在の全5assertが成功 | なし（この検査の定義範囲） |

M1について今回のlive伝播検査で実測したのは期待10/実際30だけである。
履歴関連の別検査では期待60/実際30を測定したが、これをliveのalpha=trueケースの結果に流用しない。
M2は合成データのnote本文が詳細APIから返ることを実測した。顧客データや原保護一覧は使用していない。

## 親3103へ返す修正案

以下は固定ソースを読んだ上での実装上の提案であり、この担当では実装していない。

1. `cc-job-policy/src/priority.rs::job_ranks` は、live jobの検証済み製品が依頼元に与える順位を
   解決してから、正式なwait_on/work_wait_onへ伝播する。現在は先にcode repo側の30を伝播し、
   後から依頼元だけを10へ変更する。製品そのものの順位と依存で得た順位は分けたままにし、
   done/block/wait_ready/完了した子/製品jobなしの除外条件を保持する。
2. `operations.rs` の通常表示・保護表示も、live製品を反映した同じ伝播前の入力を用いる。
   履歴だけの関連をliveの依頼やcode repo側の順位の借用根拠にしない。
3. `operations_identity.rs::assignments` の `by_id` は同じidを最後の1行へ縮めている。
   privacy向けの参照辺は全行から集める。単一の実行先を選ぶ処理とは分けて、
   `private_todo` と `worker_detail` まで先頭行のprivate job辺を保持する。
   `console_ui::PrivacyIndex` 自体は複数行を保持しており、今回その直接表示検査は成功した。
4. 設定ファイルが存在しない場合の `productBindingsOverridden` はboolean falseを返す。
   現在のOptionをそのままJSON化する処理はNullを返している。

修正後には今回未到達だった組を改めて検査する必要がある。この担当の測定結果を新Greenに流用しない。

## ソースの同一性と原記録

新規tempはsafe-delete createで所有記録を取得した。最初の空Tempは保護判定で分類未確認だったため
ソースを入れず、plan→別呼出しの通常削除→finishを完了した。その後、guardを通る自分のworktree内へ作成した。

通常archiveの差分は `tools/codex-progress-tui/activation/codex.cmd` の1ファイルだけだった。
blobは3,146 bytes、展開は3,222 bytesで、CRLF→LFの変換を戻すと完全一致した。
元のGit設定・属性を変えず、所有領域内のGit管理領域とinfo/attributesで変換を無効にして再展開した。
この専用Git領域は元のオブジェクトを読むだけで、元のref/source/configを編集しない。
最初の展開補助でPAX global headerの扱いを修正した記録も保存した。これはテスト実行前であり、
Rustコマンドの再実行ではない。

固定treeの全1,184ファイルについて、Git blob ID・実サイズ・SHA256・余分なファイルの不在を照合した。
初期、各コマンドの直前/直後、最終の全6回で完全一致。全blob一覧は各source JSONに保存した。

- `command-N.stdout.log` / `command-N.stderr.log`: コンパイルとテストを含む全文、途中で切っていない。
  両stdoutは0 bytes。nextestのテストstdout/stderrもrunnerのstderrログ内に含まれる。
- `command-N.started.json` / `command-N.result.json`: 実引数、時刻、PID、exit、全文ログのサイズとSHA256。
- `results.json`: 7件の結果、到達した比較、未到達の条件。
- `source-before.json` / `command-N.source-{before,after}.json` / `source-final.json`: 全blob照合。
- `archive-initial.json` / `archive-raw.json`: 改行差分の特定と専用属性、raw archiveのSHA256。
- `provenance.json`: 実所有者・repo ID・OAuthの本人名・固定commit・gate/sccache実行物のSHA256。
- `cleanup-*.json`: 所有領域の停止確認・plan・削除・finishの記録（後片付け後に追加）。
- `RESTART.md`: 再開時の確認手順。完了済みコマンドを重複実行しない。

親のToDo状態、PR、配布先を変更せず、Discord投稿や受領用機能の追加は行っていない。
