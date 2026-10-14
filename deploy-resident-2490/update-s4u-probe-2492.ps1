$ErrorActionPreference = 'Stop'
$root = 'D:\CC\Work\unattended-operations-20261013\deploy-resident-2490'
$source = 'D:\CC\Work\unattended-operations-20261013\resident-guard\resident-guard.exe'
$target = Join-Path $root 'resident-guard-boot-probe-2492.exe'
$backup = Join-Path $root 'resident-guard-boot-probe-before-trace-2492.exe'
$task = Get-ScheduledTask -TaskName 'CC-ResidentGuard-S4UProbe-2492'
if ($task.State -eq 'Running' -or $task.Actions.Count -ne 1 -or $task.Actions[0].Execute -ne $target -or $task.Principal.LogonType -ne 'S4U' -or $task.Principal.RunLevel -ne 'Limited') { throw 'probe_task_changed_or_busy' }
if ((Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash -ne '0C12915DD5ED174FC330373BBF27515869986C724E32141837817D2F7DABC6CC') { throw 'probe_new_source_mismatch' }
if ((Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash -ne '83C2AA8F7D264B2833E69196ADF2B316DCBC85501D289C93D62AE5F40B05A43F') { throw 'probe_existing_runtime_mismatch' }
if (Test-Path -LiteralPath $backup) { throw 'probe_backup_exists' }
Move-Item -LiteralPath $target -Destination $backup
Copy-Item -LiteralPath $source -Destination $target
Start-ScheduledTask -InputObject $task
[ordered]@{ probe_updated = $true; binary_sha256 = (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash; same_limited_s4u_principal = $true } | ConvertTo-Json -Compress
