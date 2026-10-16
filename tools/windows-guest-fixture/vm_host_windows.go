//go:build windows && amd64

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	evalISOBytes    = int64(8152356864)
	jobMemoryBytes  = int64(2 << 30)
	jobCPUCount     = 2
	errorSemTimeout = syscall.Errno(121)
	errorPipeBusy   = syscall.Errno(231)
)

var waitNamedPipeW = syscall.NewLazyDLL("kernel32.dll").NewProc("WaitNamedPipeW")

type vmReceipt struct {
	Instance        string `json:"instance"`
	ID              string `json:"vm_id"`
	Name            string `json:"name"`
	Root            string `json:"root"`
	VHD             string `json:"vhd"`
	Pipe            string `json:"pipe"`
	CPUCount        int    `json:"cpu_count"`
	Memory          int64  `json:"memory_bytes"`
	Dynamic         bool   `json:"dynamic_memory"`
	SecureBoot      bool   `json:"secure_boot"`
	NetworkAdapters int    `json:"network_adapters"`
	GPUAdapters     int    `json:"gpu_adapters"`
}

type cleanupReceipt struct {
	VMID           string `json:"vm_id"`
	Stopped        bool   `json:"stopped"`
	MemoryAssigned int64  `json:"memory_assigned"`
	Removed        bool   `json:"removed"`
}

type vmEvidence struct {
	Schema                   string          `json:"schema"`
	ObservedAt               time.Time       `json:"observed_at"`
	Instance                 string          `json:"instance"`
	ImageBytes               int64           `json:"image_bytes"`
	ImageSHA256              string          `json:"image_sha256"`
	VM                       vmPublicReceipt `json:"vm"`
	Normal                   fixtureResult   `json:"normal"`
	Cancel                   fixtureResult   `json:"cancel"`
	Cleanup                  cleanupReceipt  `json:"cleanup"`
	CredentialFree           bool            `json:"credential_free"`
	ProductSourceTransferred bool            `json:"product_source_transferred"`
	OfficialCIExecuted       bool            `json:"official_ci_executed"`
}

type vmPublicReceipt struct {
	VMID            string `json:"vm_id"`
	Generation      int    `json:"generation"`
	CPUCount        int    `json:"cpu_count"`
	MemoryBytes     int64  `json:"memory_bytes"`
	DynamicMemory   bool   `json:"dynamic_memory"`
	SecureBoot      bool   `json:"secure_boot"`
	NetworkAdapters int    `json:"network_adapters"`
	GPUAdapters     int    `json:"gpu_adapters"`
}

