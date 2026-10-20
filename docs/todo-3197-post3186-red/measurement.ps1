param([ValidateSet('prepare','01','02')][string]$Stage)
$ErrorActionPreference = 'Stop'
$Evidence = $PSScriptRoot
$Workspace = [IO.Path]::GetFullPath((Join-Path $Evidence '../..'))
$Owned = Join-Path $Workspace 'codex-owned-8daae585cac606ae8a496ca2af6b9169'
$Source = Join-Path $Owned 'source-raw'
$Fixed = 'f85630d54888e0030692d6a7823cb8d6d99154fe'
$Git = 'C:/Program Files/Git/cmd/git.exe'
$Utf8 = [Text.UTF8Encoding]::new($false)
function Write-Json($Path, $Value) {
    [IO.File]::WriteAllText($Path, ($Value | ConvertTo-Json -Depth 30) + "`n", $Utf8)
}
function Process-Info([string]$Exe, [string[]]$Arguments, [string]$Cwd) {
    $info = [Diagnostics.ProcessStartInfo]::new()
    $info.FileName = $Exe
    $info.WorkingDirectory = $Cwd
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    foreach ($arg in $Arguments) { $info.ArgumentList.Add($arg) }
    return $info
}
function Git-Text([string[]]$GitArguments) {
    $p = [Diagnostics.Process]::new()
    $p.StartInfo = Process-Info $Git $GitArguments $Workspace
    [void]$p.Start()
    $outTask = $p.StandardOutput.ReadToEndAsync()
    $errTask = $p.StandardError.ReadToEndAsync()
    $p.WaitForExit()
    $stdout = $outTask.GetAwaiter().GetResult()
    $stderr = $errTask.GetAwaiter().GetResult()
    if ($p.ExitCode -ne 0) { throw "git failed ($($p.ExitCode)): $stderr" }
    $p.Dispose()
    return $stdout
}
function Hash-Bytes([byte[]]$Bytes) {
    $hash = [Security.Cryptography.SHA256]::Create()
    try { return [Convert]::ToHexString($hash.ComputeHash($Bytes)).ToLowerInvariant() }
    finally { $hash.Dispose() }
}
function Read-Exact([IO.Stream]$Stream, [int]$Length) {
    $buffer = [byte[]]::new($Length)
    $offset = 0
    while ($offset -lt $Length) {
        $n = $Stream.Read($buffer, $offset, $Length - $offset)
        if ($n -le 0) { throw 'Unexpected EOF in git cat-file --batch' }
        $offset += $n
    }
    return ,$buffer
}
function Check-Source([string]$Label) {
    $manifest = Get-Content -LiteralPath (Join-Path $Evidence 'git-blob-sha256.json') -Raw | ConvertFrom-Json
    $actual = @(Get-ChildItem -LiteralPath $Source -File -Recurse -Force)
    $links = @(Get-ChildItem -LiteralPath $Source -Recurse -Force | Where-Object { $_.Attributes -band [IO.FileAttributes]::ReparsePoint })
    $mismatches = [Collections.Generic.List[string]]::new()
    $rows = foreach ($entry in $manifest) {
        $path = Join-Path $Source $entry.path
        $hash = if (Test-Path -LiteralPath $path -PathType Leaf) { (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() } else { 'missing' }
        if ($hash -ne $entry.sha256) { $mismatches.Add($entry.path) }
        [ordered]@{path=$entry.path;sha256=$hash}
    }
    Write-Json (Join-Path $Evidence "source-$Label.sha256.json") @($rows)
    $result = [ordered]@{
        label=$Label;at=[DateTimeOffset]::Now.ToString('o');fixed_head=$Fixed
        expected_files=@($manifest).Count;actual_files=$actual.Count;reparse_points=$links.Count
        mismatches=@($mismatches);all_git_blob_bytes_match=($mismatches.Count -eq 0 -and $actual.Count -eq @($manifest).Count -and $links.Count -eq 0)
    }
    Write-Json (Join-Path $Evidence "source-$Label.check.json") $result
    if (-not $result.all_git_blob_bytes_match) { throw "Source mismatch at $Label" }
    return $result
}
if ($Stage -eq 'prepare') {
    if (Test-Path -LiteralPath $Source) { throw 'Source already exists; inspect the saved receipts instead of overwriting.' }
    [void](New-Item -ItemType Directory -Path $Source)
    foreach ($directory in @('target','tmp')) {
        if (-not (Test-Path -LiteralPath (Join-Path $Owned $directory))) { [void](New-Item -ItemType Directory -Path (Join-Path $Owned $directory)) }
    }
    $archive = Join-Path $Owned 'fixed-source-raw.tar'
    $exportGit = Join-Path $Owned 'raw-export.git'
    [void](Git-Text @('init','--bare',$exportGit))
    $objects = (Git-Text @('rev-parse','--path-format=absolute','--git-path','objects')).Trim()
    [IO.File]::WriteAllText((Join-Path $exportGit 'objects/info/alternates'), $objects + "`n", $Utf8)
    [IO.File]::WriteAllText((Join-Path $exportGit 'info/attributes'), "* -text -filter -ident -working-tree-encoding`n", $Utf8)
    [void](Git-Text @("--git-dir=$exportGit",'-c','core.autocrlf=false','-c','core.eol=lf','archive','--format=tar',"--output=$archive",$Fixed))
    $tree = Git-Text @('ls-tree','-r','-z','--full-tree',$Fixed)
    $records = @($tree.Split([char]0, [StringSplitOptions]::RemoveEmptyEntries))
    $entries = foreach ($record in $records) {
        if ($record -notmatch '^(100644|100755) blob ([0-9a-f]{40})\t(.+)$') { throw "Nonregular tree entry: $record" }
        [ordered]@{mode=$Matches[1];blob=$Matches[2];path=$Matches[3]}
    }
    [IO.File]::WriteAllText((Join-Path $Evidence 'git-ls-tree.txt'), ($records -join "`n") + "`n", $Utf8)
    $extract = [Diagnostics.Process]::new()
    $extract.StartInfo = Process-Info 'C:/Windows/System32/tar.exe' @('-xf',$archive,'-C',$Source) $Owned
    [void]$extract.Start()
    $extractOut = $extract.StandardOutput.ReadToEndAsync()
    $extractErr = $extract.StandardError.ReadToEndAsync()
    $extract.WaitForExit()
    [IO.File]::WriteAllText((Join-Path $Evidence 'archive-extract.stdout.log'),$extractOut.GetAwaiter().GetResult(),$Utf8)
    [IO.File]::WriteAllText((Join-Path $Evidence 'archive-extract.stderr.log'),$extractErr.GetAwaiter().GetResult(),$Utf8)
    if ($extract.ExitCode -ne 0) { throw "tar failed ($($extract.ExitCode))" }
    $extract.Dispose()
    $batch = [Diagnostics.Process]::new()
    $batch.StartInfo = Process-Info $Git @('cat-file','--batch') $Workspace
    $batch.StartInfo.RedirectStandardInput = $true
    [void]$batch.Start()
    $batchErr = $batch.StandardError.ReadToEndAsync()
    $stream = $batch.StandardOutput.BaseStream
    $rows = foreach ($entry in $entries) {
        $batch.StandardInput.WriteLine($entry.blob)
        $batch.StandardInput.Flush()
        $header = [Collections.Generic.List[byte]]::new()
        while (($b=$stream.ReadByte()) -ne 10) {
            if ($b -lt 0) { throw 'Unexpected EOF in cat-file header' }
            $header.Add([byte]$b)
        }
        $text = [Text.Encoding]::ASCII.GetString($header.ToArray())
        if ($text -notmatch '^([0-9a-f]{40}) blob ([0-9]+)$' -or $Matches[1] -ne $entry.blob) { throw "Bad cat-file header: $text" }
        $length = [int]$Matches[2]
        $bytes = Read-Exact $stream $length
        if ($stream.ReadByte() -ne 10) { throw 'Missing cat-file separator' }
        [ordered]@{path=$entry.path;mode=$entry.mode;blob=$entry.blob;bytes=$length;sha256=(Hash-Bytes $bytes)}
    }
    $batch.StandardInput.Close()
    $batch.WaitForExit()
    $batchError = $batchErr.GetAwaiter().GetResult()
    if ($batch.ExitCode -ne 0) { throw "cat-file failed: $batchError" }
    $batch.Dispose()
    Write-Json (Join-Path $Evidence 'git-blob-sha256.json') @($rows)
    $archiveHash = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    $proof = Check-Source 'initial-raw'
    $selected = @('cc-job-policy/tests/product_identity.rs','localai-web-rs/src/operations_product_identity_tests.rs')
    $lineEndings = foreach ($relative in $selected) {
        $body = [IO.File]::ReadAllText((Join-Path $Source $relative))
        [ordered]@{path=$relative;crlf_count=[regex]::Matches($body,"`r`n").Count;lf_count=[regex]::Matches($body,"(?<!`r)`n").Count;archive_matches_raw_blob=$true}
    }
    Write-Json (Join-Path $Evidence 'preparation.json') ([ordered]@{
        at=[DateTimeOffset]::Now.ToString('o');fixed_head=$Fixed;archive_sha256=$archiveHash
        archive_bytes=(Get-Item -LiteralPath $archive).Length;source=$Source;target=(Join-Path $Owned 'target')
        ownership_id='8daae585cac606ae8a496ca2af6b9169';files=@($rows).Count
        checkout_core_autocrlf=(Git-Text @('config','--get','core.autocrlf')).Trim()
        raw_export='isolated bare Git directory, read-only object alternates; info/attributes disables text/filter/ident/encoding transforms; git archive followed by tar'
        blob_method='git ls-tree -r -z; git cat-file --batch raw payload bytes; SHA256 for every blob'
        selected_file_line_endings=@($lineEndings);initial_check=$proof
    })
    $proof | ConvertTo-Json
    exit 0
}
$prefix = Join-Path $Evidence "run-$Stage"
if (Test-Path -LiteralPath "$prefix.json") { throw "Run $Stage already has a receipt; never repeat it automatically." }
$proof = Check-Source "before-$Stage"
$arguments = if ($Stage -eq '01') {
    @('nextest','run','--locked','-p','cc-job-policy','--test','product_identity','-E','test(mcp_business_jobs_preserve_dependency_urgency_without_borrowing_history_priority)','--no-fail-fast')
} else {
    @('nextest','run','--locked','-p','localai-web-rs','--lib','-E','test(mcp_job_business_identity_keeps_the_more_urgent_product_or_dependency_rank) | test(mcp_raw_private_project_cannot_be_unmasked_by_a_display_override) | test(mcp_private_job_priority_keeps_its_linked_task_details_closed) | test(mcp_private_names_in_related_ids_hide_both_task_and_job_details) | test(mcp_api_reports_the_presence_of_runtime_binding_overrides)','--no-fail-fast')
}
$gate = 'D:/CC/Tools/codex-jobs/cargo_gate.exe'
$p = [Diagnostics.Process]::new()
$p.StartInfo = Process-Info $gate $arguments $Source
$p.StartInfo.Environment['CARGO_TARGET_DIR'] = Join-Path $Owned 'target'
$p.StartInfo.Environment['RUSTC_WRAPPER'] = 'D:/CC/Tools/sccache/sccache.exe'
$p.StartInfo.Environment['SCCACHE_DIR'] = 'D:/CC/BuildCache/sccache'
$p.StartInfo.Environment['SCCACHE_CACHE_SIZE'] = '30G'
$p.StartInfo.Environment['CARGO_BUILD_JOBS'] = '10'
$p.StartInfo.Environment['NEXTEST_TEST_THREADS'] = '8'
$p.StartInfo.Environment['TEMP'] = Join-Path $Owned 'tmp'
$p.StartInfo.Environment['TMP'] = Join-Path $Owned 'tmp'
$meta = [ordered]@{
    id=3197;job=$env:CC_JOB_ID;run=$Stage;fixed_head=$Fixed
    executable=$gate;arguments=$arguments;working_directory=$Source;target=(Join-Path $Owned 'target')
    create_no_window=$true;started_at=[DateTimeOffset]::Now.ToString('o');state='launch-intent'
    source_before=$proof;stdout_file="run-$Stage.stdout.log";stderr_file="run-$Stage.stderr.log"
}
Write-Json "$prefix.json" $meta
$stdout = [IO.File]::Create("$prefix.stdout.log")
$stderr = [IO.File]::Create("$prefix.stderr.log")
try {
    [void]$p.Start()
    $meta.pid=$p.Id
    $meta.process_started_at=$p.StartTime.ToUniversalTime().ToString('o')
    $meta.state='running-or-gate-waiting'
    Write-Json "$prefix.json" $meta
    $outTask=$p.StandardOutput.BaseStream.CopyToAsync($stdout)
    $errTask=$p.StandardError.BaseStream.CopyToAsync($stderr)
    $p.WaitForExit()
    $outTask.GetAwaiter().GetResult()
    $errTask.GetAwaiter().GetResult()
    $meta.ended_at=[DateTimeOffset]::Now.ToString('o')
    $meta.exit_code=$p.ExitCode
    $meta.has_exited=$p.HasExited
    $meta.state='exited-and-output-drained'
} finally {
    $stdout.Dispose()
    $stderr.Dispose()
    $p.Dispose()
    Write-Json "$prefix.json" $meta
}
$meta.stdout_sha256=(Get-FileHash -LiteralPath "$prefix.stdout.log" -Algorithm SHA256).Hash.ToLowerInvariant()
$meta.stderr_sha256=(Get-FileHash -LiteralPath "$prefix.stderr.log" -Algorithm SHA256).Hash.ToLowerInvariant()
$meta.source_after=Check-Source "after-$Stage"
Write-Json "$prefix.json" $meta
$meta | ConvertTo-Json -Depth 10
exit 0
