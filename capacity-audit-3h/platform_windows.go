//go:build windows

package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

// Only device metadata is requested. Do not add labels, serial numbers,
// mount-directory names, command lines or provider exception text.
const deviceInventoryScript = `$ErrorActionPreference='Stop'
$driveAudit=[ordered]@{volumes=@();logical=@();network=@();partitions=@();disks=@();errors=@()}
try { $driveAudit.volumes=@(Get-Volume | ForEach-Object {
 $letter=''; if ($_.DriveLetter -and [int][char]$_.DriveLetter -ne 0) {$letter=[string]$_.DriveLetter}
 [ordered]@{id=[string]$_.UniqueId;drive=$letter;total=$_.Size;free=$_.SizeRemaining;health=[string]$_.HealthStatus}
}) } catch { $driveAudit.errors+='volumes_unavailable' }
try { $driveAudit.logical=@(Get-CimInstance Win32_Volume | ForEach-Object {
 $letter=''; if ($_.DriveLetter) {$letter=([string]$_.DriveLetter).Substring(0,1)}
 [ordered]@{id=[string]$_.DeviceID;drive=$letter;total=$_.Capacity;free=$_.FreeSpace;health='Unknown'}
}) } catch { $driveAudit.errors+='volume_fallback_unavailable' }
try { $driveAudit.network=@(Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=4' | ForEach-Object {
 $letter=([string]$_.DeviceID).Substring(0,1)
 $identity=[string]$_.ProviderName;$unknown=[string]::IsNullOrWhiteSpace($identity)
 if($unknown){$identity='unidentified:'+ $letter;$driveAudit.errors+='network_identity_unavailable'}
 [ordered]@{id=('network:'+ $identity);drive=$letter;total=$_.Size;free=$_.FreeSpace;health='Unknown';identity_unverified=$unknown}
}) } catch { $driveAudit.errors+='network_drives_unavailable' }
try { $driveAudit.partitions=@(Get-Partition | ForEach-Object {
 $letter=''; if ($_.DriveLetter -and [int][char]$_.DriveLetter -ne 0) {$letter=[string]$_.DriveLetter}
 [ordered]@{disk=$_.DiskNumber;number=$_.PartitionNumber;drive=$letter;type=[string]$_.Type;bytes=$_.Size}
}) } catch { $driveAudit.errors+='partitions_unavailable' }
try { $driveAudit.disks=@(Get-Disk | ForEach-Object {
 [ordered]@{number=$_.Number;bytes=$_.Size;health=[string]$_.HealthStatus;status=(@($_.OperationalStatus)-join ',')}
}) } catch { $driveAudit.errors+='disks_unavailable' }
$driveAudit | ConvertTo-Json -Depth 6 -Compress`

type limitedDeviceOutput struct{ data []byte }

func (b *limitedDeviceOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 1024*1024 {
		return 0, errors.New("device inventory too large")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func discoverDevices(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	units := utf16.Encode([]rune(deviceInventoryScript))
	encoded := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(encoded[i*2:], u)
	}
	cmd := exec.CommandContext(ctx, `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded))
	cmd.WaitDelay = 2 * time.Second
	hide(cmd)
	var output limitedDeviceOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return nil, errors.New("device discovery unavailable")
	}
	return output.data, nil
}

var errLocked = errors.New("another audit is running")
var kernel = syscall.NewLazyDLL("kernel32.dll")
var diskFree = kernel.NewProc("GetDiskFreeSpaceExW")
var fileInformation = kernel.NewProc("GetFileInformationByHandle")

func hide(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} }
func isReparse(info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&0x400 != 0
}
func space(path string) (uint64, uint64, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var free, total uint64
	ok, _, callErr := diskFree.Call(uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if ok == 0 {
		return 0, 0, callErr
	}
	return free, total, nil
}
func lock(path string) (func(), error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, 0x00200000, 0)
	if err == syscall.Errno(32) {
		return nil, errLocked
	}
	if err != nil {
		return nil, err
	}
	var info syscall.ByHandleFileInformation
	if err = syscall.GetFileInformationByHandle(h, &info); err != nil || info.FileAttributes&0x400 != 0 {
		syscall.CloseHandle(h)
		return nil, errors.New("linked lock")
	}
	return func() { syscall.CloseHandle(h) }, nil
}
