//go:build windows && amd64

package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	evalVHDBytes  = int64(11686379520)
	evalVHDMethod = "microsoft-evaluation-vhd-offline-service"
)

func runVMProvisionFromVHD(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("owned evaluation-VHD root is not a real directory")
	}
	source := filepath.Join(root, "windows-server-2025-eval.vhdx")
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != evalVHDBytes {
		return errors.New("official evaluation VHD is unavailable or has an unexpected length")
	}
	if exists, err := ownedPathExists(root, "vm-fixture-evidence.private.json"); err != nil {
		return errors.New("owned VHD fixture evidence state could not be inspected")
	} else if exists {
		return errors.New("owned VHD fixture evidence already exists; refusing to repeat a completed fixture")
	}
	if _, err := os.Stat(filepath.Join(root, "vm-created.private.json")); err == nil {
		oldVM, readErr := readVMReceipt(root)
		if readErr != nil || validatePartialPrebuiltVMReceipt(oldVM, root) != nil {
			return errors.New("existing VHD VM receipt does not identify this fixture")
		}
		if _, cleanupErr := cleanupOwnedVM(root, oldVM); cleanupErr != nil {
			return errors.New("previous owned VHD VM could not be verified removed; preserve its receipt and root")
		}
		if err := archiveVHDVMAttempt(root, oldVM.Instance); err != nil {
			return errors.New("previous VHD VM receipts could not be preserved")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("owned VHD VM receipt could not be inspected")
	} else if err := archiveOrphanVHDFailures(root); err != nil {
		return errors.New("previous VHD provisioning failures could not be preserved")
	}

	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return err
	}
	instanceHash := sha256.Sum256(entropy[:])
	instance := hex.EncodeToString(instanceHash[:16])
	clear(entropy[:])
	client, err := newVMIdentity()
	if err != nil {
		return err
	}
	defer clearIdentity(&client)
	seedISO, err := createGuestSeed(root, instance, client, instance)
	if err != nil {
		return err
	}
	seedJSON := filepath.Join(root, "seed", "cc2746-seed.json")
	exe := filepath.Join(root, "seed", "cc2746-guest.exe")
	if _, err := os.Stat(exe); err != nil {
		return errors.New("owned seed helper copy is unavailable")
	}
	guestVHD := filepath.Join(root, "guest-"+instance+".vhdx")
	if _, err := os.Lstat(guestVHD); err == nil {
		return errors.New("nonce-owned evaluation VHD destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("nonce-owned evaluation VHD destination could not be inspected")
	}
	script := filepath.Join(root, "vm-create-vhd.ps1")
	if err := os.WriteFile(script, []byte(createPrebuiltVHDVMPS(root, source, guestVHD, seedJSON, exe, seedISO, instance)), 0600); err != nil {
		return err
	}
	if err := runAdminBroker(script, 1800); err != nil {
		return errors.New("owned Microsoft evaluation VHD provisioning failed; retain its receipts and VHDX")
	}
	vm, err := readVMReceipt(root)
	if err != nil || vm.ImageDeploymentMethod != evalVHDMethod || validateVMReceipt(vm, root, instance) != nil {
		return errors.New("owned evaluation VHD VM receipt could not be verified")
	}
	return runVMFixturesAndRecord(root, source, info.Size(), vm, client)
}

func validatePartialPrebuiltVMReceipt(vm vmReceipt, root string) error {
	if !validVMID(vm.ID) || !validInstance(vm.Instance) || vm.Name != "cc-2746-"+vm.Instance || vm.Generation != 2 ||
		!strings.EqualFold(filepath.Clean(vm.Root), filepath.Clean(root)) || filepath.Clean(vm.VHD) != filepath.Join(root, "guest-"+vm.Instance+".vhdx") ||
		filepath.Clean(vm.ImageSource) != filepath.Join(root, "windows-server-2025-eval.vhdx") || vm.ImageDeploymentMethod != evalVHDMethod ||
		vm.WindowsISO != "" || vm.SeedISO != "" || vm.DVDCount != 0 || vm.VHDCount != 1 || vm.Pipe != `\\.\pipe\cc-2746-`+vm.Instance {
		return errors.New("partial prebuilt evaluation VM receipt does not identify this owned resource")
	}
	return nil
}

