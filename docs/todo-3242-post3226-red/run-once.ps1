$ErrorActionPreference = 'Stop'
$evidence = $PSScriptRoot
$preflight = Get-Content -LiteralPath (Join-Path $evidence 'preflight.json') -Raw | ConvertFrom-Json
$before = Get-Content -LiteralPath (Join-Path $evidence 'verification-before.json') -Raw | ConvertFrom-Json
if (-not $before.all_raw_blobs_match -or $before.source_commit -cne $preflight.source_commit) { throw 'Source is not verified' }
$marker = Join-Path $evidence 'run-start.json'
if (Test-Path -LiteralPath $marker) { throw 'One invocation is already recorded; never run this measurement twice' }
$arguments = @('nextest', 'run', '--locked', '-p', 'cc-job-policy', '-p', 'localai-web-rs', '-E', 'test(mcp_effective_) | test(mcp_related_review_) | test(mcp_live_requester_dependency_exclusion_controls)', '--no-fail-fast')
$psi = [Diagnostics.ProcessStartInfo]::new()
$psi.FileName = 'D:/CC/Tools/codex-jobs/cargo_gate.exe'
$psi.WorkingDirectory = $before.source
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
foreach ($argument in $arguments) { $psi.ArgumentList.Add($argument) }
$environment = [ordered]@{
    CARGO_TARGET_DIR=(Join-Path $preflight.owned_temp.path 'target')
    CARGO_BUILD_JOBS='10';NEXTEST_TEST_THREADS='10';RUST_TEST_THREADS='10'
    RUSTC_WRAPPER='D:/CC/Tools/sccache/sccache.exe';SCCACHE_DIR='D:/CC/BuildCache/sccache'
    SCCACHE_CACHE_SIZE='30G';SCCACHE_SERVER_PORT='42259'
}
foreach ($key in $environment.Keys) { $psi.Environment[$key] = $environment[$key] }
$receipt = [ordered]@{
    todo=3242;job=$env:CC_JOB_ID;source_commit=$preflight.source_commit;started_at=(Get-Date).ToString('o')
    logical_command=(Get-Content -LiteralPath (Join-Path $evidence 'command.txt') -Raw).Trim()
    executable=$psi.FileName;arguments=$arguments;cwd=$psi.WorkingDirectory;environment=$environment
    create_no_window=$true;use_shell_execute=$false;invocation_count=1;pid=$null;status='starting'
}
$stream = [IO.File]::Open($marker, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
try {
    $bytes = [Text.Encoding]::UTF8.GetBytes(($receipt | ConvertTo-Json -Depth 6))
    $stream.Write($bytes, 0, $bytes.Length)
    $stream.Flush($true)
} finally { $stream.Dispose() }
$process = [Diagnostics.Process]::new()
$process.StartInfo = $psi
$stdout = [IO.File]::Open((Join-Path $evidence 'stdout.log'), [IO.FileMode]::CreateNew)
$stderr = [IO.File]::Open((Join-Path $evidence 'stderr.log'), [IO.FileMode]::CreateNew)
try {
    if (-not $process.Start()) { throw 'Cargo gate did not start' }
    $receipt.pid = $process.Id
    $receipt.status = 'running'
    $receipt | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $marker -Encoding utf8
    $outCopy = $process.StandardOutput.BaseStream.CopyToAsync($stdout)
    $errCopy = $process.StandardError.BaseStream.CopyToAsync($stderr)
    Write-Output "Started the single Cargo measurement (gate PID $($process.Id)); full stdout/stderr stream to evidence files."
    $process.WaitForExit()
    $outCopy.GetAwaiter().GetResult()
    $errCopy.GetAwaiter().GetResult()
    $stdout.Flush($true)
    $stderr.Flush($true)
    $receipt.status = 'exited'
    $receipt['exit_code'] = $process.ExitCode
    $receipt['finished_at'] = (Get-Date).ToString('o')
    $receipt['elapsed_seconds'] = ([DateTimeOffset]::Parse($receipt.finished_at) - [DateTimeOffset]::Parse($receipt.started_at)).TotalSeconds
    $receipt | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $evidence 'run.json') -Encoding utf8
    Write-Output "Cargo exited $($process.ExitCode); source-after verification is still required."
} catch {
    $receipt.status = 'infrastructure_error'
    $receipt['error'] = $_.Exception.Message
    $receipt['finished_at'] = (Get-Date).ToString('o')
    $receipt | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $evidence 'run.json') -Encoding utf8
    throw
} finally { $stdout.Dispose(); $stderr.Dispose(); $process.Dispose() }
