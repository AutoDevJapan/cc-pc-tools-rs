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
	"encoding/xml"
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
	Generation      int    `json:"generation"`
	Root            string `json:"root"`
	VHD             string `json:"vhd"`
	WindowsISO      string `json:"windows_iso"`
	SeedISO         string `json:"seed_iso"`
	DVDCount        int    `json:"dvd_count"`
	VHDCount        int    `json:"vhd_count"`
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
	ImageDeploymentMethod    string          `json:"image_deployment_method"`
	ImageBytes               int64           `json:"image_bytes"`
	ImageSHA256              string          `json:"image_sha256"`
	VM                       vmPublicReceipt `json:"vm"`
	Normal                   fixtureResult   `json:"normal"`
	Cancel                   fixtureResult   `json:"cancel"`
	Cleanup                  cleanupReceipt  `json:"cleanup"`
	CredentialFree           bool            `json:"credential_free"`
	UnattendEULAConfigured   bool            `json:"unattend_eula_configured"`
	OOBESkipConfigured       bool            `json:"oobe_skip_configured"`
	LocalUserCreated         bool            `json:"local_user_created"`
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
	DVDCount        int    `json:"dvd_count"`
	VHDCount        int    `json:"vhd_count"`
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
	if evidenceExists, err := ownedPathExists(root, "vm-fixture-evidence.private.json"); err != nil {
		return errors.New("owned VM evidence state could not be inspected")
	} else if evidenceExists {
		return errors.New("owned VM fixture evidence already exists; refusing to repeat a completed fixture")
	}
	hasVHD, err := ownedPathExists(root, "guest.vhdx")
	if err != nil {
		return errors.New("owned VHD state could not be inspected")
	}
	hasReceipt, err := ownedPathExists(root, "vm-created.private.json")
	if err != nil {
		return errors.New("owned VM receipt state could not be inspected")
	}
	if hasVHD || hasReceipt {
		if hasVHD && hasReceipt {
			oldVM, readErr := readVMReceipt(root)
			if readErr != nil {
				return errors.New("owned VM recovery receipt could not be read")
			}
			cleanup, cleanupErr := readCleanupReceipt(root)
			if cleanupErr == nil && cleanup.VMID == oldVM.ID && cleanup.Stopped && cleanup.MemoryAssigned == 0 && cleanup.Removed {
				return retryOnVerifiedOwnedVHD(root, isoPath, oldVM, cleanup)
			}
			return resumeVMProvision(root, isoPath)
		}
		return errors.New("owned root contains incomplete VM state without a matching VHD and receipt")
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
	createScript := filepath.Join(root, "vm-create.ps1")
	if err := os.WriteFile(createScript, []byte(createVMPS(root, isoPath, seedISO, instance, false, "")), 0600); err != nil {
		return err
	}
	if err := runAdminBroker(createScript, 1800); err != nil {
		return errors.New("owned Hyper-V guest provisioning failed; preserve its ownership receipts for recovery")
	}

	vm, err := readVMReceipt(root)
	if err != nil {
		return err
	}
	return runVMFixturesAndRecord(root, isoPath, info.Size(), vm, client)
}

func retryOnVerifiedOwnedVHD(root, isoPath string, oldVM vmReceipt, cleanup cleanupReceipt) error {
	if err := validatePartialVMReceipt(oldVM, root, isoPath); err != nil || cleanup.VMID != oldVM.ID {
		return errors.New("prior attempt receipts do not authorize reuse of the owned VHDX")
	}
	for suffix, value := range map[string]any{"vm": oldVM, "cleanup": cleanup} {
		if err := writeJSONExclusive(filepath.Join(root, "attempt-"+oldVM.Instance+"-"+suffix+".private.json"), value); err != nil {
			return errors.New("prior VM attempt record could not be preserved")
		}
	}
	failurePath := filepath.Join(root, "vm-fixture-failure.private.json")
	failureExists, err := ownedPathExists(root, "vm-fixture-failure.private.json")
	if err != nil {
		return errors.New("prior fixture failure record could not be inspected")
	}
	if failureExists {
		data, err := os.ReadFile(failurePath)
		if err != nil {
			return errors.New("prior fixture failure record could not be preserved")
		}
		if err := writeExclusiveFile(filepath.Join(root, "attempt-"+oldVM.Instance+"-failure.private.json"), data); err != nil {
			return errors.New("prior fixture failure record could not be preserved")
		}
	}
	client, err := newVMIdentity()
	if err != nil {
		return err
	}
	defer clearIdentity(&client)
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return err
	}
	instanceHash := sha256.Sum256(entropy[:])
	instance := hex.EncodeToString(instanceHash[:16])
	clear(entropy[:])
	seedISO, err := createGuestSeed(root, instance, client, instance)
	if err != nil {
		return err
	}
	script := filepath.Join(root, "vm-recreate.ps1")
	if err := os.WriteFile(script, []byte(createVMPS(root, isoPath, seedISO, instance, true, oldVM.ID)), 0600); err != nil {
		return err
	}
	if err := runAdminBroker(script, 1800); err != nil {
		return errors.New("new owned Windows guest provisioning failed; preserve its receipts and VHDX")
	}
	vm, err := readVMReceipt(root)
	if err != nil {
		return errors.New("new owned VM receipt could not be read")
	}
	info, err := os.Stat(isoPath)
	if err != nil {
		return errors.New("official evaluation ISO is unavailable during retry")
	}
	return runVMFixturesAndRecord(root, isoPath, info.Size(), vm, client)
}

func ownedPathExists(root, rel string) (bool, error) {
	info, err := os.Lstat(filepath.Join(root, rel))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("owned state path is not a regular file")
	}
	return true, nil
}

func createGuestSeed(root, instance string, client vmIdentity, seedID string) (string, error) {
	seedRoot := filepath.Join(root, "seed")
	if err := os.Mkdir(seedRoot, 0700); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
		seedInfo, statErr := os.Lstat(seedRoot)
		if statErr != nil || !seedInfo.IsDir() || seedInfo.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("owned seed directory is not a real directory")
		}
	}
	seed := vmSeed{Instance: instance, ClientSHA256: client.SHA256}
	seedBytes, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeOwnedFile(filepath.Join(seedRoot, "cc2746-seed.json"), append(seedBytes, '\n')); err != nil {
		return "", err
	}
	answer := seedAnswerFile()
	if err := validateSeedAnswerFile(answer); err != nil {
		return "", err
	}
	if err := writeOwnedFile(filepath.Join(seedRoot, "Autounattend.xml"), []byte(answer)); err != nil {
		return "", err
	}
	if err := writeOwnedFile(filepath.Join(seedRoot, "stage.ps1"), []byte(seedStageScript())); err != nil {
		return "", err
	}
	currentExe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if err := copyReplacingOwned(currentExe, filepath.Join(seedRoot, "cc2746-guest.exe")); err != nil {
		return "", err
	}
	return createSeedISO(root, seedRoot, seedID)
}

