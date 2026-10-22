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
$guardRef = '{0}:docs/evidence/capacity-3085-guard.json' -f $ReviewedCommit
$guardText = & git show $guardRef
if ($LASTEXITCODE -ne 0) { throw 'Committed guard evidence unavailable' }
$guardManifest = ($guardText -join "`n") | ConvertFrom-Json
if ($guardManifest.origin_todo_id -ne 3085 -or $guardManifest.domain -ne 'capacity-runtime-update-3085' -or $guardManifest.data_access_granted -ne $false) { throw 'Wrong guard update scope' }
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
function Hash([string]$Path) {
    $file = [IO.File]::OpenRead($Path)
    $digest = [Security.Cryptography.SHA256]::Create()
    try { return [BitConverter]::ToString($digest.ComputeHash($file)).Replace('-','').ToLowerInvariant() }
    finally { $digest.Dispose(); $file.Dispose() }
}
function Assert-Guard([string]$Path,[string]$Old,[string]$New) {
    if ($Path -match '["\r\n]') { throw 'Invalid classifier argument' }
    $start = New-Object Diagnostics.ProcessStartInfo
    $start.FileName = 'D:\CC\Tools\client-records\scrub391.exe'
    $start.Arguments = '--verify-capacity-runtime-update "' + $Path + '"'
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
    if ($parsed.qualified -ne $true -or $parsed.domain -ne 'capacity-runtime-update-3085' -or $parsed.todo_id -ne 3085 -or $parsed.old_sha256 -ne $Old -or $parsed.new_sha256 -ne $New -or $parsed.source_receipt_sha256 -ne $guardManifest.runtime_receipt_sha256 -or $parsed.data_access_granted -ne $false -or $parsed.original_registry_recovered -ne $false) { throw 'Runtime update is not qualified' }
}
function Install-PinnedRuntime {
    param([string]$Source,[string]$Target,[string]$BackupRoot,[string]$Old,[string]$New,[scriptblock]$ReplaceAction=$null,[scriptblock]$RetainAction=$null)
    foreach ($path in @($Source,$Target,$BackupRoot)) { Assert-Plain $path }
    if ((Hash $Source) -ne $New) { throw 'Source artifact changed' }
    $stage = Join-Path ([IO.Path]::GetDirectoryName($Target)) (([IO.Path]::GetFileName($Target)) + '.3085-stage')
    Assert-Plain $stage
    if ((Hash $Target) -eq $New) {
        if (Test-Path -LiteralPath $stage) {
            if ((Hash $stage) -ne $New) { throw 'Unknown stage retained' }
            # Exact task-owned stage, exact reviewed software bytes, pinned parent.
            Remove-Item -LiteralPath $stage
        }
        return 'already_current'
    }
    if ((Hash $Target) -ne $Old) { throw 'Concurrent runtime update' }
    $backup = Join-Path $BackupRoot (([IO.Path]::GetFileName($Target)) + '.before-3085')
    Assert-Plain $backup
    if (Test-Path -LiteralPath $backup) {
        if ((Hash $backup) -ne $Old) { throw 'Unknown backup retained' }
        # Preserve the first backup and give every replace its own rollback file.
        $backup += '.' + [Guid]::NewGuid().ToString('N')
        Assert-Plain $backup
        if (Test-Path -LiteralPath $backup) { throw 'Backup reservation exists' }
    }
    if (Test-Path -LiteralPath $stage) {
        if ((Hash $stage) -ne $New) { throw 'Unknown stage retained' }
    } else { Copy-Item -LiteralPath $Source -Destination $stage }
    if ((Hash $stage) -ne $New -or (Hash $Target) -ne $Old) { throw 'CAS changed during staging' }
    if ($null -eq $ReplaceAction) { $ReplaceAction = { param($s,$t,$b) [IO.File]::Replace($s,$t,$b,$false) } }
    try { & $ReplaceAction $stage $Target $backup }
    catch {
        # The same already-qualified operation may roll back its pinned old
        # software if a Windows partial failure removed the destination name.
        if (-not (Test-Path -LiteralPath $Target) -and (Test-Path -LiteralPath $backup) -and (Hash $backup) -eq $Old) {
            Assert-Plain $Target
            if ([IO.Path]::GetPathRoot($backup) -ine [IO.Path]::GetPathRoot($Target)) { throw 'Rollback must remain on the same volume' }
            # Restore the original image first: a failed optional byte copy
            # must not leave the public name missing. Rename preserves ACLs.
            [IO.File]::Move($backup,$Target)
            if ((Hash $Target) -ne $Old) { throw 'Rollback fingerprint mismatch' }
            $retained = $backup + '.retained.' + [Guid]::NewGuid().ToString('N')
            Assert-Plain $retained
            if ($null -eq $RetainAction) { $RetainAction = { param($s,$t) [IO.File]::Copy($s,$t,$false) } }
            & $RetainAction $Target $retained
            if ((Hash $retained) -ne $Old) { throw 'Retained rollback fingerprint mismatch' }
        }
        throw
    }
    if ((Hash $Target) -ne $New -or (Hash $backup) -ne $Old) { throw 'Installed artifact or rollback mismatch' }
    return 'updated'
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
foreach ($sourceFile in $guardManifest.source_files.PSObject.Properties) {
    $path = Join-Path $taskSource $sourceFile.Name
    Assert-Plain $path
    if ((Hash $path) -ne $sourceFile.Value) { throw 'Reviewed guard source changed' }
}
if ((Hash (Join-Path $taskSource 'docs\evidence\capacity-3085-runtime.json')) -ne $guardManifest.runtime_receipt_sha256) { throw 'Runtime receipt changed' }
$guardSource = Join-Path $release 'scrub391-3085.exe'
$guardTarget = 'D:\CC\Tools\client-records\scrub391.exe'
foreach ($path in @($guardSource,$guardTarget)) { Assert-Plain $path }
if ((Hash $guardSource) -ne $guardManifest.new_sha256 -or (Hash $guardTarget) -notin @($guardManifest.old_sha256,$guardManifest.new_sha256)) { throw 'Guard CAS changed; preserve concurrent registration' }
$targets = @(
    [pscustomobject]@{Source=(Join-Path $release 'todo-3085.exe');Target='D:\CC\Tools\todo\todo.exe';Old=$manifest.todo.old_sha256;New=$manifest.todo.new_sha256},
    [pscustomobject]@{Source=(Join-Path $release 'capacity-audit-3h.exe');Target='D:\CC\Tools\capacity-audit-3h\capacity-audit-3h.exe';Old=$manifest.capacity.old_sha256;New=$manifest.capacity.new_sha256}
)
# Validate all sources and both CAS expectations before changing any leaf.
foreach ($target in $targets) {
    Assert-Plain $target.Source; Assert-Plain $target.Target
    if ((Hash $target.Source) -ne $target.New -or (Hash $target.Target) -notin @($target.Old,$target.New)) { throw 'Runtime CAS changed; preserve concurrent release' }
}
# Bootstrap only the independently owned, exact old/new software classifier.
# This source-reviewed registration is not a data permit or a generic allow.
$guardInstallation = Install-PinnedRuntime -Source $guardSource -Target $guardTarget -BackupRoot $release -Old $guardManifest.old_sha256 -New $guardManifest.new_sha256
foreach ($target in $targets) { Assert-Guard $target.Target $target.Old $target.New }
foreach ($item in $targets) {
    Assert-Guard $item.Target $item.Old $item.New
    $installation = Install-PinnedRuntime -Source $item.Source -Target $item.Target -BackupRoot $release -Old $item.Old -New $item.New
    Assert-Guard $item.Target $item.Old $item.New
}
$after = Get-ScheduledTask -TaskName 'CC-CapacityAudit-3h'
$cleanupAfter = Get-ScheduledTask -TaskName 'CC-CleanupDeletionRequests-8h'
if ($after.Triggers[0].Repetition.Interval -ne 'PT3H' -or $cleanupAfter.Triggers[0].Repetition.Interval -ne 'PT1H' -or (Hash 'D:\CC\Tools\cleanup-8h\cleanup-8h.exe') -ne $manifest.cleanup_sha256) { throw 'Maintenance contract changed' }
$receipt = [ordered]@{schema=1;todo_id=3085;source_commit=$ReviewedCommit;deployed_at_utc=[DateTime]::UtcNow.ToString('o');guard_installation=$guardInstallation;guard_sha256=$guardManifest.new_sha256;qualification_domain=$guardManifest.domain;data_access_granted=$false;capacity_sha256=$manifest.capacity.new_sha256;todo_sha256=$manifest.todo.new_sha256;cleanup_changed=$false;tasks_created=0;periods=@('PT3H','PT1H');original_registry_recovered=$false}
[IO.File]::WriteAllText((Join-Path $release '3085-deployed.private.json'),($receipt|ConvertTo-Json -Depth 5),[Text.UTF8Encoding]::new($false))
$receipt | ConvertTo-Json -Depth 5 -Compress
