# ToDo3184の測定と再開

親3103 / Issue289 / PR302向けの有限な独立検査。旧実装に指定テスト1ファイルだけを重ね、指定の1件を1回だけ実行する。親の実装・現在候補の検査・配布は変更しない。

- 旧実装: `bf60fb319626210e1cb914610e63e46c2eb34c79`
- テスト: `4a0c61f594e600b00e0f3d03702974496aec388a`
- 所有付き一時領域: このworktree直下の `codex-owned-8609944fc2b7cc7d9f899c69c1036804`
- 所有記録ID: `8609944fc2b7cc7d9f899c69c1036804`
- 作業ブランチ: `scheduler/todo-3184-3103-1`

保護判定は作業場所・元登録場所・一時領域とも `client_resource=false`。GitHubの正規APIで所有者AutoDevJapan、repository ID1406818713、既存OAuthの利用者mashfrombandを照合した。秘密値・保護一覧原本は読んでいない。

`measure.ps1 -Phase prepare` はGitの生blobと展開ファイルのSHA256を全件照合する。`-Phase measure` は通常のcargo_gateとsccacheを使い、専用targetで指定テストを1回だけ実行する。標準出力と標準エラーを生バイトで別々に全量保存し、実終了コードを記録する。`-Phase verify` は実行後の全ソースを再照合する。

`run-start.json` が存在するときは再実行しない。`process.json` のPIDと開始時刻、ログ、`run-exit.json`を照合し、既存の実行結果を回収する。構文エラー・コンパイル失敗・別の先行assert失敗を期待するRedと扱わない。

展開照合は完了した。旧実装の全1182ファイルが生blobのSHA256と一致し、テスト1ファイルだけを重ねた後も指定blobと一致した。改行変換の事前検出と修正経緯は `archive-preflight.json` に保持する。Cargoはまだ起動していない。

残作業: 測定1回、失敗理由の確認、実行後照合、結果のコミット、一時領域のplan→別呼出しの削除→finish、通常checkpoint-workspace。共有ToDoの状態はschedulerが更新する。