func runVMProvision(root, isoPath string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("owned root is not a real directory")
	}
	isoPath, err = filepath.Abs(isoPath)
	if err != nil {
		return err
	}
	relativeISO, err := filepath.Rel(root, isoPath)
	if err != nil || relativeISO == ".." || strings.HasPrefix(relativeISO, ".."+string(filepath.Separator)) || filepath.IsAbs(relativeISO) {
		return errors.New("evaluation ISO must stay inside the owned root")
	}
	info, err := os.Stat(isoPath)
	if err != nil {
		return errors.New("official evaluation ISO is unavailable")
	}
	if !info.Mode().IsRegular() || info.Size() != evalISOBytes {
		return fmt.Errorf("evaluation ISO size mismatch: got %d bytes", info.Size())
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

	seed := vmSeed{Instance: instance, ClientSHA256: client.SHA256}
	if err := writeJSONExclusive(filepath.Join(root, "cc2746-seed.json"), seed); err != nil {
		return err
	}
	currentExe, err := os.Executable()
	if err != nil {
		return err
	}
	guestExe := filepath.Join(root, "cc2746-guest.exe")
	if err := copyExclusive(currentExe, guestExe); err != nil {
		return err
	}
	createScript := filepath.Join(root, "vm-create.ps1")
	if err := os.WriteFile(createScript, []byte(createVMPS(root, isoPath, guestExe, instance, client.SHA256)), 0600); err != nil {
		return err
	}
	if err := runAdminBroker(createScript, 1800); err != nil {
		return errors.New("owned Hyper-V guest provisioning failed; preserve its ownership receipts for recovery")
	}

	vm, err := readVMReceipt(root)
	if err != nil {
		return err
	}
	if err := validateVMReceipt(vm, root, instance); err != nil {
		return err
	}
	normal, cancel, fixtureErr := runVMFixtures(vm, client)
	cleanup, cleanupErr := cleanupOwnedVM(root, vm)
	if fixtureErr != nil {
		failure := map[string]any{"schema": "windows-hyperv-guest-fixture-failure-v1", "observed_at": time.Now().UTC(), "instance": instance, "fixture_error": "guest fixture did not complete", "cleanup": cleanup, "cleanup_error": cleanupErr != nil}
		_ = writeJSONAtomic(filepath.Join(root, "vm-fixture-failure.private.json"), failure)
		if cleanupErr != nil {
			return errors.New("Windows guest fixture failed and owned VM cleanup also failed; preserve the recorded VM and root for recovery")
		}
		return errors.New("Windows guest fixture failed; owned VM stopped and removed, failure receipt retained")
	}
	if cleanupErr != nil {
		return errors.New("owned VM cleanup failed; retain the VM and safe-delete root for recovery")
	}

	isoHash, err := hashFile(isoPath)
	if err != nil {
		return err
	}
	evidence := vmEvidence{
		Schema: "windows-hyperv-guest-fixture-v1", ObservedAt: time.Now().UTC(), Instance: instance,
		ImageBytes: info.Size(), ImageSHA256: isoHash,
		VM:     vmPublicReceipt{VMID: vm.ID, Generation: 2, CPUCount: vm.CPUCount, MemoryBytes: vm.Memory, DynamicMemory: vm.Dynamic, SecureBoot: vm.SecureBoot, NetworkAdapters: vm.NetworkAdapters, GPUAdapters: vm.GPUAdapters},
		Normal: normal, Cancel: cancel, Cleanup: cleanup,
		CredentialFree: true, ProductSourceTransferred: false, OfficialCIExecuted: false,
	}
	return writeJSONAtomic(filepath.Join(root, "vm-fixture-evidence.private.json"), evidence)
}

func cleanupOwnedVM(root string, vm vmReceipt) (cleanupReceipt, error) {
	cleanupScript := filepath.Join(root, "vm-cleanup.ps1")
	if err := os.WriteFile(cleanupScript, []byte(cleanupVMPS(vm)), 0600); err != nil {
		return cleanupReceipt{}, err
	}
	if err := runAdminBroker(cleanupScript, 300); err != nil {
		return cleanupReceipt{}, errors.New("CC-AdminBroker cleanup failed")
	}
	cleanup, err := readCleanupReceipt(root)
	if err != nil {
		return cleanup, err
	}
	if cleanup.VMID != vm.ID || !cleanup.Stopped || cleanup.MemoryAssigned != 0 || !cleanup.Removed {
		return cleanup, errors.New("owned VM cleanup receipt rejected")
	}
	return cleanup, nil
}

