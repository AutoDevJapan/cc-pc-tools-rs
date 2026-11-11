' Daily login check: run chatgpt_web check with no console window (Windows Terminal ignores -WindowStyle Hidden).
Set objShell = CreateObject("WScript.Shell")
result = RunHidden("""D:\CC\Tools\chatgpt-web\chatgpt_web.exe"" check")
If result = 0 Then
  result = RunHidden("""D:\CC\Tools\todo\todo.exe"" resume-ready 1974 --condition chatgpt-plus")
End If
WScript.Quit result

' WSH can return 0 for an unhandled Run exception, including a missing child.
Function RunHidden(command)
  On Error Resume Next
  Err.Clear
  code = objShell.Run(command, 0, True)
  If Err.Number <> 0 Then code = 1
  On Error GoTo 0
  RunHidden = code
End Function
