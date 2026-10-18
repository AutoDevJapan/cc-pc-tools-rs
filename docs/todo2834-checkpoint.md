# 元2834の再開手順（2026-10-19）

## 保存した成果

- 前提2829は通常CLIでdoneを確認。共通guardとproduct-ciの古い実行物を再配布していない。
- 分類修正はPR221。固定コードHEADは `46a5ee8961fd4c08381dda5b51772a94a965b092`、baseは `ee006f2`。元2834の本人依頼を根拠に、新しい継承先2839と指定の外部引き継ぎ1ファイルだけを扱う。原192件・原台帳は未復旧。
- repair-guardの関連モジュールで `go test -race ./...` と `go vet ./...` が成功。Windowsのjunction脱出は実検査で失敗を確認してから修正し、成功を確認した。候補CLIでも現在のAO sourceと明示handoffの分類を確認した。正式配布は未実施。
- 別モデルレビュー2841は固定5ファイル全差分を対象に通常ToDo/subworkで登録。モデルは既存OAuthの `claude-sonnet-5-5`。最初にprovider名を含むモデル表記を渡したaddはCLIのValueErrorで不成立だった。保存済みモデル指定と照合して正式な別名で1件だけ登録した。
- 情報システム部の未完画面受入は独立check2842として通常登録。旧2367/2349を復旧・完了にせず、現ソースの実装済み機能と既存稼働画面を照合する。新たなsource変更やGPU/Cargoは不要。旧配布フォルダは未分類のまま触らない。
- 通常APIのOperationsは取得成功、metadataOk=true、sourceOk=false、6部署。これは保護されたメタ情報の表示であり、原台帳復旧を示さない。
- 元2639/2640/2641/2642/1626とその依存先は維持し、重複担当を起動していない。

## 次の担当が実行すること

1. 2841の実応答を固定base/head・読了範囲・実モデル・実jobと照合して保存する。sourceは上記5ファイルから変えていない。Mustがあれば直し、変更範囲だけ再検査する。readonly担当に保存・commitを要求して同じレビューを繰り返さない。
2. 2842の実画面/API受入と所有タブclose結果を回収する。残る実害があれば現sourceの最小修正を担当し、済んだ履歴分離・取得制御を再実装しない。本人へPC操作を頼まない。
3. PR221の公式CIを確認し、別モデルMust0後に通常mergeする。確認時はWindows/Linuxの既存runnerがonline/busyで順番待ち、失敗は未観測だった。現在mainの並行guard変更を照合し、統合せず古い実行物を配置しない。
4. 正式guard更新前には配布先・元ソースの並行変更・稼働版を確認する。更新後に正式CLIの根拠を保存してから、指定handoffを読む。候補CLIの成功を正式配布へ読み替えない。
5. 2839の保存spec SHA・登録root・現在の実GitHub所有者と物理pathを再確認する。新しい実担当や上位工程との重複を除外し、通常の技術待機解除・schedulerで実行する。wait-retryの期限は初回登録から1時間。期限前の条件を偽造したりraw todo.jsonを書き換えたりしない。
6. 2839の実操作・成果更新、次の自然巡回での続行、製品別のDiscord無通知進捗を確認する。受付・起動だけで2834をdoneにしない。
7. 残る本人指定製品も、上位の実行可能性に従って現在の未完から進める。APIで所有者と物理rootを確認できた次候補はGoDD 13a13e1、GoDD Rosetta、GoDD Genesis、GoDD Virtual Streamer。現在の分類修正へは含めていない。過去の拒否や本人停止を新しい許可へ読み替えない。

## 作業場所に残す私的な照合記録

`current-products.private.json` は上記GoDD5対象のGitHub実所有者・repository ID・Git top-level一致と全祖先の再解析ポイント不在の結果。`product-inventory2834.private.json` は本人の現在の一覧に完全一致する登録だけのメタ情報で、保存statusの件数は生存担当の証拠ではない。`ambiguous-registration2834.private.json` は登録なし・複数path・同一pathの別表記を区別する。曖昧な対象へsource実装を始めない。

実候補とPR/review本文を置いた自分の一時フォルダはsafe-delete所有ID `b76cbf73ae7c5548c8a3d33ddf33b8bf`。正式配布までは候補を保持する。不要になればplanの通常削除を別呼び出しで実行してfinishする。拒否時はquarantineし、他担当の一時物を削除しない。

意味のある成果はこのworktreeの `scheduler/todo-2834-work` へcommit/push済み。`checkpoint2834.private.json` を実HEADへ更新し、scheduler checkpoint-workspaceのsaved=trueを確認してから待機する。元2834は2841/2842の結果だけを待つ間、通常 `todo wait --on` を使う。進み具合は完了条件4項目の達成をまだ確定できないため推測の割合を付けない。
