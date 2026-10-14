$ErrorActionPreference='Stop'
$taskWorktree='D:\CC\Work\unattended-operations-20261013'
$deployDirectory=Join-Path $taskWorktree 'cleanup-8h\deploy-2491'
$manifestPath=Join-Path $deployDirectory 'manifest.json'
$manifest=Get-Content -LiteralPath $manifestPath -Encoding UTF8|ConvertFrom-Json
if($manifest.schema-ne1 -or $manifest.todo-ne2491){throw 'Invalid deployment scope'}

function Assert-NormalAncestors([string]$Path) {
    $full=[IO.Path]::GetFullPath($Path)
    if($full.TrimEnd('\')-ne$Path.TrimEnd('\')){throw 'Noncanonical deployment target'}
    $current=$full
    while($current){
        if(Test-Path -LiteralPath $current){
            $item=Get-Item -LiteralPath $current -Force
            if(($item.Attributes-band[IO.FileAttributes]::ReparsePoint)-ne0){throw 'Linked deployment path'}
        }
        $parent=[IO.Path]::GetDirectoryName($current.TrimEnd('\'))
        if($parent-eq$current){break};$current=$parent
    }
}

$deploymentTargets=@(
    [pscustomobject]@{Task='CC-CleanupDeletionRequests-8h';Source=(Join-Path $taskWorktree 'target\readiness-cleanup-2491\release\cleanup-8h.exe');Directory='D:\CC\Tools\cleanup-8h';Exe='cleanup-8h.exe';Hash=$manifest.cleanup_sha256;Interval='PT1H';Arguments='--apply';Limit=[TimeSpan]::FromMinutes(10)},
    [pscustomobject]@{Task='CC-CapacityAudit-3h';Source=(Join-Path $taskWorktree 'capacity-audit-3h\deploy-2491\capacity-audit-3h.exe');Directory='D:\CC\Tools\capacity-audit-3h';Exe='capacity-audit-3h.exe';Hash=$manifest.capacity_sha256;Interval='PT3H';Arguments='';Limit=[TimeSpan]::FromMinutes(5)}
)
$ownerSid=[Security.Principal.WindowsIdentity]::GetCurrent().User.Value
foreach($target in $deploymentTargets){
    Assert-NormalAncestors $target.Source
    Assert-NormalAncestors $target.Directory
    if((Get-FileHash -LiteralPath $target.Source -Algorithm SHA256).Hash-ne$target.Hash){throw 'Source hash changed'}
    $task=Get-ScheduledTask -TaskName $target.Task
    if($task.State-eq'Running'){throw 'Existing maintenance run is active'}
    $taskBackup=Join-Path $deployDirectory ($target.Task+'.before.xml')
    if(-not(Test-Path -LiteralPath $taskBackup)){Export-ScheduledTask -TaskName $target.Task|Set-Content -LiteralPath $taskBackup -Encoding Unicode}
    if(-not(Test-Path -LiteralPath $target.Directory)){New-Item -ItemType Directory -Path $target.Directory|Out-Null}
    $destination=Join-Path $target.Directory $target.Exe
    if((Test-Path -LiteralPath $destination) -and (Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash-ne$target.Hash){
        Assert-NormalAncestors $destination
        $backup=Join-Path $deployDirectory ($target.Exe+'.before-2491')
        if(-not(Test-Path -LiteralPath $backup)){Copy-Item -LiteralPath $destination -Destination $backup}
    }
    Copy-Item -LiteralPath $target.Source -Destination $destination
    if((Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash-ne$target.Hash){throw 'Deployed hash mismatch'}
    if($target.Arguments){$task.Actions=@(New-ScheduledTaskAction -Execute $destination -Argument $target.Arguments -WorkingDirectory $target.Directory)}
    else{$task.Actions=@(New-ScheduledTaskAction -Execute $destination -WorkingDirectory $target.Directory)}
    $task.Principal=New-ScheduledTaskPrincipal -UserId $ownerSid -LogonType S4U -RunLevel Highest
    $task.Settings=New-ScheduledTaskSettingsSet -Hidden -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -WakeToRun -ExecutionTimeLimit $target.Limit -MultipleInstances IgnoreNew -RestartCount 2 -RestartInterval ([TimeSpan]::FromMinutes(10))
    foreach($trigger in $task.Triggers){$trigger.Enabled=$true;$trigger.Repetition.Interval=$target.Interval;$trigger.Repetition.StopAtDurationEnd=$false}
    $task.Settings.Enabled=$true
    Register-ScheduledTask -TaskName $target.Task -InputObject $task -Force|Out-Null
    $readback=Get-ScheduledTask -TaskName $target.Task
    if($readback.State-eq'Disabled' -or $readback.Triggers[0].Repetition.Interval-ne$target.Interval -or [string]$readback.Principal.LogonType-ne'S4U'){throw 'Task readback mismatch'}
}
$receipt=[ordered]@{schema=1;todo=2491;deployed_at_utc=[DateTime]::UtcNow.ToString('o');cleanup_sha256=$manifest.cleanup_sha256;capacity_sha256=$manifest.capacity_sha256;tasks_enabled=2;periods=@('PT1H','PT3H');logon_type='S4U';system_wide_prune_added=$false;original_registry_created=$false}
$receipt|ConvertTo-Json -Depth 5|Set-Content -LiteralPath (Join-Path $deployDirectory 'deployment.json') -Encoding UTF8
$receipt|ConvertTo-Json -Depth 5 -Compress
