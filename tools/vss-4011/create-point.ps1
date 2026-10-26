param(
    [Parameter(Mandatory=$true)][string]$OutputPath,
    [Parameter(Mandatory=$true)][ValidateSet('A','B')][string]$Stage,
    [string]$FirstReceiptPath
)
$ErrorActionPreference='Stop'
trap { [Console]::Error.WriteLine(('create_point_failure line={0} id={1}' -f $_.InvocationInfo.ScriptLineNumber,$_.FullyQualifiedErrorId)); exit 1 }
# At most one ordinary operation per explicit stage, never a timer or loop.
# Windows owns VSS serialization. No snapshot deletion or service restart.
$description='ToDo4011 VSS retention verification '+$Stage
$targetBytes=[uint64](3200MB)
$limit=[uint64](50GB)
$key='HKLM:\SYSTEM\CurrentControlSet\Services\VolSnap'
if((Get-ItemProperty -LiteralPath $key -Name MinDiffAreaFileSize).MinDiffAreaFileSize -ne 3200) { throw 'REVIEWED_MINIMUM_NOT_PRESENT' }
$c=Get-CimInstance Win32_Volume -Filter "DriveLetter='C:'" -Property DeviceID,FreeSpace
$storage=@(Get-CimInstance Win32_ShadowStorage)
if($storage.Count -ne 1 -or $storage[0].Volume.DeviceID -ne $c.DeviceID -or $storage[0].DiffVolume.DeviceID -ne $c.DeviceID -or [uint64]$storage[0].MaxSpace -ne $limit) { throw 'EXISTING_ASSOCIATION_CHANGED' }
if([uint64]$c.FreeSpace -lt ($targetBytes+200GB)) { throw 'C_HEADROOM_CHANGED' }
# Leave room for both stages and metadata without provoking quota eviction.
$needed=if($Stage -eq 'A'){2*$targetBytes+1GB}else{$targetBytes+1GB}
if([uint64]$storage[0].AllocatedSpace+$needed -gt $limit) {throw 'INSUFFICIENT_QUOTA_HEADROOM_PRESERVE_EXISTING_GENERATIONS'}
function CShadows {
    @(Get-CimInstance Win32_ShadowCopy | Where-Object {$_.VolumeName -eq $c.DeviceID} | ForEach-Object {
        $hash=[Security.Cryptography.SHA256]::Create()
        try {$token=([BitConverter]::ToString($hash.ComputeHash([Text.Encoding]::UTF8.GetBytes($_.ID)))).Replace('-','').Substring(0,16)} finally {$hash.Dispose()}
        [ordered]@{token=$token;created_utc=$_.InstallDate.ToUniversalTime().ToString('o');state=$_.State;persistent=$_.Persistent;client_accessible=$_.ClientAccessible}
    })
}
$before=@(CShadows)
if($Stage -eq 'B') {
    if(-not $FirstReceiptPath) {throw 'FIRST_RECEIPT_REQUIRED'}
    $first=Get-Content -LiteralPath $FirstReceiptPath -Encoding UTF8 -Raw | ConvertFrom-Json
    if($first.todo -ne 4011 -or $first.stage -ne 'A' -or $first.phase -ne 'one_new_point_and_snapshot_identified_retention_not_yet_verified' -or @($first.new_shadows).Count -ne 1) {throw 'FIRST_POINT_NOT_CONFIRMED'}
    if($first.new_shadows[0].token -notin @($before | ForEach-Object {$_.token})) {throw 'FIRST_GENERATION_NO_LONGER_PRESENT'}
}
$existing=@(Get-CimInstance -Namespace root/default -ClassName SystemRestore -Filter "Description='$description'" -Property SequenceNumber,CreationTime)
if($existing.Count -ne 0) { throw 'THIS_VERIFICATION_POINT_ALREADY_EXISTS_DO_NOT_DUPLICATE' }
if(Test-Path -LiteralPath $OutputPath) {throw 'RECEIPT_EXISTS_RECONCILE_OUTCOME_BEFORE_ANY_RETRY'}
$receipt=[ordered]@{schema=1;todo=4011;stage=$Stage;phase='begin_call_pending_outcome_unknown';started_utc=[DateTime]::UtcNow.ToString('o');begin_return=$null;end_return=$null;before_shadows=$before;after_shadows=@();new_shadows=@();created_points=@();deletions=0;service_restarts=0;reboot_requested=$false}
function SaveReceipt { [IO.File]::WriteAllText($OutputPath,($receipt | ConvertTo-Json -Depth 8),(New-Object Text.UTF8Encoding($false))) }
function CaptureRegistration {
    $receipt.created_points=@(Get-CimInstance -Namespace root/default -ClassName SystemRestore -Filter "Description='$description'" -Property CreationTime,RestorePointType,EventType,SequenceNumber | ForEach-Object {[ordered]@{created=$_.CreationTime;type=$_.RestorePointType;event_type=$_.EventType;sequence=$_.SequenceNumber}})
    $receipt.after_shadows=@(CShadows)
    $beforeTokens=@($before | ForEach-Object {$_.token})
    $receipt.new_shadows=@($receipt.after_shadows | Where-Object {$_.token -notin $beforeTokens})
}
SaveReceipt
try {
    $begin=Invoke-CimMethod -Namespace root/default -ClassName SystemRestore -MethodName CreateRestorePoint -Arguments @{Description=$description;RestorePointType=[uint32]12;EventType=[uint32]100} -OperationTimeoutSec 240
    $receipt.begin_return=[uint32]$begin.ReturnValue
    $receipt.phase='begin_returned_completion_not_yet_verified'
    SaveReceipt
    if($begin.ReturnValue -ne 0) { CaptureRegistration; SaveReceipt; throw 'CREATE_BEGIN_NONZERO_RECONCILE_NO_AUTOMATIC_RETRY' }
    $receipt.phase='end_call_pending_outcome_unknown'
    SaveReceipt
    $end=Invoke-CimMethod -Namespace root/default -ClassName SystemRestore -MethodName CreateRestorePoint -Arguments @{Description=$description;RestorePointType=[uint32]12;EventType=[uint32]101} -OperationTimeoutSec 120
    $receipt.end_return=[uint32]$end.ReturnValue
    $receipt.phase='end_returned_registration_not_yet_verified'
    CaptureRegistration
    SaveReceipt
    if($end.ReturnValue -ne 0) { throw 'CREATE_END_NONZERO_EXISTING_REGISTRATION_RECORDED' }
    if($receipt.created_points.Count -ne 1 -or $receipt.new_shadows.Count -ne 1) { throw 'NEW_POINT_OR_SNAPSHOT_AMBIGUOUS_PRESERVE_ALL' }
    if(-not $receipt.new_shadows[0].persistent -or $receipt.new_shadows[0].state -ne 12) { throw 'NEW_SNAPSHOT_NOT_PERSISTENT_CREATED' }
    $afterTokens=@($receipt.after_shadows | ForEach-Object {$_.token})
    if(@($before | Where-Object {$_.token -notin $afterTokens}).Count -ne 0) {throw 'PREEXISTING_GENERATION_DISAPPEARED'}
    $receipt.phase='one_new_point_and_snapshot_identified_retention_not_yet_verified'
    $receipt.finished_utc=[DateTime]::UtcNow.ToString('o')
    SaveReceipt
    Write-Output ($receipt | ConvertTo-Json -Depth 8)
} catch {
    $receipt.failure_utc=[DateTime]::UtcNow.ToString('o')
    $receipt.failure_hresult=$_.Exception.HResult
    SaveReceipt
    throw
}
