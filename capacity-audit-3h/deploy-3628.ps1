param([Parameter(Mandatory=$true)][string]$ReviewedCommit)
$ErrorActionPreference='Stop'
$sourceRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
Set-Location -LiteralPath $sourceRoot
if($ReviewedCommit -notmatch '^[a-f0-9]{40}$'){throw 'Invalid reviewed commit'}
if((git rev-parse HEAD) -ne $ReviewedCommit){throw 'Source HEAD changed'}
git diff --quiet HEAD --
if($LASTEXITCODE -ne 0){throw 'Tracked source changed'}
git merge-base --is-ancestor $ReviewedCommit origin/main
if($LASTEXITCODE -ne 0){throw 'Reviewed source is not merged'}

# Reuse the reviewed replacement/rollback implementation, without executing
# that old deployment's entry point or changing its old 3h/1h contract.
$tokens=$null;$parseErrors=$null
$sharedScript=Join-Path $PSScriptRoot 'deploy-3085.ps1'
$ast=[Management.Automation.Language.Parser]::ParseFile($sharedScript,[ref]$tokens,[ref]$parseErrors)
if($parseErrors.Count){throw 'Replacement helper syntax invalid'}
$names=@('Assert-Plain','Hash','Install-PinnedRuntime')
$functions=$ast.FindAll({param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $names -contains $node.Name},$true)
if($functions.Count -ne 3){throw 'Replacement helpers missing'}
foreach($definition in $functions){Invoke-Expression $definition.Extent.Text}

$manifestPath=Join-Path $sourceRoot 'docs/evidence/capacity-3628-runtime.json'
$guardPath=Join-Path $sourceRoot 'docs/evidence/capacity-3628-guard.json'
$manifest=Get-Content -LiteralPath $manifestPath -Raw|ConvertFrom-Json
$guardManifest=Get-Content -LiteralPath $guardPath -Raw|ConvertFrom-Json
if($manifest.origin_todo_id -ne 3628 -or $manifest.domain -ne 'capacity-runtime-update-3628' -or $manifest.data_access_granted -ne $false -or $manifest.original_registry_recovered -ne $false){throw 'Invalid capacity scope'}
if($guardManifest.origin_todo_id -ne 3628 -or $guardManifest.domain -ne $manifest.domain -or $guardManifest.data_access_granted -ne $false -or $guardManifest.runtime_receipt_sha256 -ne (Hash $manifestPath)){throw 'Invalid guard scope'}
function Check-BuildSource([string]$Candidate,$Evidence) {
 if($Evidence.source_commit -notmatch '^[a-f0-9]{40}$' -or $Evidence.build_vcs_modified -ne $false){throw 'Build is not from a clean fixed source'}
 git merge-base --is-ancestor $Evidence.source_commit $ReviewedCommit
 if($LASTEXITCODE -ne 0){throw 'Build source is outside the reviewed history'}
 foreach($entry in $Evidence.source_files.PSObject.Properties){
  git diff --quiet $Evidence.source_commit $ReviewedCommit -- $entry.Name
  if($LASTEXITCODE -ne 0){throw 'Reviewed production input differs from compiled source'}
 }
 $buildInfo=& go version -m $Candidate
 if($LASTEXITCODE -ne 0 -or -not($buildInfo -match ('vcs.revision='+$Evidence.source_commit+'$')) -or -not($buildInfo -match 'vcs.modified=false$') -or -not($buildInfo[0].EndsWith($Evidence.go_version))){throw 'Artifact build identity differs'}
}
foreach($table in @($manifest.source_files,$guardManifest.source_files)){
 foreach($entry in $table.PSObject.Properties){
  if($entry.Name -match '[\\:]' -or $entry.Name -match '(^|/)\.\.(/|$)'){throw 'Invalid input boundary'}
  $path=Join-Path $sourceRoot $entry.Name
  Assert-Plain $path
  if((Hash $path) -ne $entry.Value){throw 'Reviewed input changed'}
 }
}
$release=Join-Path $sourceRoot '.ci-work/3628'
$capacitySource=Join-Path $release 'capacity-audit-3h.exe'
$guardSource=Join-Path $release 'scrub391.exe'
$capacityTarget='D:\CC\Tools\capacity-audit-3h\capacity-audit-3h.exe'
$guardTarget='D:\CC\Tools\client-records\scrub391.exe'
foreach($path in @($release,$capacitySource,$guardSource,$capacityTarget,$guardTarget)){Assert-Plain $path}
if((Hash $capacitySource) -ne $manifest.new_sha256 -or (Hash $capacityTarget) -notin @($manifest.old_sha256,$manifest.new_sha256)){throw 'Concurrent capacity release retained'}
if((Hash $guardSource) -ne $guardManifest.new_sha256 -or (Hash $guardTarget) -notin @($guardManifest.old_sha256,$guardManifest.new_sha256)){throw 'Concurrent classifier release retained'}
Check-BuildSource $capacitySource $manifest
Check-BuildSource $guardSource $guardManifest
if($guardManifest.baseline_source_commit -notmatch '^[a-f0-9]{40}$'){throw 'Classifier baseline source unavailable'}
git merge-base --is-ancestor $guardManifest.baseline_source_commit $guardManifest.source_commit
if($LASTEXITCODE -ne 0){throw 'Classifier baseline history not retained'}
if((Hash $guardTarget) -eq $guardManifest.old_sha256){
 $baselineInfo=& go version -m $guardTarget
 if($LASTEXITCODE -ne 0 -or -not($baselineInfo -match ('vcs.revision='+$guardManifest.baseline_source_commit+'$')) -or -not($baselineInfo -match 'vcs.modified=false$')){throw 'Current classifier build source differs'}
}

