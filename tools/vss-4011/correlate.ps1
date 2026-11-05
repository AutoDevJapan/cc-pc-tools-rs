param([Parameter(Mandatory=$true)][string]$OutputPath)
$ErrorActionPreference='Stop'
trap { [Console]::Error.WriteLine(('correlation_failure line={0} id={1}' -f $_.InvocationInfo.ScriptLineNumber,$_.FullyQualifiedErrorId)); exit 1 }
$result=[ordered]@{schema=1;purpose='4011 finite event and IO correlation, no messages/actions/arguments';started_utc=[DateTime]::UtcNow.ToString('o');changes=0}
$rows=New-Object 'System.Collections.Generic.List[object]'
$providers=@('disk','Ntfs','Microsoft-Windows-Ntfs','stornvme','storahci','storport','Microsoft-Windows-StorPort','volmgr','partmgr','Microsoft-Windows-FilterManager','Microsoft-Windows-Storage-ClassPnP','volsnap')
$result.window_counts=@()
$anchors=@(Get-WinEvent -FilterHashtable @{LogName='System';ProviderName='volsnap';Id=25} -MaxEvents 2)
foreach($anchor in $anchors) {
    $at=[DateTimeOffset]$anchor.TimeCreated
    foreach($log in @('System','Application')) {
        $evs=@(Get-WinEvent -FilterHashtable @{LogName=$log;StartTime=$at.LocalDateTime.AddMinutes(-5);EndTime=$at.LocalDateTime.AddMinutes(5);Level=@(1,2,3)} -MaxEvents 200 -ErrorAction SilentlyContinue)
        $result.window_counts+=@([ordered]@{anchor_record_id=$anchor.RecordId;window_utc=$at.UtcDateTime.ToString('o');log=$log;all_warning_error_count=$evs.Count;limit_partial_possible=($evs.Count -ge 200)})
        foreach($event in $evs) {
            if(($log -eq 'System' -and $event.ProviderName -in $providers) -or ($log -eq 'Application' -and $event.ProviderName -in @('VSS','Microsoft-Windows-Backup','System Restore','Microsoft-Windows-System-Restore','Microsoft-Windows-SystemRestore'))) {
                $rows.Add([ordered]@{window_utc=$at.UtcDateTime.ToString('o');log=$log;provider=$event.ProviderName;id=$event.Id;utc=$event.TimeCreated.ToUniversalTime().ToString('o');level=$event.Level;record_id=$event.RecordId})
            }
        }
    }
}
$result.windows=@($rows.ToArray())
$counterPaths=@('\LogicalDisk(C:)\Disk Bytes/sec','\LogicalDisk(C:)\Avg. Disk sec/Write','\LogicalDisk(C:)\Avg. Disk sec/Read','\LogicalDisk(C:)\Current Disk Queue Length','\LogicalDisk(D:)\Disk Bytes/sec','\LogicalDisk(D:)\Avg. Disk sec/Write','\LogicalDisk(D:)\Avg. Disk sec/Read','\LogicalDisk(D:)\Current Disk Queue Length')
try {
    $result.current_io=@(Get-Counter -Counter $counterPaths -SampleInterval 1 -MaxSamples 3 | ForEach-Object {
        $timestamp=$_.Timestamp.ToUniversalTime().ToString('o')
        foreach($sample in $_.CounterSamples) {
            $drive=if($sample.Path -match '(?i)logicaldisk\(([CD]:)\)'){$Matches[1].ToUpperInvariant()}else{'unknown'}
            $metric=$sample.Path.Substring($sample.Path.LastIndexOf('\')+1)
            [ordered]@{utc=$timestamp;drive=$drive;metric=$metric;value=$sample.CookedValue;status=$sample.Status}
        }
    })
} catch { $result.io_error=[ordered]@{hresult=$_.Exception.HResult;category=[string]$_.CategoryInfo.Category} }
$result.known_backup_processes=@(foreach($name in @('vssvc','wbengine','sdrsvc','diskshadow','vssadmin')) {
    [ordered]@{name=$name;count=@(Get-Process -Name $name -ErrorAction SilentlyContinue).Count}
})
$result.finished_utc=[DateTime]::UtcNow.ToString('o')
$json=$result | ConvertTo-Json -Depth 8
[IO.File]::WriteAllText($OutputPath,$json,(New-Object Text.UTF8Encoding($false)))
Write-Output $json
