param([Parameter(Mandatory=$true)][string]$ReviewedCommit)
$ErrorActionPreference = 'Stop'
$taskSource = 'D:\CC\Work\unattended-operations-20261013-wt\todo-3085-capacity-decision'
Set-Location -LiteralPath $taskSource
if ($ReviewedCommit -notmatch '^[a-f0-9]{40}$') { throw 'Invalid reviewed commit' }
$head = git rev-parse HEAD
if ($LASTEXITCODE -ne 0 -or $head -ne $ReviewedCommit) { throw 'Source HEAD changed' }
git diff --quiet HEAD --
if ($LASTEXITCODE -ne 0) { throw 'Source has tracked changes' }
git merge-base --is-ancestor $ReviewedCommit origin/main
if ($LASTEXITCODE -ne 0) { throw 'Reviewed source has not merged' }
$manifestRef = '{0}:docs/evidence/capacity-3085-runtime.json' -f $ReviewedCommit
$manifestText = & git show $manifestRef
if ($LASTEXITCODE -ne 0) { throw 'Committed build evidence unavailable' }
$manifest = ($manifestText -join "`n") | ConvertFrom-Json
if ($manifest.origin_todo_id -ne 3085 -or $manifest.original_registry_recovered -ne $false) { throw 'Wrong finite deployment scope' }
function Assert-Plain([string]$Path) {
    $absolute = [IO.Path]::GetFullPath($Path)
    if ($absolute -cne $Path) { throw 'Noncanonical absolute deployment path' }
    $cursor = $absolute
    while ($cursor) {
        if (Test-Path -LiteralPath $cursor) {
            $entry = Get-Item -LiteralPath $cursor -Force
            if (($entry.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Linked deployment path' }
        }
        $parent = [IO.Path]::GetDirectoryName($cursor)
        if ($parent -eq $cursor) { break }
        $cursor = $parent
    }
}
function Hash([string]$Path) { (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant() }
function Assert-Guard([string]$Path) {
    if ($Path -match '["\r\n]') { throw 'Invalid classifier argument' }
    $start = New-Object Diagnostics.ProcessStartInfo
    $start.FileName = 'D:\CC\Tools\client-records\scrub391.exe'
    $start.Arguments = '--guard-path "' + $Path + '"'
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    $process = [Diagnostics.Process]::Start($start)
    $stdout = $process.StandardOutput.ReadToEndAsync()
    $stderr = $process.StandardError.ReadToEndAsync()
    if (-not $process.WaitForExit(15000)) { $process.Kill(); $process.WaitForExit(); $process.Dispose(); throw 'Classifier timed out' }
    $reply = $stdout.GetAwaiter().GetResult()
    $errorBytes = $stderr.GetAwaiter().GetResult()
    $code = $process.ExitCode
    $process.Dispose()
    if ($code -ne 0 -or $reply.Length -gt 4096 -or $errorBytes.Length -gt 0) { throw 'Classifier unavailable' }
    $parsed = $reply | ConvertFrom-Json
    if ($parsed.ok -ne $true -or $parsed.client_resource -ne $false -or $parsed.classification_unverified -ne $false) { throw 'Runtime is not qualified' }
}
$taskBefore = Get-ScheduledTask -TaskName 'CC-CapacityAudit-3h'
if ($taskBefore.State -eq 'Running' -or $taskBefore.Actions.Execute -ne 'D:\CC\Tools\capacity-audit-3h\capacity-audit-3h.exe' -or $taskBefore.Triggers[0].Repetition.Interval -ne 'PT3H') { throw 'Capacity task changed or active' }
$cleanupBefore = Get-ScheduledTask -TaskName 'CC-CleanupDeletionRequests-8h'
if ($cleanupBefore.Triggers[0].Repetition.Interval -ne 'PT1H' -or (Hash 'D:\CC\Tools\cleanup-8h\cleanup-8h.exe') -ne $manifest.cleanup_sha256) { throw 'Hourly cleaner changed' }
$release = Join-Path $taskSource '.ci-work'
foreach ($sourceFile in $manifest.source_files.PSObject.Properties) {
    $path = Join-Path $taskSource $sourceFile.Name
    Assert-Plain $path
    if ((Hash $path) -ne $sourceFile.Value) { throw 'Reviewed build source changed' }
}
$targets = @(
    [pscustomobject]@{Source=(Join-Path $taskSource 'target\release\todo.exe');Target='D:\CC\Tools\todo\todo.exe';Old=$manifest.todo.old_sha256;New=$manifest.todo.new_sha256},
    [pscustomobject]@{Source=(Join-Path $release 'capacity-audit-3h.exe');Target='D:\CC\Tools\capacity-audit-3h\capacity-audit-3h.exe';Old=$manifest.capacity.old_sha256;New=$manifest.capacity.new_sha256}
)
# Validate all sources and both CAS expectations before changing any leaf.
foreach ($target in $targets) {
    Assert-Plain $target.Source; Assert-Plain $target.Target
    if ((Hash $target.Source) -ne $target.New -or (Hash $target.Target) -notin @($target.Old,$target.New)) { throw 'Runtime CAS changed; preserve concurrent release' }
    Assert-Guard $target.Target
}
foreach ($item in $targets) {
    if ((Hash $item.Target) -eq $item.New) { Assert-Guard $item.Target; continue }
    if ((Hash $item.Target) -ne $item.Old) { throw 'Concurrent runtime update' }
    Assert-Guard $item.Target
    $backup = Join-Path $release (([IO.Path]::GetFileName($item.Target)) + '.before-3085')
    Assert-Plain $backup
    if ((Test-Path -LiteralPath $backup) -and (Hash $backup) -ne $item.Old) { throw 'Prior backup differs; preserve it' }
    $stage = Join-Path ([IO.Path]::GetDirectoryName($item.Target)) (([IO.Path]::GetFileName($item.Target)) + '.3085-stage')
    Assert-Plain $stage
    if (Test-Path -LiteralPath $stage) {
        if ((Hash $stage) -ne $item.New) { throw 'Prior stage differs; preserve it' }
    } else { Copy-Item -LiteralPath $item.Source -Destination $stage }
    if ((Hash $stage) -ne $item.New -or (Hash $item.Target) -ne $item.Old) { throw 'CAS changed during staging' }
    $replaceBackup = $backup
    if (Test-Path -LiteralPath $backup) { $replaceBackup = $null }
    [IO.File]::Replace($stage,$item.Target,$replaceBackup,$false)
    if ((Hash $item.Target) -ne $item.New) { throw 'Installed artifact mismatch' }
    Assert-Guard $item.Target
}
$after = Get-ScheduledTask -TaskName 'CC-CapacityAudit-3h'
$cleanupAfter = Get-ScheduledTask -TaskName 'CC-CleanupDeletionRequests-8h'
if ($after.Triggers[0].Repetition.Interval -ne 'PT3H' -or $cleanupAfter.Triggers[0].Repetition.Interval -ne 'PT1H' -or (Hash 'D:\CC\Tools\cleanup-8h\cleanup-8h.exe') -ne $manifest.cleanup_sha256) { throw 'Maintenance contract changed' }
$receipt = [ordered]@{schema=1;todo_id=3085;source_commit=$ReviewedCommit;deployed_at_utc=[DateTime]::UtcNow.ToString('o');guard_updated=$false;capacity_sha256=$manifest.capacity.new_sha256;todo_sha256=$manifest.todo.new_sha256;cleanup_changed=$false;tasks_created=0;periods=@('PT3H','PT1H');original_registry_recovered=$false}
[IO.File]::WriteAllText((Join-Path $release '3085-deployed.private.json'),($receipt|ConvertTo-Json -Depth 5),[Text.UTF8Encoding]::new($false))
$receipt | ConvertTo-Json -Depth 5 -Compress