func createPrebuiltVHDVMPS(root, source, guestVHD, seedJSON, exe, seedISO, instance string) string {
	q := psQuote
	return `$ErrorActionPreference='Stop'
Import-Module Hyper-V
Import-Module Storage
$root=` + q(root) + `
$source=` + q(source) + `
$guestVHD=` + q(guestVHD) + `
$seedJSON=` + q(seedJSON) + `
$exe=` + q(exe) + `
$seedISO=` + q(seedISO) + `
$instance=` + q(instance) + `
$method=` + q(evalVHDMethod) + `
$name='cc-2746-'+$instance
$pipe='\\.\pipe\cc-2746-'+$instance
if(Get-VM -Name $name -ErrorAction SilentlyContinue){throw 'OWNED_VHD_VM_NAME_ALREADY_EXISTS'}
$rootFull=[IO.Path]::GetFullPath($root).TrimEnd('\')
foreach($path in @($source,$guestVHD,$seedJSON,$exe,$seedISO)){$full=[IO.Path]::GetFullPath($path);if(-not $full.StartsWith($rootFull+'\',[StringComparison]::OrdinalIgnoreCase)){throw 'OWNED_VHD_SOURCE_CONTAINMENT_REJECTED'}}
$sourceFile=Get-Item -LiteralPath $source -ErrorAction Stop
if($sourceFile.Length -ne 11686379520){throw 'OFFICIAL_EVALUATION_VHD_LENGTH_REJECTED'}
$sourceDisk=Get-VHD -Path $source -ErrorAction Stop
if($sourceDisk.Attached -or $sourceDisk.VhdType -ne 'Dynamic' -or $sourceDisk.Size -ne 64GB){throw 'OFFICIAL_EVALUATION_VHD_TYPE_REJECTED'}
if(Test-Path -LiteralPath $guestVHD){throw 'OWNED_GUEST_VHD_DESTINATION_EXISTS'}
if(Test-Path -LiteralPath (Join-Path $root 'vm-created.private.json')){throw 'OWNED_VHD_VM_RECEIPT_ALREADY_EXISTS'}
$mounted=$false;$diskNumber=$null;$accessPath=$null;$accessPartition=$null;$loadedHives=[Collections.Generic.List[string]]::new()
try {
  Convert-VHD -Path $source -DestinationPath $guestVHD -VHDType Dynamic -ErrorAction Stop|Out-Null
  $disk=Get-VHD -Path $guestVHD -ErrorAction Stop
  if($disk.Attached -or $disk.VhdType -ne 'Dynamic' -or $disk.Size -ne 64GB){throw 'OWNED_GUEST_VHD_CONVERSION_REJECTED'}
  Mount-VHD -Path $guestVHD -NoDriveLetter|Out-Null
  $mounted=$true
  $image=Get-DiskImage -ImagePath $guestVHD -StorageType VHDX -ErrorAction Stop
  $disks=@($image|Get-Disk -ErrorAction Stop)
  if($disks.Count -ne 1){throw 'OWNED_GUEST_VHD_DISK_IDENTITY_REJECTED'}
  $diskNumber=$disks[0].Number
  $parts=@(Get-Partition -DiskNumber $diskNumber -ErrorAction Stop)
  $efi=@($parts|Where-Object{[String]::Equals([string]$_.GptType,'{C12A7328-F81F-11D2-BA4B-00A0C93EC93B}',[StringComparison]::OrdinalIgnoreCase)})
  $msr=@($parts|Where-Object{[String]::Equals([string]$_.GptType,'{E3C9E316-0B5C-4DB8-817D-F92DF00215AE}',[StringComparison]::OrdinalIgnoreCase)})
  $os=@($parts|Where-Object{[String]::Equals([string]$_.GptType,'{EBD0A0A2-B9E5-4433-87C0-68B6B72699C7}',[StringComparison]::OrdinalIgnoreCase)})
  if($parts.Count -ne 3 -or $efi.Count -ne 1 -or $msr.Count -ne 1 -or $os.Count -ne 1 -or $efi[0].Size -ne 200MB -or $msr[0].Size -ne 128MB -or $os[0].Size -lt 60GB){throw 'OWNED_GUEST_VHD_PARTITION_LAYOUT_REJECTED'}
  $osVolume=$os[0]|Get-Volume -ErrorAction Stop
  if($osVolume.FileSystem -cne 'NTFS'){throw 'OWNED_GUEST_VHD_OS_FILESYSTEM_REJECTED'}
  if(-not $os[0].DriveLetter){Add-PartitionAccessPath -DiskNumber $diskNumber -PartitionNumber $os[0].PartitionNumber -AssignDriveLetter;$accessPartition=$os[0].PartitionNumber}
  $os=Get-Partition -DiskNumber $diskNumber -PartitionNumber $os[0].PartitionNumber -ErrorAction Stop
  if(-not $os.DriveLetter){throw 'OWNED_GUEST_VHD_OS_LETTER_MISSING'}
  $accessPath=([string]$os.DriveLetter)+':\'
  $system32=Join-Path $accessPath 'Windows\System32'
  $systemHive=Join-Path $system32 'Config\SYSTEM'
  $softwareHive=Join-Path $system32 'Config\SOFTWARE'
  foreach($file in @($systemHive,$softwareHive,$seedJSON,$exe)){if(-not(Test-Path -LiteralPath $file)){throw 'OWNED_GUEST_VHD_REQUIRED_FILE_MISSING'}}
  $softName='CC2746Soft_'+[guid]::NewGuid().ToString('N')
  $reg=Join-Path $env:SystemRoot 'System32\reg.exe'
  $load=& $reg load ('HKLM\'+$softName) $softwareHive 2>&1
  if($LASTEXITCODE -ne 0){throw 'OWNED_GUEST_SOFTWARE_HIVE_LOAD_FAILED'}
  $loadedHives.Add($softName)
  try{$state=Get-ItemProperty -LiteralPath ('Registry::HKEY_LOCAL_MACHINE\'+$softName+'\Microsoft\Windows\CurrentVersion\Setup\State') -ErrorAction Stop;$cv=Get-ItemProperty -LiteralPath ('Registry::HKEY_LOCAL_MACHINE\'+$softName+'\Microsoft\Windows NT\CurrentVersion') -ErrorAction Stop;if($state.ImageState -cne 'IMAGE_STATE_GENERALIZE_RESEAL_TO_OOBE' -or $cv.ProductName -cne 'Windows Server 2025 Datacenter Evaluation'){throw 'OFFICIAL_EVAL_IMAGE_STATE_REJECTED'}}finally{$unload=& $reg unload ('HKLM\'+$softName) 2>&1;if($LASTEXITCODE -ne 0){throw 'OWNED_GUEST_SOFTWARE_HIVE_UNLOAD_FAILED'};$loadedHives.Remove($softName)|Out-Null}
  Copy-Item -LiteralPath $exe -Destination (Join-Path $system32 'cc2746-guest.exe') -Force
  Copy-Item -LiteralPath $seedJSON -Destination (Join-Path $system32 'cc2746-seed.json') -Force
  $systemName='CC2746System_'+[guid]::NewGuid().ToString('N')
  $load=& $reg load ('HKLM\'+$systemName) $systemHive 2>&1
  if($LASTEXITCODE -ne 0){throw 'OWNED_GUEST_SYSTEM_HIVE_LOAD_FAILED'}
  $loadedHives.Add($systemName)
  $registry=[Microsoft.Win32.Registry]::LocalMachine.OpenSubKey($systemName,$true)
  if(-not $registry){throw 'OWNED_GUEST_SYSTEM_HIVE_OPEN_FAILED'}
  try{$current=[int]$registry.OpenSubKey('Select',$false).GetValue('Current',0);if($current -lt 1 -or $current -gt 999){throw 'OWNED_GUEST_CURRENT_CONTROL_SET_REJECTED'};$control='ControlSet{0:D3}' -f $current;$services=$registry.OpenSubKey(($control+'\Services'),$true);if(-not $services){throw 'OWNED_GUEST_SERVICES_KEY_MISSING'};try{$existing=$services.OpenSubKey('CCGuest2746',$false);if($existing){$same=([int]$existing.GetValue('Type',0) -eq 16) -and ([int]$existing.GetValue('Start',0) -eq 2) -and ([int]$existing.GetValue('ErrorControl',0) -eq 1) -and ([string]$existing.GetValue('ObjectName','') -ceq 'LocalSystem') -and ([string]$existing.GetValue('ImagePath','') -ceq '\SystemRoot\System32\cc2746-guest.exe --service');$existing.Dispose();if(-not $same){throw 'OWNED_GUEST_SERVICE_COLLISION'}}else{$service=$services.CreateSubKey('CCGuest2746');if(-not $service){throw 'OWNED_GUEST_SERVICE_CREATE_FAILED'};try{$service.SetValue('Type',16,[Microsoft.Win32.RegistryValueKind]::DWord);$service.SetValue('Start',2,[Microsoft.Win32.RegistryValueKind]::DWord);$service.SetValue('ErrorControl',1,[Microsoft.Win32.RegistryValueKind]::DWord);$service.SetValue('ObjectName','LocalSystem',[Microsoft.Win32.RegistryValueKind]::String);$service.SetValue('ImagePath','\SystemRoot\System32\cc2746-guest.exe --service',[Microsoft.Win32.RegistryValueKind]::ExpandString);$service.SetValue('DisplayName','ToDo 2746 guest fixture',[Microsoft.Win32.RegistryValueKind]::String);$service.SetValue('Description','Credential-free finite Windows guest fixture',[Microsoft.Win32.RegistryValueKind]::String);$service.Flush()}finally{$service.Dispose()}}}finally{$services.Dispose()}}finally{$registry.Dispose()}
  $marker=@{schema='cc2746-offline-service-v1';instance=$instance;image_state=[string]$state.ImageState;image_deployment_method=$method}|ConvertTo-Json -Compress
  Set-Content -LiteralPath (Join-Path $system32 'cc2746-fixture-state.json') -Value $marker -Encoding ascii
  $unload=& $reg unload ('HKLM\'+$systemName) 2>&1
  if($LASTEXITCODE -ne 0){throw 'OWNED_GUEST_SYSTEM_HIVE_UNLOAD_FAILED'}
  $loadedHives.Remove($systemName)|Out-Null
  if($accessPartition){Remove-PartitionAccessPath -DiskNumber $diskNumber -PartitionNumber $accessPartition -AccessPath $accessPath -ErrorAction Stop;$accessPath=$null;$accessPartition=$null}
  Dismount-VHD -Path $guestVHD -ErrorAction Stop|Out-Null
  $mounted=$false
} catch {
  @{phase='vhd-provision';failure='owned Microsoft evaluation VHD provisioning failed'}|ConvertTo-Json|Set-Content -LiteralPath (Join-Path $root 'vm-provision-failure.private.json') -Encoding utf8
  throw
} finally {
  $reg=Join-Path $env:SystemRoot 'System32\reg.exe'
  for($i=$loadedHives.Count-1;$i -ge 0;$i--){$null=& $reg unload ('HKLM\'+$loadedHives[$i]) 2>&1}
  if($mounted){if($accessPartition){$part=Get-Partition -DiskNumber $diskNumber -PartitionNumber $accessPartition -ErrorAction SilentlyContinue;if($part -and $part.DriveLetter){$assigned=([string]$part.DriveLetter)+':\';Remove-PartitionAccessPath -DiskNumber $diskNumber -PartitionNumber $accessPartition -AccessPath $assigned -ErrorAction SilentlyContinue|Out-Null}};Dismount-VHD -Path $guestVHD -ErrorAction SilentlyContinue|Out-Null}
}
$vm=New-VM -Name $name -Generation 2 -MemoryStartupBytes 2GB -VHDPath $guestVHD -Path (Join-Path $root 'vm')
$initial=@{instance=$instance;vm_id=$vm.Id.ToString();name=$vm.Name;generation=$vm.Generation;root=$root;vhd=$guestVHD;windows_iso='';seed_iso='';image_source=$source;image_deployment_method=$method;dvd_count=0;vhd_count=1;pipe=$pipe;cpu_count=0;memory_bytes=0;dynamic_memory=$true;secure_boot=$false;network_adapters=-1;gpu_adapters=-1}
$initial|ConvertTo-Json -Depth 4|Set-Content -LiteralPath (Join-Path $root 'vm-created.private.json') -Encoding utf8
Set-VM -VM $vm -AutomaticStartAction Nothing -AutomaticStopAction TurnOff -CheckpointType Disabled -Notes ('cc.todo=2746;cc.instance='+$instance)
Set-VMProcessor -VM $vm -Count 2
Set-VMMemory -VM $vm -DynamicMemoryEnabled $false -StartupBytes 2GB
foreach($adapter in @($vm|Get-VMNetworkAdapter)){Remove-VMNetworkAdapter -VMNetworkAdapter $adapter}
foreach($dvd in @($vm|Get-VMDvdDrive)){Remove-VMDvdDrive -VMDvdDrive $dvd}
Set-VMComPort -VM $vm -Number 1 -Path $pipe
Set-VMFirmware -VM $vm -EnableSecureBoot On -SecureBootTemplate MicrosoftWindows
$vm=Get-VM -Id $vm.Id;$memory=Get-VMMemory -VM $vm;$nics=@($vm|Get-VMNetworkAdapter).Count;$gpus=@($vm|Get-VMGpuPartitionAdapter -ErrorAction SilentlyContinue).Count;$dvds=@($vm|Get-VMDvdDrive);$vhds=@($vm|Get-VMHardDiskDrive)
if($vm.State -ne 'Off' -or $memory.DynamicMemoryEnabled -or $memory.Startup -ne 2GB -or $vm.ProcessorCount -ne 2 -or $nics -ne 0 -or $gpus -ne 0 -or $dvds.Count -ne 0 -or $vhds.Count -ne 1 -or $vhds[0].Path -cne $guestVHD){throw 'NEW_VHD_VM_RESOURCE_CONFIGURATION_REJECTED'}
Set-VMFirmware -VM $vm -BootOrder @($vhds[0]) -PauseAfterBootFailure On
$firmware=Get-VMFirmware -VM $vm;$boot=@($firmware.BootOrder)
if([string]$firmware.SecureBoot -cne 'On' -or $firmware.SecureBootTemplate -cne 'MicrosoftWindows' -or $boot.Count -lt 1 -or -not [String]::Equals([string]$boot[0].Device.Path,$guestVHD,[StringComparison]::OrdinalIgnoreCase)){throw 'NEW_VHD_VM_BOOT_CONFIGURATION_REJECTED'}
$receipt=@{instance=$instance;vm_id=$vm.Id.ToString();name=$vm.Name;generation=$vm.Generation;root=$root;vhd=$guestVHD;windows_iso='';seed_iso='';image_source=$source;image_deployment_method=$method;dvd_count=0;vhd_count=1;pipe=$pipe;cpu_count=$vm.ProcessorCount;memory_bytes=[int64]$memory.Startup;dynamic_memory=$memory.DynamicMemoryEnabled;secure_boot=([string]$firmware.SecureBoot -ceq 'On');network_adapters=$nics;gpu_adapters=$gpus}
$receipt|ConvertTo-Json -Depth 4|Set-Content -LiteralPath (Join-Path $root 'vm-created.private.json') -Encoding utf8
Start-VM -VM $vm|Out-Null
`
}

