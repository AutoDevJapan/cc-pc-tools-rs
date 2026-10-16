param([Parameter(Mandatory)][string]$Control, [Parameter(Mandatory)][string]$SourceRoot, [Parameter(Mandatory)][string]$Sdk)
$ErrorActionPreference='Stop'
$exe=Join-Path $Control 'ci2730-v2.exe'
$runtime=Join-Path $Control 'windows/.ci-runtime'
$service=Join-Path $runtime 'service/ci2730.exe'
$worker=Join-Path $runtime 'windows/bin/Runner.Listener.exe'
foreach($path in @((Split-Path $service),(Split-Path $worker),(Join-Path $Control 'windows/.ci-work'))){[IO.Directory]::CreateDirectory($path)|Out-Null}
Copy-Item -LiteralPath $exe -Destination $service
Copy-Item -LiteralPath $exe -Destination $worker
$planPath=Join-Path $SourceRoot '.ci-work/repo1392152644/plan.private.json'
$plan=Get-Content -LiteralPath $planPath -Raw | ConvertFrom-Json
$plan.Version='1.95.0'
$plan.SDKRoot=$Sdk
$plan.Smoke='normal'
$plan.Checks=@()
$plan | ConvertTo-Json -Depth 30 | Set-Content -LiteralPath $planPath -Encoding utf8
& $exe win-control-tree $planPath
if($LASTEXITCODE -ne 0){throw 'Control Git tree failed'}
& $exe win-install-head $planPath (Join-Path $Control 'repo1392152644.private.json')
if($LASTEXITCODE -ne 0){throw 'Fixed HEAD transfer failed'}
Copy-Item -LiteralPath (Join-Path $Sdk 'rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/lib/rustlib/x86_64-pc-windows-msvc/bin/rust-lld.exe') -Destination (Join-Path $Sdk 'link.exe')
$setup=@'
param()
$ErrorActionPreference='Stop'
$ctl='@CTL@'
$src='@SRC@'
$sdk='@SDK@'
$name='CC-CI2730-Finite'
if(Get-Service -Name $name -ErrorAction SilentlyContinue){throw 'Service already exists'}
& sc.exe create $name binPath= ('"'+$ctl+'\windows\.ci-runtime\service\ci2730.exe" win-service') start= demand obj= ('NT SERVICE\'+$name)
if($LASTEXITCODE -ne 0){throw 'Service create failed'}
$sid=([Security.Principal.NTAccount]::new('NT SERVICE',$name)).Translate([Security.Principal.SecurityIdentifier]).Value
$grants=@(
 @{path=$ctl;rights='(OI)(CI)RX'},
 @{path=($ctl+'\windows\.ci-runtime\windows');rights='(OI)(CI)M'},
 @{path=($ctl+'\windows\.ci-work');rights='(OI)(CI)M'},
 @{path=$src;rights='(OI)(CI)M'},
 @{path=$sdk;rights='(OI)(CI)M'},
 @{path='D:\CC\Tools\sccache';rights='(OI)(CI)RX'},
 @{path='D:\CC\BuildCache\sccache';rights='(OI)(CI)M'},
 @{path='C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools';rights='(OI)(CI)RX'},
 @{path='C:\Program Files (x86)\Windows Kits\10';rights='(OI)(CI)RX'},
 @{path='C:\Program Files\PowerShell\7';rights='(OI)(CI)RX'}
)
$receipt=@{service=$name;sid=$sid;grants=$grants;profile_before=(Test-Path ('Registry::HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\'+$sid));hive_before=(Test-Path ('Registry::HKEY_USERS\'+$sid))}
$receipt|ConvertTo-Json -Depth 10|Set-Content -LiteralPath ($ctl+'\service-ownership.private.json') -Encoding utf8
if($receipt.profile_before -or $receipt.hive_before){throw 'Profile already exists'}
foreach($grant in $grants){ & icacls.exe $grant.path /grant ('*'+$sid+':'+$grant.rights) /q; if($LASTEXITCODE -ne 0){throw 'SID grant failed'} }
Write-Output 'finite_service_created=true profile_registered=false hive_loaded=false'
'@
$setup=$setup.Replace('@CTL@',$Control.Replace('/','\')).Replace('@SRC@',$SourceRoot.Replace('/','\')).Replace('@SDK@',$Sdk.Replace('/','\'))
[IO.File]::WriteAllText((Join-Path $Control 'service-setup.private.ps1'),$setup,[Text.UTF8Encoding]::new($false))
Write-Output 'prepared=true'
