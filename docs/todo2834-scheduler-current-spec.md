# 現在の製品継承先をschedulerへ接続する（元2834）

元2834の独立Rust実装。原本復旧や旧192件の解放をせず、本人の現在の継承先だけが通常の受付で実開発へ進むよう直す。新しいCargo付きToDoで実装・検査・別モデルレビュー・CI・通常merge・正式scheduler更新・実受付まで担当する。

最初にD:/CC/AGENTS.md、repo AGENTS.md/CLAUDE.md、親の `D:/CC/Work/unattended-operations-20261013-wt/todo-2834-work/docs/todo2834-current-succession-contract.md` を読む。親worktreeの原契約と指示を保持する。対象の実GitHub owner AutoDevJapan/repo ID1406818713を確認し、通常の保護判定と作業場所を使う。

## 再現した原因

PR221/c9f0b26のGo guardは正式配布済み。AOの新ToDo2839は02:44のwait-retryを正規resume-readyで解除しtodoだが、dispatchは原名簿NotFound時にPC修復3project/2407以外を無条件で拒否する。rootの分類が成功してもAOは受付されない。02:44:59の通常stepはreconcile/FileNotFoundError。自然tickの既存repair-stepはerrors0だがAOを選ばない。これは本人の判断待ちではない。

## 所有範囲と実施

- Rustの `cc-scheduler/src/backend.rs`、`engine.rs` と必要な小モジュール・関連検査・運用文書がこの担当の所有。Go guardと残る製品の宣言/APIは2849が所有し、同時に編集しない。親2834もRustを並行編集しない。
- 上記契約の読み取りAPIを使う、既定falseのcurrent_product_allowedを実装する。正規化path/bodyのSHA256には既存cc-job-policyのdigest等を使い、不要な新依存を足さない。原名簿・auth/configや未知の名前をモデルへ読み出さない。
- 原名簿NotFoundの場合にだけ現在の資格を確認し、通常のdispatch/prepare/admissionと元の停止・依存・資源・owner競合を維持する。Go側が未配布/未対応なら不成立とし、全projectをroot判定だけで通さない。
- `cc-scheduler/tests/parity.rs` の missing_private_registry_only_dispatches_verified_explicit_pc_repair と既存repair-step検査を維持し、新しい現在の資格の成功/拒否・別id/project/spec/root・旧状態・停止/依存・次の自然巡回の回帰を加える。RealBackendのreceipt照合にも意味のある回帰を加える。仮のsuccess statusや空の原名簿は使わない。
- Cargoは割当と共有ゲートを使い、変更クレート/利用側の必要範囲とdoctestを確認する。小修正ごとに全workspace/releaseを重ねない。配布が必要なschedulerだけを最新mainからbuildする。Go2849の関係あるmergeを保持し、CI/ソースとruntimeの並行変更を戻さない。
- 固定base/headと差分を共有ToDo --specの別モデルレビューで確認し、Mustを直して公式CI成功後に通常mergeする。子CLIを直接起動しない。必要な正式更新は既存CLI/管理者窓口・窓なしで進める。
- 2849との契約が両側で正式反映された後、同じ新AO2839を通常schedulerの有限なrepair-step/自然tickで受け付け、実job/worktree/成果と次の自然巡回での続行を確認する。同じ担当を別番号で増やさず、原1161/1368/1476/2179は未復旧のまま保つ。
- 技術的な操作を本人へ質問しない。GPU/Civ6/新費用/APIキー/他Org/非Prdを使わない。原2792/2836/2843・元5案件・product-ci/gh_gateの並行修正に触れない。

実結果は親2834へ通常CLIで返し、製品別の無通知Discordへ確認した範囲だけ反映する。登録・分類・受付・実操作・検査・配布を分ける。まだAPIの片側だけや受付だけなら完了にしない。Go2849の全体doneを待つと製品受付との循環になるため禁止。契約に指定する部分配布の実receiptと現runtimeを照合し、その部分だけ未達なら有限な技術wait-retryへ記録する。本人判断blockや新監視を作らない。
