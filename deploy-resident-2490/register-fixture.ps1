$ErrorActionPreference = 'Stop'
$taskName = 'CC-ResidentGuard-Fixture-2490'
if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) { throw 'fixture_task_exists' }
if (Get-NetTCPConnection -State Listen -LocalPort 24090 -ErrorAction SilentlyContinue) { throw 'fixture_port_in_use' }
$reference = Get-ScheduledTask -TaskName 'CC-ResidentGuard'
$executable = 'D:\CC\Tools\resident-guard\resident-guard.exe'
if (-not (Test-Path -LiteralPath $executable -PathType Leaf)) { throw 'fixture_runtime_missing' }
$action = New-ScheduledTaskAction -Execute $executable -Argument '--fixture-server --fixture-port 24090' -WorkingDirectory 'D:\CC\Tools\resident-guard'
$principal = New-ScheduledTaskPrincipal -UserId $reference.Principal.UserId -LogonType Interactive -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -ExecutionTimeLimit (New-TimeSpan -Minutes 15) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
$settings.IdleSettings.StopOnIdleEnd = $false
Register-ScheduledTask -TaskName $taskName -Action $action -Principal $principal -Settings $settings | Out-Null
[ordered]@{ fixture_registered = $true; fixture_started = $false } | ConvertTo-Json -Compress
