param([Parameter(Mandatory)][ValidateSet('before', 'after')][string]$Phase)
$ErrorActionPreference = 'Stop'
$evidence = $PSScriptRoot
$preflight = Get-Content -LiteralPath (Join-Path $evidence 'preflight.json') -Raw | ConvertFrom-Json
$workspace = $preflight.workspace
$owned = $preflight.owned_temp.path
$source = Join-Path $owned 'source'
$git = 'C:/Program Files/Git/cmd/git.exe'

function Invoke-HiddenText([string]$Executable, [string[]]$Arguments) {
    $psi = [System.Diagnostics.ProcessStartInfo]::new()
    $psi.FileName = $Executable
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.StandardOutputEncoding = [Text.Encoding]::UTF8
    foreach ($argument in $Arguments) { $psi.ArgumentList.Add($argument) }
    $process = [System.Diagnostics.Process]::new()
    $process.StartInfo = $psi
    try {
        if (-not $process.Start()) { throw 'Process did not start' }
        $outTask = $process.StandardOutput.ReadToEndAsync()
        $errTask = $process.StandardError.ReadToEndAsync()
        $process.WaitForExit()
        $outText = $outTask.GetAwaiter().GetResult()
        $errText = $errTask.GetAwaiter().GetResult()
        if ($process.ExitCode -ne 0) { throw "Child failed ($($process.ExitCode)): $errText" }
        return $outText
    } finally { $process.Dispose() }
}

function Assert-NoReparse([string]$Path) {
    $current = [IO.Path]::GetFullPath($Path)
    while ($current) {
        $item = Get-Item -LiteralPath $current -Force
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Reparse point in owned path' }
        $parent = [IO.Directory]::GetParent($current)
        $current = if ($parent) { $parent.FullName } else { $null }
    }
}

