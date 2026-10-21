# The single authorized Cargo invocation. Do not run again if invocation.json exists.
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Source-Manifest.ps1')
$invocationPath = Join-Path $TaskEvidence 'invocation.json'
if (Test-Path -LiteralPath $invocationPath) { throw 'A measurement invocation is already recorded; never rerun it.' }
$before = Get-Content -LiteralPath (Join-Path $TaskEvidence 'source-before.json') -Raw -Encoding UTF8 | ConvertFrom-Json
$current = Get-TaskSourceManifest
if ($before.aggregate_sha256 -cne $current.aggregate_sha256) { throw 'Source changed after preflight.' }
$command = 'cargo nextest run --locked -p localai-web-rs --test prd_page_preservation --no-fail-fast'
$arguments = @('nextest','run','--locked','-p','localai-web-rs','--test','prd_page_preservation','--no-fail-fast')
$start = [DateTimeOffset]::Now
$info = [Diagnostics.ProcessStartInfo]::new('D:/CC/Tools/codex-jobs/cargo_gate.exe')
$info.UseShellExecute = $false
$info.CreateNoWindow = $true
$info.WorkingDirectory = $TaskSource
$info.RedirectStandardOutput = $true
$info.RedirectStandardError = $true
foreach ($argument in $arguments) { $info.ArgumentList.Add($argument) }
$info.Environment['CARGO_TARGET_DIR'] = Join-Path $TaskOwned 'target'
$info.Environment['CARGO_BUILD_JOBS'] = '10'
$info.Environment['RUST_TEST_THREADS'] = '10'
$info.Environment['RUSTC_WRAPPER'] = 'D:/CC/Tools/sccache/sccache.exe'
$info.Environment['SCCACHE_DIR'] = 'D:/CC/BuildCache/sccache'
$info.Environment['SCCACHE_CACHE_SIZE'] = '30G'
$stdoutPath = Join-Path $TaskEvidence 'stdout.txt'
$stderrPath = Join-Path $TaskEvidence 'stderr.txt'
$stdout = [IO.File]::Open($stdoutPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
$stderr = [IO.File]::Open($stderrPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
$invocation = [ordered]@{
    todo = 3219; parent_todo = 3103; job = $env:CC_JOB_ID; source_commit = $TaskCommit
    command = $command; executable = $info.FileName; argv = $arguments
    cwd = $TaskSource; target = $info.Environment['CARGO_TARGET_DIR']
    create_no_window = $true; attempt = 1; started_at = $start.ToString('o')
    pid = $null; process_started_at = $null; state = 'launch-recorded'
}
Write-TaskJson $invocationPath $invocation
try {
    $child = [Diagnostics.Process]::Start($info)
    $invocation.pid = $child.Id
    $invocation.process_started_at = $child.StartTime.ToUniversalTime().ToString('o')
    $invocation.state = 'running'
    Write-TaskJson $invocationPath $invocation
    $outCopy = $child.StandardOutput.BaseStream.CopyToAsync($stdout)
    $errCopy = $child.StandardError.BaseStream.CopyToAsync($stderr)
    $child.WaitForExit()
    $outCopy.GetAwaiter().GetResult()
    $errCopy.GetAwaiter().GetResult()
    $exitCode = $child.ExitCode
} finally { $stdout.Dispose(); $stderr.Dispose() }
$end = [DateTimeOffset]::Now
$after = Get-TaskSourceManifest
Write-TaskJson (Join-Path $TaskEvidence 'source-after.json') $after
$unchanged = $before.aggregate_sha256 -ceq $after.aggregate_sha256
$result = [ordered]@{
    todo = 3219; job = $env:CC_JOB_ID; source_commit = $TaskCommit; command = $command
    started_at = $start.ToString('o'); finished_at = $end.ToString('o')
    elapsed_seconds = ($end - $start).TotalSeconds; exit = $exitCode
    source_unchanged = $unchanged; before_sha256 = $before.aggregate_sha256; after_sha256 = $after.aggregate_sha256
    stdout_bytes = (Get-Item -LiteralPath $stdoutPath).Length
    stderr_bytes = (Get-Item -LiteralPath $stderrPath).Length
    stdout_sha256 = (Get-FileHash -LiteralPath $stdoutPath -Algorithm SHA256).Hash.ToLowerInvariant()
    stderr_sha256 = (Get-FileHash -LiteralPath $stderrPath -Algorithm SHA256).Hash.ToLowerInvariant()
}
Write-TaskJson (Join-Path $TaskEvidence 'run-result.json') $result
$invocation.state = 'exited-and-streams-drained'
Write-TaskJson $invocationPath $invocation
$result | ConvertTo-Json -Depth 4
if (!$unchanged) { throw 'Source changed during measurement; result is not valid.' }
