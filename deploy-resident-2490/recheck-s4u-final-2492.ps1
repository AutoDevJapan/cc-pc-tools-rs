$ErrorActionPreference = 'Stop'
$name = 'CC-ResidentGuard-S4UProbe-2492'
$runtime = 'D:\CC\Tools\resident-guard\resident-guard.exe'
$task = Get-ScheduledTask -TaskName $name
if ($task.State -eq 'Running' -or $task.Principal.LogonType -ne 'S4U' -or $task.Principal.RunLevel -ne 'Limited' -or $task.Actions.Count -ne 1 -or $task.Actions[0].Arguments -notlike '--session-probe --state-dir*--expected-user-sid S-1-*') { throw 'final_probe_changed_or_busy' }
if ((Get-FileHash -LiteralPath $runtime -Algorithm SHA256).Hash -ne '4515C5BAF01E97D72BA9AD738A0F0B03739166095E46FCA2DCDF7FEAEBA51B72') { throw 'final_probe_runtime_mismatch' }
$action = New-ScheduledTaskAction -Execute $runtime -Argument $task.Actions[0].Arguments -WorkingDirectory 'D:\CC\Tools\resident-guard'
$updated = Set-ScheduledTask -TaskName $name -Action $action
Start-ScheduledTask -InputObject $updated
[ordered]@{ final_runtime_probe_started=$true; same_s4u_limited_principal=$true } | ConvertTo-Json -Compress
