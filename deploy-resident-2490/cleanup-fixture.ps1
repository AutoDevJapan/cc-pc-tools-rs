$ErrorActionPreference = 'Stop'
$taskName = 'CC-ResidentGuard-Fixture-2490'
$task = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($null -ne $task) {
    if ($task.Actions.Count -ne 1 -or -not [string]::Equals($task.Actions[0].Execute, 'D:\CC\Tools\resident-guard\resident-guard.exe', [StringComparison]::OrdinalIgnoreCase) -or $task.Actions[0].Arguments -ne '--fixture-server --fixture-port 24090') { throw 'fixture_definition_changed' }
    if ($task.State -eq 'Running') { Stop-ScheduledTask -InputObject $task }
    $deadline = [DateTime]::UtcNow.AddSeconds(10)
    while ((Get-ScheduledTask -TaskName $taskName).State -eq 'Running') {
        if ([DateTime]::UtcNow -ge $deadline) { throw 'fixture_stop_unconfirmed' }
        Start-Sleep -Milliseconds 100
    }
    Unregister-ScheduledTask -InputObject $task -Confirm:$false
}
[ordered]@{ fixture_task_removed = ($null -eq (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue)); fixture_port_closed = ($null -eq (Get-NetTCPConnection -State Listen -LocalPort 24090 -ErrorAction SilentlyContinue)) } | ConvertTo-Json -Compress
