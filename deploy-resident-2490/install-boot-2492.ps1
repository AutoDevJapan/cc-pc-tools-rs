$ErrorActionPreference = 'Stop'
$root = 'D:\CC\Work\unattended-operations-20261013\deploy-resident-2490'
$binarySource = 'D:\CC\Work\unattended-operations-20261013\resident-guard\resident-guard.exe'
$runtime = 'D:\CC\Tools\resident-guard\resident-guard.exe'
$previousRuntime = 'D:\CC\Tools\resident-guard\resident-guard.before-boot-2492.exe'
$oldHash = 'EDD2A1717193F04992BA75777105AC61933B0C5A1B3B7B0B9203DAAE417B278A'
$newHash = 'FFCC27B785B048E8F2BBCF32CC905A5BEBB75476016DA7602E73DF5315D260E2'
if ((Get-FileHash -LiteralPath $binarySource -Algorithm SHA256).Hash -ne $newHash -or (Get-FileHash -LiteralPath $runtime -Algorithm SHA256).Hash -ne $oldHash) { throw 'boot_runtime_hash_mismatch' }
$probe = Get-Content -LiteralPath (Join-Path $root 'boot-probe-state-2492\session.json') -Raw -Encoding UTF8 | ConvertFrom-Json
if (-not $probe.same_expected_user -or -not $probe.profile_matches_token -or $probe.administrator_enabled -or -not $probe.medium_integrity -or $probe.session_id -ne 0 -or $probe.inspection_error -or @($probe.cli_auth | Where-Object { $_.category -eq 'subscription_status_readable' }).Count -ne 2) { throw 's4u_probe_not_verified' }
$reference = Get-ScheduledTask -TaskName 'CC-ResidentGuard'
$user = $reference.Principal.UserId
$definitions = @(
    [pscustomobject]@{ Name='CC-ResidentGuard'; Execute=$runtime; Arguments=''; Mode=''; Delay='PT90S' },
    [pscustomobject]@{ Name='local-ai-web-autostart-10012'; Execute="$env:SystemRoot\System32\wscript.exe"; Arguments='//B //Nologo "D:\CC\Tools\localai-web-rs\resident-hidden.vbs"'; Mode='operations'; Delay='PT30S' },
    [pscustomobject]@{ Name='opencodex-proxy'; Execute="$env:SystemRoot\System32\wscript.exe"; Arguments=('/b /nologo "' + $env:USERPROFILE + '\.opencodex\opencodex-service-launcher.vbs"'); Mode='opencodex'; Delay='PT20S' },
    [pscustomobject]@{ Name='godd-ao-semantic-proxy'; Execute="$env:SystemRoot\System32\wscript.exe"; Arguments=('/b /nologo "' + $env:USERPROFILE + '\.opencodex\godd-ao\semantic-proxy-launcher.vbs"'); Mode='godd_ao'; Delay='PT60S' },
    [pscustomobject]@{ Name='CC-OcxHealthWatchdog'; Execute='D:\CC\Tools\ocx-health-watchdog\ocx_health_watchdog.exe'; Arguments='watch'; Mode='opencodex_watchdog'; Delay='PT120S' }
)
$backups = Join-Path $root 'boot-tasks-before-2492'
if (Test-Path -LiteralPath $backups) { throw 'boot_backup_directory_exists' }
$originalTasks = @{}
foreach ($definition in $definitions) {
    $task = Get-ScheduledTask -TaskName $definition.Name
    if ($task.Principal.UserId -ne $user -or $task.Principal.RunLevel -ne 'Limited' -or $task.Principal.LogonType -ne 'Interactive' -or $task.Actions.Count -ne 1 -or -not [string]::Equals($task.Actions[0].Execute, $definition.Execute, [StringComparison]::OrdinalIgnoreCase) -or -not [string]::Equals([string]$task.Actions[0].Arguments, $definition.Arguments, [StringComparison]::OrdinalIgnoreCase)) { throw 'boot_original_task_changed' }
    $originalTasks[$definition.Name] = $task
}
$listenersBefore = @{}
foreach ($port in @(10012,10100,10101)) { $listenersBefore[[string]$port] = @((Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction Stop).OwningProcess | Sort-Object -Unique) }
[void](New-Item -ItemType Directory -Path $backups)
foreach ($definition in $definitions) { Export-ScheduledTask -InputObject $originalTasks[$definition.Name] | Set-Content -LiteralPath (Join-Path $backups ($definition.Name + '.xml')) -Encoding Unicode }
$deadline = [DateTime]::UtcNow.AddSeconds(15)
while ((Get-ScheduledTask -TaskName 'CC-ResidentGuard').State -eq 'Running') {
    if ([DateTime]::UtcNow -ge $deadline) { throw 'guard_busy' }
    Start-Sleep -Milliseconds 100
}
if (Test-Path -LiteralPath $previousRuntime) { throw 'boot_previous_runtime_exists' }
$staging = 'D:\CC\Tools\resident-guard\resident-guard.boot-staging-2492.exe'
if (Test-Path -LiteralPath $staging) { throw 'boot_staging_exists' }
Copy-Item -LiteralPath $binarySource -Destination $staging
if ((Get-FileHash -LiteralPath $staging -Algorithm SHA256).Hash -ne $newHash) { throw 'boot_staging_hash_mismatch' }
Move-Item -LiteralPath $runtime -Destination $previousRuntime
Move-Item -LiteralPath $staging -Destination $runtime
$changed = @()
try {
    foreach ($definition in $definitions) {
        $task = $originalTasks[$definition.Name]
        $boot = New-ScheduledTaskTrigger -AtStartup
        $boot.Delay = $definition.Delay
        $triggers = @($task.Triggers) + @($boot)
        $principal = New-ScheduledTaskPrincipal -UserId $user -LogonType S4U -RunLevel Limited
        if ($definition.Mode) {
            $action = New-ScheduledTaskAction -Execute $runtime -Argument ('--launch-service ' + $definition.Mode) -WorkingDirectory 'D:\CC\Tools\resident-guard'
        } else { $action = $task.Actions[0] }
        $settings = $task.Settings
        $settings.MultipleInstances = 'IgnoreNew'
        if ($definition.Mode) { $settings.ExecutionTimeLimit = 'PT0S' }
        Set-ScheduledTask -TaskName $definition.Name -Action $action -Trigger $triggers -Principal $principal -Settings $settings | Out-Null
        $changed += $definition.Name
    }
    $verified = @()
    foreach ($definition in $definitions) {
        $task = Get-ScheduledTask -TaskName $definition.Name
        $info = Get-ScheduledTaskInfo -InputObject $task
        if ($task.Principal.LogonType -ne 'S4U' -or $task.Principal.RunLevel -ne 'Limited' -or $task.Principal.UserId -ne $user -or @($task.Triggers | Where-Object { $_.CimClass.CimClassName -eq 'MSFT_TaskBootTrigger' }).Count -ne 1) { throw 'boot_task_readback_mismatch' }
        if ($task.Settings.MultipleInstances -ne 'IgnoreNew' -or ($definition.Mode -and $task.Settings.ExecutionTimeLimit -ne 'PT0S')) { throw 'persistent_task_lifetime_mismatch' }
        $verified += [ordered]@{ name=$definition.Name; logon_type=[string]$task.Principal.LogonType; run_level=[string]$task.Principal.RunLevel; state=[string]$task.State; boot_delay=$definition.Delay; next_run=if($info.NextRunTime){[string]$info.NextRunTime}else{$null}; execution_limit=[string]$task.Settings.ExecutionTimeLimit }
    }
    $sameListeners = $true
    foreach ($port in @(10012,10100,10101)) {
        $current = @((Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction Stop).OwningProcess | Sort-Object -Unique)
        if (($listenersBefore[[string]$port] -join ',') -ne ($current -join ',')) { $sameListeners = $false }
    }
    if (-not $sameListeners) { throw 'live_service_identity_changed' }
    Start-ScheduledTask -TaskName 'CC-ResidentGuard'
    $receipt = [ordered]@{ installed_at=[DateTime]::UtcNow.ToString('o'); runtime_sha256=(Get-FileHash -LiteralPath $runtime -Algorithm SHA256).Hash; tasks=$verified; existing_service_pids_unchanged=$sameListeners; services_restarted=$false; chrome_gui_changed=$false; credentials_saved=$false; cold_boot_verified=$false }
    $receipt | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $root 'boot-install-receipt-2492.json') -Encoding UTF8
    $receipt | ConvertTo-Json -Depth 5
} catch {
    foreach ($name in $changed) { Register-ScheduledTask -TaskName $name -Xml (Get-Content -LiteralPath (Join-Path $backups ($name + '.xml')) -Raw) -Force | Out-Null }
    throw
}
