param([Parameter(Mandatory)][ValidateSet('red','green','doc','clippy','release','metadata','toolchain')][string]$Stage)
$ErrorActionPreference = 'Stop'
$evidence = $PSScriptRoot
$workspace = [IO.Path]::GetFullPath((Join-Path $evidence '../..'))
$ownership = Get-Content -LiteralPath (Join-Path $evidence 'cargo-ownership.json') -Raw | ConvertFrom-Json
$target = $ownership.path
$red = 'babed84edc0048ac9100cc7489402de4bf3acf53'
$green = '490ffd47e78aa0c5f7a4c5d2f054bddd05746314'
$commands = @{
    red = @('nextest','run','--locked','-p','cc-ask','-E','test(normal_lookup_redacts_synthetic_client_names_in_every_output_branch) | test(normal_lookup_posted_but_unverified_question_is_pending_never_not_sent)')
    green = @('nextest','run','--locked','-p','cc-ask','-E','test(normal_lookup_) | binary(decision_identity) | binary(conversation_bridge)')
    doc = @('test','--locked','-p','cc-ask','--doc')
    clippy = @('clippy','--locked','-p','cc-ask','--all-targets','--','-D','warnings')
    release = @('build','--locked','--release','-p','cc-ask','--bin','discord_ask')
    metadata = @('metadata','--locked','--format-version','1','--no-deps')
    toolchain = @('--version','--verbose')
}
$head = (git -C $workspace rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0) { throw 'Cannot read HEAD' }
$expected = if ($Stage -eq 'red') { $red } else { $green }
if ($head -ne $expected) { throw "Source HEAD mismatch for $Stage" }
$tracked = @(git -C $workspace status --porcelain=v1 --untracked-files=no)
if ($LASTEXITCODE -ne 0 -or $tracked.Count -ne 0) { throw 'Tracked source is dirty' }
$metaPath = Join-Path $evidence "$Stage.result.json"
if (Test-Path -LiteralPath $metaPath) { throw "Stage $Stage already recorded; inspect before any rerun" }
$stdout = Join-Path $evidence "$Stage.stdout.log"
$stderr = Join-Path $evidence "$Stage.stderr.log"
$argsForCargo = $commands[$Stage]
$meta = [ordered]@{
    todo_id = 3406
    parent_todo_id = 3392
    job = $env:CC_JOB_ID
    stage = $Stage
    started_at = [DateTimeOffset]::Now.ToString('o')
    source_head = $head
    source_tree = (git -C $workspace rev-parse 'HEAD^{tree}').Trim()
    cwd = $workspace
    executable = 'D:/CC/Tools/codex-jobs/cargo_gate.exe'
    cargo_arguments = @($argsForCargo)
    command = 'cargo ' + (($argsForCargo | ForEach-Object { if ($_ -match '[ |()]') { "'$_'" } else { $_ } }) -join ' ')
    target_dir = $target
    stdout = $stdout
    stderr = $stderr
    use_shell_execute = $false
    create_no_window = $true
    status = 'starting'
    timeout_seconds = 1800
}
$meta | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $metaPath -Encoding utf8NoBOM
$psi = [Diagnostics.ProcessStartInfo]::new()
$psi.FileName = $meta.executable
$psi.WorkingDirectory = $workspace
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
foreach ($arg in $argsForCargo) { $psi.ArgumentList.Add($arg) }
$psi.Environment['CARGO_TARGET_DIR'] = $target
$psi.Environment['RUSTC_WRAPPER'] = 'D:/CC/Tools/sccache/sccache.exe'
$psi.Environment['SCCACHE_DIR'] = 'D:/CC/BuildCache/sccache'
$psi.Environment['SCCACHE_CACHE_SIZE'] = '30G'
$psi.Environment['SCCACHE_SERVER_PORT'] = '42259'
$psi.Environment['CARGO_TERM_COLOR'] = 'never'
$psi.Environment['NEXTEST_COLOR'] = 'never'
$outFile = [IO.FileStream]::new($stdout,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::ReadWrite,1)
$errFile = [IO.FileStream]::new($stderr,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::ReadWrite,1)
$process = [Diagnostics.Process]::new()
$process.StartInfo = $psi
try {
    if (-not $process.Start()) { throw 'Cargo gate did not start' }
    $meta['pid'] = $process.Id
    $meta['process_created_at'] = $process.StartTime.ToUniversalTime().ToString('o')
    $meta['status'] = 'running'
    $meta | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $metaPath -Encoding utf8NoBOM
    $outCopy = $process.StandardOutput.BaseStream.CopyToAsync($outFile)
    $errCopy = $process.StandardError.BaseStream.CopyToAsync($errFile)
    $timer = [Diagnostics.Stopwatch]::StartNew()
    while (-not $process.WaitForExit(1000)) {
        if ($timer.Elapsed.TotalSeconds -gt $meta.timeout_seconds) {
            $meta['timed_out'] = $true
            $process.Kill($true)
            $process.WaitForExit()
            break
        }
    }
    if (-not [Threading.Tasks.Task]::WaitAll(@($outCopy,$errCopy),30000)) { throw 'Output did not drain after process exit' }
    $meta['exit_code'] = $process.ExitCode
    $meta['elapsed_seconds'] = [Math]::Round($timer.Elapsed.TotalSeconds,3)
    $meta['status'] = 'exited'
    $meta['finished_at'] = [DateTimeOffset]::Now.ToString('o')
} finally {
    $outFile.Dispose()
    $errFile.Dispose()
    $process.Dispose()
    $meta | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $metaPath -Encoding utf8NoBOM
}
$meta | ConvertTo-Json -Depth 8
exit $meta.exit_code
