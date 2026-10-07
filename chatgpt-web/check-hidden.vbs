' 毎日のログイン確認。コンソール窓を出さずに chatgpt_web check を実行する（Windows Terminal は -WindowStyle Hidden を無視するため VBS を使う）。
Set objShell = CreateObject("WScript.Shell")
objShell.Run """D:\CC\Tools\chatgpt-web\chatgpt_web.exe"" check", 0, True
