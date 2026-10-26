param([Parameter(Mandatory=$true)][string]$AppliedReceiptPath,[Parameter(Mandatory=$true)][string]$OutputPath)
$ErrorActionPreference='Stop'
trap { [Console]::Error.WriteLine(('rollback_failure line={0} id={1}' -f $_.InvocationInfo.ScriptLineNumber,$_.FullyQualifiedErrorId)); exit 1 }
# Remove only the reviewed DWORD introduced by this repair. Never remove a
# registry key, snapshot, storage association, task, or service configuration.
$applied=Get-Content -LiteralPath $AppliedReceiptPath -Encoding UTF8 -Raw | ConvertFrom-Json
if($applied.todo -ne 4011 -or $applied.before_present -ne $false -or $applied.target_MB -ne 3200 -or $applied.after_value -ne 3200 -or $applied.phase -ne 'registry_applied_effectiveness_not_yet_tested') {throw 'APPLIED_RECEIPT_NOT_OUR_CONFIRMED_VALUE'}
$key=[Microsoft.Win32.Registry]::LocalMachine.OpenSubKey('SYSTEM\CurrentControlSet\Services\VolSnap',$true)
if($null -eq $key) {throw 'VOLSNAP_KEY_MISSING'}
try {
    if($key.GetValue('MinDiffAreaFileSize',$null) -ne 3200 -or $key.GetValueKind('MinDiffAreaFileSize') -ne [Microsoft.Win32.RegistryValueKind]::DWord) {throw 'VALUE_CHANGED_PRESERVE_OTHER_ACTOR_SETTING'}
    $receipt=[ordered]@{schema=1;todo=4011;phase='rollback_call_pending_outcome_unknown';started_utc=[DateTime]::UtcNow.ToString('o');expected_MB=3200;before_present=$false;snapshot_deletions=0;storage_changes=0;reboot_requested=$false}
    [IO.File]::WriteAllText($OutputPath,($receipt | ConvertTo-Json),(New-Object Text.UTF8Encoding($false)))
    $key.DeleteValue('MinDiffAreaFileSize',$true)
    $key.Flush()
    if('MinDiffAreaFileSize' -in $key.GetValueNames()) {throw 'ROLLBACK_READBACK_FAILED'}
    $receipt.phase='own_minimum_restored_to_original_absent_snapshot_retention_not_altered'
    $receipt.finished_utc=[DateTime]::UtcNow.ToString('o')
    $json=$receipt | ConvertTo-Json
    [IO.File]::WriteAllText($OutputPath,$json,(New-Object Text.UTF8Encoding($false)))
    Write-Output $json
} finally {$key.Dispose()}