func runVMFixtures(vm vmReceipt, client vmIdentity) (fixtureResult, fixtureResult, error) {
	conn, err := openOwnedPipe(vm.Pipe, 8*time.Minute)
	if err != nil {
		return fixtureResult{}, fixtureResult{}, errors.New("owned guest serial pipe did not become ready")
	}
	defer conn.Close()
	serial := &serialConn{file: conn}

	readyLine, err := readLineWithTimeout(serial, 10*time.Minute)
	if err != nil {
		return fixtureResult{}, fixtureResult{}, errors.New("guest service readiness was not observed")
	}
	var ready vmReady
	if err := json.Unmarshal(readyLine, &ready); err != nil || ready.Instance != vm.Instance || !validHash(ready.ServerSHA256) {
		return fixtureResult{}, fixtureResult{}, errors.New("guest serial identity rejected")
	}
	secure := tlsClientConn(serial, client, ready.ServerSHA256)
	if err := tlsHandshakeWithTimeout(secure, 30*time.Second); err != nil {
		return fixtureResult{}, fixtureResult{}, errors.New("guest mutual TLS handshake failed")
	}
	defer secure.Close()
	encoder, decoder := json.NewEncoder(secure), json.NewDecoder(secure)
	if err := encoder.Encode(vmRequest{Kind: "normal", Instance: vm.Instance}); err != nil {
		return fixtureResult{}, fixtureResult{}, err
	}
	var normalReply vmReply
	if err := decoder.Decode(&normalReply); err != nil || normalReply.Kind != "normal" || normalReply.Result == nil {
		return fixtureResult{}, fixtureResult{}, errors.New("normal guest receipt missing")
	}
	if err := validateFixtureResult(*normalReply.Result, "normal"); err != nil {
		return fixtureResult{}, fixtureResult{}, err
	}
	if err := encoder.Encode(vmRequest{Kind: "cancel", Instance: vm.Instance}); err != nil {
		return fixtureResult{}, fixtureResult{}, err
	}
	var stage vmStage
	if err := decoder.Decode(&stage); err != nil || stage.Kind != "cancel-started" || !stage.Alive || len(stage.PIDs) != 2 || stage.PIDs[0] <= 0 || stage.PIDs[1] <= 0 {
		return fixtureResult{}, fixtureResult{}, errors.New("live cancellation children were not observed")
	}
	if err := encoder.Encode(vmRequest{Kind: "cancel-confirm", Instance: vm.Instance}); err != nil {
		return fixtureResult{}, fixtureResult{}, err
	}
	var cancelReply vmReply
	if err := decoder.Decode(&cancelReply); err != nil || cancelReply.Kind != "cancel" || cancelReply.Result == nil {
		return fixtureResult{}, fixtureResult{}, errors.New("cancel guest receipt missing")
	}
	if err := validateFixtureResult(*cancelReply.Result, "cancel"); err != nil {
		return fixtureResult{}, fixtureResult{}, err
	}
	if err := encoder.Encode(vmRequest{Kind: "shutdown", Instance: vm.Instance}); err != nil {
		return fixtureResult{}, fixtureResult{}, err
	}
	var shutdown vmReply
	if err := decoder.Decode(&shutdown); err != nil || shutdown.Kind != "shutdown" {
		return fixtureResult{}, fixtureResult{}, errors.New("guest service shutdown receipt missing")
	}
	return *normalReply.Result, *cancelReply.Result, nil
}

func validateFixtureResult(result fixtureResult, kind string) error {
	if result.Case != kind || !result.NamespaceCreated || !result.BothObservedAlive || !result.ChildrenExited || !result.NamespaceRemoved || !result.CredentialFree || len(result.ChildPIDs) != 2 {
		return fmt.Errorf("%s fixture result rejected", kind)
	}
	if kind == "normal" && result.CancellationFired {
		return errors.New("normal fixture unexpectedly cancelled")
	}
	if kind == "cancel" && !result.CancellationFired {
		return errors.New("cancel fixture did not observe cancellation")
	}
	return nil
}

func openOwnedPipe(name string, timeout time.Duration) (*os.File, error) {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ok, _, callErr := waitNamedPipeW.Call(uintptr(unsafe.Pointer(namePtr)), 2000)
		if ok != 0 {
			f, err := os.OpenFile(name, os.O_RDWR, 0)
			if err == nil {
				return f, nil
			}
		}
		if callErr != nil && callErr != errorSemTimeout && callErr != syscall.ERROR_FILE_NOT_FOUND && callErr != errorPipeBusy {
			return nil, fmt.Errorf("owned serial wait failed: %w", callErr)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, errors.New("owned serial pipe timeout")
}

func readLineWithTimeout(conn net.Conn, timeout time.Duration) ([]byte, error) {
	done := make(chan struct{})
	var line []byte
	var readErr error
	go func() { line, readErr = readJSONLine(conn, 4096); close(done) }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return line, readErr
	case <-timer.C:
		_ = conn.Close()
		return nil, errors.New("serial readiness timeout")
	}
}

func tlsHandshakeWithTimeout(conn net.Conn, timeout time.Duration) error {
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return errors.New("TLS serial wrapper missing")
	}
	done := make(chan error, 1)
	go func() { done <- tlsConn.Handshake() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		_ = conn.Close()
		return errors.New("TLS handshake timeout")
	}
}

func tlsClientConn(conn net.Conn, identity vmIdentity, serverPin string) *tls.Conn {
	return tls.Client(conn, vmClientTLS(identity, serverPin))
}

