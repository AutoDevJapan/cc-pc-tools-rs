' All recovery and maintenance run inside the Rust tick's measured deadline.
' CREATE_NO_WINDOW is provided by WScript's hidden window mode.
Set objShell = CreateObject("WScript.Shell")
objShell.Run "cmd.exe /c """"D:\CC\Tools\discord-ask\discord_ask.exe"" tick >> ""D:\CC\Tools\discord-ask\tick.log"" 2>&1""", 0, True
