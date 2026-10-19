#requires -Version 7.0
[CmdletBinding()]
param([Parameter(Mandatory)][string]$Launcher)

$ErrorActionPreference = 'Stop'
$installRoot = 'D:/CC/Tools/codex-progress-tui'
$npmRoot = Join-Path $env:APPDATA 'npm'
$known = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'hashes.json') -Raw -Encoding UTF8 | ConvertFrom-Json
$launcherSource = (Resolve-Path -LiteralPath $Launcher).Path
$targets = @(
    @{Source=$launcherSource; Target=(Join-Path $installRoot 'codex-launch.exe'); Name='codex-launch.exe'; Expected=$known.expected_launcher_sha256},
    @{Source=(Join-Path $PSScriptRoot 'codex.ps1'); Target=(Join-Path $npmRoot 'codex.ps1'); Name='codex.ps1'; Expected=$known.expected_ps_sha256; Prepared=$known.prepared_ps_sha256},
    @{Source=(Join-Path $PSScriptRoot 'codex.cmd'); Target=(Join-Path $npmRoot 'codex.cmd'); Name='codex.cmd'; Expected=$known.expected_cmd_sha256; Prepared=$known.prepared_cmd_sha256}
)
function Hash([string]$Path) { (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash }
function Replace([string]$Source, [string]$Target) {
    $stage = Join-Path (Split-Path -Parent $Target) ('.progress-3021-' + [guid]::NewGuid().ToString('N'))
    try {
        Copy-Item -LiteralPath $Source -Destination $stage
        [IO.File]::Move($stage, $Target, $true)
    } finally {
        if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage }
    }
}
foreach ($entry in $targets) {
    if ($entry.Expected -notmatch '^[0-9a-fA-F]{64}$') { throw 'Expected hash is missing or invalid.' }
    $entry.Before = Hash $entry.Target
    $entry.After = Hash $entry.Source
    if ($entry.Name -ne 'codex-launch.exe' -and ($entry.Prepared -notmatch '^[0-9a-fA-F]{64}$' -or $entry.After -ne $entry.Prepared)) {
        throw 'Prepared shim hash mismatch.'
    }
    if ($entry.Before -notin @($entry.Expected, $entry.After)) {
        throw "Unknown launcher shim; installation stopped before writing."
    }
}
$backup = Join-Path $installRoot ('backup-3021-' + [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfffffffZ'))
[void][IO.Directory]::CreateDirectory($backup)
foreach ($entry in $targets) {
    $entry.Backup = Join-Path $backup $entry.Name
    Copy-Item -LiteralPath $entry.Target -Destination $entry.Backup
    if ((Hash $entry.Backup) -ne $entry.Before) { throw 'Backup hash mismatch.' }
}
$changed = [Collections.Generic.List[object]]::new()
try {
    foreach ($entry in $targets) {
        if ((Hash $entry.Target) -ne $entry.Before -or (Hash $entry.Source) -ne $entry.After) { throw 'Concurrent change detected.' }
        if ($entry.Before -ne $entry.After) {
            Replace $entry.Source $entry.Target
            $changed.Add($entry)
        }
        if ((Hash $entry.Target) -ne $entry.After) { throw 'Installed hash mismatch.' }
    }
} catch {
    $original = $_
    $rollbackFailures = [Collections.Generic.List[string]]::new()
    for ($i = $changed.Count - 1; $i -ge 0; $i--) {
        $entry = $changed[$i]
        try {
            if ((Hash $entry.Target) -ne $entry.After) { throw 'Concurrent change during rollback.' }
            Replace $entry.Backup $entry.Target
            if ((Hash $entry.Target) -ne $entry.Before) { throw 'Rollback hash mismatch.' }
        } catch { $rollbackFailures.Add($entry.Name) }
    }
    if ($rollbackFailures.Count) { Write-Warning ('Rollback requires follow-up: ' + ($rollbackFailures -join ', ')) }
    throw $original
}
$receipt = [ordered]@{
    todo=3021
    installed_at=[DateTime]::UtcNow.ToString('o')
    backup=$backup
    files=@($targets | ForEach-Object { @{name=$_.Name;before=$_.Before;after=$_.After} })
    manifest_sha256=(Hash (Join-Path $installRoot 'manifest.json'))
    process_control_used=$false
}
$receipt | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $backup 'receipt.json') -Encoding utf8NoBOM
$receipt | ConvertTo-Json -Depth 5
