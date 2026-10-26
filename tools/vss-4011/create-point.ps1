param([Parameter(Mandatory=$true)][ValidateSet('A','B')][string]$Stage,[string]$OutputPath,[string]$FirstReceiptPath)
$ErrorActionPreference='Stop'
trap { [Console]::Error.WriteLine(('create_point_failure line={0} id={1}' -f $_.InvocationInfo.ScriptLineNumber,$_.FullyQualifiedErrorId)); exit 1 }
. (Join-Path $PSScriptRoot 'common.ps1')
$OutputPath=Get-Vss4011ReceiptPath ('point-'+$Stage+'-result') $OutputPath
$requestPath=Get-Vss4011ReceiptPath ('point-'+$Stage+'-request')
$beginPath=Get-Vss4011ReceiptPath ('point-'+$Stage+'-begin')
$endRequestPath=Get-Vss4011ReceiptPath ('point-'+$Stage+'-end-request')
if((Test-Path -LiteralPath $requestPath) -or (Test-Path -LiteralPath $OutputPath)) {throw 'STAGE_ATTEMPT_EXISTS_RECONCILE_NO_RETRY'}
$description='ToDo4011 VSS retention verification '+$Stage
$minimum=Get-ItemProperty -LiteralPath 'HKLM:\SYSTEM\CurrentControlSet\Services\VolSnap' -Name MinDiffAreaFileSize
if($minimum.MinDiffAreaFileSize -ne 3200) {throw 'REVIEWED_MINIMUM_NOT_PRESENT'}
$c=Get-Vss4011CVolume
$before=@(Get-Vss4011Shadows $c.DeviceID)
if($Stage -eq 'B') {
    $firstPath=Get-Vss4011ReceiptPath 'point-A-result' $FirstReceiptPath
    $first=Get-Content -LiteralPath $firstPath -Encoding UTF8 -Raw | ConvertFrom-Json
    if($first.schema -ne 2 -or $first.todo -ne 4011 -or $first.stage -ne 'A' -or $first.phase -ne 'point_and_initial_reservation_verified_retention_pending' -or @($first.new_shadows).Count -ne 1) {throw 'FIRST_POINT_NOT_CONFIRMED'}
    $firstGate=Test-Vss4011InitialReservation $first.before_storage $first.after_storage
    if(-not $firstGate.passed) {throw 'FIRST_INITIAL_RESERVATION_UNVERIFIED_ROLLBACK_BEFORE_B'}
    if($first.new_shadows[0].token -notin @($before | ForEach-Object {$_.token})) {throw 'FIRST_GENERATION_NO_LONGER_PRESENT'}
}
if(@(Get-CimInstance -Namespace root/default -ClassName SystemRestore -Filter "Description='$description'" -Property SequenceNumber).Count -ne 0) {throw 'OWN_POINT_EXISTS_NO_RETRY'}
$storage=Get-Vss4011Storage $c.DeviceID
$needed=if($Stage -eq 'A'){2*3200MB+1GB}else{3200MB+1GB}
if($storage.maximum_bytes -ne 50GB -or [uint64]$storage.allocated_bytes+$needed -gt 50GB) {throw 'INSUFFICIENT_UNCHANGED_QUOTA_PRESERVE_GENERATIONS'}
if([uint64]$c.FreeSpace -lt (3200MB+200GB)) {throw 'C_HEADROOM_INSUFFICIENT'}
# This storage sample and permanent claim are immediately before BEGIN.
$request=[ordered]@{schema=2;todo=4011;actor_job=$env:CC_JOB_ID;stage=$Stage;phase='begin_call_pending_outcome_unknown';started_utc=[DateTime]::UtcNow.ToString('o');before_storage=$storage;before_shadows=$before;snapshot_deletion_requests=0}
Write-Vss4011NewReceipt $requestPath $request
$result=[ordered]@{schema=2;todo=4011;actor_job=$env:CC_JOB_ID;stage=$Stage;phase='begin_call_pending_outcome_unknown';started_utc=$request.started_utc;before_storage=$storage;after_storage=$null;before_shadows=$before;after_shadows=@();new_shadows=@();created_points=@();begin_return=$null;end_return=$null;reservation_gate=$null;failure_code=$null}
try {
    $begin=Invoke-CimMethod -Namespace root/default -ClassName SystemRestore -MethodName CreateRestorePoint -Arguments @{Description=$description;RestorePointType=[uint32]12;EventType=[uint32]100} -OperationTimeoutSec 240
    $result.begin_return=[uint32]$begin.ReturnValue
    if($result.begin_return -eq 0) {
        try {
            Write-Vss4011NewReceipt $beginPath ([ordered]@{schema=2;todo=4011;stage=$Stage;phase='begin_returned';utc=[DateTime]::UtcNow.ToString('o');return_value=$result.begin_return})
            $result.phase='end_call_pending_outcome_unknown'
            Write-Vss4011NewReceipt $endRequestPath ([ordered]@{schema=2;todo=4011;stage=$Stage;phase=$result.phase;utc=[DateTime]::UtcNow.ToString('o')})
        } finally {
            # Close a successful BEGIN even if writing its receipt failed.
            $end=Invoke-CimMethod -Namespace root/default -ClassName SystemRestore -MethodName CreateRestorePoint -Arguments @{Description=$description;RestorePointType=[uint32]12;EventType=[uint32]101} -OperationTimeoutSec 120
            $result.end_return=[uint32]$end.ReturnValue
            $result.after_storage=Get-Vss4011Storage $c.DeviceID
        }
    } else {
        $result.after_storage=Get-Vss4011Storage $c.DeviceID
        Write-Vss4011NewReceipt $beginPath ([ordered]@{schema=2;todo=4011;stage=$Stage;phase='begin_returned';utc=[DateTime]::UtcNow.ToString('o');return_value=$result.begin_return})
    }
    # The first query after END (or failed BEGIN) records actual storage, before
    # registration queries can let ordinary IO obscure the initial allocation.
    $result.reservation_gate=Test-Vss4011InitialReservation $result.before_storage $result.after_storage
    $result.after_shadows=@(Get-Vss4011Shadows $c.DeviceID)
    $beforeTokens=@($before | ForEach-Object {$_.token})
    $result.new_shadows=@($result.after_shadows | Where-Object {$_.token -notin $beforeTokens})
    $result.created_points=@(Get-CimInstance -Namespace root/default -ClassName SystemRestore -Filter "Description='$description'" -Property CreationTime,RestorePointType,EventType,SequenceNumber | ForEach-Object {[ordered]@{created=$_.CreationTime;type=$_.RestorePointType;event_type=$_.EventType;sequence=$_.SequenceNumber}})
    if($result.begin_return -ne 0 -or $result.end_return -ne 0) {throw 'CREATE_RETURN_NONZERO_REGISTRATION_RECORDED'}
    if($result.created_points.Count -ne 1 -or $result.new_shadows.Count -ne 1) {throw 'NEW_POINT_OR_SNAPSHOT_AMBIGUOUS'}
    if(-not $result.new_shadows[0].persistent -or $result.new_shadows[0].state -ne 12) {throw 'NEW_SNAPSHOT_NOT_PERSISTENT_CREATED'}
    $afterTokens=@($result.after_shadows | ForEach-Object {$_.token})
    if(@($before | Where-Object {$_.token -notin $afterTokens}).Count -ne 0) {throw 'PREEXISTING_GENERATION_DISAPPEARED'}
    if($Stage -eq 'A' -and -not $result.reservation_gate.passed) {throw 'INITIAL_RESERVATION_UNVERIFIED_ROLLBACK_BEFORE_B'}
    if($result.after_storage.maximum_bytes -ne 50GB) {throw 'MAXIMUM_CHANGED'}
    $result.phase=if($Stage -eq 'A'){'point_and_initial_reservation_verified_retention_pending'}else{'two_generations_created_retention_pending'}
    $result.finished_utc=[DateTime]::UtcNow.ToString('o')
    Write-Vss4011NewReceipt $OutputPath $result
    Write-Output ($result | ConvertTo-Json -Depth 12)
} catch {
    $result.failure_code=if($_.Exception.Message -cmatch '^[A-Z_]+$'){$_.Exception.Message}else{'WINDOWS_FAILURE_OR_OUTCOME_UNRESOLVED'}
    $result.failure_hresult=$_.Exception.HResult
    $result.failure_utc=[DateTime]::UtcNow.ToString('o')
    $result.phase='attempt_failed_or_outcome_unresolved_no_retry'
    if(-not (Test-Path -LiteralPath $OutputPath)) {Write-Vss4011NewReceipt $OutputPath $result}
    throw
}
