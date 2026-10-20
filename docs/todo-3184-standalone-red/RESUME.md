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

展開照合、指定1件の測定1回、失敗理由の確認、実行後照合は完了した。コンパイル成功、1件選択、0成功/1失敗、exit100。453行目のタイトル用マーカー非表示assertで失敗し、それ以前の単独job存在・todo-6不在のassertは通過した。レポート用マーカーの次のassertには未到達。詳細は `RESULT.md` と実ログに保持する。

旧実装の全1182ファイルが生blobのSHA256と一致し、テスト1ファイルだけを重ねた後も指定blobと一致した。実行前後の変更はない。改行変換の事前検出と修正経緯は `archive-preflight.json` に保持する。測定は再実行しない。

測定結果は `e0ecaff825acae98c811f342ed83bfd0d27b9673` でコミットして同じ作業ブランチへpush済み。一時領域はplanの容量基準により退避対象となり、Cへの退避はQUARANTINE_CROSS_DRIVEで止まったため、指定手順で同じDの削除希望へ退避した。識別番号・3031ファイル・容量の一致と元の場所の不在を確認した。原資料・測定証拠は保持している。削除とfinishは実施していない。詳しくは `cleanup-result.json`。

有限検査の実作業は完了した。最後の片付け記録をこのブランチにコミットしてpushした後、通常の `scheduler.exe checkpoint-workspace` に自分のid/job/同じブランチ/実HEADを保存する。チェックポイント受付結果は通常のジョブ結果とscheduler側の保存記録で確認する。測定を再実行しない。親はこのブランチの `RESULT.md` とログを受領し、元の3103を続行する。共有ToDoの状態はschedulerが更新する。
