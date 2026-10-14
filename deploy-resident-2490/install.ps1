param([Parameter(Mandatory=$true)][string]$ExpectedSha256)
$ErrorActionPreference = 'Stop'
$worktree = 'D:\CC\Work\unattended-operations-20261013'
$source = Join-Path $worktree 'resident-guard\resident-guard.exe'
$destinationDirectory = 'D:\CC\Tools\resident-guard'
$destination = Join-Path $destinationDirectory 'resident-guard.exe'
$artifactDirectory = Join-Path $worktree 'deploy-resident-2490'
if ($ExpectedSha256 -notmatch '^[A-Fa-f0-9]{64}$') { throw 'invalid_hash' }
if ((Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash -ne $ExpectedSha256) { throw 'source_hash_mismatch' }
$task = Get-ScheduledTask -TaskName 'CC-ResidentGuard'
if ($task.Actions.Count -ne 1 -or -not [string]::Equals($task.Actions[0].Execute, $destination, [StringComparison]::OrdinalIgnoreCase)) { throw 'resident_task_definition_changed' }
if ($task.State -eq 'Running') { throw 'resident_task_busy' }
if (Test-Path -LiteralPath $destinationDirectory) {
    $entry = Get-Item -LiteralPath $destinationDirectory -Force
    if ($entry.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'destination_indirection' }
} else { [void](New-Item -ItemType Directory -Path $destinationDirectory) }
if (Test-Path -LiteralPath $destination) {
    if ((Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash -ne $ExpectedSha256) { throw 'unexpected_existing_runtime' }
} else {
    $staging = Join-Path $destinationDirectory 'resident-guard.staging-2490.exe'
    if (Test-Path -LiteralPath $staging) { throw 'unexpected_staging_file' }
    Copy-Item -LiteralPath $source -Destination $staging
    if ((Get-FileHash -LiteralPath $staging -Algorithm SHA256).Hash -ne $ExpectedSha256) { throw 'staging_hash_mismatch' }
    Move-Item -LiteralPath $staging -Destination $destination
}
$taskBackup = Join-Path $artifactDirectory 'task-before-2490.xml'
if (-not (Test-Path -LiteralPath $taskBackup)) { Export-ScheduledTask -InputObject $task | Set-Content -LiteralPath $taskBackup -Encoding Unicode }
$trigger = New-ScheduledTaskTrigger -Once -At ((Get-Date).AddMinutes(5)) -RepetitionInterval (New-TimeSpan -Minutes 5)
$logon = New-ScheduledTaskTrigger -AtLogOn -User $task.Principal.UserId
$settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -ExecutionTimeLimit (New-TimeSpan -Minutes 2) -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
$settings.IdleSettings.StopOnIdleEnd = $false
$action = New-ScheduledTaskAction -Execute $destination -WorkingDirectory $destinationDirectory
$updated = Set-ScheduledTask -TaskName 'CC-ResidentGuard' -Trigger @($trigger,$logon) -Settings $settings -Action $action
if ($updated.Principal.UserId -ne $task.Principal.UserId -or $updated.Principal.LogonType -ne $task.Principal.LogonType -or $updated.Principal.RunLevel -ne $task.Principal.RunLevel) { throw 'principal_changed' }
Start-ScheduledTask -InputObject $updated
[ordered]@{ installed = $true; task = 'CC-ResidentGuard'; interval = 'PT5M'; logon_trigger = $true; runtime_sha256 = (Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash; authentication_changed = $false } | ConvertTo-Json -Compress
