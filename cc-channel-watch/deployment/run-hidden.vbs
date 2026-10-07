Option Explicit
Dim shell, code
Set shell = CreateObject("WScript.Shell")
shell.CurrentDirectory = "D:\CC\Tools\channel-watch"
code = shell.Run("""D:\CC\Tools\channel-watch\channel-watch.exe"" run", 0, True)
WScript.Quit code
