# One-off measurement helper. It does not modify the measured source.
$ErrorActionPreference = 'Stop'
$TaskWorkspace = 'D:/CC/Work/unattended-operations-20261013-wt/todo-3219-3103'
$TaskCommit = 'b978286abbe1a729cb733325e58a36fc063aa393'
$TaskOwned = Join-Path $TaskWorkspace 'codex-owned-530173a87be6551df0272bbcc8d02eb0'
$TaskSource = Join-Path $TaskOwned 'source-fixed'
$TaskEvidence = Join-Path $TaskWorkspace 'docs/todo-3219-prd-page-red'
$TaskGit = 'C:/Program Files/Git/cmd/git.exe'

function Write-TaskJson($Path, $Value) {
    $json = ConvertTo-Json -InputObject $Value -Depth 12
    [IO.File]::WriteAllText($Path, $json + "`n", [Text.UTF8Encoding]::new($false))
}

function Get-TaskSourceManifest {
    param([switch]$AllowGitMismatch)
    $root = [IO.Path]::GetFullPath($TaskSource)
    $all = @(Get-ChildItem -LiteralPath $root -Recurse -Force)
    if (@($all | Where-Object { $_.Attributes -band [IO.FileAttributes]::ReparsePoint }).Count) {
        throw 'The archive source contains a reparse point.'
    }
    $entries = & $TaskGit -C $TaskWorkspace -c core.quotepath=false ls-tree -r $TaskCommit
    if ($LASTEXITCODE -ne 0) { throw 'Cannot read the fixed Git tree.' }
    $rows = [Collections.Generic.List[object]]::new()
    foreach ($entry in $entries) {
        if ($entry -notmatch '^(100644|100755) blob ([0-9a-f]{40})\t(.+)$') {
            throw 'Unexpected Git tree entry; source is not a plain-file archive.'
        }
        $mode, $expected, $relative = $Matches[1], $Matches[2], $Matches[3]
        $path = [IO.Path]::GetFullPath((Join-Path $root $relative))
        if (!$path.StartsWith($root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
            throw 'An archive member escaped the dedicated source root.'
        }
        $bytes = [IO.File]::ReadAllBytes($path)
        $header = [Text.Encoding]::ASCII.GetBytes("blob $($bytes.LongLength)`0")
        $hasher = [Security.Cryptography.IncrementalHash]::CreateHash([Security.Cryptography.HashAlgorithmName]::SHA1)
        try {
            $hasher.AppendData($header)
            $hasher.AppendData($bytes)
            $actual = [Convert]::ToHexString($hasher.GetHashAndReset()).ToLowerInvariant()
        } finally { $hasher.Dispose() }
        if ($actual -cne $expected -and !$AllowGitMismatch) { throw "Raw bytes differ from fixed Git blob: $relative" }
        $rows.Add([ordered]@{
            path = $relative; mode = $mode; bytes = $bytes.LongLength
            git_blob = $expected; actual_git_blob = $actual
            sha256 = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes)).ToLowerInvariant()
        })
    }
    if (@($all | Where-Object { !$_.PSIsContainer }).Count -ne $rows.Count) {
        throw 'Unexpected extra files were found inside the source.'
    }
    $canonical = ($rows | ForEach-Object { "$($_.path)`t$($_.mode)`t$($_.bytes)`t$($_.git_blob)`t$($_.sha256)`n" }) -join ''
    return [ordered]@{
        commit = $TaskCommit; file_count = $rows.Count
        all_git_blobs_match = @($rows | Where-Object { $_.git_blob -cne $_.actual_git_blob }).Count -eq 0; extra_files = 0
        aggregate_sha256 = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($canonical))).ToLowerInvariant()
        files = $rows.ToArray()
    }
}
