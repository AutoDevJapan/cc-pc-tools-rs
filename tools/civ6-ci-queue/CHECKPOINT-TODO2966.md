# ToDo2966 チェックポイント（親2962の独立CIキュー）

作業ブランチ: scheduler/todo-2966-runner-2962（基点 2977911）。

## 完了（PR https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/247、ブランチ scheduler/todo-2966-runner-2962）
- tools/civ6-ci-queue 全体（config/plan/ledger/ghread/scan/helper/safedelete/evidence/supervisor/tick/main）とfixtureテスト（go test -race / vet / build、-tags civ6contract でも通過）
- unattended-runner/civ6_queue.go（設定 civ6_ci_queue がある時だけ、既存tickの最後に固定コマンド tick を15秒以内で1回）と関連テスト（runner全テスト通過）
- .github/workflows/unattended-readiness.yml に tools/civ6-ci-queue を追加
- 別モデルレビューは共有ToDo 2972（--spec）で登録済み

## 2026-10-19 夜の追補（親2962の配送を反映）
- OS別adapterを実CLIへ合わせた: Windowsは prepare/collect をCC-AdminBroker、execute(tokenはstdin)/stop は root/helper.exe、成否は collect-end イベント。Linuxは one-shot run（plan はroot外、tokenはhelperが取得）と result.private.json の読み取り。
- run pathの path@ref 形は親の MatchRunPath で再照合。回収は現在sourceのVerifyに依存しない。
- residue_only は既定でOS fence。allow_residue_only は親が配備するまで無効。
- unattended-readiness.yml の変更は戻した（親の civ6-ci.yml に統合される）。

## 残り
- PR247のCI結果、2972のMust対応、通常merge
- 親2962 noteへ commit/PR/review/検査/CLI/未実施の配送
- 実運用の配備（設定ファイル、helper pin、continuation有効化）は親。この工程では行わない

## helperへ要求する契約（未確定部分）
- Linux helperも Windows と同じ動詞 prepare|execute|stop|collect|collect-unstarted|status <owned-root>、plan.json は共有Planそのもの、execute のみ登録tokenをstdin。
- 実行中runner名は Windows "CC-CIW-"+instance、Linux "CC-CIL-"+instance を仮定。
- status は {"clean":bool} のJSONを標準出力（読み取り専用の再照合用）。未対応ならfenceは解除されない（安全側）。
- 登録tokenの取得は gh_gate 経由の POST repos/AutoDevJapan/civ6-banseiki/actions/runners/registration-token。gateが拒否する場合は親が経路を決める。
