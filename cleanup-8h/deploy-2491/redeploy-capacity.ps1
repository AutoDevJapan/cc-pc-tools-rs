$ErrorActionPreference='Stop'
$worktree='D:\CC\Work\unattended-operations-20261013'
$deploy=Join-Path $worktree 'cleanup-8h\deploy-2491'
$manifest=Get-Content -LiteralPath (Join-Path $deploy 'manifest.json') -Encoding UTF8|ConvertFrom-Json
$source=Join-Path $worktree 'capacity-audit-3h\deploy-2491\capacity-audit-3h.exe'
$target='D:\CC\Tools\capacity-audit-3h\capacity-audit-3h.exe'
if($manifest.todo-ne2491 -or (Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash-ne$manifest.capacity_sha256){throw 'Capacity source changed'}
foreach($path in @($source,$target)){
    $current=[IO.Path]::GetFullPath($path)
    while($current){
        $item=Get-Item -LiteralPath $current -Force
        if(($item.Attributes-band[IO.FileAttributes]::ReparsePoint)-ne0){throw 'Linked deployment path'}
        $parent=[IO.Path]::GetDirectoryName($current.TrimEnd('\'));if($parent-eq$current){break};$current=$parent
    }
}
$task=Get-ScheduledTask -TaskName 'CC-CapacityAudit-3h'
if($task.State-eq'Running'){throw 'Capacity run is active'}
if($task.Triggers[0].Repetition.Interval-ne'PT3H' -or [string]$task.Principal.LogonType-ne'S4U'){throw 'Capacity task scope changed'}
$backup=Join-Path $deploy 'capacity-audit-3h.exe.before-gui-fix'
if(-not(Test-Path -LiteralPath $backup)){Copy-Item -LiteralPath $target -Destination $backup}
Copy-Item -LiteralPath $source -Destination $target
if((Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash-ne$manifest.capacity_sha256){throw 'Deployed hash mismatch'}
$receipt=Get-Content -LiteralPath (Join-Path $deploy 'deployment.json') -Encoding UTF8|ConvertFrom-Json
$receipt.capacity_sha256=$manifest.capacity_sha256
$receipt.deployed_at_utc=[DateTime]::UtcNow.ToString('o')
$receipt|ConvertTo-Json -Depth 5|Set-Content -LiteralPath (Join-Path $deploy 'deployment.json') -Encoding UTF8
Start-ScheduledTask -TaskName 'CC-CapacityAudit-3h'
[ordered]@{todo=2491;capacity_sha256=$manifest.capacity_sha256;capacity_restarted=$true;cleanup_unchanged=$true}|ConvertTo-Json -Compress
