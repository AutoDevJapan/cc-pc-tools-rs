$ErrorActionPreference = 'Stop'
$taskName = 'CC-ResidentGuard-S4UProbe-2492'
$root = 'D:\CC\Work\unattended-operations-20261013\deploy-resident-2490'
$source = 'D:\CC\Work\unattended-operations-20261013\resident-guard\resident-guard.exe'
$binary = Join-Path $root 'resident-guard-boot-probe-2492.exe'
$expectedHash = '83C2AA8F7D264B2833E69196ADF2B316DCBC85501D289C93D62AE5F40B05A43F'
if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) { throw 'probe_task_exists' }
if (Test-Path -LiteralPath $binary) { throw 'probe_binary_exists' }
if ((Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash -ne $expectedHash) { throw 'probe_source_hash_mismatch' }
$reference = Get-ScheduledTask -TaskName 'CC-ResidentGuard'
if ($reference.Principal.RunLevel -ne 'Limited') { throw 'reference_run_level_unexpected' }
$account = [Security.Principal.NTAccount]::new($reference.Principal.UserId)
$expectedSID = $account.Translate([Security.Principal.SecurityIdentifier]).Value
Copy-Item -LiteralPath $source -Destination $binary
$action = New-ScheduledTaskAction -Execute $binary -Argument ('--session-probe --state-dir "' + (Join-Path $root 'boot-probe-state-2492') + '" --expected-user-sid ' + $expectedSID) -WorkingDirectory $root
$principal = New-ScheduledTaskPrincipal -UserId $reference.Principal.UserId -LogonType S4U -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -ExecutionTimeLimit (New-TimeSpan -Minutes 2) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
$settings.IdleSettings.StopOnIdleEnd = $false
$task = Register-ScheduledTask -TaskName $taskName -Action $action -Principal $principal -Settings $settings
if ($task.Principal.LogonType -ne 'S4U' -or $task.Principal.RunLevel -ne 'Limited' -or $task.Principal.UserId -ne $reference.Principal.UserId) { throw 'probe_principal_mismatch' }
Start-ScheduledTask -InputObject $task
[ordered]@{ probe_registered = $true; logon_type = [string]$task.Principal.LogonType; run_level = [string]$task.Principal.RunLevel; same_existing_user = $true; binary_sha256 = (Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash; credentials_supplied = $false } | ConvertTo-Json -Compress
