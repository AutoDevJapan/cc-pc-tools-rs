param([Parameter(Mandatory=$true)][string]$OutputPath)
$ErrorActionPreference='Stop'
trap { [Console]::Error.WriteLine(('create_point_failure line={0} id={1}' -f $_.InvocationInfo.ScriptLineNumber,$_.FullyQualifiedErrorId)); exit 1 }
# A single ordinary System Restore operation. It uses Windows' own VSS
# serialization, preserves concurrent snapshots, and is never scheduled.
$description='ToDo4011 VSS retention verification'
$key='HKLM:\SYSTEM\CurrentControlSet\Services\VolSnap'
if((Get-ItemProperty -LiteralPath $key -Name MinDiffAreaFileSize).MinDiffAreaFileSize -ne 51200) { throw 'REVIEWED_MINIMUM_NOT_PRESENT' }
$c=Get-CimInstance Win32_Volume -Filter "DriveLetter='C:'" -Property DeviceID,FreeSpace
$storage=@(Get-CimInstance Win32_ShadowStorage)
if($storage.Count -ne 1 -or $storage[0].Volume.DeviceID -ne $c.DeviceID -or $storage[0].DiffVolume.DeviceID -ne $c.DeviceID -or [uint64]$storage[0].MaxSpace -ne 53687091200) { throw 'EXISTING_ASSOCIATION_CHANGED' }
if([uint64]$c.FreeSpace -lt (50GB+200GB)) { throw 'C_HEADROOM_CHANGED' }
$existing=@(Get-CimInstance -Namespace root/default -ClassName SystemRestore -Filter "Description='$description'" -Property SequenceNumber,CreationTime)
if($existing.Count -ne 0) { throw 'THIS_VERIFICATION_POINT_ALREADY_EXISTS_DO_NOT_DUPLICATE' }
$receipt=[ordered]@{schema=1;todo=4011;phase='intent_saved_not_created';started_utc=[DateTime]::UtcNow.ToString('o');begin_return=$null;end_return=$null;created_points=@();deletions=0;service_restarts=0;reboot_requested=$false}
function SaveReceipt {
    [IO.File]::WriteAllText($OutputPath,($receipt | ConvertTo-Json -Depth 6),(New-Object Text.UTF8Encoding($false)))
}
SaveReceipt
$begin=Invoke-CimMethod -Namespace root/default -ClassName SystemRestore -MethodName CreateRestorePoint -Arguments @{Description=$description;RestorePointType=[uint32]12;EventType=[uint32]100} -OperationTimeoutSec 240
$receipt.begin_return=[uint32]$begin.ReturnValue
$receipt.phase='begin_returned_completion_not_yet_verified'
SaveReceipt
if($begin.ReturnValue -ne 0) { throw 'CREATE_BEGIN_NONZERO_NO_AUTOMATIC_RETRY' }
$end=Invoke-CimMethod -Namespace root/default -ClassName SystemRestore -MethodName CreateRestorePoint -Arguments @{Description=$description;RestorePointType=[uint32]12;EventType=[uint32]101} -OperationTimeoutSec 120
$receipt.end_return=[uint32]$end.ReturnValue
$receipt.phase='end_returned_registration_not_yet_verified'
SaveReceipt
if($end.ReturnValue -ne 0) { throw 'CREATE_END_NONZERO_NO_AUTOMATIC_RETRY' }
$receipt.created_points=@(Get-CimInstance -Namespace root/default -ClassName SystemRestore -Filter "Description='$description'" -Property CreationTime,RestorePointType,EventType,SequenceNumber | ForEach-Object {[ordered]@{created=$_.CreationTime;type=$_.RestorePointType;event_type=$_.EventType;sequence=$_.SequenceNumber}})
if($receipt.created_points.Count -ne 1) { throw 'NEW_POINT_NOT_UNIQUELY_CONFIRMED' }
$receipt.phase='one_new_restore_point_registered_retention_not_yet_verified'
$receipt.finished_utc=[DateTime]::UtcNow.ToString('o')
SaveReceipt
Write-Output ($receipt | ConvertTo-Json -Depth 6)
