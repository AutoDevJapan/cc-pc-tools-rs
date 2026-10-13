$ErrorActionPreference = 'Stop'
$taskName = 'CC-ResidentGuard-S4ULaunchProbe-2492'
$runtime = 'D:\CC\Tools\resident-guard\resident-guard.exe'
$root = 'D:\CC\Work\unattended-operations-20261013\deploy-resident-2490'
$reference = Get-ScheduledTask -TaskName 'CC-ResidentGuard'
if ($reference.Principal.LogonType -ne 'S4U' -or $reference.Principal.RunLevel -ne 'Limited') { throw 'launch_probe_reference_changed' }
if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) { throw 'launch_probe_exists' }
$before = @((Get-NetTCPConnection -State Listen -LocalPort 10012 -ErrorAction Stop).OwningProcess | Sort-Object -Unique)
$principal = New-ScheduledTaskPrincipal -UserId $reference.Principal.UserId -LogonType S4U -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -ExecutionTimeLimit (New-TimeSpan -Minutes 2) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
$action = New-ScheduledTaskAction -Execute $runtime -Argument '--launch-service operations' -WorkingDirectory 'D:\CC\Tools\resident-guard'
$task = Register-ScheduledTask -TaskName $taskName -Action $action -Principal $principal -Settings $settings
try {
    Start-ScheduledTask -InputObject $task
    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    do {
        $current = Get-ScheduledTask -TaskName $taskName
        $info = Get-ScheduledTaskInfo -InputObject $current
        if ($info.LastRunTime.Year -gt 2020 -and $current.State -ne 'Running' -and $current.State -ne 'Queued') { break }
        if ([DateTime]::UtcNow -ge $deadline) { throw 'launch_probe_deadline' }
        Start-Sleep -Milliseconds 100
    } while ($true)
    if ($info.LastTaskResult -ne 0) { throw 'launch_probe_failed' }
    $after = @((Get-NetTCPConnection -State Listen -LocalPort 10012 -ErrorAction Stop).OwningProcess | Sort-Object -Unique)
    if (($before -join ',') -ne ($after -join ',')) { throw 'launch_probe_duplicated_service' }
    $receipt = [ordered]@{ checked_at=[DateTime]::UtcNow.ToString('o'); s4u_limited_launch_exit=0; existing_listener_pid_unchanged=$true; duplicate_listener_created=$false; existing_service_stopped=$false; cold_boot_verified=$false }
    $receipt | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $root 's4u-launch-receipt-2492.json') -Encoding UTF8
    $receipt | ConvertTo-Json -Compress
} finally {
    $current = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    if ($null -ne $current) {
        if ($current.State -eq 'Running') { Stop-ScheduledTask -InputObject $current }
        Unregister-ScheduledTask -InputObject $current -Confirm:$false
    }
}
