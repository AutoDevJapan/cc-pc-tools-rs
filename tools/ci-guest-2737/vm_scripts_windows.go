package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type vmState struct{ Instance, ID, Name, Root, Pipe, Stage string }

func validVMPipe(s vmState) bool {
	return s.Pipe == `\\.\pipe\cc-ci-2737-`+s.Instance || s.Pipe == `\\.\pipe\windows-ci-sdk-2737-`+s.Instance
}

func validVMID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	b, e := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return e == nil && len(b) == 16
}

// These functions run inside one admitted broker flight. A constructor failure
// rolls back using the VM object returned by New-VM, even if receipt I/O failed.
func vmPrelude(s vmState) string {
	return "Import-Module Hyper-V\n$root=" + psQuote(s.Root) + "\n$name=" + psQuote(s.Name) + "\n$instance=" + psQuote(s.Instance) + "\n$pipe=" + psQuote(s.Pipe) + "\n$notes=" + psQuote("cc.todo=2737;cc.instance="+s.Instance) + "\n" + `
function Write-Identity($v,$stage,$file) {
  $data=@{Instance=$instance;ID=$v.Id.ToString();Name=$v.Name;Root=$root;Pipe=$pipe;Stage=$stage}|ConvertTo-Json -Compress
  $stream=[IO.File]::Open((Join-Path $root $file),[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::Read)
  try {$b=[Text.Encoding]::UTF8.GetBytes($data);$stream.Write($b,0,$b.Length);$stream.Flush($true)}finally{$stream.Dispose()}
}
function Assert-Absent($id) {
  $guid=[guid]::ParseExact($id,'D').ToString()
  $entry=Get-CimInstance -Namespace root/virtualization/v2 -ClassName Msvm_ComputerSystem -Filter "Name='$guid'" -ErrorAction Stop
  if($null -ne $entry){throw 'VM_REMOVAL_UNCONFIRMED'}
}
function Remove-Owned($v,$allowUnconfigured) {
  if($v.Name -cne $name -or ($v.Notes -cne $notes -and -not ($allowUnconfigured -and [string]::IsNullOrEmpty($v.Notes)))){throw 'VM_OWNERSHIP_MISMATCH'}
  $boundary=[IO.Path]::GetFullPath($root).TrimEnd('\')+'\'
  $location=[IO.Path]::GetFullPath($v.ConfigurationLocation)
  if(-not $location.StartsWith($boundary,[StringComparison]::OrdinalIgnoreCase)){throw 'VM_LOCATION_MISMATCH'}
  $id=$v.Id.ToString()
  if($v.State -ne 'Off'){Stop-VM -VM $v -TurnOff -Force}
  $v=Get-VM -Id $id -ErrorAction Stop
  if($v.State -ne 'Off' -or $v.MemoryAssigned -ne 0){throw 'VM_STOP_UNCONFIRMED'}
  Remove-VM -VM $v -Force
  Assert-Absent $id
}
`
}

