# ToDo 3021調査メモ

2026-10-20

## 結論

既存カスタムTUIとcollector/manifestの実hashは一致している。一方、公式npm packageは0.163.0、manifestとカスタムTUIは0.162.1のまま。launcherの厳密な版照合がfalseになるため、通常対話起動は公式版へ安全にfallbackし、カスタム`/progress`表示は利用されない。主因は公式Codex更新後のpatch追従と実配備漏れ。

## 実測

- 配備manifest SHA256 `FE0D2364281A5126E12FBF78FD44FB303DA76A9B7F31C3FE71D11C3324C93616`
- 配備Codex SHA256 `6F21E83AC9FAC5ED64E5B06667A408F671DA65AC9D1094E3F84B156841CDC6A1`。manifest記録と一致。
- 配備code-mode-host SHA256 `9F865A748C984492CA9B69AAC75680E4954F31311C43EC33DDBDAE0ED77BD4BD`。manifest記録と一致。
- 配備collector SHA256 `581C813D3321C47580D21F07DD7EF7A3B654BD5D4D761CDBF7BB275CD8E7F8D1`。manifest記録と一致。collector本体は読取のみ。
- 公式npm package.jsonは0.163.0。launcher要件の0.162.1と異なる。
- npm経由の現起動はOpenCodex codex.cmd/ps1 shimを経由し、`codex-launch.exe`を呼ぶ。これは既存Go launcherで、その選択処理はpackage版不一致時に公式npm Codexへfallbackする。
- 読取済み保存用snapshotはschema_version 1、status=degraded、tasks279/jobs292、jobs partial、failed_reads=123、complete=false。exit code 1は部分取得を示す。空配列や全停止ではない。
- 既存launcherのGo検査 `go test ./...` 成功。現在のworktreeで並行作成されたprogress subcommand実装を含む。`go vet ./...` と `go build -o NUL .` は終了0。

## 対応・制約

TUI patchはrust-v0.162.1専用。公式0.163.0への追従にはCargo資源が必要なため、ToDo3023を`--needs cargo`で登録。ToDo3021は同件を完了待ちにする。

調査中、同worktreeのGo launcher、activation、Rust/API画面に未コミット変更が見えた。これらを独自に編集・stage・commitしていない。現変更はToDo3020側の実装に見えるため保持する。

親3020へcollector差分: schema1/degradedとjobs.partial/failed_reads123のとき、確認済みデータを維持し「部分取得」と表示する。exit1を失敗による空一覧または全停止に変換しない。親のGo launcherで追加中の`codex progress`経路は、既起動TUIの一覧確認に使える候補。現時点ではこのコマンドのhidden受入は未実施。

## 未完了

Cargo付き0.163.0 patch追従・関連検査・別モデルレビュー・マージ後、所有/guard・並行利用を照合して実配備する。launcherの実起動経路で対話TUI/`/progress`表示を確認し、exec等の公式経路を維持する。現在の対話sessionは操作していない。起動済みTUIへの反映も未実証。