func writeJSONExclusive(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".new"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func copyExclusive(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func readVMReceipt(root string) (vmReceipt, error) {
	var receipt vmReceipt
	data, err := os.ReadFile(filepath.Join(root, "vm-created.private.json"))
	if err != nil {
		return receipt, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	err = dec.Decode(&receipt)
	return receipt, err
}

func readCleanupReceipt(root string) (cleanupReceipt, error) {
	var receipt cleanupReceipt
	data, err := os.ReadFile(filepath.Join(root, "vm-cleanup.private.json"))
	if err != nil {
		return receipt, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	err = dec.Decode(&receipt)
	return receipt, err
}

func validateVMReceipt(vm vmReceipt, root, instance string) error {
	if vm.Instance != instance || !validInstance(vm.Instance) || vm.ID == "" || vm.Name != "cc-2746-"+instance ||
		!strings.EqualFold(filepath.Clean(vm.Root), filepath.Clean(root)) || vm.VHD != filepath.Join(root, "guest.vhdx") ||
		vm.Pipe != `\\.\pipe\cc-2746-`+instance || vm.CPUCount != jobCPUCount || vm.Memory != jobMemoryBytes || vm.Dynamic || !vm.SecureBoot ||
		vm.NetworkAdapters != 0 || vm.GPUAdapters != 0 {
		return errors.New("Hyper-V VM ownership or resource configuration rejected")
	}
	return nil
}

func runAdminBroker(script string, timeoutSec int) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec+30)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, `C:\Program Files\PowerShell\7\pwsh.exe`, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", `D:\CC\Tools\admin-broker\run-as-admin.ps1`, "-Script", script, "-TimeoutSec", fmt.Sprint(timeoutSec))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return errors.New("CC-AdminBroker operation failed")
	}
	return nil
}

func createVMPS(root, iso, guestExe, instance, clientHash string) string {
	q := psQuote
	return `$ErrorActionPreference='Stop'
Import-Module Hyper-V
Import-Module Storage
$root=` + q(root) + `
$iso=` + q(iso) + `
$guestExe=` + q(guestExe) + `
$instance=` + q(instance) + `
$clientHash=` + q(clientHash) + `
$vhd=Join-Path $root 'guest.vhdx'
$name='cc-2746-'+$instance
$pipe='\\.\pipe\cc-2746-'+$instance
if(Test-Path -LiteralPath $vhd){throw 'OWNED_VHD_ALREADY_EXISTS'}
if(Get-VM -Name $name -ErrorAction SilentlyContinue){throw 'OWNED_VM_NAME_ALREADY_EXISTS'}
$mountedIso=$false;$mountedVhd=$false;$hiveLoaded=$false;$hiveName=''
try {
  $img=Mount-DiskImage -ImagePath $iso -PassThru
  $mountedIso=$true
  $isoVol=$img | Get-Disk | Get-Partition | Get-Volume | Where-Object DriveLetter | Select-Object -First 1
  if(-not $isoVol){throw 'EVAL_ISO_VOLUME_NOT_FOUND'}
  $isoRoot=([string]$isoVol.DriveLetter)+':\'
  $setupSig=Get-AuthenticodeSignature -LiteralPath (Join-Path $isoRoot 'setup.exe')
  if($setupSig.Status -ne 'Valid' -or $setupSig.SignerCertificate.Subject -notmatch 'Microsoft'){throw 'EVAL_ISO_SIGNATURE_INVALID'}
  $wim=Join-Path $isoRoot 'sources\install.wim'
  $image=Get-WindowsImage -ImagePath $wim | Where-Object ImageName -CEQ 'Windows Server 2025 Standard Evaluation' | Select-Object -First 1
  if(-not $image){throw 'EVAL_IMAGE_INDEX_NOT_FOUND'}
  New-VHD -Path $vhd -Dynamic -SizeBytes 64GB | Out-Null
  $mounted=Mount-VHD -Path $vhd -PassThru
  $mountedVhd=$true
  $disk=$mounted | Get-Disk
  Initialize-Disk -Number $disk.Number -PartitionStyle GPT | Out-Null
  $efi=New-Partition -DiskNumber $disk.Number -Size 200MB -GptType '{c12a7328-f81f-11d2-ba4b-00a0c93ec93b}' -AssignDriveLetter
  Format-Volume -Partition $efi -FileSystem FAT32 -NewFileSystemLabel 'CC2746SYS' -Confirm:$false | Out-Null
  New-Partition -DiskNumber $disk.Number -Size 16MB -GptType '{e3c9e316-0b5c-4db8-817d-f92df00215ae}' | Out-Null
  $os=New-Partition -DiskNumber $disk.Number -UseMaximumSize -AssignDriveLetter
  Format-Volume -Partition $os -FileSystem NTFS -NewFileSystemLabel 'CC2746OS' -Confirm:$false | Out-Null
  $osRoot=([string]$os.DriveLetter)+':\'
  $efiRoot=([string]$efi.DriveLetter)+':\'
  & dism.exe /Apply-Image /ImageFile:$wim /Index:$($image.ImageIndex) /ApplyDir:$osRoot /Quiet /NoRestart
  if($LASTEXITCODE -ne 0){throw 'DISM_APPLY_IMAGE_FAILED'}
  $system32=Join-Path $osRoot 'Windows\System32'
  Copy-Item -LiteralPath $guestExe -Destination (Join-Path $system32 'cc2746-guest.exe')
  Copy-Item -LiteralPath (Join-Path $root 'cc2746-seed.json') -Destination (Join-Path $system32 'cc2746-seed.json')
  $hiveName='CC2746_'+[guid]::NewGuid().ToString('N')
  & reg.exe load ('HKLM\'+$hiveName) (Join-Path $system32 'Config\SYSTEM') | Out-Null
  if($LASTEXITCODE -ne 0){throw 'OFFLINE_SYSTEM_HIVE_LOAD_FAILED'}
  $hiveLoaded=$true
  $select=[Microsoft.Win32.Registry]::LocalMachine.OpenSubKey(($hiveName+'\Select'),$false)
  if(-not $select){throw 'OFFLINE_CONTROLSET_NOT_FOUND'}
  $current=[int]$select.GetValue('Current',0);$select.Dispose()
  if($current -lt 1 -or $current -gt 999){throw 'OFFLINE_CONTROLSET_INVALID'}
  $controlSet=('ControlSet{0:D3}' -f $current)
  $service=[Microsoft.Win32.Registry]::LocalMachine.CreateSubKey(($hiveName+'\'+$controlSet+'\Services\CCGuest2746'))
  $service.SetValue('Type',16,[Microsoft.Win32.RegistryValueKind]::DWord)
  $service.SetValue('Start',2,[Microsoft.Win32.RegistryValueKind]::DWord)
  $service.SetValue('ErrorControl',1,[Microsoft.Win32.RegistryValueKind]::DWord)
  $service.SetValue('ImagePath','\SystemRoot\System32\cc2746-guest.exe --service',[Microsoft.Win32.RegistryValueKind]::ExpandString)
  $service.SetValue('ObjectName','LocalSystem',[Microsoft.Win32.RegistryValueKind]::String)
  $service.SetValue('DisplayName','CC ToDo 2746 guest fixture',[Microsoft.Win32.RegistryValueKind]::String)
  $service.Dispose()
  & bcdboot.exe (Join-Path $osRoot 'Windows') /s $efiRoot /f UEFI
  if($LASTEXITCODE -ne 0){throw 'BCDBOOT_FAILED'}
  Dismount-VHD -Path $vhd
  $mountedVhd=$false
  Dismount-DiskImage -ImagePath $iso
  $mountedIso=$false
  $vm=New-VM -Name $name -Generation 2 -MemoryStartupBytes 2GB -VHDPath $vhd -Path (Join-Path $root 'vm')
  Set-VM -VM $vm -AutomaticStartAction Nothing -AutomaticStopAction TurnOff -CheckpointType Disabled -Notes ('cc.todo=2746;cc.instance='+$instance)
$initial=@{instance=$instance;vm_id=$vm.Id.ToString();name=$vm.Name;root=$root;vhd=$vhd;pipe=$pipe;cpu_count=0;memory_bytes=0;dynamic_memory=$true;network_adapters=-1;gpu_adapters=-1}
$initial | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $root 'vm-created.private.json') -Encoding utf8
  Set-VMProcessor -VM $vm -Count 2
  Set-VMMemory -VM $vm -DynamicMemoryEnabled $false -StartupBytes 2GB -MinimumBytes 2GB -MaximumBytes 2GB
$adapters=@($vm | Get-VMNetworkAdapter);foreach($adapter in $adapters){Remove-VMNetworkAdapter -VMNetworkAdapter $adapter}
  Set-VMComPort -VM $vm -Number 1 -Path $pipe
Set-VMFirmware -VM $vm -EnableSecureBoot On -SecureBootTemplate MicrosoftWindows
  $vm=Get-VM -Id $vm.Id
  $memory=Get-VMMemory -VM $vm
  $nics=@($vm | Get-VMNetworkAdapter).Count
  $gpus=@($vm | Get-VMGpuPartitionAdapter -ErrorAction SilentlyContinue).Count
if($vm.State -ne 'Off' -or $memory.DynamicMemoryEnabled -or $memory.Startup -ne 2GB -or $memory.Minimum -ne 2GB -or $memory.Maximum -ne 2GB -or $vm.ProcessorCount -ne 2 -or $nics -ne 0 -or $gpus -ne 0){throw 'NEW_VM_RESOURCE_CONFIGURATION_REJECTED'}
$firmware=Get-VMFirmware -VM $vm
if(-not $firmware.SecureBoot -or $firmware.SecureBootTemplate -ne 'MicrosoftWindows'){throw 'NEW_VM_SECURE_BOOT_REJECTED'}
$receipt=@{instance=$instance;vm_id=$vm.Id.ToString();name=$vm.Name;root=$root;vhd=$vhd;pipe=$pipe;cpu_count=$vm.ProcessorCount;memory_bytes=[int64]$memory.Startup;dynamic_memory=$memory.DynamicMemoryEnabled;secure_boot=$firmware.SecureBoot;network_adapters=$nics;gpu_adapters=$gpus}
  $receipt | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $root 'vm-created.private.json') -Encoding utf8
  Start-VM -VM $vm | Out-Null
} finally {
  if($hiveLoaded){& reg.exe unload ('HKLM\'+$hiveName) | Out-Null;if($LASTEXITCODE -ne 0){throw 'OFFLINE_SYSTEM_HIVE_UNLOAD_FAILED'}}
  if($mountedVhd){Dismount-VHD -Path $vhd -ErrorAction SilentlyContinue}
  if($mountedIso){Dismount-DiskImage -ImagePath $iso -ErrorAction SilentlyContinue}
}
`
}

func cleanupVMPS(vm vmReceipt) string {
	q := psQuote
	return `$ErrorActionPreference='Stop'
Import-Module Hyper-V
$id=[guid]` + q(vm.ID) + `
$name=` + q(vm.Name) + `
$root=` + q(vm.Root) + `
$instance=` + q(vm.Instance) + `
$path=` + q(vm.VHD) + `
$pipe=` + q(vm.Pipe) + `
$v=Get-VM -Id $id -ErrorAction SilentlyContinue
if($v){
  if($v.Name -cne $name -or $v.Notes -cne ('cc.todo=2746;cc.instance='+$instance) -or -not [String]::Equals([IO.Path]::GetFullPath($v.ConfigurationLocation).TrimEnd('\'),[IO.Path]::GetFullPath((Join-Path $root 'vm')).TrimEnd('\'),[StringComparison]::OrdinalIgnoreCase)){throw 'OWNED_VM_CLEANUP_IDENTITY_MISMATCH'}
  $cp=$v | Get-VMComPort | Where-Object Number -EQ 1
  if(-not $cp -or $cp.Path -cne $pipe){throw 'OWNED_VM_COM_PORT_MISMATCH'}
  if($v.State -ne 'Off'){Stop-VM -VM $v -TurnOff -Force}
  $deadline=(Get-Date).AddMinutes(2)
  do{$v=Get-VM -Id $id;if($v.State -eq 'Off' -and $v.MemoryAssigned -eq 0){break};Start-Sleep -Milliseconds 250}while((Get-Date) -lt $deadline)
  if($v.State -ne 'Off' -or $v.MemoryAssigned -ne 0){throw 'OWNED_VM_STOP_UNCONFIRMED'}
  Remove-VM -VM $v -Force
  if(Get-VM -Id $id -ErrorAction SilentlyContinue){throw 'OWNED_VM_REMOVAL_UNCONFIRMED'}
}
@{vm_id=$id.ToString();stopped=$true;memory_assigned=0;removed=$true}|ConvertTo-Json|Set-Content -LiteralPath (Join-Path $root 'vm-cleanup.private.json') -Encoding utf8
`
}

func psQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
