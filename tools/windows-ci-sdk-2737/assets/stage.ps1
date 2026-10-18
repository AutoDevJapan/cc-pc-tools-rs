$ErrorActionPreference = 'Stop'
$volume = [IO.DriveInfo]::GetDrives() | Where-Object { $_.DriveType -eq 'CDRom' -and $_.IsReady -and $_.VolumeLabel -eq 'CCSDK2737' } | Select-Object -First 1
if (-not $volume) { exit 51 }
$seedRoot = $volume.RootDirectory.FullName
$installRoot = 'C:\ProgramData\AutoDevJapan\windows-ci-sdk-2737'
New-Item -ItemType Directory -Path $installRoot -ErrorAction Stop | Out-Null
$exe = Join-Path $installRoot 'windows-ci-sdk-agent.exe'
Copy-Item -LiteralPath (Join-Path $seedRoot 'windows-ci-sdk-agent.exe') -Destination $exe -ErrorAction Stop
Copy-Item -LiteralPath (Join-Path $seedRoot 'seed.json') -Destination (Join-Path $installRoot 'seed.json') -ErrorAction Stop
$icacls = Join-Path $env:SystemRoot 'System32\icacls.exe'
& $icacls $installRoot '/inheritance:r' '/grant:r' '*S-1-5-18:(OI)(CI)F' '/T' '/C' | Out-Null
if ($LASTEXITCODE -ne 0) { exit 52 }
$service = New-Service -Name 'WindowsCISDK2737' -BinaryPathName ('"' + $exe + '"') -StartupType Automatic -DisplayName 'Windows CI SDK preparation guest service'
if (-not $service) { exit 53 }
$cim = Get-CimInstance Win32_Service -Filter "Name='WindowsCISDK2737'"
if (-not $cim -or $cim.StartName -ne 'LocalSystem' -or $cim.StartMode -ne 'Auto') { exit 54 }
Start-Service -Name 'WindowsCISDK2737'
$deadline = (Get-Date).AddSeconds(30)
do { $service = Get-Service -Name 'WindowsCISDK2737'; if ($service.Status -eq 'Running') { break }; Start-Sleep -Milliseconds 100 } while ((Get-Date) -lt $deadline)
if ($service.Status -ne 'Running') { exit 55 }
exit 0
