# ToDo2966 チェックポイント（親2962の独立CIキュー）

作業ブランチ: scheduler/todo-2966-runner-2962（基点 2977911）。

## 完了
- config/plan/ledger/ghread/scan/helper/safedelete/evidence/platform（go vet 通過）

## 残り
- supervisor.go（worker: Verify→owned root作成→prepare→execute(token stdin)→stop/collect→RequireSuccess→evidence→削除）
- tick.go（reconcile→launch→scan/admit→launch）と main.go（tick / worker / status）
- fixtureテスト（重複・予算・OS lock・crash fence・foreign repo・head advance・失敗後cleanup・再実行なし・tick継続）
- unattended-runner の有限tick接続（civ6_queue.go、config未設定なら無効）
- 別モデルレビューは共有ToDo --spec で登録、親2962 noteへ配送

## helperへ要求する契約（未確定部分）
- Linux helperも Windows と同じ動詞 prepare|execute|stop|collect|collect-unstarted|status <owned-root>、plan.json は共有Planそのもの、execute のみ登録tokenをstdin。
- 実行中runner名は Windows "CC-CIW-"+instance、Linux "CC-CIL-"+instance を仮定。
- status は {"clean":bool} のJSONを標準出力（読み取り専用の再照合用）。未対応ならfenceは解除されない（安全側）。
- 登録tokenの取得は gh_gate 経由の POST repos/AutoDevJapan/civ6-banseiki/actions/runners/registration-token。gateが拒否する場合は親が経路を決める。
