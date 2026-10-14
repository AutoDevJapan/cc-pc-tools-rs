$ErrorActionPreference='Stop'
$started=[DateTime]::UtcNow.ToString('o')
foreach($name in @('CC-CleanupDeletionRequests-8h','CC-CapacityAudit-3h')){
    if((Get-ScheduledTask -TaskName $name).State-eq'Disabled'){throw 'Maintenance task is disabled'}
    Start-ScheduledTask -TaskName $name
}
[ordered]@{todo=2491;tasks_started=2;started_at_utc=$started}|ConvertTo-Json -Compress