func preserveAttemptFile(root, instance, name string) error {
	source := filepath.Join(root, name)
	data, err := os.ReadFile(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	destination := filepath.Join(root, "attempt-vhd-"+instance+"-"+strings.TrimSuffix(name, ".private.json")+".private.json")
	if existing, err := os.ReadFile(destination); err == nil {
		if !strings.EqualFold(hex.EncodeToString(sha256Bytes(existing)), hex.EncodeToString(sha256Bytes(data))) {
			return errors.New("existing archived VHD attempt record differs")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else if err := writeExclusiveFile(destination, data); err != nil {
		return err
	}
	return os.Remove(source)
}

func archiveVHDVMAttempt(root, instance string) error {
	for _, name := range []string{"vm-created.private.json", "vm-cleanup.private.json", "vm-fixture-failure.private.json", "vm-provision-failure.private.json", "admin-operation-failure.private.json", "seed-iso-failure.private.json"} {
		if err := preserveAttemptFile(root, instance, name); err != nil {
			return err
		}
	}
	return nil
}

func archiveOrphanVHDFailures(root string) error {
	for _, name := range []string{"vm-provision-failure.private.json", "admin-operation-failure.private.json", "seed-iso-failure.private.json"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		if err := preserveAttemptFile(root, "orphan-"+hex.EncodeToString(hash[:6]), name); err != nil {
			return err
		}
	}
	return nil
}

func sha256Bytes(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}
