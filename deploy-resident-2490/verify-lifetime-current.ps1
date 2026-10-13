$ErrorActionPreference = 'Stop'
$runtime = 'D:\CC\Tools\resident-guard\resident-guard.exe'
$source = 'D:\CC\Work\unattended-operations-20261013\resident-guard\resident-guard.lifetime-next.exe'
$expectedOld = '4515C5BAF01E97D72BA9AD738A0F0B03739166095E46FCA2DCDF7FEAEBA51B72'
$expectedNew = '90117CF8DF3003B30AFB356E1BFC3082AA1490759D90BBC3B4F4EC27962D1385'
if ((Get-FileHash -LiteralPath $runtime).Hash -ne $expectedOld -or (Get-FileHash -LiteralPath $source).Hash -ne $expectedNew) { throw 'lifetime_runtime_hash_mismatch' }
$definitions = @('local-ai-web-autostart-10012','opencodex-proxy','godd-ao-semantic-proxy','CC-OcxHealthWatchdog')
foreach ($name in $definitions) {
    $task = Get-ScheduledTask -TaskName $name
    if ($task.Principal.LogonType -ne 'S4U' -or $task.Principal.RunLevel -ne 'Limited' -or $task.Actions[0].Execute -ne $runtime) { throw 'lifetime_task_changed' }
    $settings = $task.Settings
    $settings.ExecutionTimeLimit = 'PT0S'
    $settings.MultipleInstances = 'IgnoreNew'
    Set-ScheduledTask -TaskName $name -Settings $settings | Out-Null
    $after = Get-ScheduledTask -TaskName $name
    if ($after.Settings.ExecutionTimeLimit -ne 'PT0S' -or $after.Settings.MultipleInstances -ne 'IgnoreNew') { throw 'lifetime_readback_mismatch' }
}
$guard = Get-ScheduledTask -TaskName 'CC-ResidentGuard'
if ($guard.State -eq 'Running') { throw 'lifetime_guard_busy' }
$backup = 'D:\CC\Tools\resident-guard\resident-guard.before-lifetime-2488.exe'
$stage = 'D:\CC\Tools\resident-guard\resident-guard.lifetime-stage-2488.exe'
if ((Test-Path -LiteralPath $backup) -or (Test-Path -LiteralPath $stage)) { throw 'lifetime_backup_exists' }
Copy-Item -LiteralPath $source -Destination $stage
Move-Item -LiteralPath $runtime -Destination $backup
Move-Item -LiteralPath $stage -Destination $runtime
Start-ScheduledTask -TaskName 'CC-ResidentGuard'
[ordered]@{ runtime_sha256=(Get-FileHash -LiteralPath $runtime).Hash; persistent_tasks_unlimited=$true; duplicate_action_suppressed=$true; service_tasks_restarted=$false; cold_boot_verified=$false } | ConvertTo-Json -Compress
