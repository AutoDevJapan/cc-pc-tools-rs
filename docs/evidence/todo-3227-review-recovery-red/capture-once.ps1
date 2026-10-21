$ErrorActionPreference = 'Stop'
$evidencePath = $PSScriptRoot
$workspacePath = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '../../..')).Path
$fixedSource = '4b2cf1da5321600fe5443c272191de9e7372edea'
$ownedPath = 'C:\Users\makhmeto\AppData\Local\Temp\codex-owned-0611463fdafba59587d653031ed6fedb'
$targetPath = Join-Path $ownedPath 'target'
$tempPath = Join-Path $ownedPath 'tmp'
$gatePath = 'D:/CC/Tools/codex-jobs/cargo_gate.exe'
$realCargoPath = 'C:/Users/makhmeto/.cargo/bin/cargo.exe'
$filter = 'test(review_model_recovery_excludes_the_failed_model_only_for_current_routing) | test(review_after_writer_exit_uses_durable_model_history)'
$arguments = @('nextest','run','--locked','-p','cc-jobs','--test','parity','-E',$filter,'--no-fail-fast','--status-level','all')
$actualHead = (& git -C $workspacePath rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $actualHead -ne $fixedSource) { throw 'Fixed source mismatch; no test started.' }
& git -C $workspacePath diff --quiet $fixedSource -- . ':!docs/evidence/todo-3227-review-recovery-red'
if ($LASTEXITCODE -ne 0) { throw 'Source differs from fixed commit; no test started.' }
if ($env:CC_JOB_ID -ne '20261021-094050-todo-3227') { throw 'Job mismatch; no test started.' }
if (-not (Test-Path -LiteralPath $ownedPath -PathType Container)) { throw 'Owned target root missing.' }
$intent = [ordered]@{todo_id=3227;job=$env:CC_JOB_ID;intent_at=[DateTimeOffset]::Now.ToString('o');source_commit=$actualHead;attempt=1;argv=$arguments}
$intentFile = [IO.File]::Open((Join-Path $evidencePath 'run-intent.json'),[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::Read)
try { $bytes=[Text.Encoding]::UTF8.GetBytes(($intent|ConvertTo-Json -Depth 6)); $intentFile.Write($bytes,0,$bytes.Length) } finally { $intentFile.Dispose() }
[IO.Directory]::CreateDirectory($targetPath) | Out-Null
[IO.Directory]::CreateDirectory($tempPath) | Out-Null
$settings = [Diagnostics.ProcessStartInfo]::new()
$settings.FileName = $gatePath
$settings.WorkingDirectory = $workspacePath
$settings.UseShellExecute = $false
$settings.CreateNoWindow = $true
$settings.RedirectStandardOutput = $true
$settings.RedirectStandardError = $true
$settings.RedirectStandardInput = $true
foreach ($argument in $arguments) { $settings.ArgumentList.Add($argument) }
$settings.Environment['CODEX_JOBS_REAL_CARGO'] = $realCargoPath
$settings.Environment['CODEX_JOBS_CARGO_LOCK_DIR'] = 'D:/CC/Tools/codex-jobs'
$settings.Environment['CARGO_TARGET_DIR'] = $targetPath
$settings.Environment['CARGO_BUILD_JOBS'] = '10'
$settings.Environment['RUST_TEST_THREADS'] = '10'
$settings.Environment['NEXTEST_TEST_THREADS'] = '10'
$settings.Environment['RUSTC_WRAPPER'] = 'D:/CC/Tools/sccache/sccache.exe'
$settings.Environment['SCCACHE_DIR'] = 'D:/CC/BuildCache/sccache'
$settings.Environment['SCCACHE_CACHE_SIZE'] = '30G'
$settings.Environment['SCCACHE_SERVER_PORT'] = '42259'
$settings.Environment['TEMP'] = $tempPath
$settings.Environment['TMP'] = $tempPath
$metadata = [ordered]@{
  todo_id=3227;parent_todo=3081;issue=277;job=$env:CC_JOB_ID;attempt=1
  source_commit=$actualHead;product_base='027b7ecce88cff154bcd617aa0e7f24be6ac1d3f'
  source_ref='scheduler/todo-3081-review-recovery-red';branch=(& git -C $workspacePath branch --show-current).Trim()
  workspace=$workspacePath;resources_assigned=@('cargo');process_launcher=$gatePath;real_cargo=$realCargoPath;argv=$arguments
  command="cargo nextest run --locked -p cc-jobs --test parity -E '$filter' --no-fail-fast --status-level all"
  create_no_window=$true;stdout='stdout.txt';stderr='stderr.txt';capture='Concurrent raw BaseStream copies, no output pipeline or encoding conversion'
  target=$targetPath;owned_temp_id='0611463fdafba59587d653031ed6fedb';temp=$tempPath
  requested_build_jobs=10;requested_test_threads=10;shared_gate_config=([IO.File]::ReadAllText('D:/CC/Tools/codex-jobs/cargo-gate.json')|ConvertFrom-Json -AsHashtable)
  sccache='D:/CC/Tools/sccache/sccache.exe';sccache_dir='D:/CC/BuildCache/sccache';sccache_cache_size='30G';sccache_port=42259
  started_at=[DateTimeOffset]::Now.ToString('o');completed_at=$null;gate_pid=$null;gate_created_at=$null;exit_code=$null;status='launching'
}
$metadataPath = Join-Path $evidencePath 'run-metadata.json'
$metadata | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $metadataPath -Encoding utf8
$outFile = [IO.File]::Open((Join-Path $evidencePath 'stdout.txt'),[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::Read)
$errFile = [IO.File]::Open((Join-Path $evidencePath 'stderr.txt'),[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::Read)
$process = [Diagnostics.Process]::new()
$process.StartInfo = $settings
try {
  if (-not $process.Start()) { throw 'Cargo gate failed to start.' }
  $outCopy = $process.StandardOutput.BaseStream.CopyToAsync($outFile)
  $errCopy = $process.StandardError.BaseStream.CopyToAsync($errFile)
  $process.StandardInput.Close()
  $metadata.gate_pid = $process.Id
  $metadata.gate_created_at = $process.StartTime.ToUniversalTime().ToString('o')
  $metadata.status = 'running'
  $metadata | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $metadataPath -Encoding utf8
  $process.WaitForExit()
  $childExit = $process.ExitCode
  [Threading.Tasks.Task]::WhenAll([Threading.Tasks.Task[]]@($outCopy,$errCopy)).GetAwaiter().GetResult()
  $outFile.Flush($true)
  $errFile.Flush($true)
  $metadata.exit_code = $childExit
  $metadata.completed_at = [DateTimeOffset]::Now.ToString('o')
  $metadata.status = 'completed'
} finally {
  $outFile.Dispose()
  $errFile.Dispose()
  $process.Dispose()
  $metadata | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $metadataPath -Encoding utf8
}
$hashes = foreach ($name in @('stdout.txt','stderr.txt')) {
  $file = Get-Item -LiteralPath (Join-Path $evidencePath $name)
  [ordered]@{file=$name;bytes=$file.Length;sha256=(Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()}
}
$hashes | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $evidencePath 'raw-log-hashes.json') -Encoding utf8
[ordered]@{exit_code=$metadata.exit_code;status=$metadata.status;started_at=$metadata.started_at;completed_at=$metadata.completed_at;hashes=$hashes} | ConvertTo-Json -Depth 5
