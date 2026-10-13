$ErrorActionPreference = 'Stop'
$taskName = 'CC-DiscordAskTick'
$runtime = 'D:\CC\Tools\discord-ask\unattended-runner.exe'
$launcher = 'D:\CC\Tools\discord-ask\discord-ask-tick-hidden.vbs'
if (-not (Test-Path -LiteralPath $runtime -PathType Leaf)) { throw 'Runner executable is missing' }
$task = Get-ScheduledTask -TaskName $taskName
if ($task.Actions.Count -ne 1 -or $task.Actions[0].Execute -notmatch '(?i)\\wscript\.exe$' -or $task.Actions[0].Arguments -notlike '*discord-ask-tick-hidden.vbs*') { throw 'Original tick task differs' }
$original = [ordered]@{ task = $taskName; user = $task.Principal.UserId; logon = [string]$task.Principal.LogonType; runlevel = [string]$task.Principal.RunLevel; enabled = $task.Settings.Enabled }
$before = 'D:\CC\Tools\discord-ask\unattended\tick-before.json'
if (-not (Test-Path -LiteralPath $before)) { $original | ConvertTo-Json | Set-Content -LiteralPath $before -Encoding UTF8 }
@'
Set shell = CreateObject("WScript.Shell")
shell.CurrentDirectory = "D:\CC\Tools\discord-ask"
result = shell.Run("""D:\CC\Tools\discord-ask\unattended-runner.exe""", 0, True)
WScript.Quit result
'@ | Set-Content -LiteralPath $launcher -Encoding ASCII
$principal = New-ScheduledTaskPrincipal -UserId $task.Principal.UserId -LogonType S4U -RunLevel $task.Principal.RunLevel
$settings = New-ScheduledTaskSettingsSet -Hidden -MultipleInstances IgnoreNew -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit (New-TimeSpan -Minutes 2) -RestartCount 2 -RestartInterval (New-TimeSpan -Minutes 2)
$interval = New-ScheduledTaskTrigger -Once -At ((Get-Date).AddMinutes(1)) -RepetitionInterval (New-TimeSpan -Minutes 1)
$startup = New-ScheduledTaskTrigger -AtStartup
$logon = New-ScheduledTaskTrigger -AtLogOn
$definition = New-ScheduledTask -Action $task.Actions -Principal $principal -Settings $settings -Trigger @($interval,$startup,$logon)
Register-ScheduledTask -TaskName $taskName -InputObject $definition -Force | Out-Null
$current = Get-ScheduledTask -TaskName $taskName
if ([string]$current.Principal.LogonType -ne 'S4U' -or $current.Principal.UserId -ne $task.Principal.UserId -or -not $current.Settings.Enabled) { throw 'Tick task read-back differs' }
[ordered]@{ task = $taskName; logon = [string]$current.Principal.LogonType; runlevel = [string]$current.Principal.RunLevel; user_unchanged = $true; hidden = $current.Settings.Hidden; enabled = $current.Settings.Enabled; interval = @($current.Triggers | ForEach-Object { $_.Repetition.Interval }); launcher = 'new owner intake only; original ledger unchanged' } | ConvertTo-Json -Depth 3
Start-ScheduledTask -TaskName $taskName
