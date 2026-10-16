param([Parameter(Mandatory)][string]$Control, [Parameter(Mandatory)][string]$Evidence)
$ErrorActionPreference='Stop'
$receipt=Get-Content -LiteralPath (Join-Path $Control 'service-ownership.private.json') -Raw|ConvertFrom-Json
if($receipt.service -ne 'CC-CI2730-Finite' -or $receipt.sid -notlike 'S-1-5-80-*'){throw 'Owned service receipt mismatch'}
$service=Get-CimInstance Win32_Service -Filter "Name='CC-CI2730-Finite'"
$expected=Join-Path $Control 'windows/.ci-runtime/service/ci2730.exe'
if(!$service -or $service.State -ne 'Stopped' -or $service.ProcessId -ne 0 -or !$service.PathName.Replace('/','\').Contains($expected.Replace('/','\'))){throw 'Owned service not stopped or identity mismatch'}
$prefix=[IO.Path]::GetFullPath($Control).TrimEnd('\')+'\'
$owned=@(Get-CimInstance Win32_Process|Where-Object {$_.ExecutablePath -and $_.ExecutablePath.StartsWith($prefix,[StringComparison]::OrdinalIgnoreCase)})
if($owned.Count -ne 0){throw 'Owned executable still running'}
$removed=@()
foreach($grant in $receipt.grants){
  & icacls.exe $grant.path /remove:g ('*'+$receipt.sid) /q
  if($LASTEXITCODE -ne 0){throw 'Own SID grant removal failed'}
  $remaining=@((Get-Acl -LiteralPath $grant.path).GetAccessRules($true,$false,[Security.Principal.SecurityIdentifier])|Where-Object {$_.IdentityReference.Value -eq $receipt.sid})
  if($remaining.Count -ne 0){throw 'Own SID explicit grant remains'}
  $removed+=$grant.path
}
& sc.exe delete $receipt.service
if($LASTEXITCODE -ne 0){throw 'Own service deletion failed'}
if(Get-Service -Name $receipt.service -ErrorAction SilentlyContinue){throw 'Own service still registered'}
$profile=Test-Path ('Registry::HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\'+$receipt.sid)
$hive=Test-Path ('Registry::HKEY_USERS\'+$receipt.sid)
$observation=@{profile_registered=$profile;hive_loaded=$hive;service_deleted=$true;explicit_grants_removed=$removed;at=[DateTimeOffset]::Now.ToString('o')}
$observation|ConvertTo-Json -Depth 8|Set-Content -LiteralPath (Join-Path $Evidence 'windows-immediate-cleanup.private.json') -Encoding utf8
if($profile -or $hive){throw 'Unexpected virtual service profile/hive'}
$result=@{service=$receipt.service;sid=$receipt.sid;stopped_pid_zero=$true;owned_executable_processes=0;removed_explicit_grants=$removed;service_absent=$true;profile_registered=$profile;hive_loaded=$hive;at=[DateTimeOffset]::Now.ToString('o')}
$result|ConvertTo-Json -Depth 8|Set-Content -LiteralPath (Join-Path $Evidence 'windows-cleanup.private.json') -Encoding utf8
Write-Output ('owned_service_removed=true own_sid_grants_removed='+$removed.Count+' profile_registered=false hive_loaded=false')
