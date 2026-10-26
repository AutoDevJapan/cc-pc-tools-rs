param([string]$AppliedReceiptPath,[string]$OutputPath)
$ErrorActionPreference='Stop'
trap { [Console]::Error.WriteLine(('rollback_failure line={0} id={1}' -f $_.InvocationInfo.ScriptLineNumber,$_.FullyQualifiedErrorId)); exit 1 }
. (Join-Path $PSScriptRoot 'common.ps1')
$AppliedReceiptPath=Get-Vss4011ReceiptPath 'configuration-request' $AppliedReceiptPath
$OutputPath=Get-Vss4011ReceiptPath 'rollback-result' $OutputPath
$requestPath=Get-Vss4011ReceiptPath 'rollback-request'
$applied=Get-Content -LiteralPath $AppliedReceiptPath -Encoding UTF8 -Raw | ConvertFrom-Json
if($applied.schema -ne 2 -or $applied.todo -ne 4011 -or $applied.before_present -ne $false -or $applied.target_MB -ne 3200 -or $applied.phase -ne 'configuration_call_pending_outcome_unknown') {throw 'OWN_CONFIGURATION_REQUEST_NOT_CONFIRMED'}
$key=[Microsoft.Win32.Registry]::LocalMachine.OpenSubKey('SYSTEM\CurrentControlSet\Services\VolSnap',$true)
if($null -eq $key) {throw 'VOLSNAP_KEY_MISSING'}
try {
    $present='MinDiffAreaFileSize' -in $key.GetValueNames()
    if((Test-Path -LiteralPath $OutputPath) -and $present) {throw 'VALUE_REAPPEARED_AFTER_ROLLBACK_PRESERVE_OTHER_ACTOR'}
    if($present -and ($key.GetValue('MinDiffAreaFileSize',$null) -ne 3200 -or $key.GetValueKind('MinDiffAreaFileSize') -ne [Microsoft.Win32.RegistryValueKind]::DWord)) {throw 'VALUE_CHANGED_PRESERVE_OTHER_ACTOR_SETTING'}
    if(-not (Test-Path -LiteralPath $requestPath)) {Write-Vss4011NewReceipt $requestPath ([ordered]@{schema=2;todo=4011;actor_job=$env:CC_JOB_ID;phase='rollback_call_pending_outcome_unknown';started_utc=[DateTime]::UtcNow.ToString('o');expected_MB=3200;original_present=$false;configuration_request_sha256=(Get-Vss4011FileHash $AppliedReceiptPath)})}
    if($present) {$key.DeleteValue('MinDiffAreaFileSize',$true);$key.Flush()}
    if('MinDiffAreaFileSize' -in $key.GetValueNames()) {throw 'ROLLBACK_READBACK_FAILED'}
    $c=Get-Vss4011CVolume
    $result=[ordered]@{schema=2;todo=4011;actor_job=$env:CC_JOB_ID;phase='registry_absent_driver_effect_unverified';finished_utc=[DateTime]::UtcNow.ToString('o');already_absent=(-not $present);snapshot_deletion_requests=0;storage_changes=0;storage=(Get-Vss4011Storage $c.DeviceID);shadows=@(Get-Vss4011Shadows $c.DeviceID)}
    if(Test-Path -LiteralPath $OutputPath) { $prior=Get-Content -LiteralPath $OutputPath -Encoding UTF8 -Raw | ConvertFrom-Json; if($prior.schema -ne 2 -or $prior.todo -ne 4011 -or $prior.phase -ne $result.phase) {throw 'EXISTING_ROLLBACK_RESULT_UNRECONCILED'} } else {Write-Vss4011NewReceipt $OutputPath $result}
    Write-Output ($result | ConvertTo-Json -Depth 10)
} finally {$key.Dispose()}
