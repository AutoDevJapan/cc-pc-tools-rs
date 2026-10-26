param([Parameter(Mandatory=$true)][string]$OutputPath)
$ErrorActionPreference='Stop'
trap { [Console]::Error.WriteLine(('configure_failure line={0} id={1}' -f $_.InvocationInfo.ScriptLineNumber,$_.FullyQualifiedErrorId)); exit 1 }
# One invocation through CC-AdminBroker, after independent review. No storage
# resize/reassociation, snapshot deletion, service restart, or task registration.
$limit=[uint64]53687091200
$minimumMB=51200
$volumes=@(Get-CimInstance Win32_Volume -Property DeviceID,DriveLetter,FreeSpace)
$c=@($volumes | Where-Object {$_.DriveLetter -eq 'C:'})
if($c.Count -ne 1) { throw 'C_VOLUME_NOT_UNIQUE' }
$storage=@(Get-CimInstance Win32_ShadowStorage)
if($storage.Count -ne 1 -or $storage[0].Volume.DeviceID -ne $c[0].DeviceID -or $storage[0].DiffVolume.DeviceID -ne $c[0].DeviceID -or [uint64]$storage[0].MaxSpace -ne $limit) { throw 'EXISTING_C_C_50GIB_ASSOCIATION_CHANGED' }
$foreign=@(Get-CimInstance Win32_ShadowCopy | Where-Object {$_.VolumeName -ne $c[0].DeviceID})
if($foreign.Count -ne 0) { throw 'OTHER_VOLUME_SNAPSHOTS_PRESENT' }
$providers=@(Get-CimInstance Win32_ShadowProvider)
if($providers.Count -ne 1 -or $providers[0].Name -ne 'Microsoft Software Shadow Copy provider 1.0') { throw 'PROVIDER_CHANGED' }
if([uint64]$c[0].FreeSpace -lt ($limit + 200GB)) { throw 'C_HEADROOM_BELOW_PREALLOCATION_PLUS_200GIB' }
foreach($path in @('HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\SystemRestore','HKLM:\SOFTWARE\Policies\Microsoft\Windows NT\SystemRestore')) {
    $disabled=Get-ItemProperty -LiteralPath $path -Name DisableSR -ErrorAction SilentlyContinue
    if($disabled -and $disabled.DisableSR -ne 0) {throw 'SYSTEM_RESTORE_EXPLICITLY_DISABLED'}
}
$key=[Microsoft.Win32.Registry]::LocalMachine.OpenSubKey('SYSTEM\CurrentControlSet\Services\VolSnap',$true)
if($null -eq $key) { throw 'VOLSNAP_KEY_MISSING' }
try {
    if('MinDiffAreaFileSize' -in $key.GetValueNames()) { throw 'MINIMUM_NO_LONGER_ABSENT_DO_NOT_OVERWRITE' }
    $receipt=[ordered]@{schema=1;todo=4011;phase='intent_saved_not_applied';started_utc=[DateTime]::UtcNow.ToString('o');before_present=$false;before_value=$null;target_MB=$minimumMB;unchanged_maximum_bytes=$limit;source='C:';storage='C:';free_before_bytes=[uint64]$c[0].FreeSpace;existing_snapshot_count=@(Get-CimInstance Win32_ShadowCopy).Count;deletions=0;reboot_requested=$false}
    [IO.File]::WriteAllText($OutputPath,($receipt | ConvertTo-Json -Depth 5),(New-Object Text.UTF8Encoding($false)))
    # REG_DWORD is a single atomic value update; it cannot delete a snapshot or
    # change the existing source/storage association. No empty registry created.
    $key.SetValue('MinDiffAreaFileSize',$minimumMB,[Microsoft.Win32.RegistryValueKind]::DWord)
    $key.Flush()
    $readback=$key.GetValue('MinDiffAreaFileSize',$null)
    if($readback -ne $minimumMB -or $key.GetValueKind('MinDiffAreaFileSize') -ne [Microsoft.Win32.RegistryValueKind]::DWord) { throw 'MINIMUM_READBACK_FAILED' }
    $receipt.phase='registry_applied_effectiveness_not_yet_tested'
    $receipt.after_value=$readback
    $receipt.finished_utc=[DateTime]::UtcNow.ToString('o')
    $json=$receipt | ConvertTo-Json -Depth 5
    [IO.File]::WriteAllText($OutputPath,$json,(New-Object Text.UTF8Encoding($false)))
    Write-Output $json
} finally { $key.Dispose() }
