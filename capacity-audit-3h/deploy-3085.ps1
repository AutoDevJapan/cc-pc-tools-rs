param([Parameter(Mandatory=$true)][string]$ReviewedCommit)
$ErrorActionPreference = 'Stop'
$taskSource = 'D:\CC\Work\unattended-operations-20261013-wt\todo-3085-capacity-decision'
Set-Location -LiteralPath $taskSource
if ($ReviewedCommit -notmatch '^[a-f0-9]{40}$') { throw 'Invalid reviewed commit' }
$head = git rev-parse HEAD
if ($LASTEXITCODE -ne 0 -or $head -ne $ReviewedCommit) { throw 'Source HEAD changed' }
git merge-base --is-ancestor $ReviewedCommit origin/main
if ($LASTEXITCODE -ne 0) { throw 'Reviewed source has not merged' }
$manifest = Get-Content -Raw -Encoding UTF8 -LiteralPath (Join-Path $taskSource 'docs\evidence\capacity-3085-runtime.json') | ConvertFrom-Json
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
    $reply = & 'D:\CC\Tools\client-records\scrub391.exe' --guard-path $Path
    if ($LASTEXITCODE -ne 0) { throw 'Classifier unavailable' }
    $parsed = $reply | ConvertFrom-Json
    if ($parsed.ok -ne $true -or $parsed.client_resource -ne $false -or $parsed.classification_unverified -ne $false) { throw 'Runtime is not qualified' }
}
$taskBefore = Get-ScheduledTask -TaskName 'CC-CapacityAudit-3h'
if ($taskBefore.State -eq 'Running' -or $taskBefore.Actions.Execute -ne 'D:\CC\Tools\capacity-audit-3h\capacity-audit-3h.exe' -or $taskBefore.Triggers[0].Repetition.Interval -ne 'PT3H') { throw 'Capacity task changed or active' }
$cleanupBefore = Get-ScheduledTask -TaskName 'CC-CleanupDeletionRequests-8h'
if ($cleanupBefore.Triggers[0].Repetition.Interval -ne 'PT1H' -or (Hash 'D:\CC\Tools\cleanup-8h\cleanup-8h.exe') -ne $manifest.cleanup_sha256) { throw 'Hourly cleaner changed' }
$release = Join-Path $taskSource '.ci-work'
$guardSource = Join-Path $release 'scrub391.exe'
$guardTarget = 'D:\CC\Tools\client-records\scrub391.exe'
$guardExpected = 'd017dd329903b65c9257a50c52a2acccd5fe9085de11909e3f7448fc1f232351'
$guardNew = Get-Content -Raw -Encoding UTF8 -LiteralPath (Join-Path $release 'guard.sha256')
$guardNew = $guardNew.Trim().ToLowerInvariant()
if ($guardNew -notmatch '^[a-f0-9]{64}$' -or (Hash $guardSource) -ne $guardNew) { throw 'Guard artifact changed' }
$targets = @(
    [pscustomobject]@{Source=(Join-Path $taskSource 'target\release\todo.exe');Target='D:\CC\Tools\todo\todo.exe';Old=$manifest.todo.old_sha256;New=$manifest.todo.new_sha256},
    [pscustomobject]@{Source=(Join-Path $release 'capacity-audit-3h.exe');Target='D:\CC\Tools\capacity-audit-3h\capacity-audit-3h.exe';Old=$manifest.capacity.old_sha256;New=$manifest.capacity.new_sha256}
)
# Validate all sources and both CAS expectations before changing any leaf.
foreach ($target in $targets) {
    Assert-Plain $target.Source; Assert-Plain $target.Target
    if ((Hash $target.Source) -ne $target.New -or (Hash $target.Target) -ne $target.Old) { throw 'Runtime CAS changed; preserve concurrent release' }
}
Assert-Plain $guardSource; Assert-Plain $guardTarget
if ((Hash $guardTarget) -ne $guardExpected) { throw 'Classifier CAS changed; integrate current source first' }
$items = @([pscustomobject]@{Source=$guardSource;Target=$guardTarget;Old=$guardExpected;New=$guardNew}) + $targets
foreach ($item in $items) {
    if ((Hash $item.Target) -ne $item.Old) { throw 'Concurrent runtime update' }
    if ($item.Target -ne $guardTarget) { Assert-Guard $item.Target }
    $backup = Join-Path $release (([IO.Path]::GetFileName($item.Target)) + '.before-3085')
    if (Test-Path -LiteralPath $backup) { throw 'Prior backup exists; inspect previous deployment' }
    $stage = Join-Path ([IO.Path]::GetDirectoryName($item.Target)) (([IO.Path]::GetFileName($item.Target)) + '.3085-stage')
    Assert-Plain $stage
    if (Test-Path -LiteralPath $stage) { throw 'Prior stage exists' }
    Copy-Item -LiteralPath $item.Source -Destination $stage
    if ((Hash $stage) -ne $item.New -or (Hash $item.Target) -ne $item.Old) { throw 'CAS changed during staging' }
    [IO.File]::Replace($stage,$item.Target,$backup,$false)
    if ((Hash $item.Target) -ne $item.New) { throw 'Installed artifact mismatch' }
    if ($item.Target -ne $guardTarget) { Assert-Guard $item.Target }
}
$after = Get-ScheduledTask -TaskName 'CC-CapacityAudit-3h'
$cleanupAfter = Get-ScheduledTask -TaskName 'CC-CleanupDeletionRequests-8h'
if ($after.Triggers[0].Repetition.Interval -ne 'PT3H' -or $cleanupAfter.Triggers[0].Repetition.Interval -ne 'PT1H' -or (Hash 'D:\CC\Tools\cleanup-8h\cleanup-8h.exe') -ne $manifest.cleanup_sha256) { throw 'Maintenance contract changed' }
$receipt = [ordered]@{schema=1;todo_id=3085;source_commit=$ReviewedCommit;deployed_at_utc=[DateTime]::UtcNow.ToString('o');guard_sha256=$guardNew;capacity_sha256=$manifest.capacity.new_sha256;todo_sha256=$manifest.todo.new_sha256;cleanup_changed=$false;tasks_created=0;periods=@('PT3H','PT1H');original_registry_recovered=$false}
[IO.File]::WriteAllText((Join-Path $release '3085-deployed.private.json'),($receipt|ConvertTo-Json -Depth 5),[Text.UTF8Encoding]::new($false))
$receipt | ConvertTo-Json -Depth 5 -Compress
