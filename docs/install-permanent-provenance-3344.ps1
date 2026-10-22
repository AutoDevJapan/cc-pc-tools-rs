# Finite ToDo3344 / PR377 deployment. Validate by default; apply only after
# the fixed source's separate model review, all applicable CI, and normal merge.
param([switch]$Apply)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$reader = 'D:/CC/RuntimeSources/todo-3344/4fd59dccf724385783d86ccf124e33f417a07a6b/guard-reader.private.exe'
$readerSHA = '7a434bff4d7d11a8dd1c43f47bfd3384a8120a0811a11d9a888b033c74e2c7d0'
$stage = 'D:/CC/RuntimeSources/todo-3344/deployment-377'
$guardTarget = 'D:/CC/Tools/client-records/scrub391.exe'
$journal = "$stage/deployment-3344.private.jsonl"
$changes = @(
    @{kind='guard';target=$guardTarget;source='D:/CC/RuntimeSources/todo-3344/8a4452d47a01895eee6701dd9f153585869e17f8/guard-artifact.private.exe';old='3fa853b5ff57bbd95cefa6571577fd4d24d3ca92aae4f813f90fe08bd6f8faa9';new='8ad7256ecd0b443ea0d9f90a995dd2fe99d352e1504895072bcff0753f1208ac';head='8a4452d47a01895eee6701dd9f153585869e17f8'}
)
$retain = @(
    @{target='D:/CC/Tools/codex-jobs/codex_jobs.exe';sha='85beac9a785971e15d452a293c4f5c9d17f5133ef4fac00965dfcf0bd297a71f'},
    @{target='D:/CC/Tools/scheduler/scheduler.exe';sha='012c7abd31a0a38142a029dbfb1b2ec222575c87b252e5a3da43beb54b351bc1'},
    @{target='D:/CC/Tools/discord-ask/discord_ask.exe';sha='88cca802a3813fdcbe9e4ed9973cbd56aa25bd3407ac987f5578ba2202a8ec65'}
)
$purposeRetain = @(
    @{target='D:/CC/Tools/todo/todo.exe';kind='todo';sha='f8710b516d3ecdd37564d7a1ab978c8689ab1bdd2ffaec4150d0e4bed4c0b983'},
    @{target='D:/CC/Tools/capacity-audit-3h/capacity-audit-3h.exe';kind='capacity';sha='43d2e177ec55a1f3f0a281063105d5bd0ad2c6be89f1ab1f14a078773d724a3d'}
)
function Digest([string]$Path) { (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant() }
function Plain([string]$Path) {
    $p = [IO.Path]::GetFullPath($Path)
    while ($p) {
        if (((Get-Item -LiteralPath $p -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Reparse path refused' }
        $parent = [IO.Directory]::GetParent($p)
        if ($null -eq $parent) { break }
        $p = $parent.FullName
    }
}
function Hidden([string]$Program, [string[]]$Arguments) {
    $start = [Diagnostics.ProcessStartInfo]::new()
    $start.FileName = $Program
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    foreach ($value in $Arguments) { $start.ArgumentList.Add($value) }
    $p = [Diagnostics.Process]::new()
    $p.StartInfo = $start
    try {
        [void]$p.Start()
        $out = $p.StandardOutput.ReadToEndAsync()
        $err = $p.StandardError.ReadToEndAsync()
        if (-not $p.WaitForExit(30000)) { $p.Kill($true); throw 'Qualification deadline exceeded' }
        if (-not $out.Wait(5000) -or -not $err.Wait(5000)) { throw 'Qualification output drain incomplete' }
        if ($p.ExitCode -ne 0) { throw "Qualification command failed: $($p.ExitCode)" }
        return $out.Result
    } finally { $p.Dispose() }
}
function Record([string]$State, $Facts) {
    $line = @{todo=3344;parent=3338;at=[DateTimeOffset]::Now.ToString('o');state=$State;facts=$Facts} | ConvertTo-Json -Depth 6 -Compress
    [IO.File]::AppendAllText($journal, $line + "`n", [Text.UTF8Encoding]::new($false))
}
function Qualify($Item) {
    Plain $Item.target
    Plain $Item.source
    if ((Digest $reader) -ne $readerSHA) { throw 'Fixed reader changed' }
    $q = (Hidden $reader @('--verify-model-runtime-update', $Item.target)) | ConvertFrom-Json
    if ($q.qualified -ne $true -or $q.domain -ne 'model-runtime-update-3081-v1' -or $q.todo_id -ne 3081 -or
        $q.target_kind -ne $Item.kind -or $q.new_sha256 -ne $Item.new -or $q.source_head -ne $Item.head -or
        $q.data_access_granted -ne $false) { throw 'Exact software qualification mismatch' }
    $current = Digest $Item.target
    if ($current -ne $Item.old -and $current -ne $Item.new) { throw 'Expected current software changed' }
    if ((Digest $Item.source) -ne $Item.new) { throw 'Source software changed' }
    return $current
}
Plain $stage
Plain $reader
foreach ($item in $retain) {
    $q = (Hidden $guardTarget @('--guard-path', $item.target)) | ConvertFrom-Json
    if ($q.client_resource -ne $false -or $q.classification_unverified -ne $false -or (Digest $item.target) -ne $item.sha) { throw 'Retained runtime changed' }
}
foreach ($item in $purposeRetain) {
    Plain $item.target
    $q = (Hidden $guardTarget @('--verify-capacity-runtime-update', $item.target)) | ConvertFrom-Json
    if ($q.qualified -ne $true -or $q.domain -ne 'capacity-runtime-update-3085' -or $q.todo_id -ne 3085 -or
        $q.target_kind -ne $item.kind -or $q.data_access_granted -ne $false -or $q.new_sha256 -ne $item.sha -or
        (Digest $item.target) -ne $item.sha) { throw 'Retained capacity purpose changed' }
}
foreach ($item in $changes) { [void](Qualify $item) }
# Compare the exact staged engine through the ordinary consumer interface.
# This is read-only and also runs in the default validation mode.
foreach ($path in @($retain.target) + @('D:/OMB/evidence/omb-runtime-3344.jpg')) {
    $q = (Hidden $changes[0].source @('--guard-path', $path)) | ConvertFrom-Json
    if ($q.ok -ne $true -or $q.client_resource -ne $false -or $q.classification_unverified -ne $false) { throw 'Staged normal classification changed' }
}
foreach ($item in $purposeRetain) {
    $q = (Hidden $changes[0].source @('--verify-capacity-runtime-update', $item.target)) | ConvertFrom-Json
    if ($q.qualified -ne $true -or $q.domain -ne 'capacity-runtime-update-3085' -or $q.todo_id -ne 3085 -or
        $q.target_kind -ne $item.kind -or $q.data_access_granted -ne $false -or $q.new_sha256 -ne $item.sha) { throw 'Staged capacity purpose changed' }
}
if (-not $Apply) { @{validated=$true;applied=$false;replacements=1;retained_runtimes=5;todo_preserved='actual PR282 f8710b51; only read through exact3085 software purpose; not replaced here'} | ConvertTo-Json -Compress; exit 0 }
$lockPath = 'D:/CC/Tools/scheduler/scheduler.lock'
Plain $lockPath
$lock = [IO.File]::Open($lockPath, [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, [IO.FileShare]::ReadWrite)
$held = $false
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while (-not $held) {
        try { $lock.Lock(0, 1); $held = $true } catch [IO.IOException] {
            if ([DateTime]::UtcNow -ge $deadline) { throw }
            Start-Sleep -Milliseconds 200
        }
    }
    Record 'started' @{reader=$readerSHA;changes=1;retained=5;todo_not_changed=$true}
    foreach ($item in $changes) {
        $current = Qualify $item
        if ($current -eq $item.new) { Record 'already-current' @{kind=$item.kind;sha256=$current}; continue }
        $name = [IO.Path]::GetFileName($item.target)
        $copy = "$stage/3344-install-$name"
        $backup = "$stage/3344-before-$name"
        if (Test-Path -LiteralPath $backup) { throw 'Prior backup needs reconciliation; no overwrite' }
        if (Test-Path -LiteralPath $copy) {
            Plain $copy
            if ((Digest $copy) -ne $item.new) { throw 'Prior staged bytes need reconciliation; no overwrite' }
            Record 'reused-verified-copy' @{kind=$item.kind;sha256=$item.new;path=$copy}
        } else {
            [IO.File]::Copy($item.source, $copy, $false)
        }
        Plain $copy
        $acl = Get-Acl -LiteralPath $item.target
        Set-Acl -LiteralPath $copy -AclObject $acl
        if ((Get-Acl -LiteralPath $copy).Sddl -ne $acl.Sddl) { throw 'Original security descriptor was not retained before replacement' }
        if ((Qualify $item) -ne $item.old -or (Digest $copy) -ne $item.new) { throw 'Final comparison changed' }
        Record 'before-replace' @{kind=$item.kind;before=$item.old;after=$item.new;backup=$backup}
        [IO.File]::Replace($copy, $item.target, $backup, $false)
        $security = (Get-Acl -LiteralPath $item.target).Sddl -eq $acl.Sddl
        $after = Digest $item.target
        $before = Digest $backup
        Record 'replaced' @{kind=$item.kind;before=$before;after=$after;security_preserved=$security;backup=$backup}
        if (-not $security -or $before -ne $item.old -or $after -ne $item.new) { throw 'Replacement requires reconciliation; preserve backup and current files' }
    }
    foreach ($item in @($retain) + @($purposeRetain)) { if ((Digest $item.target) -ne $item.sha) { throw 'Retained runtime changed during update' } }
    foreach ($item in $purposeRetain) {
        $q = (Hidden $guardTarget @('--verify-capacity-runtime-update', $item.target)) | ConvertFrom-Json
        if ($q.qualified -ne $true -or $q.domain -ne 'capacity-runtime-update-3085' -or $q.todo_id -ne 3085 -or
            $q.target_kind -ne $item.kind -or $q.data_access_granted -ne $false -or $q.new_sha256 -ne $item.sha) { throw 'Installed capacity purpose changed' }
    }
    Record 'complete' @{replacements=1;retained=5;todo_not_changed=$true;workers_stopped=0;configuration_changed=$false;console_changed=$false}
    @{applied=$true;replacements=1;retained_runtimes=5;todo_not_changed=$true;journal=$journal} | ConvertTo-Json -Compress
} catch {
    Record 'stopped' @{reason=$_.Exception.Message;owner_input_required=$false;automatic_rollback=$false}
    throw
} finally { if ($held) { $lock.Unlock(0, 1) }; $lock.Dispose() }
