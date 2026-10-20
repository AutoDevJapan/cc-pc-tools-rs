param([Parameter(Mandatory)][ValidateSet('prepare','measure','verify')][string]$Phase)
$ErrorActionPreference = 'Stop'
$Repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$Owned = Join-Path $Repo 'codex-owned-8609944fc2b7cc7d9f899c69c1036804'
$Source = Join-Path $Owned 'source'
$EmptyAttributes = Join-Path $Owned 'empty-attributes'
$Baseline = 'bf60fb319626210e1cb914610e63e46c2eb34c79'
$TestSource = '4a0c61f594e600b00e0f3d03702974496aec388a'
$TestPath = 'localai-web-rs/src/operations_product_identity_tests.rs'
$TestName = 'mcp_standalone_job_keeps_all_private_links_when_its_primary_is_old_done'
$Git = 'C:/Program Files/Git/cmd/git.exe'
$Gate = 'D:/CC/Tools/codex-jobs/cargo_gate.exe'

function Save-Json($Name, $Value) {
    $Value | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath (Join-Path $PSScriptRoot $Name) -Encoding utf8NoBOM
}
function New-Process($File, [string[]]$Arguments, $Directory) {
    $p = [Diagnostics.Process]::new()
    $p.StartInfo = [Diagnostics.ProcessStartInfo]::new()
    $p.StartInfo.FileName = $File
    $p.StartInfo.WorkingDirectory = $Directory
    $p.StartInfo.UseShellExecute = $false
    $p.StartInfo.CreateNoWindow = $true
    $p.StartInfo.RedirectStandardOutput = $true
    $p.StartInfo.RedirectStandardError = $true
    foreach ($a in $Arguments) { $p.StartInfo.ArgumentList.Add($a) }
    if ($File -eq $Git -and $Arguments -contains 'archive') {
        $p.StartInfo.Environment['GIT_INDEX_FILE'] = Join-Path $Owned 'absent-archive-index'
    }
    return $p
}
function Run-Checked($File, [string[]]$Arguments, $Directory) {
    $p = New-Process $File $Arguments $Directory
    [void]$p.Start()
    $outTask = $p.StandardOutput.ReadToEndAsync()
    $errTask = $p.StandardError.ReadToEndAsync()
    $p.WaitForExit()
    $outText = $outTask.GetAwaiter().GetResult()
    $errText = $errTask.GetAwaiter().GetResult()
    if ($p.ExitCode -ne 0) { throw "Command failed ($($p.ExitCode)): $File $Arguments`n$errText" }
    $p.Dispose()
    return $outText
}
function Get-Sha256([string]$Path) { return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant() }
function Get-SourceSnapshot {
    $rows = @(Get-ChildItem -LiteralPath $Source -Recurse -File -Force | ForEach-Object {
        if ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Source contains a reparse point' }
        [pscustomobject]@{path=[IO.Path]::GetRelativePath($Source,$_.FullName).Replace('\','/');bytes=$_.Length;sha256=Get-Sha256 $_.FullName}
    } | Sort-Object path)
    return $rows
}
function Assert-Snapshot($Actual, $Expected) {
    $a = @($Actual | ForEach-Object { "$($_.path)`t$($_.bytes)`t$($_.sha256)" })
    $e = @($Expected | ForEach-Object { "$($_.path)`t$($_.bytes)`t$($_.sha256)" })
    $diff = @(Compare-Object $e $a)
    if ($diff.Count) { throw "Source manifest mismatch: $($diff | ConvertTo-Json -Compress)" }
}

if ($Phase -eq 'prepare') {
    if (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'inputs.json')) { throw 'Preparation already completed; inspect receipts instead of overwriting' }
    $listing = Run-Checked $Git @('-C',$Repo,'ls-tree','-r',$Baseline) $Repo
    $entries = @($listing -split "`n" | Where-Object { $_ } | ForEach-Object {
        if ($_ -notmatch '^100644 blob ([0-9a-f]{40})\t(.+)$') { throw 'Unexpected tree mode or entry' }
        [pscustomobject]@{path=$Matches[2].TrimEnd("`r");blob=$Matches[1]}
    })
    $testBlob = (Run-Checked $Git @('-C',$Repo,'rev-parse',"${TestSource}:$TestPath") $Repo).Trim()
    $batch = New-Process $Git @('-C',$Repo,'cat-file','--batch') $Repo
    $batch.StartInfo.RedirectStandardInput = $true
    [void]$batch.Start()
    $batchError = $batch.StandardError.ReadToEndAsync()
    $stream = $batch.StandardOutput.BaseStream
    $blobRows = @{}
    foreach ($oid in @(@($entries.blob) + $testBlob | Select-Object -Unique)) {
        $batch.StandardInput.WriteLine($oid)
        $batch.StandardInput.Flush()
        $header = [Text.StringBuilder]::new()
        while (($b = $stream.ReadByte()) -ne 10) {
            if ($b -lt 0) { throw 'Unexpected end of git batch header' }
            [void]$header.Append([char]$b)
        }
        $parts = $header.ToString().Split(' ')
        if ($parts[0] -ne $oid -or $parts[1] -ne 'blob') { throw 'Unexpected git batch response' }
        $size = [long]$parts[2]
        $remaining = $size
        $hasher = [Security.Cryptography.IncrementalHash]::CreateHash([Security.Cryptography.HashAlgorithmName]::SHA256)
        $buffer = [byte[]]::new(65536)
        while ($remaining -gt 0) {
            $n = $stream.Read($buffer,0,[int][Math]::Min($remaining,$buffer.Length))
            if ($n -le 0) { throw 'Unexpected end of git blob' }
            $hasher.AppendData($buffer,0,$n)
            $remaining -= $n
        }
        if ($stream.ReadByte() -ne 10) { throw 'Invalid git batch separator' }
        $blobRows[$oid] = [pscustomobject]@{bytes=$size;sha256=[Convert]::ToHexString($hasher.GetHashAndReset()).ToLowerInvariant()}
        $hasher.Dispose()
    }
    $batch.StandardInput.Close()
    $batch.WaitForExit()
    if ($batch.ExitCode -ne 0) { throw $batchError.GetAwaiter().GetResult() }
    $batch.Dispose()
    $expectedBaseline = @($entries | ForEach-Object {
        [pscustomobject]@{path=$_.path;blob=$_.blob;bytes=$blobRows[$_.blob].bytes;sha256=$blobRows[$_.blob].sha256}
    } | Sort-Object path)
    Save-Json 'git-baseline-sha256.json' $expectedBaseline
    [void](New-Item -ItemType Directory -Path $Source -Force)
    [void](New-Item -ItemType Directory -Path $EmptyAttributes -Force)
    # Missing work-tree attribute files fall back to the archive's tree.
    # Supply explicit conversion-free files at every tracked attribute location.
    foreach ($entry in @($entries | Where-Object { $_.path -match '(^|/)\.gitattributes$' })) {
        $attrPath = Join-Path $EmptyAttributes $entry.path
        [void](New-Item -ItemType Directory -Path ([IO.Path]::GetDirectoryName($attrPath)) -Force)
        [IO.File]::WriteAllText($attrPath,"* -text -eol -ident -filter -working-tree-encoding -export-ignore -export-subst`n",[Text.UTF8Encoding]::new($false))
    }
    $gitDir = (Run-Checked $Git @('-C',$Repo,'rev-parse','--absolute-git-dir') $Repo).Trim()
    $archive = Join-Path $Owned 'baseline.tar'
    [void](Run-Checked $Git @('-C',$EmptyAttributes,"--git-dir=$gitDir","--work-tree=$EmptyAttributes",'-c','core.autocrlf=false','-c','core.eol=lf','-c','core.attributesFile=NUL','archive','--worktree-attributes','--format=tar',"--output=$archive",$Baseline) $EmptyAttributes)
    [void](Run-Checked 'C:/Windows/System32/tar.exe' @('-xf',$archive,'-C',$Source) $Repo)
    $extracted = @(Get-SourceSnapshot)
    Save-Json 'extracted-baseline-sha256.json' $extracted
    Assert-Snapshot $extracted $expectedBaseline
    $testArchive = Join-Path $Owned 'test.tar'
    [void](Run-Checked $Git @('-C',$EmptyAttributes,"--git-dir=$gitDir","--work-tree=$EmptyAttributes",'-c','core.autocrlf=false','-c','core.eol=lf','-c','core.attributesFile=NUL','archive','--worktree-attributes','--format=tar',"--output=$testArchive",$TestSource,$TestPath) $EmptyAttributes)
    [void](Run-Checked 'C:/Windows/System32/tar.exe' @('-xf',$testArchive,'-C',$Source) $Repo)
    $expectedFinal = @($expectedBaseline | ForEach-Object {
        if ($_.path -eq $TestPath) { [pscustomobject]@{path=$TestPath;blob=$testBlob;bytes=$blobRows[$testBlob].bytes;sha256=$blobRows[$testBlob].sha256} } else { $_ }
    })
    Save-Json 'git-expected-overlaid-sha256.json' $expectedFinal
    $before = @(Get-SourceSnapshot)
    Save-Json 'source-before-sha256.json' $before
    Assert-Snapshot $before $expectedFinal
    Copy-Item -LiteralPath (Join-Path $Source $TestPath) -Destination (Join-Path $PSScriptRoot 'test-source.rs')
    Save-Json 'inputs.json' ([ordered]@{
        todo=3184;parent=3103;job=$env:CC_JOB_ID;repo_id=1406818713;repo='AutoDevJapan/cc-pc-tools-rs';oauth_login='mashfromband'
        baseline=$Baseline;test_source=$TestSource;test_path=$TestPath;test_blob=$testBlob;test_sha256=$blobRows[$testBlob].sha256
        source_file_count=$entries.Count;baseline_archive_sha256=Get-Sha256 $archive;test_archive_sha256=Get-Sha256 $testArchive
        source=$Source;target=(Join-Path $Owned 'target');owner_receipt_id='8609944fc2b7cc7d9f899c69c1036804'
        archive_options=@('-C',$EmptyAttributes,"--git-dir=$gitDir","--work-tree=$EmptyAttributes",'-c','core.autocrlf=false','-c','core.eol=lf','-c','core.attributesFile=NUL','--worktree-attributes');archive_cwd=$EmptyAttributes;archive_index=(Join-Path $Owned 'absent-archive-index');archive_attribute_override='* -text -eol -ident -filter -working-tree-encoding -export-ignore -export-subst';prepared_at=[DateTimeOffset]::Now.ToString('o')
    })
    'Prepared: baseline and one-file overlay match all Git blob SHA256 values.'
}

if ($Phase -eq 'measure') {
    if (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'run-start.json')) { throw 'This one-run measurement has already started; do not rerun' }
    $expected = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'git-expected-overlaid-sha256.json') -Raw | ConvertFrom-Json
    Assert-Snapshot @(Get-SourceSnapshot) $expected
    $arguments = @('nextest','run','--locked','-p','localai-web-rs','--lib','-E',"test($TestName)",'--no-fail-fast')
    $p = New-Process $Gate $arguments $Source
    $settings = [ordered]@{
        CARGO_TARGET_DIR=(Join-Path $Owned 'target');CARGO_BUILD_JOBS='10';RUST_TEST_THREADS='10';CARGO_INCREMENTAL='0'
        RUSTC_WRAPPER='D:/CC/Tools/sccache/sccache.exe';SCCACHE_DIR='D:/CC/BuildCache/sccache';SCCACHE_CACHE_SIZE='30G';SCCACHE_SERVER_PORT='42259'
    }
    foreach ($key in $settings.Keys) { $p.StartInfo.Environment[$key] = $settings[$key] }
    Save-Json 'run-start.json' ([ordered]@{started_at=[DateTimeOffset]::Now.ToString('o');command="cargo nextest run --locked -p localai-web-rs --lib -E 'test($TestName)' --no-fail-fast";executable=$Gate;arguments=$arguments;cwd=$Source;explicit_environment=$settings;attempt=1;create_no_window=$true})
    $stdout = [IO.File]::Open((Join-Path $PSScriptRoot 'stdout.txt'),[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::Read)
    $stderr = [IO.File]::Open((Join-Path $PSScriptRoot 'stderr.txt'),[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::Read)
    try {
        [void]$p.Start()
        Save-Json 'process.json' ([ordered]@{pid=$p.Id;started_at=$p.StartTime.ToUniversalTime().ToString('o');gate=$Gate;job=$env:CC_JOB_ID})
        $outTask = $p.StandardOutput.BaseStream.CopyToAsync($stdout)
        $errTask = $p.StandardError.BaseStream.CopyToAsync($stderr)
        $p.WaitForExit()
        $outTask.GetAwaiter().GetResult()
        $errTask.GetAwaiter().GetResult()
        Save-Json 'run-exit.json' ([ordered]@{ended_at=[DateTimeOffset]::Now.ToString('o');exit_code=$p.ExitCode;attempt=1;output_drained=$true})
        "Measurement process exited: $($p.ExitCode)"
    } finally { $stdout.Dispose();$stderr.Dispose();$p.Dispose() }
}

if ($Phase -eq 'verify') {
    if (!(Test-Path -LiteralPath (Join-Path $PSScriptRoot 'run-exit.json'))) { throw 'No completed measurement receipt' }
    $after = @(Get-SourceSnapshot)
    Save-Json 'source-after-sha256.json' $after
    $expected = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'git-expected-overlaid-sha256.json') -Raw | ConvertFrom-Json
    Assert-Snapshot $after $expected
    $before = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'source-before-sha256.json') -Raw | ConvertFrom-Json
    Assert-Snapshot $after $before
    Save-Json 'verification.json' ([ordered]@{verified_at=[DateTimeOffset]::Now.ToString('o');file_count=$after.Count;all_files_match_git_blobs=$true;source_unchanged_during_run=$true;stdout_sha256=Get-Sha256 (Join-Path $PSScriptRoot 'stdout.txt');stderr_sha256=Get-Sha256 (Join-Path $PSScriptRoot 'stderr.txt')})
    'Verified: all source files are unchanged and match the intended Git blobs.'
}