func resumeVMProvision(root, isoPath string) error {
	vm, err := readVMReceipt(root)
	if err != nil {
		return errors.New("owned VM recovery receipt could not be read")
	}
	if err := validatePartialVMReceipt(vm, root, isoPath); err != nil {
		return err
	}
	client, err := newVMIdentity()
	if err != nil {
		return err
	}
	defer clearIdentity(&client)
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return err
	}
	seedHash := sha256.Sum256(entropy[:])
	seedID := hex.EncodeToString(seedHash[:16])
	clear(entropy[:])
	seedISO, err := createGuestSeed(root, vm.Instance, client, seedID)
	if err != nil {
		return err
	}
	script := filepath.Join(root, "vm-resume.ps1")
	if err := os.WriteFile(script, []byte(resumeVMPS(root, vm, seedISO)), 0600); err != nil {
		return err
	}
	if err := runAdminBroker(script, 300); err != nil {
		return errors.New("owned Hyper-V guest resume failed; retain its receipt and VHDX for recovery")
	}
	vm, err = readVMReceipt(root)
	if err != nil {
		return errors.New("resumed VM receipt could not be read")
	}
	info, err := os.Stat(isoPath)
	if err != nil {
		return errors.New("official evaluation ISO is unavailable during recovery")
	}
	return runVMFixturesAndRecord(root, isoPath, info.Size(), vm, client)
}

func validatePartialVMReceipt(vm vmReceipt, root, isoPath string) error {
	if !validVMID(vm.ID) || !validInstance(vm.Instance) || vm.Name != "cc-2746-"+vm.Instance || vm.Generation != 2 ||
		!strings.EqualFold(filepath.Clean(vm.Root), filepath.Clean(root)) || vm.VHD != filepath.Join(root, "guest.vhdx") ||
		vm.WindowsISO != isoPath || !validSeedISOPath(root, vm.SeedISO) || vm.Pipe != `\\.\pipe\cc-2746-`+vm.Instance {
		return errors.New("partial Hyper-V receipt does not identify this owned VM")
	}
	return nil
}

func validVMID(value string) bool {
	parts := strings.Split(value, "-")
	if len(parts) != 5 || len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		return false
	}
	_, err := hex.DecodeString(strings.Join(parts, ""))
	return err == nil
}

func validSeedISOPath(root, path string) bool {
	cleaned := filepath.Clean(path)
	if !strings.EqualFold(filepath.Dir(cleaned), filepath.Clean(root)) {
		return false
	}
	name := filepath.Base(cleaned)
	if !strings.HasPrefix(name, "cc2746-seed-") || !strings.HasSuffix(name, ".iso") {
		return false
	}
	return validInstance(strings.TrimSuffix(strings.TrimPrefix(name, "cc2746-seed-"), ".iso"))
}

func resumeVMPS(root string, vm vmReceipt, seedISO string) string {
	q := psQuote
	return `$ErrorActionPreference='Stop'
Import-Module Hyper-V
$id=[guid]` + q(vm.ID) + `
$name=` + q(vm.Name) + `
$root=` + q(root) + `
$instance=` + q(vm.Instance) + `
$vhd=` + q(vm.VHD) + `
$windowsISO=` + q(vm.WindowsISO) + `
$oldSeedISO=` + q(vm.SeedISO) + `
$newSeedISO=` + q(seedISO) + `
$pipe=` + q(vm.Pipe) + `
if(-not (Test-Path -LiteralPath $newSeedISO)){throw 'OWNED_NEW_SEED_ISO_MISSING'}
$v=Get-VM -Id $id -ErrorAction Stop
$configRoot=[IO.Path]::GetFullPath((Join-Path $root 'vm')).TrimEnd('\')
$configPath=[IO.Path]::GetFullPath($v.ConfigurationLocation).TrimEnd('\')
$configInside=[String]::Equals($configPath,$configRoot,[StringComparison]::OrdinalIgnoreCase) -or $configPath.StartsWith($configRoot+'\',[StringComparison]::OrdinalIgnoreCase)
if($v.Name -cne $name -or $v.Notes -cne ('cc.todo=2746;cc.instance='+$instance) -or -not $configInside){throw 'OWNED_VM_RECOVERY_IDENTITY_MISMATCH'}
$cp=@($v|Get-VMComPort|Where-Object Path -CEQ $pipe)
$dvds=@($v|Get-VMDvdDrive)
$vhds=@($v|Get-VMHardDiskDrive)
$memory=Get-VMMemory -VM $v
$firmware=Get-VMFirmware -VM $v
$nics=@($v|Get-VMNetworkAdapter).Count
$gpus=@($v|Get-VMGpuPartitionAdapter -ErrorAction SilentlyContinue).Count
if($v.State -ne 'Off' -or $v.Generation -ne 2 -or $v.ProcessorCount -ne 2 -or $memory.DynamicMemoryEnabled -or $memory.Startup -ne 2GB -or $nics -ne 0 -or $gpus -ne 0 -or $dvds.Count -ne 2 -or $vhds.Count -ne 1 -or $vhds[0].Path -cne $vhd -or $cp.Count -ne 1 -or @($dvds|Where-Object Path -CEQ $windowsISO).Count -ne 1 -or @($dvds|Where-Object Path -CEQ $oldSeedISO).Count -ne 1){throw 'OWNED_VM_RECOVERY_RESOURCE_MISMATCH'}
if([string]$firmware.SecureBoot -cne 'On' -or $firmware.SecureBootTemplate -cne 'MicrosoftWindows'){throw 'OWNED_VM_RECOVERY_SECURE_BOOT_MISMATCH'}
$winDvd=$dvds|Where-Object Path -CEQ $windowsISO|Select-Object -First 1
$seedDvd=$dvds|Where-Object Path -CEQ $oldSeedISO|Select-Object -First 1
Set-VMDvdDrive -VMDvdDrive $seedDvd -Path $newSeedISO|Out-Null
Set-VMFirmware -VM $v -EnableSecureBoot On -SecureBootTemplate MicrosoftWindows -FirstBootDevice $winDvd
$receipt=@{instance=$instance;vm_id=$v.Id.ToString();name=$v.Name;generation=$v.Generation;root=$root;vhd=$vhd;windows_iso=$windowsISO;seed_iso=$newSeedISO;dvd_count=2;vhd_count=1;pipe=$pipe;cpu_count=$v.ProcessorCount;memory_bytes=[int64]$memory.Startup;dynamic_memory=$memory.DynamicMemoryEnabled;secure_boot=([string]$firmware.SecureBoot -ceq 'On');network_adapters=$nics;gpu_adapters=$gpus}
$receipt|ConvertTo-Json -Depth 4|Set-Content -LiteralPath (Join-Path $root 'vm-created.private.json') -Encoding utf8
Start-VM -VM $v|Out-Null
`
}

