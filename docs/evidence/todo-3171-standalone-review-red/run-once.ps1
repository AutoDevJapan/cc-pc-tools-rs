$ErrorActionPreference = 'Stop'
$evidence = $PSScriptRoot
$workspace = [IO.Path]::GetFullPath((Join-Path $evidence '../../..'))
$fixedHead = '5ba9a1c274d7a882387542d15c39fd2b2858f990'
$expectedBranch = 'scheduler/todo-3171-standalone-review-red'
$utf8 = [Text.UTF8Encoding]::new($false)

function Read-Git([string[]]$gitArgs) {
    $info = [Diagnostics.ProcessStartInfo]::new()
    $info.FileName = (Get-Command git.exe).Source
    $info.WorkingDirectory = $workspace
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    foreach ($arg in $gitArgs) { [void]$info.ArgumentList.Add($arg) }
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $info
    [void]$process.Start()
    $stdout = $process.StandardOutput.ReadToEndAsync()
    $stderr = $process.StandardError.ReadToEndAsync()
    $process.WaitForExit()
    $code = $process.ExitCode
    $value = $stdout.Result.Trim()
    $errorText = $stderr.Result
    $process.Dispose()
    if ($code -ne 0) { throw "Git inspection failed: $errorText" }
    return $value
}

$head = Read-Git @('rev-parse', 'HEAD')
$branch = Read-Git @('branch', '--show-current')
if ($head -ne $fixedHead -or $branch -ne $expectedBranch) {
    throw 'Fixed source or owned branch mismatch; no test was launched.'
}
$trackedDiff = Read-Git @('diff', '--name-only', $fixedHead)
if ($trackedDiff) { throw 'Tracked source changed; no test was launched.' }
$ownership = (Get-Content -LiteralPath (Join-Path $evidence 'target-ownership.json') -Raw -Encoding UTF8 | ConvertFrom-Json).stdout | ConvertFrom-Json
if ($ownership.state -ne 'owned' -or $ownership.project -ne 'todo-3171') {
    throw 'Dedicated target ownership mismatch; no test was launched.'
}
$target = [IO.Path]::GetFullPath((Join-Path $ownership.path 'target'))
$gate = 'D:/CC/Tools/codex-jobs/cargo_gate.exe'
$arguments = @('nextest', 'run', '--locked', '-p', 'cc-jobs', '--test', 'parity', '-E',
    'test(standalone_review_registers_without_guessing_authors_from_a_shared_cwd)',
    '--no-fail-fast', '--status-level', 'all')
$selectedEnvironment = [ordered]@{
    CARGO_TARGET_DIR = $target
    CARGO_BUILD_JOBS = '8'
    RUST_TEST_THREADS = '8'
    NEXTEST_TEST_THREADS = '8'
    RUSTC_WRAPPER = 'D:/CC/Tools/sccache/sccache.exe'
    SCCACHE_DIR = 'D:/CC/BuildCache/sccache'
    SCCACHE_CACHE_SIZE = '30G'
    SCCACHE_SERVER_PORT = '42259'
}
$metadata = [ordered]@{
    todo = 3171
    parent_todo = 3081
    job = $env:CC_JOB_ID
    issue = 'https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/277'
    head = $head
    branch = $branch
    cwd = $workspace
    executable = $gate
    arguments = $arguments
    environment = $selectedEnvironment
    target_ownership_id = $ownership.id
    started_at = [DateTimeOffset]::Now.ToString('o')
    attempt = 1
    state = 'started'
    create_no_window = $true
}
$reservation = [IO.File]::Open((Join-Path $evidence 'run-started.json'), [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
try {
    $bytes = $utf8.GetBytes(($metadata | ConvertTo-Json -Depth 8) + [Environment]::NewLine)
    $reservation.Write($bytes, 0, $bytes.Length)
    $reservation.Flush($true)
} finally { $reservation.Dispose() }

$outFile = [IO.File]::Open((Join-Path $evidence 'stdout.txt'), [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
$errFile = [IO.File]::Open((Join-Path $evidence 'stderr.txt'), [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
$process = [Diagnostics.Process]::new()
try {
    $info = [Diagnostics.ProcessStartInfo]::new()
    $info.FileName = $gate
    $info.WorkingDirectory = $workspace
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    foreach ($arg in $arguments) { [void]$info.ArgumentList.Add($arg) }
    foreach ($key in $selectedEnvironment.Keys) { $info.Environment[$key] = $selectedEnvironment[$key] }
    $process.StartInfo = $info
    [void]$process.Start()
    $metadata.process_id = $process.Id
    $outCopy = $process.StandardOutput.BaseStream.CopyToAsync($outFile)
    $errCopy = $process.StandardError.BaseStream.CopyToAsync($errFile)
    $process.WaitForExit()
    $metadata.exit_code = $process.ExitCode
    [Threading.Tasks.Task]::WaitAll([Threading.Tasks.Task[]]@($outCopy, $errCopy))
    $outFile.Flush($true)
    $errFile.Flush($true)
    $metadata.state = 'finished'
} catch {
    $metadata.state = 'runner-error'
    $metadata.runner_error = $_.Exception.Message
    throw
} finally {
    $outFile.Dispose()
    $errFile.Dispose()
    $process.Dispose()
    $metadata.finished_at = [DateTimeOffset]::Now.ToString('o')
    [IO.File]::WriteAllText((Join-Path $evidence 'run-result.json'), ($metadata | ConvertTo-Json -Depth 8) + [Environment]::NewLine, $utf8)
}
$metadata | ConvertTo-Json -Depth 8
