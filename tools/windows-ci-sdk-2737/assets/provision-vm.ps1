$ErrorActionPreference = 'Stop'
Import-Module Hyper-V
Import-Module Storage
Import-Module Dism
$root = [IO.Path]::GetFullPath('__CCSDK2737_OWNED_ROOT__')
$configPath = Join-Path $root 'provision.json'
$cfg = Get-Content -Raw -LiteralPath $configPath | ConvertFrom-Json
if ([IO.Path]::GetFullPath($cfg.root) -cne $root) { throw 'OWNED_ROOT_MISMATCH' }
if ($cfg.instance -notmatch '^[A-Za-z0-9_-]{1,128}$') { throw 'INSTANCE_REJECTED' }
$name = 'cc-sdk-2737-' + $cfg.instance
$pipe = '\\.\pipe\windows-ci-sdk-2737-' + $cfg.instance
$vhd = Join-Path $root 'guest.vhdx'
foreach ($p in @($cfg.iso, $cfg.seedISO)) {
  $full = [IO.Path]::GetFullPath($p)
  if (-not $full.StartsWith($root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'MEDIA_PATH_ESCAPED_ROOT' }
}
if (Get-VM -Name $name -ErrorAction SilentlyContinue) { throw 'OWNED_VM_NAME_ALREADY_EXISTS' }
$switch = Get-VMSwitch -Name 'Default Switch' -ErrorAction Stop
if (-not (Test-Path -LiteralPath $cfg.iso -PathType Leaf) -or -not (Test-Path -LiteralPath $cfg.seedISO -PathType Leaf)) { throw 'OWNED_MEDIA_MISSING' }
$image = Mount-DiskImage -ImagePath $cfg.iso -StorageType ISO -PassThru
try {
  $vol = @($image | Get-Volume | Where-Object DriveLetter | Select-Object -First 1)
  if (-not $vol) { throw 'OFFICIAL_ISO_VOLUME_MISSING' }
  $drive = $vol[0].DriveLetter + ':\'
  $setup = Get-AuthenticodeSignature -LiteralPath (Join-Path $drive 'setup.exe')
  $signer = $null
  if ($setup.SignerCertificate) { $signer = $setup.SignerCertificate.GetNameInfo([Security.Cryptography.X509Certificates.X509NameType]::SimpleName, $false) }
  if ($setup.Status -ne 'Valid' -or $signer -ne 'Microsoft Corporation') { throw 'MICROSOFT_SETUP_SIGNATURE_INVALID' }
  $wim = Join-Path $drive 'sources\install.wim'
  $edition = Get-WindowsImage -ImagePath $wim | Where-Object ImageName -CEQ 'Windows Server 2025 Standard Evaluation' | Select-Object -First 1
  if (-not $edition) { throw 'EXPECTED_EVALUATION_IMAGE_MISSING' }
} finally { Dismount-DiskImage -ImagePath $cfg.iso -ErrorAction SilentlyContinue | Out-Null }
$seedImage = Mount-DiskImage -ImagePath $cfg.seedISO -StorageType ISO -PassThru
try {
  $seedVol = @($seedImage | Get-Volume | Where-Object DriveLetter | Select-Object -First 1)
  if (-not $seedVol -or $seedVol[0].FileSystemLabel -cne 'CCSDK2737') { throw 'OWNED_SEED_LABEL_REJECTED' }
  $seedDrive = $seedVol[0].DriveLetter + ':\'
  foreach ($file in @('Autounattend.xml', 'stage.ps1', 'windows-ci-sdk-agent.exe', 'seed.json')) { if (-not (Test-Path -LiteralPath (Join-Path $seedDrive $file) -PathType Leaf)) { throw 'OWNED_SEED_FILE_MISSING' } }
  $answer = [xml](Get-Content -Raw -LiteralPath (Join-Path $seedDrive 'Autounattend.xml'))
  if ($answer.unattend.settings | Where-Object pass -EQ 'oobeSystem') { throw 'OOBE_SETTINGS_FORBIDDEN' }
  if ($answer.OuterXml -match 'SkipMachineOOBE|UserAccounts|AdministratorPassword|<Password>') { throw 'ACCOUNT_OR_OOBE_BYPASS_FORBIDDEN' }
  $seed = Get-Content -Raw -LiteralPath (Join-Path $seedDrive 'seed.json') | ConvertFrom-Json
  if (@($seed.PSObject.Properties).Count -ne 2 -or $seed.instance -cne $cfg.instance -or $seed.clientCertificateSHA256 -notmatch '^[0-9a-f]{64}$') { throw 'PUBLIC_SEED_REJECTED' }
} finally { Dismount-DiskImage -ImagePath $cfg.seedISO -ErrorAction SilentlyContinue | Out-Null }
if (Test-Path -LiteralPath $vhd) { throw 'OWNED_GUEST_VHD_ALREADY_EXISTS' }
$vm = $null
$phase = 'create-vhd'
try {
  New-VHD -Path $vhd -Dynamic -SizeBytes 64GB | Out-Null
  $phase = 'create-vm'
  $vm = New-VM -Name $name -Generation 1 -MemoryStartupBytes 4GB -VHDPath $vhd -Path (Join-Path $root 'vm')
  $receipt = @{instance=$cfg.instance;vm_id=$vm.Id.ToString();name=$name;root=$root;vhd=$vhd;windows_iso=$cfg.iso;seed_iso=$cfg.seedISO;pipe=$pipe;generation=1;cpu_count=0;startup_memory_bytes=0;dynamic_memory=$true;network_adapters=-1;gpu_adapters=-1;state='Off'}
  $receipt | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $root 'vm-created.private.json') -Encoding utf8
  $phase = 'configure-vm'
  Set-VM -VM $vm -AutomaticStartAction Nothing -AutomaticStopAction TurnOff -CheckpointType Disabled -Notes ('todo2783;instance=' + $cfg.instance)
  Set-VMProcessor -VM $vm -Count 2
  Set-VMMemory -VM $vm -DynamicMemoryEnabled $false -StartupBytes 4GB
  foreach ($adapter in @($vm | Get-VMNetworkAdapter)) { Remove-VMNetworkAdapter -VMNetworkAdapter $adapter }
  Add-VMNetworkAdapter -VM $vm -SwitchName $switch.Name -Name 'SDKGuestNetwork' | Out-Null
  $dvds = @($vm | Get-VMDvdDrive)
  if ($dvds.Count -gt 0) { Set-VMDvdDrive -VMDvdDrive $dvds[0] -Path $cfg.iso | Out-Null } else { Add-VMDvdDrive -VM $vm -Path $cfg.iso | Out-Null }
  Add-VMDvdDrive -VM $vm -Path $cfg.seedISO | Out-Null
  Set-VMComPort -VM $vm -Number 1 -Path $pipe
  Set-VMBios -VM $vm -StartupOrder @('IDE','CD','LegacyNetworkAdapter','Floppy')
  $vm = Get-VM -Id $vm.Id
  $memory = Get-VMMemory -VM $vm
  $nics = @($vm | Get-VMNetworkAdapter)
  $gpus = @($vm | Get-VMGpuPartitionAdapter -ErrorAction SilentlyContinue)
  $dvds = @($vm | Get-VMDvdDrive)
  $disks = @($vm | Get-VMHardDiskDrive)
  if ($vm.State -ne 'Off' -or $memory.DynamicMemoryEnabled -or $memory.Startup -ne 4GB -or $vm.ProcessorCount -ne 2 -or $nics.Count -ne 1 -or $nics[0].SwitchName -cne 'Default Switch' -or $gpus.Count -ne 0 -or $dvds.Count -ne 2 -or $disks.Count -ne 1 -or $disks[0].Path -cne $vhd) { throw 'NEW_VM_RESOURCE_CONFIGURATION_REJECTED' }
  $receipt = @{instance=$cfg.instance;vm_id=$vm.Id.ToString();name=$name;root=$root;vhd=$vhd;windows_iso=$cfg.iso;seed_iso=$cfg.seedISO;pipe=$pipe;generation=1;cpu_count=2;startup_memory_bytes=4294967296;dynamic_memory=$false;network_adapters=1;gpu_adapters=0;state='Off'}
  $receipt | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $root 'vm-created.private.json') -Encoding utf8
  $phase = 'start-vm'
  Start-VM -VM $vm | Out-Null
  $deadline = (Get-Date).AddMinutes(2)
  do { $vm = Get-VM -Id $vm.Id; if ($vm.State -eq 'Running') { break }; Start-Sleep -Milliseconds 250 } while ((Get-Date) -lt $deadline)
  if ($vm.State -ne 'Running') { throw 'OWNED_VM_START_TIMEOUT' }
  $receipt.state = 'Running'
  $receipt | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $root 'vm-created.private.json') -Encoding utf8
} catch {
  @{phase=$phase;failure='fresh owned Windows SDK guest provisioning failed';error_id=$_.FullyQualifiedErrorId;error_detail=$_.Exception.Message} | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath (Join-Path $root 'vm-provision-failure.private.json') -Encoding utf8
  throw 'OWNED_WINDOWS_SDK_GUEST_PROVISION_FAILED'
}