func runVMFixturesAndRecord(root, isoPath string, imageBytes int64, vm vmReceipt, client vmIdentity) error {
	if err := validateVMReceipt(vm, root, vm.Instance); err != nil {
		return err
	}
	normal, cancel, fixtureErr := runVMFixtures(vm, client)
	cleanup, cleanupErr := cleanupOwnedVM(root, vm)
	if fixtureErr != nil {
		failure := map[string]any{"schema": "windows-hyperv-guest-fixture-failure-v1", "observed_at": time.Now().UTC(), "instance": vm.Instance, "fixture_error": fixtureErr.Error(), "cleanup": cleanup, "cleanup_error": cleanupErr != nil}
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
		Schema: "windows-hyperv-guest-fixture-v1", ObservedAt: time.Now().UTC(), Instance: vm.Instance,
		ImageDeploymentMethod: "dism-offline-apply+bcdboot", ImageBytes: imageBytes, ImageSHA256: isoHash,
		VM:     vmPublicReceipt{VMID: vm.ID, Generation: vm.Generation, CPUCount: vm.CPUCount, MemoryBytes: vm.Memory, DynamicMemory: vm.Dynamic, SecureBoot: vm.SecureBoot, DVDCount: vm.DVDCount, VHDCount: vm.VHDCount, NetworkAdapters: vm.NetworkAdapters, GPUAdapters: vm.GPUAdapters},
		Normal: normal, Cancel: cancel, Cleanup: cleanup,
		CredentialFree: true, UnattendEULAConfigured: true, OOBESkipConfigured: false, LocalUserCreated: false,
		ProductSourceTransferred: false, OfficialCIExecuted: false,
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
	deadline := time.Now().Add(30 * time.Minute)
	conn, err := openOwnedPipe(vm.Pipe, time.Until(deadline))
	if err != nil {
		return fixtureResult{}, fixtureResult{}, errors.New("owned guest serial pipe did not become ready")
	}
	defer conn.Close()
	serial := &serialConn{file: conn}

	readyLine, err := readLineWithTimeout(serial, time.Until(deadline))
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
	return writeExclusiveFile(path, data)
}

func writeExclusiveFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("existing attempt record is not a regular file")
		}
		existing, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(existing, data) {
			return errors.New("existing attempt record differs from the prior receipt")
		}
		return nil
	}
	if _, err := f.Write(data); err != nil {
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

func copyReplacingOwned(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	flags := os.O_CREATE | os.O_WRONLY
	if info, statErr := os.Lstat(to); statErr == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("owned helper destination is not a regular file")
		}
		flags |= os.O_TRUNC
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	} else {
		flags |= os.O_EXCL
	}
	out, err := os.OpenFile(to, flags, 0700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func writeOwnedFile(path string, data []byte) error {
	flags := os.O_CREATE | os.O_WRONLY
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("owned seed path is not a regular file")
		}
		flags |= os.O_TRUNC
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
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

func seedAnswerFile() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
  <settings pass="windowsPE">
    <component name="Microsoft-Windows-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
      <DiskConfiguration>
        <Disk wcm:action="add">
          <DiskID>0</DiskID><WillWipeDisk>true</WillWipeDisk>
          <CreatePartitions>
            <CreatePartition wcm:action="add"><Order>1</Order><Size>200</Size><Type>EFI</Type></CreatePartition>
            <CreatePartition wcm:action="add"><Order>2</Order><Size>16</Size><Type>MSR</Type></CreatePartition>
            <CreatePartition wcm:action="add"><Order>3</Order><Extend>true</Extend><Type>Primary</Type></CreatePartition>
          </CreatePartitions>
          <ModifyPartitions>
            <ModifyPartition wcm:action="add"><Order>1</Order><PartitionID>1</PartitionID><Format>FAT32</Format><Label>System</Label></ModifyPartition>
            <ModifyPartition wcm:action="add"><Order>2</Order><PartitionID>3</PartitionID><Format>NTFS</Format><Label>Windows</Label><Letter>C</Letter></ModifyPartition>
          </ModifyPartitions>
        </Disk>
        <WillShowUI>OnError</WillShowUI>
      </DiskConfiguration>
      <ImageInstall>
        <OSImage>
          <InstallFrom><MetaData wcm:action="add"><Key>/IMAGE/NAME</Key><Value>Windows Server 2025 Standard Evaluation</Value></MetaData></InstallFrom>
          <InstallTo><DiskID>0</DiskID><PartitionID>3</PartitionID></InstallTo>
          <WillShowUI>OnError</WillShowUI>
        </OSImage>
      </ImageInstall>
      <UserData><AcceptEula>true</AcceptEula></UserData>
      <DynamicUpdate><Enable>false</Enable></DynamicUpdate>
    </component>
    <component name="Microsoft-Windows-International-Core-WinPE" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
      <SetupUILanguage><UILanguage>en-US</UILanguage></SetupUILanguage>
      <InputLocale>en-US</InputLocale><SystemLocale>en-US</SystemLocale><UILanguage>en-US</UILanguage><UserLocale>en-US</UserLocale>
    </component>
  </settings>
  <settings pass="specialize">
    <component name="Microsoft-Windows-Deployment" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
      <RunSynchronous>
        <RunSynchronousCommand wcm:action="add">
          <Order>1</Order><Description>Install finite credential-free guest service</Description>
          <Path>%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -ExecutionPolicy Bypass -Command &quot;$d=[IO.DriveInfo]::GetDrives() | Where-Object { $_.DriveType -eq [IO.DriveType]::CDRom -and $_.IsReady } | Where-Object { $_.VolumeLabel -eq 'CC2746SEED' } | Select-Object -First 1; if(-not $d){exit 51}; &amp; (Join-Path $d.RootDirectory.FullName 'stage.ps1')&quot;</Path>
          <WillReboot>Never</WillReboot>
        </RunSynchronousCommand>
      </RunSynchronous>
    </component>
  </settings>
</unattend>
`
}

func validateSeedAnswerFile(answer string) error {
	decoder := xml.NewDecoder(strings.NewReader(answer))
	passes := map[string]bool{}
	rootSeen, eulaAccepted, inAcceptEula := false, false, false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("unattend XML is malformed")
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "unattend" {
				rootSeen = true
			}
			if value.Name.Local == "settings" {
				for _, attr := range value.Attr {
					if attr.Name.Local == "pass" {
						if attr.Value == "oobeSystem" {
							return errors.New("OOBE settings are forbidden in this fixture")
						}
						if attr.Value == "windowsPE" || attr.Value == "specialize" {
							passes[attr.Value] = true
						}
					}
				}
			}
			if value.Name.Local == "UserAccounts" || value.Name.Local == "Password" {
				return errors.New("account or password material is forbidden in the answer file")
			}
			if value.Name.Local == "AcceptEula" {
				inAcceptEula = true
			}
		case xml.CharData:
			if inAcceptEula && strings.TrimSpace(string(value)) == "true" {
				eulaAccepted = true
			}
		case xml.EndElement:
			if value.Name.Local == "AcceptEula" {
				inAcceptEula = false
			}
		}
	}
	if !rootSeen || !passes["windowsPE"] || !passes["specialize"] || !eulaAccepted {
		return errors.New("unattend XML is missing required standard setup passes")
	}
	return nil
}

func seedStageScript() string {
	return `$ErrorActionPreference='Stop'
$seed=$null
foreach($drive in [IO.DriveInfo]::GetDrives()){
  try{if($drive.DriveType -eq [IO.DriveType]::CDRom -and $drive.IsReady -and $drive.VolumeLabel -ceq 'CC2746SEED'){$seed=$drive.RootDirectory.FullName;break}}catch{}
}
if(-not $seed){exit 61}
$system32=Join-Path $env:SystemRoot 'System32'
$exe=Join-Path $system32 'cc2746-guest.exe'
Copy-Item -LiteralPath (Join-Path $seed 'cc2746-guest.exe') -Destination $exe
Copy-Item -LiteralPath (Join-Path $seed 'cc2746-seed.json') -Destination (Join-Path $system32 'cc2746-seed.json')
$service=New-Service -Name CCGuest2746 -BinaryPathName ($exe+' --service') -StartupType Automatic -DisplayName 'ToDo 2746 guest fixture' -Description 'Credential-free finite Windows guest fixture'
if(-not $service){exit 62}
$cim=Get-CimInstance Win32_Service -Filter "Name='CCGuest2746'"
if(-not $cim -or $cim.StartName -ne 'LocalSystem' -or $cim.StartMode -ne 'Auto'){exit 63}
Start-Service -Name CCGuest2746
$deadline=(Get-Date).AddSeconds(30)
do{$service=Get-Service -Name CCGuest2746;if($service.Status -eq 'Running'){break};Start-Sleep -Milliseconds 100}while((Get-Date) -lt $deadline)
if($service.Status -ne 'Running'){exit 64}
exit 0
`
}

func createSeedISO(root, seedDir, instance string) (string, error) {
	if !validInstance(instance) {
		return "", errors.New("seed ISO instance id rejected")
	}
	isoPath := filepath.Join(root, "cc2746-seed-"+instance+".iso")
	scriptPath := filepath.Join(root, "make-seed-iso-"+instance+".ps1")
	script := `$ErrorActionPreference='Stop'
Add-Type -TypeDefinition @'
using System;
using System.IO;
using System.Runtime.InteropServices;
public static class CC2746ImageStreamWriter {
  [UnmanagedFunctionPointer(CallingConvention.StdCall)]
  private delegate int ReadDelegate(IntPtr self, [Out, MarshalAs(UnmanagedType.LPArray, SizeParamIndex=2)] byte[] buffer, uint count, IntPtr read);
  public static void CopyToFile(object streamObject, string destination) {
    IntPtr unknown=Marshal.GetIUnknownForObject(streamObject);
    Guid iid=new Guid("0000000c-0000-0000-C000-000000000046");
    IntPtr stream=IntPtr.Zero;
    IntPtr readCount=Marshal.AllocHGlobal(4);
    try {
      Marshal.ThrowExceptionForHR(Marshal.QueryInterface(unknown, in iid, out stream));
      IntPtr vtable=Marshal.ReadIntPtr(stream);
      IntPtr readAddress=Marshal.ReadIntPtr(vtable, 3*IntPtr.Size);
      var read=(ReadDelegate)Marshal.GetDelegateForFunctionPointer(readAddress, typeof(ReadDelegate));
      using(var output=new FileStream(destination, FileMode.CreateNew, FileAccess.Write, FileShare.None)) {
        byte[] buffer=new byte[1024*1024];
        while(true) {
          Marshal.WriteInt32(readCount,0);
          int hr=read(stream,buffer,(uint)buffer.Length,readCount);
          if(hr<0) Marshal.ThrowExceptionForHR(hr);
          int n=Marshal.ReadInt32(readCount);
          if(n<0 || n>buffer.Length) throw new IOException("IMAPI stream returned an invalid byte count");
          if(n==0) break;
          output.Write(buffer,0,n);
        }
        output.Flush(true);
      }
    } finally {
      Marshal.FreeHGlobal(readCount);
      if(stream!=IntPtr.Zero) Marshal.Release(stream);
      Marshal.Release(unknown);
    }
  }
}
'@
$source=` + psQuote(seedDir) + `
$destination=` + psQuote(isoPath) + `
$image=New-Object -ComObject IMAPI2FS.MsftFileSystemImage
$image.ChooseImageDefaultsForMediaType(9)
$image.FileSystemsToCreate=3
$image.VolumeName='CC2746SEED'
$image.Root.AddTree($source,$false)
$result=$image.CreateResultImage()
[CC2746ImageStreamWriter]::CopyToFile($result.ImageStream,$destination)
`
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, `C:\Program Files\PowerShell\7\pwsh.exe`, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	systemRoot := os.Getenv("SystemRoot")
	cmd.Env = []string{"SystemRoot=" + systemRoot, "PATH=" + systemRoot + `\System32;C:\Program Files\PowerShell\7`, "TEMP=" + systemRoot + `\Temp`, "TMP=" + systemRoot + `\Temp`}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		_ = writeJSONAtomic(filepath.Join(root, "seed-iso-failure.private.json"), map[string]any{"error": "seed ISO creator failed", "exit": err.Error(), "stdout": boundedText(stdout.String(), 8192), "stderr": boundedText(stderr.String(), 8192)})
		return "", errors.New("credential-free seed ISO creation failed")
	}
	stat, err := os.Stat(isoPath)
	if err != nil || !stat.Mode().IsRegular() || stat.Size() == 0 || stat.Size() > 128<<20 {
		return "", errors.New("seed ISO size rejected")
	}
	return isoPath, nil
}

func readVMReceipt(root string) (vmReceipt, error) {
	var receipt vmReceipt
	data, err := os.ReadFile(filepath.Join(root, "vm-created.private.json"))
	if err != nil {
		return receipt, err
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
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
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	err = dec.Decode(&receipt)
	return receipt, err
}

func validateVMReceipt(vm vmReceipt, root, instance string) error {
	if vm.Instance != instance || !validInstance(vm.Instance) || vm.ID == "" || vm.Name != "cc-2746-"+instance ||
		!strings.EqualFold(filepath.Clean(vm.Root), filepath.Clean(root)) || vm.VHD != filepath.Join(root, "guest.vhdx") ||
		vm.WindowsISO != filepath.Join(root, "windows-server-2025-eval.iso") || !validSeedISOPath(root, vm.SeedISO) || vm.DVDCount != 2 || vm.VHDCount != 1 ||
		vm.Generation != 2 || vm.Pipe != `\\.\pipe\cc-2746-`+instance || vm.CPUCount != jobCPUCount || vm.Memory != jobMemoryBytes || vm.Dynamic || !vm.SecureBoot ||
		vm.NetworkAdapters != 0 || vm.GPUAdapters != 0 {
		return errors.New("Hyper-V VM ownership or resource configuration rejected")
	}
	return nil
}

func runAdminBroker(script string, timeoutSec int) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec+30)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", `D:\CC\Tools\admin-broker\run-as-admin.ps1`, "-Script", script, "-TimeoutSec", fmt.Sprint(timeoutSec))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	systemRoot := os.Getenv("SystemRoot")
	cmd.Env = []string{
		"SystemRoot=" + systemRoot,
		"PATH=" + systemRoot + `\System32;` + systemRoot + `\System32\WindowsPowerShell\v1.0;C:\Program Files\PowerShell\7`,
		"PATHEXT=.COM;.EXE;.BAT;.CMD;.VBS;.VBE;.JS;.JSE;.WSF;.WSH;.MSC",
		"TEMP=" + systemRoot + `\Temp`,
		"TMP=" + systemRoot + `\Temp`,
		`PSModulePath=C:\Program Files\PowerShell\Modules;` + systemRoot + `\System32\WindowsPowerShell\v1.0\Modules`,
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		root := filepath.Dir(script)
		_ = writeJSONAtomic(filepath.Join(root, "admin-operation-failure.private.json"), map[string]any{"script": filepath.Base(script), "error": err.Error(), "stdout": boundedText(stdout.String(), 8192), "stderr": boundedText(stderr.String(), 8192)})
		return errors.New("CC-AdminBroker operation failed")
	}
	return nil
}

func boundedText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...[truncated]"
}

func createVMPS(root, iso, seedISO, instance string, reuseBlankVHD bool, previousVMID string) string {
	q := psQuote
	reuse := "$false"
	previousID := "[guid]::Empty"
	if reuseBlankVHD {
		reuse = "$true"
		previousID = "[guid]" + q(previousVMID)
	}
	return `$ErrorActionPreference='Stop'
Import-Module Hyper-V
Import-Module Storage
Import-Module Dism
$root=` + q(root) + `
$iso=` + q(iso) + `
$seedISO=` + q(seedISO) + `
$instance=` + q(instance) + `
$reuseBlankVHD=` + reuse + `
$previousVMID=` + previousID + `
$vhd=Join-Path $root 'guest.vhdx'
$name='cc-2746-'+$instance
$pipe='\\.\pipe\cc-2746-'+$instance
if(Get-VM -Name $name -ErrorAction SilentlyContinue){throw 'OWNED_VM_NAME_ALREADY_EXISTS'}
$mountedIso=$false;$mountedSeed=$false;$mountedVHD=$false;$diskNumber=$null;$espPartition=$null;$osPartition=$null;$efiAccessPath=$null;$osAccessPath=$null
try {
  $image=Mount-DiskImage -ImagePath $iso -PassThru
  $mountedIso=$true
  $image=Get-DiskImage -ImagePath $iso -StorageType ISO -ErrorAction Stop
  if(-not $image.Attached -or -not $image.DevicePath){throw 'EVAL_ISO_NOT_ATTACHED'}
  $volume=@($image | Get-Volume -ErrorAction Stop | Where-Object DriveLetter | Select-Object -First 1)
  if(-not $volume){throw 'EVAL_ISO_VOLUME_NOT_FOUND'}
  $isoRoot=([string]$volume[0].DriveLetter)+':\'
  if(-not (Test-Path -LiteralPath (Join-Path $isoRoot 'setup.exe')) -or -not (Test-Path -LiteralPath (Join-Path $isoRoot 'sources\install.wim'))){throw 'EVAL_ISO_SETUP_FILES_NOT_FOUND'}
  $signature=Get-AuthenticodeSignature -LiteralPath (Join-Path $isoRoot 'setup.exe')
  if($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch 'Microsoft'){throw 'EVAL_SETUP_SIGNATURE_INVALID'}
  $wim=Join-Path $isoRoot 'sources\install.wim'
  $edition=Get-WindowsImage -ImagePath $wim | Where-Object ImageName -CEQ 'Windows Server 2025 Standard Evaluation' | Select-Object -First 1
  if(-not $edition){throw 'EVAL_STANDARD_CORE_IMAGE_NOT_FOUND'}
  Dismount-DiskImage -ImagePath $iso
  $mountedIso=$false
  $seedImage=Mount-DiskImage -ImagePath $seedISO -StorageType ISO -PassThru
  $mountedSeed=$true
  $seedImage=Get-DiskImage -ImagePath $seedISO -StorageType ISO -ErrorAction Stop
  if(-not $seedImage.Attached -or -not $seedImage.DevicePath){throw 'OWNED_SEED_IMAGE_NOT_ATTACHED'}
  $seedVolume=@($seedImage | Get-Volume -ErrorAction Stop | Where-Object DriveLetter | Select-Object -First 1)
  if(-not $seedVolume -or $seedVolume[0].FileSystemLabel -cne 'CC2746SEED'){throw 'OWNED_SEED_VOLUME_REJECTED'}
  $seedRoot=([string]$seedVolume[0].DriveLetter)+':\'
  foreach($file in @('Autounattend.xml','stage.ps1','cc2746-guest.exe','cc2746-seed.json')){if(-not (Test-Path -LiteralPath (Join-Path $seedRoot $file))){throw 'OWNED_SEED_FILE_MISSING'}}
  $answer=[xml](Get-Content -Raw -LiteralPath (Join-Path $seedRoot 'Autounattend.xml'))
  if($answer.unattend.settings | Where-Object pass -EQ 'oobeSystem'){throw 'OOBE_SETTINGS_UNEXPECTED'}
  if(Test-Path -LiteralPath $vhd){
    if(-not $reuseBlankVHD -or $previousVMID -eq [guid]::Empty){throw 'OWNED_VHD_ALREADY_EXISTS'}
    if(Get-VM -Id $previousVMID -ErrorAction SilentlyContinue){throw 'PREVIOUS_OWNED_VM_STILL_PRESENT'}
    $oldCleanup=Get-Content -Raw -LiteralPath (Join-Path $root 'vm-cleanup.private.json')|ConvertFrom-Json
    if($oldCleanup.vm_id -cne $previousVMID.ToString() -or -not $oldCleanup.stopped -or $oldCleanup.memory_assigned -ne 0 -or -not $oldCleanup.removed){throw 'PREVIOUS_VM_CLEANUP_RECEIPT_REJECTED'}
    $imageState=Get-DiskImage -ImagePath $vhd -StorageType VHDX -ErrorAction Stop
    if($imageState.Attached){throw 'OWNED_VHD_STILL_ATTACHED'}
    $virtualDisk=Get-VHD -Path $vhd -ErrorAction Stop
    if($virtualDisk.Attached -or $virtualDisk.VhdType -ne 'Dynamic' -or $virtualDisk.Size -ne 64GB){throw 'OWNED_VHD_TYPE_OR_ATTACHMENT_REJECTED'}
    $vhdx=Mount-VHD -Path $vhd -ReadOnly -NoDriveLetter -PassThru
    $mountedVHD=$true
    $vhdImage=Get-DiskImage -ImagePath $vhd -StorageType VHDX -ErrorAction Stop
  $vhdDisks=@($vhdImage|Get-Disk -ErrorAction Stop)
  if($vhdDisks.Count -ne 1){throw 'OWNED_VHD_DISK_IDENTITY_REJECTED'}
  $diskNumber=$vhdDisks[0].Number
    $oldPartitions=@(Get-Partition -DiskNumber $diskNumber -ErrorAction SilentlyContinue)
    if($oldPartitions.Count -notin @(0,3,4)){throw 'OWNED_RETRY_VHD_PARTITION_LAYOUT_REJECTED'}
    Dismount-VHD -Path $vhd -ErrorAction Stop|Out-Null
    $mountedVHD=$false
  }else{
    if($reuseBlankVHD){throw 'EXPECTED_OWNED_BLANK_VHD_MISSING'}
    New-VHD -Path $vhd -Dynamic -SizeBytes 64GB | Out-Null
  }
  $vhdx=Mount-VHD -Path $vhd -PassThru
  $mountedVHD=$true
  $vhdImage=Get-DiskImage -ImagePath $vhd -StorageType VHDX -ErrorAction Stop
  if(-not $vhdImage.Attached -or -not $vhdImage.DevicePath){throw 'OWNED_VHD_MOUNT_FAILED'}
  $vhdDisks=@($vhdImage|Get-Disk -ErrorAction Stop)
  if($vhdDisks.Count -ne 1){throw 'OWNED_VHD_DISK_IDENTITY_REJECTED'}
  $diskNumber=$vhdDisks[0].Number
  $partitions=@(Get-Partition -DiskNumber $diskNumber -ErrorAction SilentlyContinue)
  if($partitions.Count -ne 0){
    if(-not $reuseBlankVHD -or $partitions.Count -notin @(3,4)){throw 'OWNED_RETRY_VHD_PARTITION_LAYOUT_REJECTED'}
    $oldESP=@($partitions|Where-Object { [String]::Equals([string]$_.GptType,'{C12A7328-F81F-11D2-BA4B-00A0C93EC93B}',[StringComparison]::OrdinalIgnoreCase) })
    $oldMSR=@($partitions|Where-Object { [String]::Equals([string]$_.GptType,'{E3C9E316-0B5C-4DB8-817D-F92DF00215AE}',[StringComparison]::OrdinalIgnoreCase) })
    $oldOS=@($partitions|Where-Object { [String]::Equals([string]$_.GptType,'{EBD0A0A2-B9E5-4433-87C0-68B6B72699C7}',[StringComparison]::OrdinalIgnoreCase) })
    if($oldESP.Count -ne 1 -or $oldMSR.Count -notin @(1,2) -or $oldOS.Count -ne 1 -or $oldESP[0].Size -ne 100MB -or @($oldMSR|Where-Object {$_.Size -lt 15MB -or $_.Size -gt 16MB}).Count -ne 0 -or $oldOS[0].Size -lt 60GB){throw 'OWNED_RETRY_VHD_PARTITION_LAYOUT_REJECTED'}
    $oldESPVolume=$oldESP[0]|Get-Volume -ErrorAction Stop
    $oldOSVolume=$oldOS[0]|Get-Volume -ErrorAction Stop
    if($oldESPVolume.FileSystemLabel -cne 'CC2746EFI' -or $oldESPVolume.FileSystem -cne 'FAT32' -or $oldOSVolume.FileSystemLabel -cne 'CC2746OS' -or $oldOSVolume.FileSystem -cne 'NTFS'){throw 'OWNED_RETRY_VHD_FILESYSTEM_LABEL_REJECTED'}
    if(-not $oldESP[0].DriveLetter){Add-PartitionAccessPath -DiskNumber $diskNumber -PartitionNumber $oldESP[0].PartitionNumber -AssignDriveLetter}
    $oldESP=Get-Partition -DiskNumber $diskNumber -PartitionNumber $oldESP[0].PartitionNumber -ErrorAction Stop
    if(-not $oldESP.DriveLetter){throw 'OWNED_RETRY_EFI_VOLUME_LETTER_MISSING'}
    $oldESPRoot=([string]$oldESP.DriveLetter)+':\'
    $oldESPAccess=$oldESPRoot
    $efiAccessPath=$oldESPAccess
    $oldESPContents=@(Get-ChildItem -LiteralPath $oldESPRoot -Force|Where-Object Name -NotIn @('System Volume Information','$RECYCLE.BIN'))
    if((Test-Path -LiteralPath (Join-Path $oldESPRoot 'EFI')) -or $oldESPContents.Count -ne 0){throw 'OWNED_RETRY_VHD_EFI_PARTITION_NOT_EMPTY'}
    Remove-PartitionAccessPath -DiskNumber $diskNumber -PartitionNumber $oldESP.PartitionNumber -AccessPath $oldESPAccess
    $efiAccessPath=$null
    if(-not $oldOS[0].DriveLetter){Add-PartitionAccessPath -DiskNumber $diskNumber -PartitionNumber $oldOS[0].PartitionNumber -AssignDriveLetter}
    $oldOS=Get-Partition -DiskNumber $diskNumber -PartitionNumber $oldOS[0].PartitionNumber -ErrorAction Stop
    if(-not $oldOS.DriveLetter){throw 'OWNED_RETRY_OS_VOLUME_LETTER_MISSING'}
    $oldOSRoot=([string]$oldOS.DriveLetter)+':\'
    $oldOSAccess=$oldOSRoot
    $osAccessPath=$oldOSAccess
    $oldContents=@(Get-ChildItem -LiteralPath $oldOSRoot -Force|Where-Object Name -NotIn @('System Volume Information','$RECYCLE.BIN'))
    if((Test-Path -LiteralPath (Join-Path $oldOSRoot 'Windows')) -or $oldContents.Count -ne 0){throw 'OWNED_RETRY_VHD_OS_PARTITION_NOT_EMPTY'}
    Remove-PartitionAccessPath -DiskNumber $diskNumber -PartitionNumber $oldOS.PartitionNumber -AccessPath $oldOSAccess
    $oldOSAccess=$null
    $osAccessPath=$null
    Clear-Disk -Number $diskNumber -RemoveData -RemoveOEM -Confirm:$false
  }
  Initialize-Disk -Number $diskNumber -PartitionStyle GPT -PassThru|Out-Null
  $initialPartitions=@(Get-Partition -DiskNumber $diskNumber -ErrorAction SilentlyContinue)
  if($initialPartitions.Count -eq 1 -and [String]::Equals([string]$initialPartitions[0].GptType,'{E3C9E316-0B5C-4DB8-817D-F92DF00215AE}',[StringComparison]::OrdinalIgnoreCase) -and $initialPartitions[0].Size -ge 15MB -and $initialPartitions[0].Size -le 16MB){
    Remove-Partition -DiskNumber $diskNumber -PartitionNumber $initialPartitions[0].PartitionNumber -Confirm:$false
  }elseif($initialPartitions.Count -ne 0){throw 'OWNED_INITIALIZED_GPT_LAYOUT_REJECTED'}
  $espPartition=New-Partition -DiskNumber $diskNumber -Size 100MB -GptType '{C12A7328-F81F-11D2-BA4B-00A0C93EC93B}'
  $null=Format-Volume -Partition $espPartition -FileSystem FAT32 -NewFileSystemLabel 'CC2746EFI' -Confirm:$false
  $null=New-Partition -DiskNumber $diskNumber -Size 16MB -GptType '{E3C9E316-0B5C-4DB8-817D-F92DF00215AE}'
  $osPartition=New-Partition -DiskNumber $diskNumber -UseMaximumSize -GptType '{EBD0A0A2-B9E5-4433-87C0-68B6B72699C7}'
  $null=Format-Volume -Partition $osPartition -FileSystem NTFS -NewFileSystemLabel 'CC2746OS' -Confirm:$false
  $createdPartitions=@(Get-Partition -DiskNumber $diskNumber|Sort-Object Offset)
  if($createdPartitions.Count -ne 3 -or -not [String]::Equals([string]$createdPartitions[0].GptType,'{C12A7328-F81F-11D2-BA4B-00A0C93EC93B}',[StringComparison]::OrdinalIgnoreCase) -or $createdPartitions[0].Size -ne 100MB -or -not [String]::Equals([string]$createdPartitions[1].GptType,'{E3C9E316-0B5C-4DB8-817D-F92DF00215AE}',[StringComparison]::OrdinalIgnoreCase) -or $createdPartitions[1].Size -ne 16MB -or -not [String]::Equals([string]$createdPartitions[2].GptType,'{EBD0A0A2-B9E5-4433-87C0-68B6B72699C7}',[StringComparison]::OrdinalIgnoreCase) -or $createdPartitions[2].Size -lt 60GB){throw 'OWNED_GPT_PARTITION_LAYOUT_REJECTED'}
  $espPartition=$createdPartitions[0]
  $osPartition=$createdPartitions[2]
  if(-not $espPartition.DriveLetter){Add-PartitionAccessPath -DiskNumber $diskNumber -PartitionNumber $espPartition.PartitionNumber -AssignDriveLetter}
  $espPartition=Get-Partition -DiskNumber $diskNumber -PartitionNumber $espPartition.PartitionNumber -ErrorAction Stop
  if(-not $espPartition.DriveLetter){throw 'OWNED_EFI_VOLUME_LETTER_MISSING'}
  $efiRoot=([string]$espPartition.DriveLetter)+':\'
  $efiAccessPath=$efiRoot
  if(-not $osPartition.DriveLetter){Add-PartitionAccessPath -DiskNumber $diskNumber -PartitionNumber $osPartition.PartitionNumber -AssignDriveLetter}
  $osPartition=Get-Partition -DiskNumber $diskNumber -PartitionNumber $osPartition.PartitionNumber -ErrorAction Stop
  if(-not $osPartition.DriveLetter){throw 'OWNED_OS_VOLUME_LETTER_MISSING'}
  $osRoot=([string]$osPartition.DriveLetter)+':\'
  $osAccessPath=$osRoot
  $osVolume=$osPartition|Get-Volume -ErrorAction Stop
  if($osVolume.FileSystemLabel -cne 'CC2746OS' -or $osVolume.FileSystem -cne 'NTFS'){throw 'OWNED_OS_VOLUME_REJECTED'}
  $dism=Join-Path $env:SystemRoot 'System32\dism.exe'
  $dismLog=Join-Path $root ('dism-apply-'+$instance+'.log')
  & $dism '/Apply-Image' (('/ImageFile:{0}' -f $wim)) (('/Index:{0}' -f $edition.ImageIndex)) (('/ApplyDir:{0}' -f $osRoot)) '/CheckIntegrity' (('/LogPath:{0}' -f $dismLog)) *> $null
  if($LASTEXITCODE -ne 0){throw 'OWNED_OFFLINE_WIM_APPLY_FAILED'}
  $windowsRoot=Join-Path $osRoot 'Windows'
  if(-not (Test-Path -LiteralPath (Join-Path $windowsRoot 'System32\Config\SYSTEM'))){throw 'OWNED_APPLIED_WINDOWS_HIVE_MISSING'}
  $panther=Join-Path $windowsRoot 'Panther'
  if(-not (Test-Path -LiteralPath $panther)){New-Item -ItemType Directory -Path $panther|Out-Null}
  Copy-Item -LiteralPath (Join-Path $seedRoot 'Autounattend.xml') -Destination (Join-Path $panther 'Unattend.xml')
  $bcdboot=Join-Path $env:SystemRoot 'System32\bcdboot.exe'
  & $bcdboot $windowsRoot /s ($espPartition.DriveLetter+':') /f UEFI /l en-us *> $null
  if($LASTEXITCODE -ne 0){throw 'OWNED_UEFI_BCD_BOOT_CREATION_FAILED'}
  if(-not (Test-Path -LiteralPath (Join-Path $efiRoot 'EFI\Microsoft\Boot\BCD')) -or -not (Test-Path -LiteralPath (Join-Path $efiRoot 'EFI\Boot\bootx64.efi'))){throw 'OWNED_UEFI_BOOT_FILES_MISSING'}
  Remove-PartitionAccessPath -DiskNumber $diskNumber -PartitionNumber $espPartition.PartitionNumber -AccessPath $efiAccessPath
  $efiAccessPath=$null
  Remove-PartitionAccessPath -DiskNumber $diskNumber -PartitionNumber $osPartition.PartitionNumber -AccessPath $osAccessPath
  $osAccessPath=$null
  Dismount-VHD -Path $vhd -ErrorAction Stop|Out-Null
  $mountedVHD=$false
  Dismount-DiskImage -ImagePath $iso -ErrorAction Stop|Out-Null
  $mountedIso=$false
  Dismount-DiskImage -ImagePath $seedISO -ErrorAction Stop|Out-Null
  $mountedSeed=$false
  $vm=New-VM -Name $name -Generation 2 -MemoryStartupBytes 2GB -VHDPath $vhd -Path (Join-Path $root 'vm')
$initial=@{instance=$instance;vm_id=$vm.Id.ToString();name=$vm.Name;generation=$vm.Generation;root=$root;vhd=$vhd;windows_iso=$iso;seed_iso=$seedISO;dvd_count=0;vhd_count=1;pipe=$pipe;cpu_count=0;memory_bytes=0;dynamic_memory=$true;secure_boot=$false;network_adapters=-1;gpu_adapters=-1}
  $initial | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $root 'vm-created.private.json') -Encoding utf8
  Set-VM -VM $vm -AutomaticStartAction Nothing -AutomaticStopAction TurnOff -CheckpointType Disabled -Notes ('cc.todo=2746;cc.instance='+$instance)
  Set-VMProcessor -VM $vm -Count 2
  Set-VMMemory -VM $vm -DynamicMemoryEnabled $false -StartupBytes 2GB
  $adapters=@($vm | Get-VMNetworkAdapter);foreach($adapter in $adapters){Remove-VMNetworkAdapter -VMNetworkAdapter $adapter}
  $existingDVD=@($vm | Get-VMDvdDrive)
  if($existingDVD.Count -gt 0){$winDvd=Set-VMDvdDrive -VMDvdDrive $existingDVD[0] -Path $iso -Passthru}else{$winDvd=Add-VMDvdDrive -VM $vm -Path $iso -Passthru}
  $seedDvd=Add-VMDvdDrive -VM $vm -Path $seedISO -Passthru
  Set-VMComPort -VM $vm -Number 1 -Path $pipe
  Set-VMFirmware -VM $vm -EnableSecureBoot On -SecureBootTemplate MicrosoftWindows
  $vm=Get-VM -Id $vm.Id
  $memory=Get-VMMemory -VM $vm
  $nics=@($vm | Get-VMNetworkAdapter).Count
  $gpus=@($vm | Get-VMGpuPartitionAdapter -ErrorAction SilentlyContinue).Count
  $dvds=@($vm | Get-VMDvdDrive)
  $vhds=@($vm | Get-VMHardDiskDrive)
  if($vm.State -ne 'Off' -or $memory.DynamicMemoryEnabled -or $memory.Startup -ne 2GB -or $vm.ProcessorCount -ne 2 -or $nics -ne 0 -or $gpus -ne 0 -or $dvds.Count -ne 2 -or $vhds.Count -ne 1 -or $vhds[0].Path -cne $vhd){throw 'NEW_VM_RESOURCE_CONFIGURATION_REJECTED'}
  Set-VMFirmware -VM $vm -BootOrder @($vhds[0],$winDvd,$seedDvd)
  $firmware=Get-VMFirmware -VM $vm
  if([string]$firmware.SecureBoot -cne 'On' -or $firmware.SecureBootTemplate -cne 'MicrosoftWindows'){throw 'NEW_VM_SECURE_BOOT_REJECTED'}
  $boot=@($firmware.BootOrder)
  if($boot.Count -lt 1 -or $boot[0].Path -cne $vhd){throw 'NEW_VM_BOOT_ORDER_REJECTED'}
$receipt=@{instance=$instance;vm_id=$vm.Id.ToString();name=$vm.Name;generation=$vm.Generation;root=$root;vhd=$vhd;windows_iso=$iso;seed_iso=$seedISO;dvd_count=$dvds.Count;vhd_count=$vhds.Count;pipe=$pipe;cpu_count=$vm.ProcessorCount;memory_bytes=[int64]$memory.Startup;dynamic_memory=$memory.DynamicMemoryEnabled;secure_boot=([string]$firmware.SecureBoot -ceq 'On');network_adapters=$nics;gpu_adapters=$gpus}
  $receipt | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $root 'vm-created.private.json') -Encoding utf8
  Start-VM -VM $vm | Out-Null
} catch {
  @{phase='provision';failure='owned offline Windows image deployment or VM provisioning failed'}|ConvertTo-Json|Set-Content -LiteralPath (Join-Path $root 'vm-provision-failure.private.json') -Encoding utf8
  throw
} finally {
  if($mountedVHD){
    if($null -ne $espPartition -and $null -ne $diskNumber -and $null -ne $efiAccessPath){Remove-PartitionAccessPath -DiskNumber $diskNumber -PartitionNumber $espPartition.PartitionNumber -AccessPath $efiAccessPath -ErrorAction SilentlyContinue|Out-Null}
    if($null -ne $osPartition -and $null -ne $diskNumber -and $null -ne $osAccessPath){Remove-PartitionAccessPath -DiskNumber $diskNumber -PartitionNumber $osPartition.PartitionNumber -AccessPath $osAccessPath -ErrorAction SilentlyContinue|Out-Null}
    Dismount-VHD -Path $vhd -ErrorAction SilentlyContinue|Out-Null
  }
  if($mountedIso){Dismount-DiskImage -ImagePath $iso -ErrorAction SilentlyContinue | Out-Null}
  if($mountedSeed){Dismount-DiskImage -ImagePath $seedISO -ErrorAction SilentlyContinue | Out-Null}
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
$windowsISO=` + q(vm.WindowsISO) + `
$seedISO=` + q(vm.SeedISO) + `
$pipe=` + q(vm.Pipe) + `
$v=Get-VM -Id $id -ErrorAction SilentlyContinue
if($v){
  $configRoot=[IO.Path]::GetFullPath((Join-Path $root 'vm')).TrimEnd('\')
  $configPath=[IO.Path]::GetFullPath($v.ConfigurationLocation).TrimEnd('\')
  $configInside=[String]::Equals($configPath,$configRoot,[StringComparison]::OrdinalIgnoreCase) -or $configPath.StartsWith($configRoot+'\',[StringComparison]::OrdinalIgnoreCase)
  if($v.Name -cne $name -or $v.Notes -cne ('cc.todo=2746;cc.instance='+$instance) -or -not $configInside){throw 'OWNED_VM_CLEANUP_IDENTITY_MISMATCH'}
  $cp=@($v | Get-VMComPort | Where-Object Path -CEQ $pipe)
  if($cp.Count -ne 1){throw 'OWNED_VM_COM_PORT_MISMATCH'}
  $dvds=@($v | Get-VMDvdDrive)
  if($dvds.Count -ne 2 -or @($dvds | Where-Object Path -CEQ $windowsISO).Count -ne 1 -or @($dvds | Where-Object Path -CEQ $seedISO).Count -ne 1){throw 'OWNED_VM_DVD_IDENTITY_MISMATCH'}
  $vhds=@($v | Get-VMHardDiskDrive)
  if($vhds.Count -ne 1 -or $vhds[0].Path -cne $path){throw 'OWNED_VM_VHD_IDENTITY_MISMATCH'}
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