if (-not $owned.StartsWith($workspace.TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Owned temp is outside this workspace'
}
Assert-NoReparse $owned
$commit = (Invoke-HiddenText $git @('-C', $workspace, 'rev-parse', 'a1aafe7^{commit}')).Trim()
if ($commit -cne $preflight.source_commit) { throw 'Fixed commit changed' }
$tree = Invoke-HiddenText $git @('-C', $workspace, '-c', 'core.quotePath=false', 'ls-tree', '-r', '-l', '-z', $commit)
$treeHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($tree))).ToLowerInvariant()
$entries = @($tree.Split([char]0, [StringSplitOptions]::RemoveEmptyEntries) | ForEach-Object {
    if ($_ -notmatch '^(100644|100755) blob ([0-9a-f]{40}) +([0-9]+)\t(.+)$') { throw 'Unsupported Git tree entry' }
    [pscustomobject][ordered]@{mode=$Matches[1];blob=$Matches[2];bytes=[long]$Matches[3];path=$Matches[4]}
})
if ($Phase -eq 'before') {
    if (Test-Path -LiteralPath (Join-Path $evidence 'run-start.json')) { throw 'Measurement already started: never rearchive' }
    if (Test-Path -LiteralPath (Join-Path $evidence 'raw-archive.json')) {
        $raw = Get-Content -LiteralPath (Join-Path $evidence 'raw-archive.json') -Raw | ConvertFrom-Json
        if ($raw.source_commit -cne $commit -or -not $raw.extracted) { throw 'Raw archive is not ready' }
    } else {
        if (Test-Path -LiteralPath $source) { throw 'Archive source already exists: preserve it for inspection' }
        $archive = Join-Path $owned 'fixed-source.tar'
        $null = Invoke-HiddenText $git @('-C', $workspace, 'archive', '--format=tar', "--output=$archive", $commit)
        New-Item -ItemType Directory -Path $source | Out-Null
        $null = Invoke-HiddenText 'C:/Windows/System32/tar.exe' @('-xf', $archive, '-C', $source)
        [IO.File]::WriteAllText((Join-Path $evidence 'git-tree.json'), ($entries | ConvertTo-Json -Depth 4), [Text.UTF8Encoding]::new($false))
        [ordered]@{archive_sha256=(Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant();archive_bytes=(Get-Item -LiteralPath $archive).Length;source=$source;tree_sha256=$treeHash;method='Initial git archive; raw-byte verification must still pass before measurement'} |
            ConvertTo-Json | Set-Content -LiteralPath (Join-Path $evidence 'archive.json') -Encoding utf8
    }
}
Assert-NoReparse $source
$allItems = @(Get-ChildItem -LiteralPath $source -Recurse -Force)
if (@($allItems | Where-Object { $_.Attributes -band [IO.FileAttributes]::ReparsePoint }).Count) { throw 'Reparse point in archive' }
$files = @($allItems | Where-Object { -not $_.PSIsContainer })
if ($files.Count -ne $entries.Count) { throw "File count differs: archive=$($files.Count), git=$($entries.Count)" }
$manifestPath = Join-Path $evidence "source-$Phase.jsonl"
$writer = [IO.StreamWriter]::new($manifestPath, $false, [Text.UTF8Encoding]::new($false))
$totalBytes = 0L
try {
    foreach ($entry in $entries) {
        $file = Join-Path $source $entry.path
        $bytes = [IO.File]::ReadAllBytes($file)
        if ($bytes.LongLength -ne $entry.bytes) { throw "Size differs: $($entry.path)" }
        $sha1 = [Security.Cryptography.IncrementalHash]::CreateHash([Security.Cryptography.HashAlgorithmName]::SHA1)
        try {
            $sha1.AppendData([Text.Encoding]::ASCII.GetBytes("blob $($bytes.LongLength)`0"))
            $sha1.AppendData($bytes)
            $actualBlob = [Convert]::ToHexString($sha1.GetHashAndReset()).ToLowerInvariant()
        } finally { $sha1.Dispose() }
        if ($actualBlob -cne $entry.blob) { throw "Raw Git blob differs (no filters): $($entry.path)" }
        $byteText = [Text.Encoding]::Latin1.GetString($bytes)
        $crlf = [regex]::Matches($byteText, '\r\n').Count
        $record = [ordered]@{
            path=$entry.path;mode=$entry.mode;bytes=$bytes.LongLength;git_blob=$entry.blob;raw_blob=$actualBlob
            sha256=[Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes)).ToLowerInvariant()
            crlf=$crlf;lone_lf=([regex]::Matches($byteText, '\n').Count - $crlf);lone_cr=([regex]::Matches($byteText, '\r').Count - $crlf)
        }
        $writer.WriteLine(($record | ConvertTo-Json -Compress))
        $totalBytes += $bytes.LongLength
    }
} finally { $writer.Dispose() }
$receipt = [ordered]@{
    phase=$Phase;checked_at=(Get-Date).ToString('o');source_commit=$commit;source=$source
    tree_sha256=$treeHash;files=$entries.Count;bytes=$totalBytes;all_raw_blobs_match=$true
    manifest_sha256=(Get-FileHash -LiteralPath $manifestPath -Algorithm SHA256).Hash.ToLowerInvariant()
    line_endings='Every raw blob matched Git bytes, with CRLF/lone LF/lone CR counts recorded per file; core.autocrlf=true was never applied'
}
$receipt | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $evidence "verification-$Phase.json") -Encoding utf8
if ($Phase -eq 'after') {
    $before = Get-Content -LiteralPath (Join-Path $evidence 'verification-before.json') -Raw | ConvertFrom-Json
    if ($before.manifest_sha256 -cne $receipt.manifest_sha256 -or $before.tree_sha256 -cne $receipt.tree_sha256) { throw 'Whole-source pre/post manifest differs' }
    [ordered]@{source_commit=$commit;files=$entries.Count;bytes=$totalBytes;all_raw_blobs_match_before_and_after=$true;all_source_unchanged=$true;manifest_sha256=$receipt.manifest_sha256;tree_sha256=$treeHash;checked_at=(Get-Date).ToString('o')} |
        ConvertTo-Json | Set-Content -LiteralPath (Join-Path $evidence 'source-invariance.json') -Encoding utf8
}
$receipt | ConvertTo-Json -Compress
