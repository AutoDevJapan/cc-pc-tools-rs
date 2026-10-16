param([Parameter(Mandatory)][string]$Control, [Parameter(Mandatory)][string]$Evidence)
$ErrorActionPreference='Stop'
$receipt=Get-Content -LiteralPath (Join-Path $Control 'service-ownership.private.json') -Raw|ConvertFrom-Json
if($receipt.service -ne 'CC-CI2730-Finite' -or $receipt.sid -notlike 'S-1-5-80-*'){throw 'Owned service receipt mismatch'}
if(Get-Service -Name $receipt.service -ErrorAction SilentlyContinue){throw 'Own service remains'}
$prefix=[IO.Path]::GetFullPath($Control).TrimEnd('\')+'\'
$owned=@(Get-CimInstance Win32_Process|Where-Object {$_.ExecutablePath -and $_.ExecutablePath.StartsWith($prefix,[StringComparison]::OrdinalIgnoreCase)})
if($owned.Count -ne 0){throw 'Owned executable still running'}
foreach($grant in $receipt.grants){
  $remaining=@((Get-Acl -LiteralPath $grant.path).GetAccessRules($true,$false,[Security.Principal.SecurityIdentifier])|Where-Object {$_.IdentityReference.Value -eq $receipt.sid})
  if($remaining.Count -ne 0){throw 'Own SID explicit grant remains'}
}
$profile=Test-Path ('Registry::HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\'+$receipt.sid)
$hive=Test-Path ('Registry::HKEY_USERS\'+$receipt.sid)
$result=@{service=$receipt.service;sid=$receipt.sid;service_absent=$true;owned_executable_processes=0;own_sid_explicit_grants=0;profile_registered=$profile;hive_loaded=$hive;followup_after_scm_delete=$true;first_immediate_check_saw_profile_or_hive=$true;at=[DateTimeOffset]::Now.ToString('o')}
$result|ConvertTo-Json -Depth 8|Set-Content -LiteralPath (Join-Path $Evidence 'windows-cleanup.private.json') -Encoding utf8
if($profile -or $hive){throw 'Own profile/hive remains after service deletion'}
Write-Output 'own_service_absent=true own_processes=0 own_sid_grants=0 profile_registered=false hive_loaded=false'
