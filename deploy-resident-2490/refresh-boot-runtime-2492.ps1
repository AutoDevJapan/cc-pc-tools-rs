$ErrorActionPreference = 'Stop'
$source = 'D:\CC\Work\unattended-operations-20261013\resident-guard\resident-guard.exe'
$runtime = 'D:\CC\Tools\resident-guard\resident-guard.exe'
$backup = 'D:\CC\Tools\resident-guard\resident-guard.before-boot-final-2492.exe'
$staging = 'D:\CC\Tools\resident-guard\resident-guard.boot-final-staging-2492.exe'
$expectedOld = 'FFCC27B785B048E8F2BBCF32CC905A5BEBB75476016DA7602E73DF5315D260E2'
$expectedNew = '4515C5BAF01E97D72BA9AD738A0F0B03739166095E46FCA2DCDF7FEAEBA51B72'
if ((Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash -ne $expectedNew -or (Get-FileHash -LiteralPath $runtime -Algorithm SHA256).Hash -ne $expectedOld) { throw 'boot_final_hash_mismatch' }
foreach ($name in @('CC-ResidentGuard','local-ai-web-autostart-10012','opencodex-proxy','godd-ao-semantic-proxy','CC-OcxHealthWatchdog')) {
    $task = Get-ScheduledTask -TaskName $name
    if ($task.Principal.LogonType -ne 'S4U' -or $task.Principal.RunLevel -ne 'Limited' -or $task.Actions.Count -ne 1 -or $task.Actions[0].Execute -ne $runtime) { throw 'boot_final_task_changed' }
}
$deadline = [DateTime]::UtcNow.AddSeconds(15)
while ((Get-ScheduledTask -TaskName 'CC-ResidentGuard').State -eq 'Running') {
    if ([DateTime]::UtcNow -ge $deadline) { throw 'boot_final_guard_busy' }
    Start-Sleep -Milliseconds 100
}
if ((Test-Path -LiteralPath $backup) -or (Test-Path -LiteralPath $staging)) { throw 'boot_final_backup_or_staging_exists' }
Copy-Item -LiteralPath $source -Destination $staging
if ((Get-FileHash -LiteralPath $staging -Algorithm SHA256).Hash -ne $expectedNew) { throw 'boot_final_staging_hash_mismatch' }
Move-Item -LiteralPath $runtime -Destination $backup
Move-Item -LiteralPath $staging -Destination $runtime
Start-ScheduledTask -TaskName 'CC-ResidentGuard'
[ordered]@{ runtime_updated = $true; runtime_sha256 = (Get-FileHash -LiteralPath $runtime -Algorithm SHA256).Hash; service_tasks_restarted = $false; credentials_changed = $false } | ConvertTo-Json -Compress