func createVM(ctx context.Context, root, instance, image string) (vmState, error) {
	return createVMStage(ctx, root, instance, image, "")
}
func createVMStage(ctx context.Context, root, instance, image, fault string) (vmState, error) {
	return createVMBudget(ctx, root, instance, image, fault, 2, 2)
}
func createVMBudget(ctx context.Context, root, instance, image, fault string, cpus, memoryGiB int) (vmState, error) {
	return createVMPlatform(ctx, root, instance, image, fault, cpus, memoryGiB, false)
}
func createVMPlatform(ctx context.Context, root, instance, image, fault string, cpus, memoryGiB int, windows bool) (vmState, error) {
	s := vmState{Instance: instance, Name: "cc-ci-2737-" + instance, Root: root, Pipe: `\\.\pipe\cc-ci-2737-` + instance}
	if windows {
		s.Pipe = `\\.\pipe\windows-ci-sdk-2737-` + instance
	}
	if !validInstance(instance) || cpus < 1 || cpus > 10 || memoryGiB < 2 || memoryGiB > 16 || (fault != "" && fault != "after-receipt" && fault != "receipt-write" && fault != "receipt-and-rollback") {
		return s, errors.New("VM instance rejected")
	}
	if e := record(root, "vm-intent.private.json", s); e != nil {
		return s, e
	}
	if fault == "receipt-write" || fault == "receipt-and-rollback" {
		// Make the actual exclusive receipt write fail. This is a fresh owned
		// attempt; the constructor must still roll back its returned VM object.
		if e := writeNew(filepath.Join(root, "vm-created.private.json"), []byte("fixture-blocker")); e != nil {
			return s, e
		}
	}
	body := vmPrelude(s) + `
if(@(Get-VM -ErrorAction Stop|Where-Object {$_.Name -ceq $name}).Count -ne 0){throw 'OWNED_NAME_ALREADY_EXISTS'}
$disk=Join-Path $root 'guest.vhdx'
if(Test-Path -LiteralPath $disk){throw 'OWNED_DISK_ALREADY_EXISTS'}
` + "Convert-VHD -Path " + psQuote(image) + " -DestinationPath $disk -VHDType Dynamic\n" + `
Resize-VHD -Path $disk -SizeBytes 64GB
$vm=$null
# Reserve an exclusive, fixed-size journal BEFORE New-VM can have effects.
# Updating it needs no new file allocation if the ordinary receipt fails.
$journal=[IO.File]::Open((Join-Path $root 'vm-recovery.private.json'),[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::Read)
function Journal-VM($id,$stage) {
  $data=@{Instance=$instance;ID=$id;Name=$name;Root=$root;Pipe=$pipe;Stage=$stage}|ConvertTo-Json -Compress
  if($data.Length -gt 32000){throw 'VM_JOURNAL_TOO_LARGE'}
  $bytes=[Text.Encoding]::UTF8.GetBytes($data.PadRight(65536,' '))
  $journal.Position=0;$journal.Write($bytes,0,$bytes.Length);$journal.Flush($true)
}
try {
  Journal-VM '' 'intent'
  $vm=New-VM -Name $name -Generation 1 -MemoryStartupBytes 2GB -VHDPath $disk -Path (Join-Path $root 'vm') -SwitchName 'Default Switch'
  Journal-VM $vm.Id.ToString() 'created'
  Write-Identity $vm 'created' 'vm-created.private.json'
`
	if fault == "after-receipt" {
		body += "  throw 'EXPECTED_CONSTRUCTOR_FIXTURE_FAILURE'\n"
	}
	body += `
  Set-VM -VM $vm -AutomaticStartAction Nothing -AutomaticStopAction TurnOff -Notes $notes
  Set-VMProcessor -VM $vm -Count 2
  Set-VMMemory -VM $vm -DynamicMemoryEnabled $false -StartupBytes 2GB
  Set-VMComPort -VM $vm -Number 1 -Path $pipe
  $dvd=Get-VMDvdDrive -VM $vm
  if($dvd){$dvd|Set-VMDvdDrive -Path (Join-Path $root 'seed.iso')}else{Add-VMDvdDrive -VM $vm -Path (Join-Path $root 'seed.iso')}
  Start-VM -VM $vm
}catch {
  $original=$_
  if($null -ne $vm){
`
	if fault == "receipt-and-rollback" {
		body += "    throw 'EXPECTED_ROLLBACK_FIXTURE_FAILURE'\n"
	}
	body += `
    Remove-Owned $vm $true
    Write-Identity $vm 'removed' 'vm-constructor-rollback.private.json'
  }
  throw $original
}finally {
  $journal.Dispose()
}
`
	body = strings.Replace(body, "Set-VMProcessor -VM $vm -Count 2", "Set-VMProcessor -VM $vm -Count "+strconv.Itoa(cpus), 1)
	body = strings.Replace(body, "-StartupBytes 2GB", "-StartupBytes "+strconv.Itoa(memoryGiB)+"GB", 1)
	if windows {
		// The clone is already hash-verified and offline-staged. Never mount or
		// mutate the retained SDK; all VM identity journalling stays unchanged.
		var e error
		body, e = windowsPreparedVMBody(body, image)
		if e != nil {
			return s, e
		}
	}
	adminErr := admin(ctx, root, "vm-create", body)
	// Never skip identity recovery because the constructor returned failure.
	// Prefer the rollback proof when creation receipt itself could not be saved.
	for _, name := range []string{"vm-constructor-rollback.private.json", "vm-created.private.json", "vm-recovery.private.json"} {
		b, e := os.ReadFile(filepath.Join(root, name))
		if e != nil {
			continue
		}
		var actual vmState
		if json.Unmarshal(bytes.TrimPrefix(b, []byte{239, 187, 191}), &actual) == nil && validVMID(actual.ID) && actual.Instance == instance && actual.Name == s.Name && actual.Root == root && actual.Pipe == s.Pipe && ((name != "vm-constructor-rollback.private.json" && actual.Stage == "created") || (name == "vm-constructor-rollback.private.json" && actual.Stage == "removed")) {
			return actual, adminErr
		}
	}
	return s, errors.Join(adminErr, errors.New("VM identity unavailable; retain intent for reconciliation"))
}
func windowsPreparedVMBody(body, image string) (string, error) {
	from := "if(Test-Path -LiteralPath $disk){throw 'OWNED_DISK_ALREADY_EXISTS'}\nConvert-VHD -Path " + psQuote(image) + " -DestinationPath $disk -VHDType Dynamic\nResize-VHD -Path $disk -SizeBytes 64GB"
	dvd := "  $dvd=Get-VMDvdDrive -VM $vm\n  if($dvd){$dvd|Set-VMDvdDrive -Path (Join-Path $root 'seed.iso')}else{Add-VMDvdDrive -VM $vm -Path (Join-Path $root 'seed.iso')}\n"
	if strings.Count(body, from) != 1 || strings.Count(body, dvd) != 1 {
		return "", errors.New("prepared Windows VM template changed")
	}
	body = strings.Replace(body, from, "if(-not (Test-Path -LiteralPath $disk)){throw 'PREPARED_CLONE_ABSENT'}\nif((Get-VHD -Path $disk -ErrorAction Stop).Attached){throw 'PREPARED_CLONE_ATTACHED'}", 1)
	return strings.Replace(body, dvd, "", 1), nil
}
func cleanupVM(ctx context.Context, s vmState) error {
	if !validInstance(s.Instance) || !validVMID(s.ID) || s.Name != "cc-ci-2737-"+s.Instance || !validVMPipe(s) {
		return errors.New("owned VM cleanup identity rejected")
	}
	body := vmPrelude(s)
	if s.Stage == "removed" {
		body += "Assert-Absent " + psQuote(s.ID) + "\n"
	} else if s.Stage == "created" {
		body += "$v=Get-VM -Id " + psQuote(s.ID) + " -ErrorAction Stop\nRemove-Owned $v $true\n"
	} else {
		return errors.New("VM cleanup stage rejected")
	}
	body += "@{vm_id=" + psQuote(s.ID) + ";stopped=$true;memory_assigned=0;removed=$true}|ConvertTo-Json|Set-Content -LiteralPath (Join-Path $root 'vm-cleanup.private.json') -Encoding utf8\n"
	return admin(ctx, s.Root, "vm-cleanup", body)
}

func constructorFailure(ctx context.Context, root, fault string) error {
	attempt := filepath.Join(root, "constructor-attempt-"+newID())
	if e := os.Mkdir(attempt, 0700); e != nil {
		return e
	}
	s, e := createVMStage(ctx, attempt, newID(), filepath.Join(root, "linux-generic.vhdx"), fault)
	expectedStage := "removed"
	if fault == "receipt-and-rollback" {
		expectedStage = "created"
	}
	if e == nil || s.Stage != expectedStage || !validVMID(s.ID) {
		return errors.Join(e, errors.New("constructor failure rollback unverified"))
	}
	if e = cleanupVM(context.Background(), s); e != nil {
		return e
	}
	return record(attempt, "constructor-fixture.private.json", map[string]any{"failure_injected": fault, "vm_id": s.ID, "recovered_from_journal": fault == "receipt-and-rollback", "removed": true, "official_ci_success": false})
}
