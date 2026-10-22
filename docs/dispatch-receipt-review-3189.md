# 3189のレビュー対応

3198の読取レビューはbase cb26d311 / head e846181、Must 0。
実ジョブ20261021-061543-todo-3198と対応sessionのturn_contextで
anthropic/claude-opus-5-5 / maxを確認した。原応答とUTF-8 SHAはprivate記録へ保存した。
静的レビューと実検証を区別し、未読の呼出先を読了扱いにしない。

- 提案1・4: preparedの元予約だけを対象にし、一意な実起動と安価な全条件が一致してから正式guardを呼ぶ。
  phase違いと起動receiptなしの検査を追加し、旧コードで失敗を確認した。
- 提案2: 消費した待機のkeyは履歴の照合用に残す。正規のrelease_readyがjob/key/selectionを除去する。
  実際のlinked worktreeで解除後のprepareを通し、新しい32文字のkeyが生成されることを確認する。
  過去のjob/keyを新担当へ流用しない。
- 提案3: dependency_readyの除去と実started_atの優先を通常linkに合わせる。
  終了済み実行をrunningと記録しないため、result_pendingを維持する。
  このphaseは、既存reconcile_itemへ未回収結果を渡す途中状態であり、新しい実行中担当の表示ではない。
- 提案5: 自動巡回は回収エラーを対象IDごとのguardで記録し、後続の回収・依存解除へ進む。
  todo.lockを実際に保持した検査で、旧コードの巡回中断を再現した。
  明示CLIの競合は成功とせず、理由を返す。
- 提案6: 不適格なapplyは失敗を返す。同一の実回収receipt・世代・履歴を持つ再実行だけを
  already_recorded / saved=falseと区別する。停止や新しい指示への書き込みは行わない。
- 提案7: failedの理由、守衛呼出しなし、linked worktree、通常prepareの新keyを検査する。
  行変更の注入を初回確認前と最終guard中へ分け、正規ロック内の行全体比較も検査する。
  待機は実行結果の定型文だけで推測せず、正規に保存された依存と実終了を根拠にする。
- Nit: SHAは大文字・小文字を区別せず照合する。検査件数の誤記を訂正する。
  fixed_review_requalificationやfinite_researchの追加指示は再現せず、不一致なら無変更で拒否する。

元3126の実回収と共有実行物の反映は、新しい差分のレビュー・必要CI・通常merge後に行う。
自分3189の今回の実担当は同じ不具合で未連結だったため、全検査が成功したe846181生成物
（初回Must 0の固定版）による対象1件の正規CASで連結した。新しい差分の適用や共有配布ではない。
原応答と実操作の記録を保持し、前担当や別担当へ偽装していない。

## 3208の追加確認とCI

実ジョブ20261021-072844-todo-3208と対応turn_contextでOpus5.5/maxを確認した。
base e846181 / head a14fe15の追加レビューはMust0。原文はprivate記録へ保持する。

- S1: 回収のロック待ちだけが失敗し、その後の依存解除ではロックが取れた場合に、
  未連結のままkeyが消えることを実ロックでRed再現した。失敗IDを返し、その回の解除から除外する。
  他の完了した依存は解除し、次の巡回で残した起動receiptを回収できることを確認した。
- S2: 3126を回収してsaved/already_recordedを読むまで3189を完了しない。
  phase/question_close_pendingを含む最終CLIで、実際の状態を再検査する。
- N1/N2/N3: 未評価の守衛はnull、理由を確定した不一致はfalseとし、no-opの印も検査する。
  不適格なapplyは入力形式の誤りと異なるReceiptIneligibleを返す。
- Build speed39505456362は1304検査中1303成功、linked worktreeの受入1件がInputRejectedで失敗した。
  同じ実体をjunctionから参照するとworkspaceだけが不一致になることをRed再現した。
  checkpointと同じ既存resolveをGit比較の前に使い、実体とbranchの照合を維持する。
  この修正でCIの実失敗が解消したかは、新しい固定HEADのCIで確認する。
