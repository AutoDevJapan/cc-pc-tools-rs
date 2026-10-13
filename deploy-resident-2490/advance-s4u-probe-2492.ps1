$ErrorActionPreference = 'Stop'
$root = 'D:\CC\Work\unattended-operations-20261013\deploy-resident-2490'
$source = 'D:\CC\Work\unattended-operations-20261013\resident-guard\resident-guard.exe'
$target = Join-Path $root 'resident-guard-boot-probe-2492.exe'
$expectedCurrent = 'FC6645242CD1DA39C5C79205E79F8CF1A523D640B305C11B053E11FA62BC2948'
$expectedSource = '5F9BFF483EE6458C9982A61AB59BBD987CB1B3F4890E8936D0634A2444D9A238'
$task = Get-ScheduledTask -TaskName 'CC-ResidentGuard-S4UProbe-2492'
if ($task.State -eq 'Running' -or $task.Actions.Count -ne 1 -or $task.Actions[0].Execute -ne $target -or $task.Principal.LogonType -ne 'S4U' -or $task.Principal.RunLevel -ne 'Limited') { throw 'probe_task_changed_or_busy' }
if ((Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash -ne $expectedSource) { throw 'probe_new_source_mismatch' }
if ((Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash -ne $expectedCurrent) { throw 'probe_existing_runtime_mismatch' }
$backup = Join-Path $root ('resident-guard-boot-probe-before-' + $expectedCurrent.Substring(0,12) + '-2492.exe')
if (Test-Path -LiteralPath $backup) { throw 'probe_backup_exists' }
$receipt = Join-Path $root 'boot-probe-state-2492\session.json'
if (Test-Path -LiteralPath $receipt) { Copy-Item -LiteralPath $receipt -Destination (Join-Path $root ('boot-probe-state-2492\session-before-' + $expectedCurrent.Substring(0,12) + '.json')) }
Move-Item -LiteralPath $target -Destination $backup
Copy-Item -LiteralPath $source -Destination $target
Start-ScheduledTask -InputObject $task
[ordered]@{ probe_updated = $true; binary_sha256 = (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash; same_limited_s4u_principal = $true } | ConvertTo-Json -Compress
