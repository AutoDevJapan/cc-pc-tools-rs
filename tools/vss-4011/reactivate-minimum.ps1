param(
    [Parameter(Mandatory=$true)][ValidateSet('Apply','Undo')][string]$Mode,
    [Parameter(Mandatory=$true)][string]$QueryExe,
    [Parameter(Mandatory=$true)][string]$QueryExeSha256
)
$ErrorActionPreference='Stop'
trap { [Console]::Error.WriteLine(('reactivation_failure line={0} id={1}' -f $_.InvocationInfo.ScriptLineNumber,$_.FullyQualifiedErrorId)); exit 1 }
. (Join-Path $PSScriptRoot 'common.ps1')
if((Get-Vss4011FileHash $QueryExe) -cne $QueryExeSha256) {throw 'READ_ONLY_QUERY_BINARY_CHANGED'}
$ledger=Split-Path -Parent (Get-Vss4011ReceiptPath 'configuration-request')
$requestPath=Join-Path $ledger 'reactivation-request.private.json'
$resultPath=Join-Path $ledger ('reactivation-'+$Mode.ToLowerInvariant()+'-result.private.json')
function ReadMinimum {
    $si=New-Object Diagnostics.ProcessStartInfo
    $si.FileName=$QueryExe;$si.UseShellExecute=$false;$si.CreateNoWindow=$true
    $si.RedirectStandardOutput=$true;$si.RedirectStandardError=$true
    $p=[Diagnostics.Process]::Start($si)
    $out=$p.StandardOutput.ReadToEndAsync();$err=$p.StandardError.ReadToEndAsync()
    if(-not $p.WaitForExit(30000)) {
        # Only the read-only helper just started by this invocation is stopped.
        if(-not $p.HasExited) {$p.Kill()};[void]$p.WaitForExit(5000)
        throw 'READ_ONLY_QUERY_NOT_FINISHED_RECONCILE_NO_RETRY'
    }
    $text=$out.GetAwaiter().GetResult();[void]$err.GetAwaiter().GetResult()
    if($p.ExitCode -ne 0) {throw 'READ_ONLY_QUERY_FAILED'}
    $row=$text|ConvertFrom-Json
    if($row.source -ne 'IVssSnapshotMgmt2' -or $row.phase -ne 'GetMinDiffAreaSize' -or $row.hresult -ne '0x00000000' -or $row.changes -ne 0 -or $row.minimum_bytes -le 0) {throw 'READ_ONLY_QUERY_RESULT_INVALID'}
    return $row
}
$key=[Microsoft.Win32.Registry]::LocalMachine.OpenSubKey('SYSTEM\CurrentControlSet\Services\VolSnap',$true)
if($null -eq $key) {throw 'VOLSNAP_KEY_MISSING'}
try {
    if(Test-Path -LiteralPath $resultPath) {throw 'REACTIVATION_STAGE_ALREADY_RECORDED_NO_RETRY'}
    if($Mode -eq 'Undo') {
        $request=Get-Content -LiteralPath $requestPath -Encoding UTF8 -Raw|ConvertFrom-Json
        if($request.todo -ne 4011 -or $request.phase -ne 'reactivation_call_pending_outcome_unknown' -or $request.before_present -ne $false -or $request.target_MB -ne 3200) {throw 'REACTIVATION_REQUEST_NOT_OURS'}
        $present='MinDiffAreaFileSize' -in $key.GetValueNames()
        if($present -and ($key.GetValue('MinDiffAreaFileSize') -ne 3200 -or $key.GetValueKind('MinDiffAreaFileSize') -ne [Microsoft.Win32.RegistryValueKind]::DWord)) {throw 'SETTING_CHANGED_PRESERVE_OTHER_ACTOR'}
        if($present) {$key.DeleteValue('MinDiffAreaFileSize',$true);$key.Flush()}
        if('MinDiffAreaFileSize' -in $key.GetValueNames()) {throw 'UNDO_READBACK_FAILED'}
        $result=[ordered]@{todo=4011;phase='registry_absent_provider_observation_follows';utc=[DateTime]::UtcNow.ToString('o');snapshot_deletion_requests=0;registry_present=$false}
        # The registry result survives even if the subsequent read-only API fails.
        Write-Vss4011NewReceipt $resultPath $result
        Write-Output ($result|ConvertTo-Json)
        ReadMinimum|ConvertTo-Json
        exit 0
    }
    if(Test-Path -LiteralPath $requestPath) {throw 'REACTIVATION_ATTEMPT_EXISTS_NO_RETRY'}
    if('MinDiffAreaFileSize' -in $key.GetValueNames()) {throw 'MINIMUM_NO_LONGER_ABSENT'}
    $prior=Get-Content -LiteralPath (Get-Vss4011ReceiptPath 'configuration-request') -Encoding UTF8 -Raw|ConvertFrom-Json
    $rollback=Get-Content -LiteralPath (Get-Vss4011ReceiptPath 'rollback-result') -Encoding UTF8 -Raw|ConvertFrom-Json
    $attempt=Get-Content -LiteralPath (Get-Vss4011ReceiptPath 'point-A-result') -Encoding UTF8 -Raw|ConvertFrom-Json
    if($prior.todo -ne 4011 -or $prior.before_present -ne $false -or $prior.target_MB -ne 3200 -or $rollback.todo -ne 4011 -or $rollback.phase -ne 'registry_absent_driver_effect_unverified' -or $attempt.todo -ne 4011 -or $attempt.phase -ne 'attempt_failed_or_outcome_unresolved_no_retry') {throw 'ORIGINAL_ATTEMPT_AND_ROLLBACK_NOT_RECONCILED'}
    $c=Get-Vss4011CVolume;$storage=Get-Vss4011Storage $c.DeviceID
    if($storage.maximum_bytes -ne 50GB -or [uint64]$c.FreeSpace -lt (3200MB+200GB)) {throw 'CAPACITY_OR_MAXIMUM_CHANGED'}
    $providers=@(Get-CimInstance Win32_ShadowProvider)
    if($providers.Count -ne 1 -or $providers[0].Name -ne 'Microsoft Software Shadow Copy provider 1.0') {throw 'PROVIDER_CHANGED'}
    foreach($path in @('HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\SystemRestore','HKLM:\SOFTWARE\Policies\Microsoft\Windows NT\SystemRestore')) {
        $disabled=Get-ItemProperty -LiteralPath $path -Name DisableSR -ErrorAction SilentlyContinue
        if($disabled -and $disabled.DisableSR -ne 0) {throw 'SYSTEM_RESTORE_EXPLICITLY_DISABLED'}
    }
    if(@(Get-CimInstance Win32_ShadowCopy|Where-Object {$_.VolumeName -ne $c.DeviceID}).Count -ne 0) {throw 'OTHER_VOLUME_SNAPSHOTS_PRESENT'}
    $shadows=@(Get-Vss4011Shadows $c.DeviceID)
    $points=@(Get-CimInstance -Namespace root/default -ClassName SystemRestore -Filter "Description='ToDo4011 VSS retention verification A'" -Property SequenceNumber)
    $tokens=@($shadows|ForEach-Object {$_.token})
    if($points.Count -ne 1 -or $points[0].SequenceNumber -ne 263 -or $tokens.Count -lt 2 -or '305F3E2D8D193BBC' -notin $tokens -or 'EFA3F28F34D9899B' -notin $tokens -or @($shadows|Where-Object {-not $_.persistent -or $_.state -ne 12}).Count -ne 0) {throw 'RECONCILED_GENERATIONS_NOT_PRESENT'}
    $before=ReadMinimum
    if($before.minimum_bytes -ne 320MB) {throw 'ORIGINAL_PROVIDER_MINIMUM_CHANGED'}
    $request=[ordered]@{schema=1;todo=4011;actor_job=$env:CC_JOB_ID;phase='reactivation_call_pending_outcome_unknown';utc=[DateTime]::UtcNow.ToString('o');before_present=$false;target_MB=3200;before_provider=$before;storage=$storage;shadows=$shadows;query_exe_sha256=$QueryExeSha256;snapshot_creations=0;snapshot_deletion_requests=0}
    Write-Vss4011NewReceipt $requestPath $request
    $set=$false
    try {
        $key.SetValue('MinDiffAreaFileSize',3200,[Microsoft.Win32.RegistryValueKind]::DWord);$set=$true;$key.Flush()
        $after=ReadMinimum
        if($key.GetValue('MinDiffAreaFileSize') -ne 3200 -or $key.GetValueKind('MinDiffAreaFileSize') -ne [Microsoft.Win32.RegistryValueKind]::DWord -or $after.minimum_bytes -ne 3200MB) {throw 'LIVE_PROVIDER_MINIMUM_NOT_VERIFIED'}
        $afterTokens=@(Get-Vss4011Shadows $c.DeviceID|ForEach-Object {$_.token})
        if(@($tokens|Where-Object {$_ -notin $afterTokens}).Count -ne 0) {throw 'GENERATION_CHANGED_DURING_SETTING'}
        $result=[ordered]@{schema=1;todo=4011;phase='provider_minimum_verified_retention_and_io_effect_unverified';utc=[DateTime]::UtcNow.ToString('o');before_provider=$before;after_provider=$after;snapshot_creations=0;snapshot_deletion_requests=0;original_token_count=$tokens.Count;all_original_tokens_present=$true}
        Write-Vss4011NewReceipt $resultPath $result
        Write-Output ($result|ConvertTo-Json -Depth 10)
    } catch {
        if($set -and $key.GetValue('MinDiffAreaFileSize') -eq 3200 -and $key.GetValueKind('MinDiffAreaFileSize') -eq [Microsoft.Win32.RegistryValueKind]::DWord) {$key.DeleteValue('MinDiffAreaFileSize',$true);$key.Flush()}
        throw
    }
} finally {$key.Dispose()}
