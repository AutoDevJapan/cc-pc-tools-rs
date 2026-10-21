# ChatGPTの定期確認の起動失敗を返す（ToDo3232）

既存の `check-hidden.vbs` は `WScript.Shell.Run` の起動例外を捕捉しない。
2026-10-21の実確認では、呼先の未検出エラーを表示してもhostが0を返した。
既存タスクの成功と本処理の成功を区別するため、起動例外を1として返す。

- `chatgpt_web.exe check` を非表示・同期実行する。
- 起動例外なら1で終わり、`resume-ready` を呼ばない。
- checkが非0ならその実コードで終わり、`resume-ready` を呼ばない。
- checkが0のときだけ既存の `todo.exe resume-ready 1974 --condition chatgpt-plus`
  を非表示・同期実行する。起動例外は1、その他は実終了コードを返す。
- 引数、既存のプラン判定、元1974の完了条件は変えない。

`chatgpt-web/wrapper-tests` は実際のWindows Script Hostでwrapperを実行する。
正規source内の2つの実行先だけを検査専用exeへ差し替え、欠落・失敗・成功を
作る。検査専用exeはGoのtest binaryで、Chromeや本物の1974へ接続しない。
未検出、checkの失敗、resumeの未検出・失敗、成功と正確な引数を照合する。
検査はcscript、本番タスクはwscriptを使う。VBSはGoモジュールの外にあるため、
`go test -count=1 -timeout 60s ./...` で結果を再利用せず毎回実行する。

配布先の保護判定は別途維持する。このソースの検査成功だけでは配布、実check、
Plusの確認、元1974の受入を完了としない。
