' Daily login check: run chatgpt_web check with no console window (Windows Terminal ignores -WindowStyle Hidden).
Set objShell = CreateObject("WScript.Shell")
objShell.Run """D:\CC\Tools\chatgpt-web\chatgpt_web.exe"" check", 0, True
