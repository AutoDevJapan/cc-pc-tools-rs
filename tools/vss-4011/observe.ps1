param([Parameter(Mandatory=$true)][string]$OutputPath,[string]$SinceUtc)
$ErrorActionPreference = 'Stop'
trap {
    [Console]::Error.WriteLine(('observer_failure line={0} id={1}' -f $_.InvocationInfo.ScriptLineNumber,$_.FullyQualifiedErrorId))
    exit 1
}
$result = [ordered]@{ schema=1; purpose='4011 finite read-only Windows VSS metadata'; started_utc=[DateTime]::UtcNow.ToString('o'); changes=0 }
$errors = New-Object 'System.Collections.Generic.List[object]'
function ErrorCode($part, $err) {
    $errors.Add([ordered]@{part=$part; hresult=$err.Exception.HResult; category=[string]$err.CategoryInfo.Category})
}
function Timestamp($value) { if($null -eq $value) {return $null}; return $value.ToString('o') }
$volumes = @(Get-CimInstance Win32_Volume -Property DeviceID,DriveLetter,Capacity,FreeSpace,FileSystem,DriveType)
$map = @{}
foreach($v in $volumes) { $map[$v.DeviceID]= if($v.DriveLetter -in @('C:','D:','E:')) {$v.DriveLetter} else {'other'} }
function VolumeAlias($reference) {
    if($null -eq $reference) { return 'unresolved' }
    $id = if($reference -is [string]) {$reference} else {$reference.DeviceID}
    if($id -and $map.ContainsKey($id)) { return $map[$id] }
    return 'unresolved'
}
$result.volumes = @($volumes | Where-Object {$_.DriveLetter -in @('C:','D:','E:')} | ForEach-Object {
    [ordered]@{drive=$_.DriveLetter;capacity_bytes=[uint64]$_.Capacity;free_bytes=[uint64]$_.FreeSpace;filesystem=$_.FileSystem;drive_type=$_.DriveType}
})
$os = Get-CimInstance Win32_OperatingSystem -Property Version,BuildNumber,LastBootUpTime
$result.os = [ordered]@{version=$os.Version;build=$os.BuildNumber;last_boot_utc=$os.LastBootUpTime.ToUniversalTime().ToString('o')}
try {
    $result.storage = @(Get-CimInstance Win32_ShadowStorage | ForEach-Object {
        [ordered]@{source=(VolumeAlias $_.Volume);storage=(VolumeAlias $_.DiffVolume);used_bytes=[uint64]$_.UsedSpace;allocated_bytes=[uint64]$_.AllocatedSpace;maximum_bytes=[uint64]$_.MaxSpace}
    })
} catch { ErrorCode 'shadow_storage' $_ }
try {
    $result.shadows = @(Get-CimInstance Win32_ShadowCopy | ForEach-Object {
        $hash=[Security.Cryptography.SHA256]::Create()
        try { $token=([BitConverter]::ToString($hash.ComputeHash([Text.Encoding]::UTF8.GetBytes($_.ID)))).Replace('-','').Substring(0,16) } finally {$hash.Dispose()}
        [ordered]@{token=$token;source=(VolumeAlias $_.VolumeName);created_utc=$_.InstallDate.ToUniversalTime().ToString('o');state=$_.State;persistent=$_.Persistent;client_accessible=$_.ClientAccessible;no_auto_release=$_.NoAutoRelease;no_writers=$_.NoWriters;differential=$_.Differential;hardware_assisted=$_.HardwareAssisted}
    })
} catch { ErrorCode 'shadow_copies' $_ }
try {
    $result.providers = @(Get-CimInstance Win32_ShadowProvider | ForEach-Object {
        [ordered]@{microsoft_software=($_.Name -eq 'Microsoft Software Shadow Copy provider 1.0');version=$_.Version;type=$_.Type}
    })
} catch { ErrorCode 'providers' $_ }
try {
    $writerText = (& "$env:SystemRoot/System32/vssadmin.exe" list writers 2>&1 | Out-String)
    $writerExit = $LASTEXITCODE
    $states = @([regex]::Matches($writerText, '(?m)^\s*(?:State|状態)\s*:\s*\[(\d+)\]') | ForEach-Object {[int]$_.Groups[1].Value})
    $knownWriters=@('Task Scheduler Writer','VSS Metadata Store Writer','Performance Counters Writer','System Writer','ASR Writer','Registry Writer','COM+ REGDB Writer','Shadow Copy Optimization Writer','WMI Writer','MSSearch Service Writer','BITS Writer','SqlServerWriter')
    $writerRows=@(foreach($match in [regex]::Matches($writerText, "(?ms)^\s*(?:Writer name|ライター名):\s*'([^']+)'.*?^\s*(?:State|状態)\s*:\s*\[(\d+)\]([^\r\n]*).*?^\s*(?:Last error|最後のエラー)\s*:\s*([^\r\n]*)")) {
        $writerName=$match.Groups[1].Value
        [ordered]@{writer=if($writerName -in $knownWriters){$writerName}else{'other'};state=[int]$match.Groups[2].Value;last_error_none=($match.Groups[4].Value -match '^(No error|エラーなし)\s*$')}
    })
    $result.writers = [ordered]@{exit=$writerExit;parsed_count=$states.Count;states=$states;rows=$writerRows;all_parsed_stable=($states.Count -gt 0 -and @($states | Where-Object {$_ -ne 1}).Count -eq 0);raw_exported=$false}
} catch { ErrorCode 'writers' $_ }
$result.services = @(Get-CimInstance Win32_Service -Filter "Name='VSS' OR Name='swprv' OR Name='wbengine' OR Name='SDRSVC'" -Property Name,State,StartMode | ForEach-Object {[ordered]@{name=$_.Name;state=$_.State;start_mode=$_.StartMode}})
$reg = @(
    @{key='HKLM:\SYSTEM\CurrentControlSet\Services\VolSnap';values=@('MinDiffAreaFileSize','TimeOut','Start')},
    @{key='HKLM:\SYSTEM\CurrentControlSet\Services\VSS\Settings';values=@('MaxShadowCopies','IdleTimeout')},
    @{key='HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\SystemRestore';values=@('DisableSR','DisableConfig','RPSessionInterval','RPGlobalInterval','RPLifeInterval','SystemRestorePointCreationFrequency')},
    @{key='HKLM:\SOFTWARE\Policies\Microsoft\Windows NT\SystemRestore';values=@('DisableSR','DisableConfig')}
)
$result.registry = @(
    foreach($entry in $reg) {
        foreach($name in $entry.values) {
            $item = Get-ItemProperty -LiteralPath $entry.key -Name $name -ErrorAction SilentlyContinue
            [ordered]@{key=$entry.key;name=$name;present=($null -ne $item);value=if($null -ne $item){$item.$name}else{$null}}
        }
    }
)
try {
    $result.restore_points = @(Get-CimInstance -Namespace root/default -ClassName SystemRestore -Property CreationTime,RestorePointType,EventType,SequenceNumber | ForEach-Object {
        [ordered]@{created=$_.CreationTime;type=$_.RestorePointType;event_type=$_.EventType;sequence=$_.SequenceNumber}
    })
} catch { ErrorCode 'system_restore_points' $_ }
try {
    $result.restore_config = @(Get-CimInstance -Namespace root/default -ClassName SystemRestoreConfig -Property RPLifeInterval,RPGlobalInterval,RPSessionInterval,DiskPercent | ForEach-Object {
        [ordered]@{life_interval=$_.RPLifeInterval;global_interval=$_.RPGlobalInterval;session_interval=$_.RPSessionInterval;disk_percent=$_.DiskPercent}
    })
} catch { ErrorCode 'system_restore_config' $_ }
$result.tasks = @(
    foreach($taskSpec in @(@{path='\Microsoft\Windows\SystemRestore\';name='SR'},@{path='\Microsoft\Windows\WindowsBackup\';name='AutomaticBackup'},@{path='\Microsoft\Windows\Defrag\';name='ScheduledDefrag'})) {
        $task=Get-ScheduledTask -TaskPath $taskSpec.path -TaskName $taskSpec.name -ErrorAction SilentlyContinue
        if($task) {
            $info=$task | Get-ScheduledTaskInfo
            [ordered]@{path=$taskSpec.path;name=$taskSpec.name;enabled=$task.Settings.Enabled;state=[string]$task.State;last_run=(Timestamp $info.LastRunTime);next_run=(Timestamp $info.NextRunTime);last_result=$info.LastTaskResult;trigger_count=@($task.Triggers).Count;action_count=@($task.Actions).Count}
        } else { [ordered]@{path=$taskSpec.path;name=$taskSpec.name;present=$false} }
    }
)
try {
    $result.event_definitions = @((Get-WinEvent -ListProvider volsnap).Events | Where-Object {$_.Id -in @(24,25,33,35,36)} | ForEach-Object {[ordered]@{id=$_.Id;description=$_.Description;substitutions='provider placeholders'}})
} catch { ErrorCode 'volsnap_definitions' $_ }
try {
    $cVolume = ($volumes | Where-Object {$_.DriveLetter -eq 'C:'}).DeviceID
    $cGuid = if($cVolume -match '\{[^}]+\}') {$Matches[0]} else {''}
    $fltmcText=(& "$env:SystemRoot/System32/fltmc.exe" volumes 2>&1 | Out-String)
    $cKernel=if($fltmcText -match '(?m)^\s*C:\s+(\S+)') {$Matches[1]} else {''}
    $start=if($SinceUtc){[DateTimeOffset]::Parse($SinceUtc).LocalDateTime}else{[DateTime]::Now.AddDays(-3)}
    try {
        $events = @(Get-WinEvent -FilterHashtable @{LogName='System';ProviderName='volsnap';StartTime=$start} -MaxEvents 100 -ErrorAction Stop)
    } catch {
        if($_.FullyQualifiedErrorId -like 'NoMatchingEventsFound*') {$events=@()} else {throw}
    }
    $result.volsnap_query=[ordered]@{start_utc=$start.ToUniversalTime().ToString('o');max_events=100;returned=$events.Count;limit_partial_possible=($events.Count -ge 100);complete=($events.Count -lt 100)}
    $result.volsnap = @($events | ForEach-Object {
        $xml=[xml]$_.ToXml()
        $values=@($xml.Event.EventData.Data | ForEach-Object {if($_ -is [string]){$_}else{$_.InnerText}})
        $joined=$values -join ' '
        $matchedC=($joined -match '(?i)(^|\s)C:(\\|\s|$)') -or ($cGuid -and $joined.IndexOf($cGuid,[StringComparison]::OrdinalIgnoreCase) -ge 0) -or ($cKernel -and @($values | Where-Object {$_ -eq $cKernel}).Count -gt 0)
        [ordered]@{id=$_.Id;created_utc=$_.TimeCreated.ToUniversalTime().ToString('o');level=$_.Level;record_id=$_.RecordId;source_C_matched=[bool]$matchedC;source_volume_unresolved=(-not $matchedC);event_process_pid=$_.ProcessId;data_count=$values.Count;message_exported=$false}
    })
    if($result.event_definitions.Count -eq 0) {
        $result.event_definitions=@($events | Group-Object Id | ForEach-Object {
            $event=$_.Group[0]
            [ordered]@{id=$event.Id;description=$event.FormatDescription([object[]]@('0','0','0','0','0','0','0','0','0','0','0','0','0','0','0','0'));substitutions='16 constant zero strings; actual event values not formatted'}
        })
    }
} catch { ErrorCode 'volsnap_events' $_ }
$result.errors=@($errors.ToArray())
$result.finished_utc=[DateTime]::UtcNow.ToString('o')
$json=$result | ConvertTo-Json -Depth 10
[IO.File]::WriteAllText($OutputPath,$json,(New-Object Text.UTF8Encoding($false)))
Write-Output $json
