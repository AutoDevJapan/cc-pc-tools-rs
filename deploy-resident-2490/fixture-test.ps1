$ErrorActionPreference = 'Stop'
$taskName = 'CC-ResidentGuard-Fixture-2490'
$executable = 'D:\CC\Tools\resident-guard\resident-guard.exe'
$stateDirectory = 'D:\CC\Work\unattended-operations-20261013\deploy-resident-2490\fixture-state-2490'
$receiptPath = 'D:\CC\Work\unattended-operations-20261013\deploy-resident-2490\fixture-receipt-2490.json'
$healthUri = 'http://127.0.0.1:24090/healthz'
if (Test-Path -LiteralPath $stateDirectory) { throw 'fixture_state_exists' }
$task = Get-ScheduledTask -TaskName $taskName
if ($task.State -eq 'Running') { throw 'fixture_already_running' }
function Wait-FixtureHealthy {
    $deadline = [DateTime]::UtcNow.AddSeconds(10)
    do {
        try {
            $reply = Invoke-WebRequest -UseBasicParsing -Uri $healthUri -TimeoutSec 1
            $json = $reply.Content | ConvertFrom-Json
            if ($reply.StatusCode -eq 200 -and $json.status -eq 'ok' -and $json.service -eq 'opencodex') {
                return @(Get-NetTCPConnection -State Listen -LocalPort 24090 -ErrorAction Stop)[0].OwningProcess
            }
        } catch { }
        if ([DateTime]::UtcNow -ge $deadline) { throw 'fixture_healthy_timeout' }
        Start-Sleep -Milliseconds 100
    } while ($true)
}
function Wait-FixtureStopped {
    $deadline = [DateTime]::UtcNow.AddSeconds(10)
    do {
        if (-not (Get-NetTCPConnection -State Listen -LocalPort 24090 -ErrorAction SilentlyContinue)) { return }
        if ([DateTime]::UtcNow -ge $deadline) { throw 'fixture_stop_timeout' }
        Start-Sleep -Milliseconds 100
    } while ($true)
}
function Run-GuardFixture {
    # A pipeline makes Windows PowerShell 5.1 wait for a windowsgui executable
    # and collect its exit code instead of reusing an earlier native exit code.
    $result = & $executable --state-dir $stateDirectory --fixture-port 24090 --fixture-task $taskName | Out-String
    if ($LASTEXITCODE -ne 0) { throw 'guard_fixture_failed' }
    return ($result | ConvertFrom-Json)
}
Start-ScheduledTask -TaskName $taskName
$firstPID = Wait-FixtureHealthy
Stop-ScheduledTask -TaskName $taskName
Wait-FixtureStopped
$absentCheck = Run-GuardFixture
if ($absentCheck.services.Count -ne 1 -or $absentCheck.services[0].probe.port -ne 'closed' -or $absentCheck.services[0].action -ne 'task_start_requested') { throw 'fixture_recovery_not_requested' }
$secondPID = Wait-FixtureHealthy
if ($firstPID -eq $secondPID) { throw 'fixture_pid_not_replaced' }
$healthyCheck = Run-GuardFixture
if (-not $healthyCheck.healthy -or $healthyCheck.services[0].action -ne 'healthy' -or $healthyCheck.services[0].attempts_last_hour -ne 1) { throw 'fixture_recovery_unconfirmed' }
Stop-ScheduledTask -TaskName $taskName
Wait-FixtureStopped
$cooldownCheck = Run-GuardFixture
if ($cooldownCheck.services[0].action -ne 'cooldown' -or $cooldownCheck.services[0].attempts_last_hour -ne 1) { throw 'fixture_cooldown_unconfirmed' }
$receipt = [ordered]@{
    checked_at = [DateTime]::UtcNow.ToString('o')
    scheduled_fixture_healthy_before_stop = $true
    deliberate_fixture_stop_confirmed = $true
    scheduled_task_restart_requested = $true
    recovered_pid_changed = $true
    recovered_health_confirmed = $true
    cooldown_no_duplicate_start = $true
    production_services_stopped = $false
    fixture_threshold = 1
    production_threshold = 2
    guard_runtime_sha256 = (Get-FileHash -LiteralPath $executable -Algorithm SHA256).Hash
}
$receipt | ConvertTo-Json | Set-Content -LiteralPath $receiptPath -Encoding UTF8
$receipt | ConvertTo-Json -Compress
