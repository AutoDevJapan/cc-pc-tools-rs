$ErrorActionPreference = 'Stop'
$taskRoot = 'D:/CC/Work/unattended-operations-20261013-wt/todo-3269-work'
$evidenceRoot = Join-Path $taskRoot 'docs/evidence/todo-3269-frozen-request-red'
$ownedRoot = 'C:/Users/makhmeto/AppData/Local/Temp/codex-owned-d111d047a5d0d972c63580ef437f7392'
Set-Location -LiteralPath $taskRoot
$sourceHead = (git rev-parse HEAD).Trim()
if ($sourceHead -ne '519cd0cb17e6a7b9a7c4830ae98ac40208f8771a') { throw 'Fixed source does not match' }
$sourceChanges = @(git diff --name-only HEAD -- . ':!docs/evidence/todo-3269-frozen-request-red')
if ($sourceChanges.Count -ne 0) { throw 'Source was modified before the single run' }
$attemptPath = Join-Path $evidenceRoot 'attempt.json'
$attemptLock = [System.IO.File]::Open($attemptPath, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write, [System.IO.FileShare]::Read)
$intent = [ordered]@{id=3269;parent_todo=3081;issue=277;job=$env:CC_JOB_ID;source=$sourceHead;branch=(git branch --show-current).Trim();attempt=1;additional_attempts_allowed=0;requested_at=[DateTimeOffset]::Now.ToString('o');state='reserved-single-invocation'}
$intentBytes = [Text.Encoding]::UTF8.GetBytes(($intent | ConvertTo-Json))
$attemptLock.Write($intentBytes,0,$intentBytes.Length)
$attemptLock.Dispose()
New-Item -ItemType Directory -Path (Join-Path $ownedRoot 'target') | Out-Null
$gateArgs = @('nextest','run','--locked','-p','localai-web-rs','--bin','localai-web-rs','-E','test(watchdog_review_recovery_passes_scoped_routing_and_provenance_through_tick) | test(explicit_console_resume_excludes_retired_threads_and_keeps_compatible_threads)','--no-fail-fast','--status-level','all')
$startInfo = [Diagnostics.ProcessStartInfo]::new()
$startInfo.FileName = 'D:/CC/Tools/codex-jobs/cargo_gate.exe'
$startInfo.WorkingDirectory = $taskRoot
$startInfo.UseShellExecute = $false
$startInfo.CreateNoWindow = $true
$startInfo.RedirectStandardOutput = $true
$startInfo.RedirectStandardError = $true
foreach ($gateArg in $gateArgs) { $startInfo.ArgumentList.Add($gateArg) }
$startInfo.Environment['CARGO_TARGET_DIR'] = Join-Path $ownedRoot 'target'
$startInfo.Environment['CARGO_BUILD_JOBS'] = '10'
$startInfo.Environment['RUSTC_WRAPPER'] = 'D:/CC/Tools/sccache/sccache.exe'
$startInfo.Environment['SCCACHE_DIR'] = 'D:/CC/BuildCache/sccache'
$startInfo.Environment['SCCACHE_CACHE_SIZE'] = '30G'
$startInfo.Environment['SCCACHE_SERVER_PORT'] = '42259'
$startInfo.Environment['NEXTEST_TEST_THREADS'] = '8'
$startInfo.Environment['RUST_TEST_THREADS'] = '8'
$startInfo.Environment['CARGO_TERM_COLOR'] = 'never'
$metadata = [ordered]@{id=3269;parent_todo=3081;issue=277;job=$env:CC_JOB_ID;source=$sourceHead;source_ref='scheduler/todo-3081-frozen-request-red';product_baseline=(git rev-parse 072c776).Trim();source_tree=(git rev-parse 'HEAD^{tree}').Trim();branch=(git branch --show-current).Trim();logical_command="cargo nextest run --locked -p localai-web-rs --bin localai-web-rs -E 'test(watchdog_review_recovery_passes_scoped_routing_and_provenance_through_tick) | test(explicit_console_resume_excludes_retired_threads_and_keeps_compatible_threads)' --no-fail-fast --status-level all";executable=$startInfo.FileName;args=$gateArgs;cwd=$taskRoot;use_shell_execute=$false;create_no_window=$true;allocation=@('cargo');gpu_used=$false;civ6_used=$false;real_ai_requests=0;target_dir=$startInfo.Environment['CARGO_TARGET_DIR'];owned_temp_id='d111d047a5d0d972c63580ef437f7392';max_build_jobs=10;max_test_threads=8;rustc_wrapper=$startInfo.Environment['RUSTC_WRAPPER'];sccache_dir=$startInfo.Environment['SCCACHE_DIR'];sccache_cache_size='30G';sccache_server_port=42259;started_at=[DateTimeOffset]::Now.ToString('o');started_utc=[DateTimeOffset]::UtcNow.ToString('o');gate_sha256=(Get-FileHash -LiteralPath $startInfo.FileName -Algorithm SHA256).Hash;phase='starting'}
$metadataPath = Join-Path $evidenceRoot 'run.json'
$metadata | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $metadataPath -Encoding utf8
$outFile = [System.IO.File]::Open((Join-Path $evidenceRoot 'stdout.log'),[System.IO.FileMode]::CreateNew)
$errFile = [System.IO.File]::Open((Join-Path $evidenceRoot 'stderr.log'),[System.IO.FileMode]::CreateNew)
$process = [Diagnostics.Process]::new()
$process.StartInfo = $startInfo
try {
    if (-not $process.Start()) { throw 'Cargo gate did not start' }
    $metadata['pid'] = $process.Id
    $metadata['process_started_at'] = $process.StartTime.ToUniversalTime().ToString('o')
    $metadata['phase'] = 'running'
    $metadata | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $metadataPath -Encoding utf8
    $outCopy = $process.StandardOutput.BaseStream.CopyToAsync($outFile)
    $errCopy = $process.StandardError.BaseStream.CopyToAsync($errFile)
    $process.WaitForExit()
    $metadata['exit_code'] = $process.ExitCode
    $metadata['process_ended_at'] = [DateTimeOffset]::Now.ToString('o')
    $metadata['process_ended_utc'] = [DateTimeOffset]::UtcNow.ToString('o')
    [Threading.Tasks.Task]::WhenAll(@($outCopy,$errCopy)).GetAwaiter().GetResult()
    $outFile.Flush($true)
    $errFile.Flush($true)
    $metadata['phase'] = 'complete-outputs-drained'
} finally {
    $outFile.Dispose()
    $errFile.Dispose()
    $metadata['recorded_at'] = [DateTimeOffset]::Now.ToString('o')
    $metadata | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $metadataPath -Encoding utf8
    $process.Dispose()
}
(Get-FileHash -LiteralPath (Join-Path $evidenceRoot 'stdout.log'),(Join-Path $evidenceRoot 'stderr.log') -Algorithm SHA256 | Select-Object Path,Hash) | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $evidenceRoot 'log-hashes.json') -Encoding utf8
Write-Output ($metadata | ConvertTo-Json -Depth 8)