$ErrorActionPreference = 'Stop'
$evidence = $PSScriptRoot
$workspace = (Resolve-Path (Join-Path $evidence '../../..')).Path
$fixedSource = '6d7358a3b9334ad92ae3fc822e2c81b23af9f831'
$gate = 'D:/CC/Tools/codex-jobs/cargo_gate.exe'
$owned = Get-Content -LiteralPath (Join-Path $evidence 'owned-temp-receipt.json') -Raw | ConvertFrom-Json
if ($owned.project -ne 'todo3191' -or $owned.state -ne 'owned') { throw 'Unexpected temporary ownership receipt' }
if ((git -C $workspace branch --show-current) -ne 'scheduler/todo-3191-resume-red-check') { throw 'Unexpected branch' }
git -C $workspace diff --exit-code $fixedSource -- . ':(exclude)docs/evidence/todo3191-resume-red'
if ($LASTEXITCODE -ne 0) { throw 'The fixed source has changed' }
$executionHead = git -C $workspace rev-parse HEAD
$commands = @(
    @{ id = '01-policy'; args = @('nextest', 'run', '--locked', '-p', 'cc-job-policy', '--test', 'resume_policy', '--no-fail-fast', '--status-level', 'all') },
    @{ id = '02-discord'; args = @('nextest', 'run', '--locked', '-p', 'cc-ask', '--test', 'resume', '-E', 'test(every_discord_resume_launcher_applies_policy_before_passing_a_native_session)', '--no-fail-fast', '--status-level', 'all') }
)
foreach ($spec in $commands) {
    $prefix = Join-Path $evidence $spec.id
    $intentPath = $prefix + '.intent.json'
    if (Test-Path -LiteralPath $intentPath) { throw ('Already attempted: ' + $spec.id + '; inspect saved evidence, never repeat automatically') }
    $intent = [ordered]@{
        id = $spec.id; fixed_source = $fixedSource; execution_head = $executionHead
        job = $env:CC_JOB_ID; started_at = [DateTimeOffset]::Now.ToString('o')
        program = $gate; arguments = $spec.args
        target = (Join-Path $owned.path 'target'); owned_temp_id = $owned.id
        gate_sha256 = (Get-FileHash -LiteralPath $gate -Algorithm SHA256).Hash
        max_requested_build_jobs = 10; max_requested_test_threads = 10
        cargo_gate_config = (Get-Content -LiteralPath 'D:/CC/Tools/codex-jobs/cargo-gate.json' -Raw | ConvertFrom-Json)
        sccache = 'D:/CC/Tools/sccache/sccache.exe'; sccache_dir = 'D:/CC/BuildCache/sccache'
        sccache_port = '42259'; sccache_cache_size = '30G'
        stdout = $spec.id + '.stdout.log'; stderr = $spec.id + '.stderr.log'
        capture = 'Raw redirected streams; process exit code read independently of output copying'
    }
    [IO.File]::WriteAllText($intentPath, ($intent | ConvertTo-Json -Depth 12) + "`n")
    $psi = [Diagnostics.ProcessStartInfo]::new()
    $psi.FileName = $gate
    $psi.WorkingDirectory = $workspace
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.RedirectStandardInput = $true
    foreach ($arg in $spec.args) { $psi.ArgumentList.Add($arg) }
    $psi.Environment['CARGO_TARGET_DIR'] = $intent.target
    $psi.Environment['CARGO_BUILD_JOBS'] = '10'
    $psi.Environment['RUST_TEST_THREADS'] = '10'
    $psi.Environment['NEXTEST_TEST_THREADS'] = '10'
    $psi.Environment['RUSTC_WRAPPER'] = $intent.sccache
    $psi.Environment['SCCACHE_DIR'] = $intent.sccache_dir
    $psi.Environment['SCCACHE_SERVER_PORT'] = $intent.sccache_port
    $psi.Environment['SCCACHE_CACHE_SIZE'] = $intent.sccache_cache_size
    $psi.Environment['CARGO_INCREMENTAL'] = '0'
    $psi.Environment['CARGO_TERM_COLOR'] = 'never'
    $psi.Environment['NEXTEST_COLOR'] = 'never'
    $stdout = [IO.File]::Open($prefix + '.stdout.log', [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
    $stderr = [IO.File]::Open($prefix + '.stderr.log', [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $psi
    try {
        if (-not $process.Start()) { throw 'Cargo gate did not start' }
        $process.StandardInput.Close()
        $intent['gate_pid'] = $process.Id
        $intent['gate_process_started_at'] = $process.StartTime.ToUniversalTime().ToString('o')
        [IO.File]::WriteAllText($intentPath, ($intent | ConvertTo-Json -Depth 12) + "`n")
        $outCopy = $process.StandardOutput.BaseStream.CopyToAsync($stdout)
        $errCopy = $process.StandardError.BaseStream.CopyToAsync($stderr)
        Write-Output ($spec.id + ': started gate PID ' + $process.Id)
        $process.WaitForExit()
        $cargoExit = $process.ExitCode
        $outCopy.GetAwaiter().GetResult()
        $errCopy.GetAwaiter().GetResult()
        $stdout.Flush(); $stderr.Flush()
        $result = [ordered]@{
            id = $spec.id; ended_at = [DateTimeOffset]::Now.ToString('o')
            exit_code = $cargoExit; exit_code_source = 'Exited cargo_gate.exe; gate returns the real Cargo status code'
            streams_drained = $true; source_head_after = (git -C $workspace rev-parse HEAD)
        }
        [IO.File]::WriteAllText($prefix + '.result.json', ($result | ConvertTo-Json -Depth 8) + "`n")
        Write-Output ($spec.id + ': cargo exit ' + $cargoExit)
    } finally {
        $stdout.Dispose(); $stderr.Dispose(); $process.Dispose()
    }
}
