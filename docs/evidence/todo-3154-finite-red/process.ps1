$ErrorActionPreference = 'Stop'

# One-off receipt helper for ToDo3154. No shell, no console window.
function Invoke-Hidden {
    param(
        [Parameter(Mandatory)][string]$File,
        [string[]]$ArgList = @(),
        [string]$WorkingDirectory = 'D:/CC/Work/unattended-operations-20261013-wt/todo-3154-3081',
        [string]$LogStem,
        [hashtable]$EnvironmentOverrides = @{}
    )
    $si = [Diagnostics.ProcessStartInfo]::new($File)
    $si.UseShellExecute = $false
    $si.CreateNoWindow = $true
    $si.RedirectStandardOutput = $true
    $si.RedirectStandardError = $true
    $si.RedirectStandardInput = $true
    $si.WorkingDirectory = $WorkingDirectory
    foreach ($value in $ArgList) { $si.ArgumentList.Add($value) }
    foreach ($key in $EnvironmentOverrides.Keys) { $si.Environment[$key] = $EnvironmentOverrides[$key] }
    $p = [Diagnostics.Process]::new()
    $p.StartInfo = $si
    $started = [DateTimeOffset]::Now.ToString('o')
    [void]$p.Start()
    $pidStarted = $p.Id
    $processCreated = $p.StartTime.ToUniversalTime().ToString('o')
    $p.StandardInput.Close()
    if ($LogStem) {
        [ordered]@{status='running'; pid=$pidStarted; process_created_utc=$processCreated; executable=$File; arguments=$ArgList} |
            ConvertTo-Json -Depth 5 | Set-Content -LiteralPath "$LogStem.process.json" -Encoding utf8NoBOM
        $stdoutFile = [IO.File]::Open("$LogStem.stdout.log", [IO.FileMode]::Create, [IO.FileAccess]::Write, [IO.FileShare]::Read)
        $stderrFile = [IO.File]::Open("$LogStem.stderr.log", [IO.FileMode]::Create, [IO.FileAccess]::Write, [IO.FileShare]::Read)
        try {
            $stdoutTask = $p.StandardOutput.BaseStream.CopyToAsync($stdoutFile)
            $stderrTask = $p.StandardError.BaseStream.CopyToAsync($stderrFile)
            $p.WaitForExit()
            [Threading.Tasks.Task]::WhenAll([Threading.Tasks.Task[]]@($stdoutTask, $stderrTask)).GetAwaiter().GetResult()
        } finally {
            $stdoutFile.Dispose()
            $stderrFile.Dispose()
        }
        $stdout = $null
        $stderr = $null
    } else {
        $stdoutTask = $p.StandardOutput.ReadToEndAsync()
        $stderrTask = $p.StandardError.ReadToEndAsync()
        $p.WaitForExit()
        $stdout = $stdoutTask.GetAwaiter().GetResult()
        $stderr = $stderrTask.GetAwaiter().GetResult()
    }
    $result = [pscustomobject]@{
        executable = $File; arguments = $ArgList; working_directory = $WorkingDirectory
        started_at = $started; finished_at = [DateTimeOffset]::Now.ToString('o')
        pid = $pidStarted; process_created_utc = $processCreated; exit_code = $p.ExitCode
        create_no_window = $true; concurrent_stdout_stderr = $true; waited_for_exit = $true
        stdout = $stdout; stderr = $stderr
    }
    $p.Dispose()
    if ($LogStem) {
        [ordered]@{status='exited'; result=$result} | ConvertTo-Json -Depth 8 |
            Set-Content -LiteralPath "$LogStem.process.json" -Encoding utf8NoBOM
    }
    return $result
}

function Invoke-FiniteRed {
    param([string]$Name, [string]$ExpectedHead, [string[]]$CargoArguments)
    $root = 'D:/CC/Work/unattended-operations-20261013-wt/todo-3154-3081'
    $owned = "$root/codex-owned-b4e5f2d379be26397ee60a13f2b53458"
    $git = @(Get-Command git.exe -CommandType Application)[0].Source
    $headResult = Invoke-Hidden $git @('rev-parse', 'HEAD')
    if ($headResult.exit_code -ne 0 -or $headResult.stdout.Trim() -ne $ExpectedHead) { throw 'Fixed HEAD mismatch' }
    $branchResult = Invoke-Hidden $git @('branch', '--show-current')
    $diff = Invoke-Hidden $git @('diff', '--exit-code', 'HEAD', '--', 'Cargo.toml', 'Cargo.lock', '.config', 'cc-job-policy', 'cc-jobs')
    if ($diff.exit_code -ne 0) { throw 'Tested source is modified' }
    $stem = "$owned/$Name"
    $environment = @{
        CARGO_TARGET_DIR = "$owned/target"
        CARGO_BUILD_JOBS = '8'; NEXTEST_TEST_THREADS = '8'; RUST_TEST_THREADS = '8'
        RUSTC_WRAPPER = 'D:/CC/Tools/sccache/sccache.exe'
        SCCACHE_DIR = 'D:/CC/BuildCache/sccache'; SCCACHE_CACHE_SIZE = '30G'; SCCACHE_SERVER_PORT = '42259'
        CARGO_TERM_COLOR = 'never'; NO_COLOR = '1'
    }
    $receipt = [ordered]@{
        todo_id = 3154; parent_todo_id = 3081; job = $env:CC_JOB_ID
        status = 'running'; actual_head = $headResult.stdout.Trim(); branch = $branchResult.stdout.Trim()
        gate = 'D:/CC/Tools/codex-jobs/cargo_gate.exe'; cargo_arguments = $CargoArguments
        environment_overrides = $environment; stdout_file = "$Name.stdout.log"; stderr_file = "$Name.stderr.log"
    }
    $receipt | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath "$stem.json" -Encoding utf8NoBOM
    $result = Invoke-Hidden -File $receipt.gate -ArgList $CargoArguments -LogStem $stem -EnvironmentOverrides $environment
    $receipt.status = 'exited'
    $receipt.result = $result
    $receipt.stdout_sha256 = (Get-FileHash -LiteralPath "$stem.stdout.log" -Algorithm SHA256).Hash.ToLowerInvariant()
    $receipt.stderr_sha256 = (Get-FileHash -LiteralPath "$stem.stderr.log" -Algorithm SHA256).Hash.ToLowerInvariant()
    $receipt | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath "$stem.json" -Encoding utf8NoBOM
    $receipt | ConvertTo-Json -Depth 10
}
