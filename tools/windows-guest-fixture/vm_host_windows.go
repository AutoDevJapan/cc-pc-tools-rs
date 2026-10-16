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
	for _, rel := range []string{"guest.vhdx", "vm-created.private.json"} {
		if _, err := os.Lstat(filepath.Join(root, rel)); err == nil {
			return errors.New("owned root already contains a VM resource; use its saved receipt for recovery")
		} else if !errors.Is(err, os.ErrNotExist) {
			return errors.New("owned VM resource state could not be inspected")
		}
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

	seedRoot := filepath.Join(root, "seed")
	if err := os.Mkdir(seedRoot, 0700); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		seedInfo, statErr := os.Lstat(seedRoot)
		if statErr != nil || !seedInfo.IsDir() || seedInfo.Mode()&os.ModeSymlink != 0 {
			return errors.New("owned seed directory is not a real directory")
		}
	}
	seed := vmSeed{Instance: instance, ClientSHA256: client.SHA256}
	seedBytes, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		return err
	}
	if err := writeOwnedFile(filepath.Join(seedRoot, "cc2746-seed.json"), append(seedBytes, '\n')); err != nil {
		return err
	}
	answer := seedAnswerFile()
	if err := validateSeedAnswerFile(answer); err != nil {
		return err
	}
	if err := writeOwnedFile(filepath.Join(seedRoot, "Autounattend.xml"), []byte(answer)); err != nil {
		return err
	}
	if err := writeOwnedFile(filepath.Join(seedRoot, "stage.ps1"), []byte(seedStageScript())); err != nil {
		return err
	}
	currentExe, err := os.Executable()
	if err != nil {
		return err
	}
	guestExe := filepath.Join(seedRoot, "cc2746-guest.exe")
	if err := copyReplacingOwned(currentExe, guestExe); err != nil {
		return err
	}
	seedISO, err := createSeedISO(root, seedRoot, instance)
	if err != nil {
		return err
	}
	createScript := filepath.Join(root, "vm-create.ps1")
	if err := os.WriteFile(createScript, []byte(createVMPS(root, isoPath, seedISO, instance)), 0600); err != nil {
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
		vm.WindowsISO != filepath.Join(root, "windows-server-2025-eval.iso") || vm.SeedISO != filepath.Join(root, "cc2746-seed-"+instance+".iso") || vm.DVDCount != 2 || vm.VHDCount != 1 ||
		vm.Generation != 2 || vm.Pipe != `\\.\pipe\cc-2746-`+instance || vm.CPUCount != jobCPUCount || vm.Memory != jobMemoryBytes || vm.Dynamic || !vm.SecureBoot ||
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
	systemRoot := os.Getenv("SystemRoot")
	cmd.Env = []string{
		"SystemRoot=" + systemRoot,
		"PATH=" + systemRoot + `\System32;` + systemRoot + `\System32\WindowsPowerShell\v1.0;C:\Program Files\PowerShell\7`,
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

func createVMPS(root, iso, seedISO, instance string) string {
	q := psQuote
	return `$ErrorActionPreference='Stop'
Import-Module Hyper-V
Import-Module Storage
Import-Module Dism
$root=` + q(root) + `
$iso=` + q(iso) + `
$seedISO=` + q(seedISO) + `
$instance=` + q(instance) + `
$vhd=Join-Path $root 'guest.vhdx'
$name='cc-2746-'+$instance
$pipe='\\.\pipe\cc-2746-'+$instance
if(Test-Path -LiteralPath $vhd){throw 'OWNED_VHD_ALREADY_EXISTS'}
if(Get-VM -Name $name -ErrorAction SilentlyContinue){throw 'OWNED_VM_NAME_ALREADY_EXISTS'}
$mountedIso=$false;$mountedSeed=$false
try {
  $image=Mount-DiskImage -ImagePath $iso -PassThru
  $mountedIso=$true
  $volume=$image | Get-Disk | Get-Partition | Get-Volume | Where-Object DriveLetter | Select-Object -First 1
  if(-not $volume){throw 'EVAL_ISO_VOLUME_NOT_FOUND'}
  $isoRoot=([string]$volume.DriveLetter)+':\'
  $signature=Get-AuthenticodeSignature -LiteralPath (Join-Path $isoRoot 'setup.exe')
  if($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch 'Microsoft'){throw 'EVAL_SETUP_SIGNATURE_INVALID'}
  $wim=Join-Path $isoRoot 'sources\install.wim'
  $edition=Get-WindowsImage -ImagePath $wim | Where-Object ImageName -CEQ 'Windows Server 2025 Standard Evaluation' | Select-Object -First 1
  if(-not $edition){throw 'EVAL_STANDARD_CORE_IMAGE_NOT_FOUND'}
  Dismount-DiskImage -ImagePath $iso
  $mountedIso=$false
  $seedImage=Mount-DiskImage -ImagePath $seedISO -PassThru
  $mountedSeed=$true
  $seedVolume=$seedImage | Get-Disk | Get-Partition | Get-Volume | Where-Object DriveLetter | Select-Object -First 1
  if(-not $seedVolume -or $seedVolume.FileSystemLabel -cne 'CC2746SEED'){throw 'OWNED_SEED_VOLUME_REJECTED'}
  $seedRoot=([string]$seedVolume.DriveLetter)+':\'
  foreach($file in @('Autounattend.xml','stage.ps1','cc2746-guest.exe','cc2746-seed.json')){if(-not (Test-Path -LiteralPath (Join-Path $seedRoot $file))){throw 'OWNED_SEED_FILE_MISSING'}}
  $answer=[xml](Get-Content -Raw -LiteralPath (Join-Path $seedRoot 'Autounattend.xml'))
  if($answer.unattend.settings | Where-Object pass -EQ 'oobeSystem'){throw 'OOBE_SETTINGS_UNEXPECTED'}
  Dismount-DiskImage -ImagePath $seedISO
  $mountedSeed=$false
  New-VHD -Path $vhd -Dynamic -SizeBytes 64GB | Out-Null
  $vm=New-VM -Name $name -Generation 2 -MemoryStartupBytes 2GB -VHDPath $vhd -Path (Join-Path $root 'vm')
$initial=@{instance=$instance;vm_id=$vm.Id.ToString();name=$vm.Name;generation=$vm.Generation;root=$root;vhd=$vhd;windows_iso=$iso;seed_iso=$seedISO;dvd_count=0;vhd_count=1;pipe=$pipe;cpu_count=0;memory_bytes=0;dynamic_memory=$true;secure_boot=$false;network_adapters=-1;gpu_adapters=-1}
  $initial | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $root 'vm-created.private.json') -Encoding utf8
  Set-VM -VM $vm -AutomaticStartAction Nothing -AutomaticStopAction TurnOff -CheckpointType Disabled -Notes ('cc.todo=2746;cc.instance='+$instance)
  Set-VMProcessor -VM $vm -Count 2
  Set-VMMemory -VM $vm -DynamicMemoryEnabled $false -StartupBytes 2GB
  $adapters=@($vm | Get-VMNetworkAdapter);foreach($adapter in $adapters){Remove-VMNetworkAdapter -VMNetworkAdapter $adapter}
  $existingDVD=@($vm | Get-VMDvdDrive)
  if($existingDVD.Count -gt 0){$winDvd=Set-VMDvdDrive -VMDvdDrive $existingDVD[0] -Path $iso -Passthru}else{$winDvd=Add-VMDvdDrive -VM $vm -Path $iso -Passthru}
  Add-VMDvdDrive -VM $vm -Path $seedISO | Out-Null
  Set-VMComPort -VM $vm -Number 1 -Path $pipe
  Set-VMFirmware -VM $vm -EnableSecureBoot On -SecureBootTemplate MicrosoftWindows -FirstBootDevice $winDvd
  $vm=Get-VM -Id $vm.Id
  $memory=Get-VMMemory -VM $vm
  $nics=@($vm | Get-VMNetworkAdapter).Count
  $gpus=@($vm | Get-VMGpuPartitionAdapter -ErrorAction SilentlyContinue).Count
  $dvds=@($vm | Get-VMDvdDrive)
  $vhds=@($vm | Get-VMHardDiskDrive)
  if($vm.State -ne 'Off' -or $memory.DynamicMemoryEnabled -or $memory.Startup -ne 2GB -or $vm.ProcessorCount -ne 2 -or $nics -ne 0 -or $gpus -ne 0 -or $dvds.Count -ne 2 -or $vhds.Count -ne 1 -or $vhds[0].Path -cne $vhd){throw 'NEW_VM_RESOURCE_CONFIGURATION_REJECTED'}
  $firmware=Get-VMFirmware -VM $vm
  if(-not $firmware.SecureBoot -or $firmware.SecureBootTemplate -ne 'MicrosoftWindows'){throw 'NEW_VM_SECURE_BOOT_REJECTED'}
$receipt=@{instance=$instance;vm_id=$vm.Id.ToString();name=$vm.Name;generation=$vm.Generation;root=$root;vhd=$vhd;windows_iso=$iso;seed_iso=$seedISO;dvd_count=$dvds.Count;vhd_count=$vhds.Count;pipe=$pipe;cpu_count=$vm.ProcessorCount;memory_bytes=[int64]$memory.Startup;dynamic_memory=$memory.DynamicMemoryEnabled;secure_boot=$firmware.SecureBoot;network_adapters=$nics;gpu_adapters=$gpus}
  $receipt | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $root 'vm-created.private.json') -Encoding utf8
  Start-VM -VM $vm | Out-Null
} catch {
  @{phase='provision';failure='owned standard Windows Setup or VM provisioning failed'}|ConvertTo-Json|Set-Content -LiteralPath (Join-Path $root 'vm-provision-failure.private.json') -Encoding utf8
  throw
} finally {
  if($mountedIso){Dismount-DiskImage -ImagePath $iso -ErrorAction SilentlyContinue}
  if($mountedSeed){Dismount-DiskImage -ImagePath $seedISO -ErrorAction SilentlyContinue}
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
  if($v.Name -cne $name -or $v.Notes -cne ('cc.todo=2746;cc.instance='+$instance) -or -not [String]::Equals([IO.Path]::GetFullPath($v.ConfigurationLocation).TrimEnd('\'),[IO.Path]::GetFullPath((Join-Path $root 'vm')).TrimEnd('\'),[StringComparison]::OrdinalIgnoreCase)){throw 'OWNED_VM_CLEANUP_IDENTITY_MISMATCH'}
  $cp=$v | Get-VMComPort | Where-Object Number -EQ 1
  if(-not $cp -or $cp.Path -cne $pipe){throw 'OWNED_VM_COM_PORT_MISMATCH'}
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
