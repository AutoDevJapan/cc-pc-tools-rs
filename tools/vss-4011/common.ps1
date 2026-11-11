# Shared, finite receipt and observation functions; loading this file changes no OS state.
function Get-Vss4011FileHash {
    param([Parameter(Mandatory=$true)][string]$Path)
    $file=[IO.File]::OpenRead($Path)
    $hash=[Security.Cryptography.SHA256]::Create()
    try {return ([BitConverter]::ToString($hash.ComputeHash($file))).Replace('-','')} finally {$hash.Dispose();$file.Dispose()}
}
function Get-Vss4011ReceiptPath {
    param([Parameter(Mandatory=$true)][string]$Name,[string]$RequestedPath)
    if($Name -notmatch '^(configuration-(request|result)|point-[AB]-(request|begin|end-request|result)|rollback-(request|result))$') {throw 'INVALID_RECEIPT_NAME'}
    $root=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../.ci-work/4011/os-v2'))
    $path=Join-Path $root ($Name+'.private.json')
    if($RequestedPath -and -not [StringComparer]::OrdinalIgnoreCase.Equals([IO.Path]::GetFullPath($RequestedPath),$path)) {throw 'FIXED_RECEIPT_PATH_REQUIRED'}
    return $path
}
function Write-Vss4011NewReceipt {
    param([Parameter(Mandatory=$true)][string]$Path,[Parameter(Mandatory=$true)]$Value)
    [void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($Path))
    $bytes=(New-Object Text.UTF8Encoding($false)).GetBytes(($Value | ConvertTo-Json -Depth 14))
    # CreateNew is the permanent claim: even a partial record blocks another attempt.
    $file=[IO.FileStream]::new($Path,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::Read,4096,[IO.FileOptions]::WriteThrough)
    try {$file.Write($bytes,0,$bytes.Length);$file.Flush($true)} finally {$file.Dispose()}
}
function Get-Vss4011CVolume {
    $rows=@(Get-CimInstance Win32_Volume -Filter "DriveLetter='C:'" -Property DeviceID,FreeSpace)
    if($rows.Count -ne 1) {throw 'C_VOLUME_NOT_UNIQUE'}
    return $rows[0]
}
function Get-Vss4011Storage {
    param([Parameter(Mandatory=$true)][string]$CDeviceId)
    $rows=@(Get-CimInstance Win32_ShadowStorage)
    if($rows.Count -ne 1 -or $rows[0].Volume.DeviceID -ne $CDeviceId -or $rows[0].DiffVolume.DeviceID -ne $CDeviceId) {throw 'EXISTING_C_C_ASSOCIATION_CHANGED'}
    return [pscustomobject]@{sampled_utc=[DateTime]::UtcNow.ToString('o');source='C:';storage='C:';used_bytes=[uint64]$rows[0].UsedSpace;allocated_bytes=[uint64]$rows[0].AllocatedSpace;maximum_bytes=[uint64]$rows[0].MaxSpace}
}
function Get-Vss4011Shadows {
    param([Parameter(Mandatory=$true)][string]$CDeviceId)
    @(Get-CimInstance Win32_ShadowCopy | Where-Object {$_.VolumeName -eq $CDeviceId} | ForEach-Object {
        $hash=[Security.Cryptography.SHA256]::Create()
        try {$token=([BitConverter]::ToString($hash.ComputeHash([Text.Encoding]::UTF8.GetBytes($_.ID)))).Replace('-','').Substring(0,16)} finally {$hash.Dispose()}
        [pscustomobject]@{token=$token;created_utc=$_.InstallDate.ToUniversalTime().ToString('o');state=$_.State;persistent=$_.Persistent;client_accessible=$_.ClientAccessible;no_auto_release=$_.NoAutoRelease}
    })
}
function Test-Vss4011InitialReservation {
    param([Parameter(Mandatory=$true)]$Before,[Parameter(Mandatory=$true)]$After)
    $allocatedDelta=[int64]$After.allocated_bytes-[int64]$Before.allocated_bytes
    $usedDelta=[int64]$After.used_bytes-[int64]$Before.used_bytes
    $unusedDelta=$allocatedDelta-$usedDelta
    $passed=$Before.source -eq 'C:' -and $Before.storage -eq 'C:' -and $After.source -eq 'C:' -and $After.storage -eq 'C:' -and
        $Before.maximum_bytes -eq 50GB -and $After.maximum_bytes -eq 50GB -and $After.allocated_bytes -le 50GB -and
        $After.used_bytes -le $After.allocated_bytes -and $usedDelta -ge 0 -and $allocatedDelta -ge 3200MB -and $unusedDelta -ge 2880MB
    return [pscustomobject]@{passed=[bool]$passed;allocated_delta_bytes=$allocatedDelta;used_delta_bytes=$usedDelta;unused_reservation_delta_bytes=$unusedDelta;required_allocation_delta_bytes=[int64](3200MB);required_unused_delta_bytes=[int64](2880MB)}
}
