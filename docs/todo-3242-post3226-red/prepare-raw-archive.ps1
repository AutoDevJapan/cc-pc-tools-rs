# One-off preparation: read committed blobs through Git's binary batch protocol.
# Git archive honored core.autocrlf=true, so its first extraction was rejected.
# No Cargo invocation has occurred; retain that original tar and its SHA256.
$ErrorActionPreference = 'Stop'
$evidence = $PSScriptRoot
if (Test-Path -LiteralPath (Join-Path $evidence 'run-start.json')) { throw 'Measurement started: never change source' }
$preflight = Get-Content -LiteralPath (Join-Path $evidence 'preflight.json') -Raw | ConvertFrom-Json
$entries = Get-Content -LiteralPath (Join-Path $evidence 'git-tree.json') -Raw | ConvertFrom-Json
$source = Join-Path $preflight.owned_temp.path 'source'
$archive = Join-Path $preflight.owned_temp.path 'fixed-raw-source.tar'
$psi = [Diagnostics.ProcessStartInfo]::new()
$psi.FileName = 'C:/Program Files/Git/cmd/git.exe'
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true
$psi.RedirectStandardInput = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
foreach ($argument in @('-C', $preflight.workspace, 'cat-file', '--batch')) { $psi.ArgumentList.Add($argument) }
$process = [Diagnostics.Process]::new()
$process.StartInfo = $psi
$tarStream = [IO.File]::Open($archive, [IO.FileMode]::CreateNew)
$writer = [System.Formats.Tar.TarWriter]::new($tarStream, [System.Formats.Tar.TarEntryFormat]::Pax, $true)
$conversions = [Collections.Generic.List[object]]::new()
try {
    if (-not $process.Start()) { throw 'Git binary batch reader failed to start' }
    $errorTask = $process.StandardError.ReadToEndAsync()
    $process.StandardInput.AutoFlush = $true
    $inputBytes = $process.StandardOutput.BaseStream
    foreach ($entry in $entries) {
        $process.StandardInput.WriteLine($entry.blob)
        $header = [Text.StringBuilder]::new()
        while (($next = $inputBytes.ReadByte()) -ne 10) {
            if ($next -lt 0) { throw 'Unexpected end of Git blob header' }
            [void]$header.Append([char]$next)
        }
        if ($header.ToString() -cne "$($entry.blob) blob $($entry.bytes)") { throw 'Git binary batch header differs' }
        $bytes = [byte[]]::new($entry.bytes)
        $inputBytes.ReadExactly($bytes, 0, $bytes.Length)
        if ($inputBytes.ReadByte() -ne 10) { throw 'Missing Git binary batch delimiter' }
        $originalPath = Join-Path $source $entry.path
        $rawSha = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes)).ToLowerInvariant()
        $oldSha = (Get-FileHash -LiteralPath $originalPath -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($oldSha -cne $rawSha) {
            $conversions.Add([ordered]@{path=$entry.path;git_blob=$entry.blob;converted_bytes=(Get-Item -LiteralPath $originalPath).Length;raw_bytes=$bytes.LongLength;converted_sha256=$oldSha;raw_sha256=$rawSha})
        }
        $tarEntry = [System.Formats.Tar.PaxTarEntry]::new([System.Formats.Tar.TarEntryType]::RegularFile, $entry.path)
        $tarEntry.Mode = [IO.UnixFileMode]$(if ($entry.mode -eq '100755') {493} else {420})
        $data = [IO.MemoryStream]::new($bytes, $false)
        try { $tarEntry.DataStream = $data; $writer.WriteEntry($tarEntry) } finally { $data.Dispose() }
    }
    $process.StandardInput.Close()
    $process.WaitForExit()
    $errors = $errorTask.GetAwaiter().GetResult()
    if ($process.ExitCode -ne 0) { throw "Git binary batch failed: $errors" }
} finally { $writer.Dispose(); $tarStream.Dispose(); $process.Dispose() }
$conversions | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $evidence 'archive-line-ending-conversions.json') -Encoding utf8
$extract = [Diagnostics.ProcessStartInfo]::new()
$extract.FileName = 'C:/Windows/System32/tar.exe'
$extract.UseShellExecute = $false
$extract.CreateNoWindow = $true
$extract.RedirectStandardOutput = $true
$extract.RedirectStandardError = $true
foreach ($argument in @('-xf', $archive, '-C', $source)) { $extract.ArgumentList.Add($argument) }
$extractProcess = [Diagnostics.Process]::new()
$extractProcess.StartInfo = $extract
try {
    if (-not $extractProcess.Start()) { throw 'Raw archive extractor failed to start' }
    $outTask = $extractProcess.StandardOutput.ReadToEndAsync()
    $errTask = $extractProcess.StandardError.ReadToEndAsync()
    $extractProcess.WaitForExit()
    $null = $outTask.GetAwaiter().GetResult()
    $errors = $errTask.GetAwaiter().GetResult()
    if ($extractProcess.ExitCode -ne 0) { throw "Raw archive extraction failed: $errors" }
} finally { $extractProcess.Dispose() }
[ordered]@{
    source_commit=$preflight.source_commit;created_at=(Get-Date).ToString('o');archive=$archive
    archive_sha256=(Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    archive_bytes=(Get-Item -LiteralPath $archive).Length;files=$entries.Count;converted_files_detected=$conversions.Count
    method='git cat-file --batch raw blob bytes from the shared object database, written as a raw PAX tar with System.Formats.Tar, then extracted; no checkout/clean/smudge/EOL conversion'
    initial_preflight_error='Size differs: .config/nextest.toml; no Cargo started; initial git archive retained in owned temp with archive.json SHA256'
    extracted=$true;requires_all_blob_verification_before_run=$true
} | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $evidence 'raw-archive.json') -Encoding utf8
Write-Output "Raw archive prepared: $($entries.Count) files; initial EOL conversions detected in $($conversions.Count) files. Full raw-blob verification is still required."
