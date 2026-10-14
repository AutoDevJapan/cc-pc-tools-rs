$ErrorActionPreference = 'Stop'
$root = [IO.Path]::GetFullPath('D:\CC\Work\unattended-operations-20261013\deploy-resident-2490')
$name = 'CC-ResidentGuard-S4UProbe-2492'
$task = Get-ScheduledTask -TaskName $name -ErrorAction SilentlyContinue
if ($null -ne $task) {
    if ($task.State -eq 'Running' -or $task.Principal.LogonType -ne 'S4U' -or $task.Principal.RunLevel -ne 'Limited' -or $task.Actions.Count -ne 1 -or $task.Actions[0].Execute -ne 'D:\CC\Tools\resident-guard\resident-guard.exe' -or $task.Actions[0].Arguments -notlike '--session-probe --state-dir*--expected-user-sid S-1-*') { throw 'cleanup_probe_changed_or_busy' }
    Unregister-ScheduledTask -InputObject $task -Confirm:$false
}
$ownedHashes = @(
    '83C2AA8F7D264B2833E69196ADF2B316DCBC85501D289C93D62AE5F40B05A43F',
    '0C12915DD5ED174FC330373BBF27515869986C724E32141837817D2F7DABC6CC',
    'B5D2F9191ED5DAAD09077B80486BCBE33B32173287A3A57CE47FD07FDE36CD49',
    '1BF3D7929CC87636E66E4AC4DF4AFC9FF0A62141561B2B1F4A995E9AAD0A5F40',
    'B8AD09518833C43937DA5FE0C05C6D24F1846D5B305D98781CDC8C4FBAB6836B',
    'FC6645242CD1DA39C5C79205E79F8CF1A523D640B305C11B053E11FA62BC2948',
    '5F9BFF483EE6458C9982A61AB59BBD987CB1B3F4890E8936D0634A2444D9A238'
)
$prototypes = @(Get-ChildItem -LiteralPath $root -File | Where-Object { $_.Name -match '^resident-guard-boot-probe(?:-before-[A-Za-z0-9-]+)?-2492\.exe$' })
$allActions = @(Get-ScheduledTask | ForEach-Object { $_.Actions } | ForEach-Object { $_.Execute })
$processes = @(Get-CimInstance Win32_Process)
$records = @()
foreach ($file in $prototypes) {
    $absolute = [IO.Path]::GetFullPath($file.FullName)
    if (-not $absolute.StartsWith($root + '\',[StringComparison]::OrdinalIgnoreCase) -or ($file.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'cleanup_prototype_out_of_scope' }
    $hash = (Get-FileHash -LiteralPath $absolute -Algorithm SHA256).Hash
    if ($hash -notin $ownedHashes) { throw 'cleanup_prototype_hash_unknown' }
    if (@($allActions | Where-Object { [string]::Equals($_,$absolute,[StringComparison]::OrdinalIgnoreCase) }).Count -ne 0 -or @($processes | Where-Object { [string]::Equals($_.ExecutablePath,$absolute,[StringComparison]::OrdinalIgnoreCase) }).Count -ne 0) { throw 'cleanup_prototype_still_used' }
    $records += [ordered]@{ sha256=$hash; bytes=$file.Length }
}
# All planned targets have been verified before removing any of them.
foreach ($file in $prototypes) { Remove-Item -LiteralPath $file.FullName }
$receipt = [ordered]@{ cleaned_at=[DateTime]::UtcNow.ToString('o'); temporary_task_removed=($null -eq (Get-ScheduledTask -TaskName $name -ErrorAction SilentlyContinue)); binaries_removed=$records.Count; bytes_removed=(@($records | ForEach-Object { $_.bytes }) | Measure-Object -Sum).Sum; owned_binary_receipts=$records; production_runtime_preserved=$true }
$receipt | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $root 's4u-cleanup-receipt-2492.json') -Encoding UTF8
[ordered]@{ temporary_task_removed=$receipt.temporary_task_removed; binaries_removed=$receipt.binaries_removed; bytes_removed=$receipt.bytes_removed; production_runtime_preserved=$true } | ConvertTo-Json -Compress