function Maintenance-State {
 $audit=Get-ScheduledTask -TaskName 'CC-CapacityAudit-3h'
 $clean=Get-ScheduledTask -TaskName 'CC-CleanupDeletionRequests-8h'
 if($audit.State -eq 'Running' -or $audit.Actions.Count -ne 1 -or $audit.Actions[0].Execute -ne $capacityTarget -or $audit.Triggers.Count -ne 1 -or $audit.Triggers[0].Repetition.Interval -ne 'PT6H' -or -not $audit.Settings.Hidden -or [string]$audit.Settings.MultipleInstances -ne 'IgnoreNew' -or $audit.Settings.ExecutionTimeLimit -ne 'PT5M'){throw 'Six-hour task changed or active'}
 if($clean.Triggers[0].Repetition.Interval -ne 'PT1H'){throw 'Hourly cleaner changed'}
 return [ordered]@{cleaner_sha256=(Hash 'D:\CC\Tools\cleanup-8h\cleanup-8h.exe');cleaner_task=(Export-ScheduledTask -TaskName $clean.TaskName);audit_task=(Export-ScheduledTask -TaskName $audit.TaskName);docker_task=(Export-ScheduledTask -TaskName 'CC-DockerVerificationCleanup')}
}
$before=Maintenance-State

# Bootstrap the exact independently built classifier, preserving all of its
# existing registrations. The new purpose qualifies only the same one binary.
$guardInstall=Install-PinnedRuntime -Source $guardSource -Target $guardTarget -BackupRoot $release -Old $guardManifest.old_sha256 -New $guardManifest.new_sha256
function Check-CapacityQualification {
 $start=[Diagnostics.ProcessStartInfo]::new()
 $start.FileName=$guardTarget
 $start.Arguments='--verify-capacity-runtime-update "'+$capacityTarget+'"'
 $start.UseShellExecute=$false;$start.CreateNoWindow=$true
 $start.RedirectStandardOutput=$true;$start.RedirectStandardError=$true
 $child=[Diagnostics.Process]::Start($start)
 $out=$child.StandardOutput.ReadToEndAsync();$err=$child.StandardError.ReadToEndAsync()
 if(-not $child.WaitForExit(15000)){$child.Kill();$child.WaitForExit();$child.Dispose();throw 'Qualifier timed out'}
 $body=$out.GetAwaiter().GetResult();$diagnostic=$err.GetAwaiter().GetResult();$code=$child.ExitCode;$child.Dispose()
 if($code -ne 0 -or $body.Length -gt 4096 -or $diagnostic.Length -gt 0){throw 'Qualifier unavailable'}
 $result=$body|ConvertFrom-Json
 if($result.qualified -ne $true -or $result.domain -ne $manifest.domain -or $result.todo_id -ne 3628 -or $result.old_sha256 -ne $manifest.old_sha256 -or $result.new_sha256 -ne $manifest.new_sha256 -or $result.source_receipt_sha256 -ne $guardManifest.runtime_receipt_sha256 -or $result.data_access_granted -ne $false -or $result.original_registry_recovered -ne $false){throw 'Exact software purpose not qualified'}
}
Check-CapacityQualification
$capacityInstall=Install-PinnedRuntime -Source $capacitySource -Target $capacityTarget -BackupRoot $release -Old $manifest.old_sha256 -New $manifest.new_sha256
Check-CapacityQualification
$after=Maintenance-State
foreach($name in $before.Keys){if($before[$name] -cne $after[$name]){throw 'Maintenance task or hourly cleaner changed'}}
$receipt=[ordered]@{schema=1;todo_id=3628;reviewed_commit=$ReviewedCommit;deployed_at_utc=[DateTime]::UtcNow.ToString('o');capacity_installation=$capacityInstall;guard_installation=$guardInstall;capacity_sha256=$manifest.new_sha256;guard_sha256=$guardManifest.new_sha256;tasks_created=0;task_configuration_changed=$false;hourly_cleaner_changed=$false;docker_task_changed=$false;data_access_granted=$false;original_registry_recovered=$false}
[IO.File]::WriteAllText((Join-Path $release 'deployed.private.json'),($receipt|ConvertTo-Json -Depth 5),[Text.UTF8Encoding]::new($false))
$receipt|ConvertTo-Json -Compress
