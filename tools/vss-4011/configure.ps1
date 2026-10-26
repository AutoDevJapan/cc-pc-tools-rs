param([string]$OutputPath)
$ErrorActionPreference='Stop'
trap { [Console]::Error.WriteLine(('configure_failure line={0} id={1}' -f $_.InvocationInfo.ScriptLineNumber,$_.FullyQualifiedErrorId)); exit 1 }
. (Join-Path $PSScriptRoot 'common.ps1')
$OutputPath=Get-Vss4011ReceiptPath 'configuration-result' $OutputPath
$requestPath=Get-Vss4011ReceiptPath 'configuration-request'
if((Test-Path -LiteralPath $requestPath) -or (Test-Path -LiteralPath $OutputPath)) {throw 'CONFIGURATION_ATTEMPT_EXISTS_RECONCILE_BEFORE_ANY_RETRY'}
$c=Get-Vss4011CVolume
$storage=Get-Vss4011Storage $c.DeviceID
if($storage.maximum_bytes -ne 50GB) {throw 'EXISTING_50GIB_MAXIMUM_CHANGED'}
if(@(Get-CimInstance Win32_ShadowCopy | Where-Object {$_.VolumeName -ne $c.DeviceID}).Count -ne 0) {throw 'OTHER_VOLUME_SNAPSHOTS_PRESENT'}
$providers=@(Get-CimInstance Win32_ShadowProvider)
if($providers.Count -ne 1 -or $providers[0].Name -ne 'Microsoft Software Shadow Copy provider 1.0') {throw 'PROVIDER_CHANGED'}
if([uint64]$c.FreeSpace -lt (3200MB+200GB)) {throw 'C_HEADROOM_INSUFFICIENT'}
foreach($path in @('HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\SystemRestore','HKLM:\SOFTWARE\Policies\Microsoft\Windows NT\SystemRestore')) {
    $disabled=Get-ItemProperty -LiteralPath $path -Name DisableSR -ErrorAction SilentlyContinue
    if($disabled -and $disabled.DisableSR -ne 0) {throw 'SYSTEM_RESTORE_EXPLICITLY_DISABLED'}
}
$key=[Microsoft.Win32.Registry]::LocalMachine.OpenSubKey('SYSTEM\CurrentControlSet\Services\VolSnap',$true)
if($null -eq $key) {throw 'VOLSNAP_KEY_MISSING'}
try {
    if('MinDiffAreaFileSize' -in $key.GetValueNames()) {throw 'MINIMUM_NO_LONGER_ABSENT_DO_NOT_OVERWRITE'}
    $request=[ordered]@{schema=2;todo=4011;actor_job=$env:CC_JOB_ID;phase='configuration_call_pending_outcome_unknown';started_utc=[DateTime]::UtcNow.ToString('o');before_present=$false;before_value=$null;target_MB=3200;before_storage=$storage;before_shadows=@(Get-Vss4011Shadows $c.DeviceID);free_before_bytes=[uint64]$c.FreeSpace;snapshot_deletion_requests=0}
    # The immutable request remains readable if the process stops after SetValue.
    Write-Vss4011NewReceipt $requestPath $request
    $key.SetValue('MinDiffAreaFileSize',3200,[Microsoft.Win32.RegistryValueKind]::DWord)
    $key.Flush()
    if($key.GetValue('MinDiffAreaFileSize',$null) -ne 3200 -or $key.GetValueKind('MinDiffAreaFileSize') -ne [Microsoft.Win32.RegistryValueKind]::DWord) {throw 'MINIMUM_READBACK_FAILED'}
    $result=[ordered]@{schema=2;todo=4011;actor_job=$env:CC_JOB_ID;phase='registry_applied_effectiveness_not_yet_tested';finished_utc=[DateTime]::UtcNow.ToString('o');before_present=$false;target_MB=3200;after_value=3200;request_sha256=(Get-Vss4011FileHash $requestPath);snapshot_deletion_requests=0}
    Write-Vss4011NewReceipt $OutputPath $result
    Write-Output ($result | ConvertTo-Json -Depth 8)
} finally {$key.Dispose()}
