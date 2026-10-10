' Daily login check: run chatgpt_web check with no console window (Windows Terminal ignores -WindowStyle Hidden).
Set objShell = CreateObject("WScript.Shell")
result = objShell.Run("""D:\CC\Tools\chatgpt-web\chatgpt_web.exe"" check", 0, True)
If result = 0 Then
  result = objShell.Run("""D:\CC\Tools\todo\todo.exe"" resume-ready 1974 --condition chatgpt-plus", 0, True)
End If
WScript.Quit result
