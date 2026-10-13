$ErrorActionPreference = 'Stop'
$root = [IO.Path]::GetFullPath('D:\CC\Work\unattended-operations-20261013\deploy-resident-2490')
$source = [IO.Path]::GetFullPath((Join-Path $root 'fixture-state-2490'))
$destination = [IO.Path]::GetFullPath((Join-Path $root 'fixture-first-recovery-2490'))
if (-not $source.StartsWith($root + '\', [StringComparison]::OrdinalIgnoreCase) -or -not $destination.StartsWith($root + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'fixture_path_out_of_scope' }
if (Test-Path -LiteralPath $destination) { throw 'fixture_archive_exists' }
if (Get-ScheduledTask -TaskName 'CC-ResidentGuard-Fixture-2490' -ErrorAction SilentlyContinue) { throw 'fixture_task_still_registered' }
$directory = Get-Item -LiteralPath $source -Force
if ($directory.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'fixture_indirection' }
$files = @(Get-ChildItem -LiteralPath $source -Force)
if ($files.Count -ne 3 -or @($files | Where-Object { $_.PSIsContainer -or $_.Name -notin @('state.json','status.json','events.jsonl') -or ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) }).Count -ne 0) { throw 'fixture_contents_unexpected' }
$state = Get-Content -LiteralPath (Join-Path $source 'state.json') -Raw -Encoding UTF8 | ConvertFrom-Json
if ($state.services.PSObject.Properties.Name -ne 'fixture') { throw 'fixture_state_unknown_service' }
Move-Item -LiteralPath $source -Destination $destination
[ordered]@{ first_recovery_evidence_preserved = $true } | ConvertTo-Json -Compress
